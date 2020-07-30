/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"flag"
	"os"

	"k8s.io/klog"

	"github.com/intel/cdi/pkg/cdi-driver"
)

func init() {
	klog.InitFlags(nil)
}

func main() {
	flag.Parse()
	os.Exit(cdidriver.Main())
}
