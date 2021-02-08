package dmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/intel/cdi/pkg/cdispec"
	"github.com/intel/cdi/pkg/common"
	"google.golang.org/grpc/codes"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
)

const (
	deviceTypeParamName = "deviceType"
	vendorParamName     = "vendor"
	intelVendor         = "0x8086"
)

var (
	// CommonRequiredParameters is a list of mandatory device parameters
	CommonRequiredParameters = []string{deviceTypeParamName, vendorParamName}
)

type iDeviceTypeManager interface {
	discoverDevices() ([]*DeviceInfo, error)
	checkParams(di *DeviceInfo, params map[string]string) codes.Code
}

// DeviceInfo represents a block device
type DeviceInfo struct {
	// ID is a unique device ID on the node
	ID string
	// Volumes are set when device is allocated
	Volumes map[string]*csi.Volume
	// Paths list of actual device paths
	Paths []string
	// Size size allocated for block device
	Size int64
	// Device parameters, key->value pairs
	Parameters map[string]string
}

func totalUsedParam(volumes map[string]*csi.Volume, paramName string) int64 {
	totalUsedParam := int64(0)
	for _, volume := range volumes {
		if paramValue, ok := volume.VolumeContext[paramName]; ok {
			quantity, err := resource.ParseQuantity(paramValue)
			if err == nil {
				if paramAmount, _ := quantity.AsInt64(); paramAmount > 0 {
					totalUsedParam += paramAmount
				}
			}
		}
	}
	return totalUsedParam
}
func (di *DeviceInfo) checkParamFits(params map[string]string, paramName string) codes.Code {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if di.checkParamExists(params, paramName) {
		paramValue := params[paramName]
		quantity, err := resource.ParseQuantity(paramValue)
		if err == nil {
			totalUsedParam := totalUsedParam(di.Volumes, paramName)
			paramAmount, _ := quantity.AsInt64()
			diValue := di.Parameters[paramName]
			quantity, err = resource.ParseQuantity(diValue)
			if err == nil {
				diParamAmount, _ := quantity.AsInt64()
				klog.V(5).Infof("Device %v param %v amount:%v used:%v request:%v",
					di.ID, paramName, diParamAmount, totalUsedParam, paramAmount)
				if paramAmount <= (diParamAmount - totalUsedParam) {
					return codes.OK
				}
				if paramAmount <= diParamAmount {
					return codes.ResourceExhausted
				}
				return codes.NotFound
			}
			klog.Warningf("bad device info param %v value %v", paramName, diValue)
		} else {
			klog.Warningf("bad param %v value %v", paramName, paramValue)
		}
	}

	return codes.InvalidArgument
}

func (di *DeviceInfo) checkParamExists(params map[string]string, name string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	_, ok := params[name]
	if !ok {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' not passed", di.ID, name)
		klog.V(5).Info("DI params:", di.Parameters)
		klog.V(5).Info("in params:", params)
		return false
	}
	_, ok = di.Parameters[name]
	if !ok {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' doesn't exist", di.ID, name)
		klog.V(5).Info("DI params:", di.Parameters)
		klog.V(5).Info("in params:", params)
		return false
	}
	return true
}

func (di *DeviceInfo) checkParam(params map[string]string, name string) codes.Code {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	paramValue, ok := params[name]
	if !ok {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' not passed", di.ID, name)
		return codes.InvalidArgument
	}
	deviceValue, ok := di.Parameters[name]
	if !ok {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' doesn't exist", di.ID, name)
		return codes.InvalidArgument
	}
	if deviceValue != paramValue {
		klog.V(5).Infof("DeviceInfo.Match: device: %s: parameter '%s' mismatch: device: '%s', param: '%s'", di.ID, name, deviceValue, paramValue)
		return codes.OutOfRange
	}
	return codes.OK
}

func (di *DeviceInfo) checkParams(params map[string]string, requiredParams []string) codes.Code {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	for _, name := range requiredParams {
		if code := di.checkParam(params, name); code != codes.OK {
			return code
		}
	}
	return codes.OK
}

// Match compares passed parameters with device parameters
func (di *DeviceInfo) Match(params map[string]string) codes.Code {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	code := codes.OK
	if code = di.checkParams(params, CommonRequiredParameters); code == codes.OK {
		return devManager.checkParams(di, params)
	}

	return code
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
	devicesByDeviceID   DeviceInfoMap
	devicesByVolumeName DeviceInfoMap
	deviceTypeManagers  DeviceTypeManagerMap
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
	if devManager.devicesByDeviceID == nil {
		devices, err := devManager.discoverDevices(nodeID)
		if err != nil {
			return nil, err
		}
		devManager.devicesByDeviceID = devices
	}
	if devManager.devicesByVolumeName == nil {
		devManager.devicesByVolumeName = DeviceInfoMap{}
	}
	return devManager, nil
}

func (dm *DeviceManager) checkParams(di *DeviceInfo, params map[string]string) codes.Code {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	dtm, ok := dm.deviceTypeManagers[params["deviceType"]]
	if ok {
		return dtm.checkParams(di, params)
	}
	return codes.InvalidArgument
}

// GetDevice returns DeviceInfo by device ID
func (dm *DeviceManager) GetDevice(ID string) (*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if dev, ok := dm.devicesByDeviceID[ID]; ok {
		return dev, nil
	}
	return nil, fmt.Errorf("Device id %s not found", ID)
}

// GetDeviceForVolume returns DeviceInfo by volume name
func (dm *DeviceManager) GetDeviceForVolume(volumeName string) (*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	if dev, ok := dm.devicesByVolumeName[volumeName]; ok {
		return dev, nil
	}
	return nil, fmt.Errorf("Device for volume %s not found", volumeName)
}

// ListDevices returns list of node devices
func (dm *DeviceManager) ListDevices() DeviceInfoMap {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return devManager.devicesByDeviceID
}

// Allocate allocates volume to the device
func (dm *DeviceManager) Allocate(deviceID, volumeName string) error {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	device, err := dm.GetDevice(deviceID)
	if err != nil {
		return err
	}
	if _, ok := device.Volumes[volumeName]; ok {
		return fmt.Errorf("device %s is already allocated to the volume %s", device.ID, volumeName)
	}
	device.Volumes[volumeName] = nil
	dm.devicesByVolumeName[volumeName] = device
	return nil
}

// DeAllocate deallocates volume from the device
func (dm *DeviceManager) DeAllocate(volumeName string) error {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ", "volumeName:"+volumeName) + " ->")
	}
	device, err := dm.GetDeviceForVolume(volumeName)
	if err != nil {
		return err
	}

	delete(device.Volumes, volumeName)
	delete(dm.devicesByVolumeName, volumeName)
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
