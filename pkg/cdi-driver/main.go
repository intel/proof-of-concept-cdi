/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"flag"
	"fmt"

	"k8s.io/klog"

	common "github.com/intel/cdi/pkg/common"
)

var (
	config = Config{
		Mode:           Controller,
		RegistryName:   "cdi-registry",
		ControllerName: "cdi-node-controller",
	}
	showVersion = flag.Bool("version", false, "Show release version and exit")
	version     = "unknown" // Set version during build time
)

func init() {
	/* generic options */
	flag.StringVar(&config.DriverName, "drivername", "cdi.intel.com", "name of the driver")
	flag.StringVar(&config.NodeID, "nodeid", "nodeid", "node id")
	flag.StringVar(&config.Endpoint, "endpoint", "unix:///var/lib/cdi.intel.com/csi.sock", "CSI endpoint")
	flag.BoolVar(&config.TestEndpoint, "testEndpoint", false, "also expose controller interface via endpoint (for testing only)")
	flag.Var(&config.Mode, "mode", "driver run mode: controller or node")
	flag.StringVar(&config.RegistryEndpoint, "registryEndpoint", "", "endpoint to connect/listen registry server")
	flag.StringVar(&config.CAFile, "caFile", "", "Root CA certificate file to use for verifying connections")
	flag.StringVar(&config.CertFile, "certFile", "", "SSL certificate file to use for authenticating client connections(RegistryServer/NodeControllerServer)")
	flag.StringVar(&config.KeyFile, "keyFile", "", "Private key file associated to certificate")
	flag.StringVar(&config.ClientCertFile, "clientCertFile", "", "Client SSL certificate file to use for authenticating peer connections, defaults to 'certFile'")
	flag.StringVar(&config.ClientKeyFile, "clientKeyFile", "", "Client private key associated to client certificate, defaults to 'keyFile'")
	/* Node mode options */
	flag.StringVar(&config.ControllerEndpoint, "controllerEndpoint", "", "internal node controller endpoint")
	flag.StringVar(&config.StateBasePath, "statePath", "", "Directory path where to persist the state of the driver running on a node, defaults to /var/lib/<drivername>")

	flag.Set("logtostderr", "true")
}

// Main driver entry point
func Main() int {
	if *showVersion {
		fmt.Println(version)
		return 0
	}

	klog.V(3).Info("Version: ", version)

	config.Version = version
	config.DriverTopologyKey = config.DriverName + "/node"

	driver, err := getDriver(config)
	if err != nil {
		common.ExitError("failed to initialize driver", err)
		return 1
	}

	if err = driver.Run(); err != nil {
		common.ExitError("failed to run driver", err)
		return 1
	}

	return 0
}
