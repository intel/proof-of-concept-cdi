package dmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/intel/cdi/pkg/cdispec"
	"github.com/intel/cdi/pkg/common"
	"k8s.io/klog"
)

const (
	intelVendor = "0x8086"
)

var (
	// CommonRequiredParameters is a list of mandatory device parameters
	CommonRequiredParameters = []string{"deviceType", "vendor"}
)

type iDeviceTypeManager interface {
	discoverDevices() ([]*DeviceInfo, error)
	checkParams(di *DeviceInfo, params map[string]string) bool
}

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

func (di *DeviceInfo) checkParam(params map[string]string, name string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
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
	if deviceValue != paramValue {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' mismatch: device: '%s', param: '%s'", di.ID, name, deviceValue, paramValue)
		return false
	}
	return true
}

func (di *DeviceInfo) checkParams(params map[string]string, requiredParams []string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	for _, name := range requiredParams {
		if !di.checkParam(params, name) {
			return false
		}
	}
	return true
}

// Match compares passed parameters with device parameters
func (di *DeviceInfo) Match(params map[string]string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if di.checkParams(params, CommonRequiredParameters) {
		return devManager.checkParams(di, params)
	}

	return false
}

// Marshall writes device info in CDI JSON format
// https://github.com/container-orchestrated-devices/container-device-interface
func (di *DeviceInfo) Marshall(path string) error {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
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

// DeviceInfoMap is a map of deviceinfos
type DeviceInfoMap map[string]*DeviceInfo

// DeviceTypeManagerMap is a map of device type managers by device type name
type DeviceTypeManagerMap map[string]iDeviceTypeManager

// DeviceManager manages list of node devices
type DeviceManager struct {
	devices            DeviceInfoMap
	deviceTypeManagers DeviceTypeManagerMap
}

var devManager = &DeviceManager{
	deviceTypeManagers: DeviceTypeManagerMap{
		fpgaDeviceType: NewFPGAManager(),
		gpuDeviceType:  NewGPUManager(),
	},
}

// NewDeviceManager returns device manager
func NewDeviceManager(nodeID string) (*DeviceManager, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if devManager.devices == nil {
		devices, err := devManager.discoverDevices(nodeID)
		if err != nil {
			return nil, err
		}
		devManager.devices = devices
	}
	return devManager, nil
}

func (dm *DeviceManager) checkParams(di *DeviceInfo, params map[string]string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	dtm, ok := dm.deviceTypeManagers[params["deviceType"]]
	if ok {
		return dtm.checkParams(di, params)
	}
	return false
}

// GetDevice returns DeviceInfo by device ID
func (dm *DeviceManager) GetDevice(ID string) (*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if dev, ok := dm.devices[ID]; ok {
		return dev, nil
	}
	return nil, fmt.Errorf("Device id %s not found", ID)
}

// ListDevices returns list of node devices
func (dm *DeviceManager) ListDevices() map[string]*DeviceInfo {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return devManager.devices
}

// Allocate allocates device to the volume
func (dm *DeviceManager) Allocate(deviceID, volumeName string) error {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
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
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	device, err := dm.GetDevice(deviceID)
	if err != nil {
		return err
	}
	device.VolumeName = ""
	return nil
}

func (dm *DeviceManager) discoverDevices(nodeID string) (DeviceInfoMap, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}

	dim := DeviceInfoMap{}
	for _, deviceTypeManager := range dm.deviceTypeManagers {
		deviceInfos, err := deviceTypeManager.discoverDevices()
		if err != nil {
			klog.Error(err.Error())
			return nil, err
		}

		for _, deviceInfo := range deviceInfos {
			dim.addDeviceInfo(fmt.Sprintf("%s_%d", nodeID, len(dim)), deviceInfo)
		}
	}

	return dim, nil
}

func (dim DeviceInfoMap) addDeviceInfo(newDeviceInfoID string, deviceInfo *DeviceInfo) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	deviceInfo.ID = newDeviceInfoID
	dim[newDeviceInfoID] = deviceInfo
}
