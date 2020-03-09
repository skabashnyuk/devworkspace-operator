//
// Copyright (c) 2019-2020 Red Hat, Inc.
// This program and the accompanying materials are made
// available under the terms of the Eclipse Public License 2.0
// which is available at https://www.eclipse.org/legal/epl-2.0/
//
// SPDX-License-Identifier: EPL-2.0
//
// Contributors:
//   Red Hat, Inc. - initial API and implementation
//

package component

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	workspaceApi "github.com/che-incubator/che-workspace-operator/pkg/apis/workspace/v1alpha1"
	. "github.com/che-incubator/che-workspace-operator/pkg/controller/workspace/model"
	devworkspace "github.com/che-incubator/devworkspace-api/pkg/apis/workspaces/v1alpha1"

	utilYaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

func flattenDevWorkspaceTemplate(wkspCtx *WorkspaceContext, template *devworkspace.DevWorkspaceTemplateSpec) (*devworkspace.DevWorkspaceTemplateSpec, error) {

	alreadUsedUris := []string{}
	alreadUsedCustomResources := []devworkspace.KubernetesCustomResourceParentLocation{}

	for {
		if template == nil {
			return nil, nil
		}
		if template.Parent == nil {
			return template, nil
		}

		theParent := *template.Parent

		var err error = nil
		var parentTemplate *devworkspace.DevWorkspaceTemplateSpec = nil

		switch {
		case theParent.Uri != "":
			parentTemplate, err = getParentFromUri(theParent.Uri, alreadUsedUris)
			alreadUsedUris = append(alreadUsedUris, theParent.Uri)
		case theParent.Kubernetes != nil:
			parentTemplate, err = getParentFromKubernetesCustomresource(wkspCtx, theParent.Kubernetes, alreadUsedCustomResources)
			alreadUsedCustomResources = append(alreadUsedCustomResources, *theParent.Kubernetes)
		}

		if err != nil {
			return nil, err
		}

		// Do the changes to the parent template based on child
		template, err = mergeChildTemplateIntoParent(template, parentTemplate)
		if err != nil {
			return nil, err
		}
	}
}

type Devfile2 struct {
	devworkspace.DevWorkspaceTemplateSpec `json:",inline"`
	Name                                  string `json:"name"`
	SchemaVersion                         string `json:"schemaVersion"`
}

func getParentFromUri(uri string, alreadyFetchedUris []string) (*devworkspace.DevWorkspaceTemplateSpec, error) {

	for _, alreadyFetched := range alreadyFetchedUris {
		if alreadyFetched == uri {
			return nil, fmt.Errorf(
				"failed to fetch parent devfile from URL '%s': cyclic dependency in parents",
				uri)
		}
	}

	httpClient := http.DefaultClient

	resp, err := httpClient.Get(uri)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"failed to fetch parent devfile from URL '%s': %s",
			uri, resp.Status)
	}

	parentDevfile := Devfile2{}
	yamlReader := utilYaml.NewYAMLReader(bufio.NewReaderSize(resp.Body, 10))
	bytes, err := yamlReader.Read()
	if err != nil && err != io.EOF {
		return nil, err
	}

	if len(bytes) != 0 {
		err := yaml.Unmarshal(bytes, &parentDevfile, func(decoder *json.Decoder) *json.Decoder {
			decoder.DisallowUnknownFields()
			return decoder
		})
		if err != nil {
			return nil, err
		}
	}
	return &parentDevfile.DevWorkspaceTemplateSpec, nil
}

func getParentFromKubernetesCustomresource(wkspCtx *WorkspaceContext, cr *devworkspace.KubernetesCustomResourceParentLocation, alreadUsedCustomResources []devworkspace.KubernetesCustomResourceParentLocation) (*devworkspace.DevWorkspaceTemplateSpec, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	theScheme := runtime.NewScheme()
	scheme.AddToScheme(theScheme)
	devworkspace.SchemeBuilder.AddToScheme(theScheme)

	noncachedClient, err := client.New(cfg, client.Options{
		Scheme: theScheme,
	})
	if err != nil {
		return nil, err
	}

	template := devworkspace.DevWorkspaceTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workspaces.ecd.eclipse.org/v1alpha1",
		},
	}
	namespace := cr.Namespace
	if namespace == "" {
		namespace = wkspCtx.Namespace
	}

	if err = noncachedClient.Get(context.TODO(), client.ObjectKey{
		Name:      cr.Name,
		Namespace: namespace,
	}, &template); err != nil {
		return nil, err
	}

	return &template.Spec, nil
}

