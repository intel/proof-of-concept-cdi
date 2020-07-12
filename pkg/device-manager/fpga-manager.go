package dmanager

import (
	"sync"
)

type fpgaDman struct {
	mutex   *sync.Mutex
	devices map[string]*DeviceInfo
}

var _ DeviceManager = &fpgaDman{}

func (dm *fpgaDman) GetType() DeviceType {
	return DeviceTypeFPGA
}

// listDevices Lists available devices
func listDevices() (map[string]*DeviceInfo, error) {
	return map[string]*DeviceInfo{
		"1": &DeviceInfo{},
		"2": &DeviceInfo{},
	}, nil
}

// NewFpgaDman creates new fpgaDman structure
func NewFpgaDman() (DeviceManager, error) {
	devices, err := listDevices()
	if err != nil {
		return nil, err
	}

	return &fpgaDman{
		mutex:   &sync.Mutex{},
		devices: devices,
	}, nil
}

func (dm *fpgaDman) GetCapacity() (uint64, error) {
	return 100, nil
}

func (dm *fpgaDman) CreateDevice(volumeID string, size uint64) error {
	return nil
}

func (dm *fpgaDman) DeleteDevice(volumeID string, flush bool) error {
	return nil
}

func (dm *fpgaDman) ListDevices() (map[string]*DeviceInfo, error) {
	return listDevices()
}

func (dm *fpgaDman) GetDevice(volumeID string) (*DeviceInfo, error) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	return dm.getDevice(volumeID)
}

func (dm *fpgaDman) getDevice(volumeID string) (*DeviceInfo, error) {
	if dev, ok := dm.devices[volumeID]; ok {
		return dev, nil
	}
	return nil, ErrDeviceNotFound
}
