/*
Copyright 2019,2020 Intel Corporation

SPDX-License-Identifier: Apache-2.0
*/

package parameters

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
)

// Origin defines a key in a validation map to validate parameters
type Origin string

// Beware of API and backwards-compatibility breaking when changing these string constants!
const (
	CacheSize          = "cacheSize"
	EraseAfter         = "eraseafter"
	Name               = "name"
	VolumeID           = "_id"
	Size               = "size"
	Storage            = "storage"
	StorageProvisioner = "volume.beta.kubernetes.io/storage-provisioner"
	SelectedNode       = "volume.kubernetes.io/selected-node"

	// Additional, unknown parameters that are okay.
	PodInfoPrefix = "csi.storage.k8s.io/"

	// Added by https://github.com/kubernetes-csi/external-provisioner/blob/feb67766f5e6af7db5c03ac0f0b16255f696c350/pkg/controller/controller.go#L584
	ProvisionerID = "storage.kubernetes.io/csiProvisionerIdentity"

	//CreateVolumeOrigin is for parameters from the storage class in controller CreateVolume.
	CreateVolumeOrigin Origin = "CreateVolumeOrigin"
	// CreateVolumeInternalOrigin is for the node CreateVolume parameters.
	CreateVolumeInternalOrigin = "CreateVolumeInternalOrigin"
	// PersistentVolumeOrigin represents parameters for a persistent volume in NodePublishVolume.
	PersistentVolumeOrigin = "PersistentVolumeOrigin"
	// NodeVolumeOrigin is for the parameters stored in node volume list.
	NodeVolumeOrigin = "NodeVolumeOrigin"

	// InterfaceID is an FPGA interface id passed from the storage class and PVC to CreateVolume.
	DeviceType  = "deviceType"
	Vendor      = "vendor"
	InterfaceID = "interfaceID"
	AfuID       = "afuID"
)

// valid is a whitelist of which parameters are valid in which context.
var valid = map[Origin][]string{
	// Parameters from Kubernetes and users for a persistent volume.
	CreateVolumeOrigin: []string{
		CacheSize,
		EraseAfter,
		Storage,
		StorageProvisioner,
		SelectedNode,

		// CDI: FPGA parameters from storage class
		DeviceType,
		Vendor,
		InterfaceID,
		AfuID,
	},

	// The volume context prepared by CreateVolume. We replicate
	// the CreateVolume parameters in the context because a future
	// version of PMEM-CSI might need them (the current one
	// doesn't) and add the volume name for logging purposes.
	// Kubernetes adds pod info and provisioner ID.
	PersistentVolumeOrigin: []string{
		CacheSize,
		EraseAfter,

		Name,
		PodInfoPrefix,
		ProvisionerID,
	},

	// Internally we store everything except the volume ID,
	// which is handled separately.
	NodeVolumeOrigin: []string{
		CacheSize,
		EraseAfter,
		Name,
		Size,

		// CDI: FPGA parameters from storage class
		InterfaceID,
		AfuID,
	},
}

// Volume represents all settings for a volume.
// Values can be unset or set explicitly to some value.
// The accessor functions always return a value, if unset
// the default.
type Volume struct {
	EraseAfter  *bool
	Name        *string
	Size        *int64
	VolumeID    *string
	Vendor      *string
	DeviceType  *string
	InterfaceID *string
	AfuID       *string
}

// VolumeContext represents the same settings as a string map.
type VolumeContext map[string]string

// Parse converts the string map that PMEM-CSI is given
// in CreateVolume (master and node) and NodePublishVolume. Depending
// on the origin of the string map, different keys are valid. An
// error is returned for invalid keys and values and invalid
// combinations of parameters.
func Parse(origin Origin, stringmap map[string]string) (Volume, error) {
	klog.V(4).Infof("Parameters parsing: %s: %v", origin, stringmap)
	var result Volume
	validKeys := valid[origin]

	for key, value := range stringmap {
		valid := false
		for _, validKey := range validKeys {
			if validKey == key ||
				strings.HasPrefix(key, PodInfoPrefix) && validKey == PodInfoPrefix {
				valid = true
				break
			}
		}
		if !valid {
			return result, fmt.Errorf("parameter %q invalid in this context", key)
		}

		value := value // Ensure that we get a new instance in case that we take the address below.
		switch key {
		case Name:
			result.Name = &value
		case VolumeID:
			/* volume id provided by master controller (needed for cache volumes) */
			result.VolumeID = &value
		case Size:
			quantity, err := resource.ParseQuantity(value)
			if err != nil {
				return result, fmt.Errorf("parameter %q: failed to parse %q as int64: %v", key, value, err)
			}
			s := quantity.Value()
			result.Size = &s
		case EraseAfter:
			b, err := strconv.ParseBool(value)
			if err != nil {
				return result, fmt.Errorf("parameter %q: failed to parse %q as boolean: %v", key, value, err)
			}
			result.EraseAfter = &b
		case DeviceType:
			result.DeviceType = &value
		case Vendor:
			result.Vendor = &value
		case InterfaceID:
			result.InterfaceID = &value
		case AfuID:
			result.AfuID = &value
		case ProvisionerID:
		case Storage:
		case StorageProvisioner:
		case SelectedNode:
		default:
			if !strings.HasPrefix(key, PodInfoPrefix) {
				return result, fmt.Errorf("unknown parameter: %q", key)
			}
		}
	}

	klog.V(4).Infof("Parameters parsing: %s: result: %v", origin, result)
	return result, nil
}

// ToContext converts back to a string map for use in
// CreateVolumeResponse.Volume.VolumeContext and for storing in the
// node's volume list.
func (v Volume) ToContext() VolumeContext {
	result := VolumeContext{}

	if v.EraseAfter != nil {
		result[EraseAfter] = fmt.Sprintf("%v", *v.EraseAfter)
	}
	if v.Name != nil {
		result[Name] = *v.Name
	}
	if v.Size != nil {
		result[Size] = fmt.Sprintf("%d", *v.Size)
	}
	if v.Vendor != nil {
		result[Vendor] = *v.Vendor
	}
	if v.DeviceType != nil {
		result[DeviceType] = *v.DeviceType
	}
	if v.InterfaceID != nil {
		result[InterfaceID] = *v.InterfaceID
	}
	if v.AfuID != nil {
		result[AfuID] = *v.AfuID
	}

	return result
}