func getCommandId(c devworkspace.Command) string {
	switch {
	case c.Exec != nil:
		if c.Exec.Alias != "" {
			return c.Exec.Alias
		}
	}
	return ""
}

func getComponentId(c devworkspace.Component) string {
	switch {
	case c.CheEditor != nil:
		if c.CheEditor.Alias != "" {
			return "CheEditor.Alias/" + c.CheEditor.Alias
		}
	case c.ChePlugin != nil:
		if c.ChePlugin.Alias != "" {
			return "ChePlugin.Alias/" + c.ChePlugin.Alias
		}
		if c.ChePlugin.RegistryEntry != nil {
			return "ChePlugin.RegistryEntry/" + c.ChePlugin.RegistryEntry.Id
		}
		if c.ChePlugin.Uri != "" {
			return "ChePlugin.Uri/" + c.ChePlugin.Uri
		}

	case c.Container != nil:
		if c.Container.Alias != "" {
			return c.Container.Alias
		}
		return c.Container.Name
	}
	return ""
}

func getProjectId(p devworkspace.Project) string {
	return p.Name
}

func addOrReplaceCommand(command devworkspace.Command, list []devworkspace.Command) []devworkspace.Command {
	newList := []devworkspace.Command{}
	hasReplaced := false
	id := getCommandId(command)
	for _, el := range list {
		elId := getCommandId(el)
		if elId != "" && elId == id {
			newList = append(newList, command)
			hasReplaced = true
		} else {
			newList = append(newList, el)
		}
	}
	if !hasReplaced {
		// Add it
		newList = append(newList, command)
	}
	return newList
}

func addOrReplaceComponent(component devworkspace.Component, list []devworkspace.Component) []devworkspace.Component {
	newList := []devworkspace.Component{}
	hasReplaced := false
	id := getComponentId(component)
	for _, el := range list {
		elId := getComponentId(el)
		if elId != "" && elId == id {
			newList = append(newList, component)
			hasReplaced = true
		} else {
			newList = append(newList, el)
		}
	}
	if !hasReplaced {
		// Add it
		newList = append(newList, component)
	}
	return newList
}

func addOrReplaceProject(project devworkspace.Project, list []devworkspace.Project) []devworkspace.Project {
	newList := []devworkspace.Project{}
	hasReplaced := false
	id := getProjectId(project)
	for _, el := range list {
		elId := getProjectId(el)
		if elId != "" && elId == id {
			newList = append(newList, project)
			hasReplaced = true
		} else {
			newList = append(newList, el)
		}
	}
	if !hasReplaced {
		// Add it
		newList = append(newList, project)
	}
	return newList
}

func mergeChildTemplateIntoParent(template *devworkspace.DevWorkspaceTemplateSpec, parent *devworkspace.DevWorkspaceTemplateSpec) (*devworkspace.DevWorkspaceTemplateSpec, error) {
	for _, childCommand := range template.Commands {
		parent.Commands = addOrReplaceCommand(childCommand, parent.Commands)
	}
	for _, childProject := range template.Projects {
		parent.Projects = addOrReplaceProject(childProject, parent.Projects)
	}
	for _, childComponent := range template.Components {
		parent.Components = addOrReplaceComponent(childComponent, parent.Components)
	}
	parent.Parent = nil

	return parent, nil
}

