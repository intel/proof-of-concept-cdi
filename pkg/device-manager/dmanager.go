package dmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/intel/cdi/pkg/cdispec"
	"k8s.io/klog"
)

// DeviceInfo represents a block device
type DeviceInfo struct {
	// ID is a unique device ID on the node
	ID string
	// VolumeName is set when device is allocated
	VolumeName string
	// Paths list of actual device paths
	Paths []string
	// Size size allocated for block device
	Size int64
	// Device parameters, key->value pairs
	Parameters map[string]string
}

// Match compares passed parameters with device parameters
func (di *DeviceInfo) Match(params map[string]string) bool {
	for _, name := range []string{"afuID", "interfaceID", "deviceType", "vendor"} {
		paramValue, ok := params[name]
		if !ok {
			klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' not passed", di.ID, name)
			return false
		}
		deviceValue, ok := di.Parameters[name]
		if !ok {
			klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' doesn't exist", di.ID, name)
			return false
		}
		if di.Parameters[name] != params[name] {
			klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' mismatch: device: '%s', param: '%s'", di.ID, name, deviceValue, paramValue)
			return false
		}
	}
	return true
}

// Marshall writes device info in CDI JSON format
// https://github.com/container-orchestrated-devices/container-device-interface
func (di *DeviceInfo) Marshall(path string) error {
	spec := cdispec.NewCDISpec()

	devs := []*cdispec.Device{}
	for _, dPath := range di.Paths {
		devs = append(devs, &cdispec.Device{HostPath: dPath, ContainerPath: dPath})
	}
	spec.AddDevice(di.ID, devs)

	data, err := json.MarshalIndent(spec, "", " ")
	if err != nil {
		return errors.New("Failed to marshall device info to JSON")
	}

	err = ioutil.WriteFile(path, data, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("Failed to write CDI JSON to %s: %v", path, err)
	}

	return nil
}

// DeviceManager manages list of node devices
type DeviceManager struct {
	devices map[string]*DeviceInfo
}

var devManager = &DeviceManager{}

// NewDeviceManager returns device manager
func NewDeviceManager(nodeID string) (*DeviceManager, error) {
	if devManager.devices == nil {
		devices, err := discoverDevices(nodeID)
		if err != nil {
			return nil, err
		}
		devManager.devices = devices
	}
	return devManager, nil
}

// GetDevice returns DeviceInfo by device ID
func (dm *DeviceManager) GetDevice(ID string) (*DeviceInfo, error) {
	if dev, ok := dm.devices[ID]; ok {
		return dev, nil
	}
	return nil, fmt.Errorf("Device id %s not found", ID)
}

// ListDevices returns list of node devices
func (dm *DeviceManager) ListDevices() map[string]*DeviceInfo {
	return devManager.devices
}

// Allocate allocates device to the volume
func (dm *DeviceManager) Allocate(deviceID, volumeName string) error {
	device, err := dm.GetDevice(deviceID)
	if err != nil {
		return err
	}
	if device.VolumeName != "" {
		return fmt.Errorf("device %s is already allocated to the volume %s", device.ID, device.VolumeName)
	}
	device.VolumeName = volumeName
	return nil
}

// DeAllocate deallocates device from the volume
func (dm *DeviceManager) DeAllocate(deviceID string) error {
	device, err := dm.GetDevice(deviceID)
	if err != nil {
		return err
	}
	device.VolumeName = ""
	return nil
}

func discoverDevices(nodeID string) (map[string]*DeviceInfo, error) {
	// FIXME: discover real devices
	arria10 := &DeviceInfo{
		ID:    fmt.Sprintf("%s_0", nodeID),
		Paths: []string{"/dev/loop0"},
		Size:  100,
		Parameters: map[string]string{
			"vendor":      "0x8086",
			"deviceType":  "fpga",
			"interfaceID": "69528db6eb31577a8c3668f9faa081f6",
			"afuID":       "d8424dc4a4a3c413f89e433683f9040b",
		},
	}
	stratix10 := &DeviceInfo{
		ID:    fmt.Sprintf("%s_1", nodeID),
		Paths: []string{"/dev/loop1"},
		Size:  100,
		Parameters: map[string]string{
			"vendor":      "0x8086",
			"deviceType":  "fpga",
			"interfaceID": "bfac4d851ee856fe8c95865ce1bbaa2d",
			"afuID":       "f7df405cbd7acf7222f144b0b93acd18",
		},
	}

	return map[string]*DeviceInfo{arria10.ID: arria10, stratix10.ID: stratix10}, nil
}
