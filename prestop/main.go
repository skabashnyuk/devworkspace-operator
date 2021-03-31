//
// Copyright (c) 2019-2021 Red Hat, Inc.
// This program and the accompanying materials are made
// available under the terms of the Eclipse Public License 2.0
// which is available at https://www.eclipse.org/legal/epl-2.0/
//
// SPDX-License-Identifier: EPL-2.0
//
// Contributors:
//   Red Hat, Inc. - initial API and implementation
//
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var (
	scheme = runtime.NewScheme()
	log    = logf.Log.WithName("cmd")
)

var (
	DevWorkspaceAPIResource = schema.GroupVersionResource{
		Group:    "workspace.devfile.io",
		Version:  "v1alpha1",
		Resource: "devworkspaces",
	}

	DevWorkspaceGroupVersion = &schema.GroupVersion{
		Group:   "workspace.devfile.io",
		Version: "v1alpha1",
	}
)

func main() {
	logf.SetLogger(zap.Logger())

	log.Info("Loaded successfully")

	// Get a config to talk to the apiserver
	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}
	log.Info("Loaded client config")

	// namespace, err := k8sutil.GetWatchNamespace()
	// if err != nil {
	// 	log.Error(err, "Failed to get watch namespace")
	// 	os.Exit(1)
	// }

	// hardcoded this for now
	namespace := "openshift-operators"

	log.Info("Found namespace")
	log.Info(namespace)

	cfg.GroupVersion = DevWorkspaceGroupVersion
	cfg.APIPath = "apis"

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	err = client.Resource(DevWorkspaceAPIResource).DeleteCollection(&v1.DeleteOptions{}, v1.ListOptions{})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace: namespace,
	})
	if err != nil {
		log.Error(err, "Failed to get create manager")
		os.Exit(1)
	}

	var shutdownChan = make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGTERM)

	log.Info("Starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