func toDevfileCommand(c devworkspace.Command) *workspaceApi.CommandSpec {
	switch {
	case c.Exec != nil:
		name := c.Exec.Label
		if name == "" {
			name = c.Exec.Alias
		}
		return &workspaceApi.CommandSpec{
			Actions: []workspaceApi.CommandActionSpec{
				workspaceApi.CommandActionSpec{
					Command:   nilIfEmpty(c.Exec.CommandLine),
					Component: nilIfEmpty(c.Exec.Component),
					Workdir:   &c.Exec.Workdir,
					Type:      "exec",
				},
			},
			Name: name,
		}
	case c.VscodeLaunch != nil:
		name := c.VscodeLaunch.Alias
		return &workspaceApi.CommandSpec{
			Actions: []workspaceApi.CommandActionSpec{
				workspaceApi.CommandActionSpec{
					Type:             "vscode-launch",
					ReferenceContent: nilIfEmpty(c.VscodeLaunch.Inlined),
				},
			},
			Name: name,
		}
	case c.VscodeTask != nil:
		name := c.VscodeTask.Alias
		return &workspaceApi.CommandSpec{
			Actions: []workspaceApi.CommandActionSpec{
				workspaceApi.CommandActionSpec{
					Type:             "vscode-task",
					ReferenceContent: nilIfEmpty(c.VscodeTask.Inlined),
				},
			},
			Name: name,
		}
	}

	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	newString := s
	return &newString
}

func toDevfileEndpoints(eps []devworkspace.Endpoint) []workspaceApi.Endpoint {
	devfileEndpoints := []workspaceApi.Endpoint{}
	for _, e := range eps {
		attributes := map[workspaceApi.EndpointAttribute] string {}
		if e.Configuration != nil {
			attributes[workspaceApi.PROTOCOL_ENDPOINT_ATTRIBUTE] = e.Configuration.Scheme
		}

		devfileEndpoints = append(devfileEndpoints, workspaceApi.Endpoint{
			Name: e.Name,
			Port: int64(e.TargetPort),
			Attributes: attributes,
		})
	}
	return devfileEndpoints
}

func toDevfileComponent(c devworkspace.Component) *workspaceApi.ComponentSpec {
	switch {
	case c.CheEditor != nil:
		return &workspaceApi.ComponentSpec{
			Type:        workspaceApi.CheEditor,
			Alias:       c.CheEditor.Alias,
			Id:          nilIfEmpty(c.CheEditor.RegistryEntry.Id),
			MemoryLimit: nilIfEmpty(c.CheEditor.MemoryLimit),
		}
	case c.ChePlugin != nil:
		return &workspaceApi.ComponentSpec{
			Type:        workspaceApi.ChePlugin,
			Alias:       c.ChePlugin.Alias,
			Id:          nilIfEmpty(c.ChePlugin.RegistryEntry.Id),
			MemoryLimit: nilIfEmpty(c.ChePlugin.MemoryLimit),
		}
	case c.Container != nil:
		return &workspaceApi.ComponentSpec{
			Type:         workspaceApi.Dockerimage,
			Alias:        c.Container.Alias,
			Image:        nilIfEmpty(c.Container.Image),
			MemoryLimit:  nilIfEmpty(c.Container.MemoryLimit),
			MountSources: &c.Container.MountSources,
			Endpoints:    toDevfileEndpoints(c.Container.Endpoints),
		}
	}

	return nil
}

func toDevfileProject(p devworkspace.Project) *workspaceApi.ProjectSpec {
	var theLocation string
	var theType string
	
	switch {
	case p.Zip != nil:
		theLocation = p.Zip.Location
		theType = "zip"
	case p.Git != nil:
		theLocation = p.Git.Location
		theType = "git"
	case p.Github != nil:
		theLocation = p.Github.Location
		theType = "github"
	}
	return &workspaceApi.ProjectSpec {
		Name: p.Name,
		Source: workspaceApi.ProjectSourceSpec {
			Location: theLocation,
			Type: theType,
		},
	}
}

func completeDevfileFromDevworkspaceTemplate(template *devworkspace.DevWorkspaceTemplateSpec, devfile *workspaceApi.DevfileSpec) {
	for _, templateCommand := range template.Commands {
		command := toDevfileCommand(templateCommand)
		if command != nil {
			devfile.Commands = append(devfile.Commands, *command)
		}
	}

	for _, templateComponent := range template.Components {
		component := toDevfileComponent(templateComponent)
		if component != nil {
			devfile.Components = append(devfile.Components, *component)
		}
	}

	for _, templateProject := range template.Projects {
		project := toDevfileProject(templateProject)
		if project != nil {
			devfile.Projects = append(devfile.Projects, *project)
		}
	}
}

