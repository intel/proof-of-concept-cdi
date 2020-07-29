/*
Copyright 2020 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package cdispec

var (
	// Version is a current CDI spec version
	Version = "0.2.0"
	// Kind is a CDI kind
	Kind = "intel.com/device"
)

// Device defines device structure
type Device struct {
	HostPath      string `json:"hostPath"`
	ContainerPath string `json:"containerPath"`
}

// ContainerDevices is a list of Devices
type ContainerDevices struct {
	Devices []*Device `json:"devices"`
}

// CDIDevice defines named CDI devices
type CDIDevice struct {
	Name          string            `json:"name"`
	ContainerSpec *ContainerDevices `json:"containerSpec"`
}

// Mount defines container mount
type Mount struct {
	HostPath      string `json:"hostPath"`
	ContainerPath string `json:"containerPath"`
}

// Hook defines CDI hook structure
type Hook struct {
	Path string `json:"path"`
}

// Hooks defines structure to hold CDI hooks
type Hooks struct {
	CreateContainer *Hook `json:"create-container,omitempty"`
	StartContainer  *Hook `json:"start-container,omitempty"`
}

// ContainerSpec is a set of devices, hooks and mounts
type ContainerSpec struct {
	Devices []*Device `json:"devices"`
	Mounts  []*Mount  `json:"mounts"`
	Hooks   []*Hooks  `json:"hooks"`
}

// CDISpec is a top level structure to define CDI spec
type CDISpec struct {
	CDIVersion    string         `json:"cdiVersion"`
	Kind          string         `json:"kind"`
	CDIDevices    []*CDIDevice   `json:"cdiDevices"`
	ContainerSpec *ContainerSpec `json:"containerSpec"`
}

// NewCDISpec creates new CDISpec structure
func NewCDISpec() *CDISpec {
	return &CDISpec{
		CDIVersion: Version,
		Kind:       Kind,
		CDIDevices: []*CDIDevice{},
		ContainerSpec: &ContainerSpec{
			Devices: []*Device{},
			Mounts:  []*Mount{},
			Hooks:   []*Hooks{},
		},
	}
}

// AddDevice adds device info to the spec
func (spec *CDISpec) AddDevice(name string, devices []*Device) {
	spec.CDIDevices = append(spec.CDIDevices,
		&CDIDevice{
			Name: name,
			ContainerSpec: &ContainerDevices{
				Devices: devices,
			},
		})
	spec.ContainerSpec.Devices = append(spec.ContainerSpec.Devices, devices...)
}
