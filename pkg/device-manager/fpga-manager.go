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
	// FIXME: discover devices
	arria10 := &DeviceInfo{
		Path: "/dev/loop0",
		Size: 100,
		Parameters: map[string]string{
			"vendor":      "0x8086",
			"interfaceID": "69528db6eb31577a8c3668f9faa081f6",
			"afuID":       "d8424dc4a4a3c413f89e433683f9040b",
		},
	}
	stratix10 := &DeviceInfo{
		Path: "/dev/loop1",
		Size: 100,
		Parameters: map[string]string{
			"vendor":      "0x8086",
			"interfaceID": "bfac4d851ee856fe8c95865ce1bbaa2d",
			"afuID":       "f7df405cbd7acf7222f144b0b93acd18",
		},
	}

	return map[string]*DeviceInfo{
		"0": arria10,
		"1": stratix10,
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
