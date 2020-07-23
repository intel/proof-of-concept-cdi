package dmanager

import (
	"errors"
	"os"
)

var (
	// ErrInvalid invalid argument passed
	ErrInvalid = os.ErrInvalid

	// ErrPermission no permission to complete the task
	ErrPermission = os.ErrPermission

	// ErrDeviceExists device with given id already exists
	ErrDeviceExists = errors.New("device exists")

	// ErrDeviceNotFound device does not exists
	ErrDeviceNotFound = errors.New("device not found")

	// ErrDeviceInUse device is in use
	ErrDeviceInUse = errors.New("device in use")

	// ErrDeviceNotReady device not ready yet
	ErrDeviceNotReady = errors.New("device not ready")

	// ErrNotEnoughSpace no space to create the device
	ErrNotEnoughSpace = errors.New("not enough space")
)

// DeviceType is a type for supported device types
type DeviceType string

const (
	// DeviceTypeFPGA is a type for FPGA devices
	DeviceTypeFPGA DeviceType = "fpga"
	// DeviceTypeGPU is a type for GPU devices
	DeviceTypeGPU DeviceType = "gpu"
)

// Set validates and sets device type value
func (dtype *DeviceType) Set(value string) error {
	switch value {
	case string(DeviceTypeFPGA), string(DeviceTypeGPU):
		*dtype = DeviceType(value)
	default:
		// The flag package will add the value to the final output, no need to do it here.
		return errors.New("invalid device type")
	}
	return nil
}

// String implements stringer interface for DeviceType
func (dtype *DeviceType) String() string {
	return string(*dtype)
}

//DeviceInfo represents a block device
type DeviceInfo struct {
	//VolumeId is name of the block device
	VolumeID string
	//Path actual device path
	Path string
	//Size size allocated for block device
	Size uint64
	// Device parameters, key->value pairs
	Parameters map[string]string
}

//DeviceManager interface to manage devices
type DeviceManager interface {
	// GetType returns current device manager's device type
	GetType() DeviceType

	// GetCapacity returns the available maximum capacity that can be assigned to a Device/Volume
	GetCapacity() (uint64, error)

	// CreateDevice creates a new block device with give name, size and namespace mode
	// Possible errors: ErrNotEnoughSpace, ErrInvalid, ErrDeviceExists
	CreateDevice(name string, size uint64) error

	// GetDevice returns the block device information for given name
	// Possible errors: ErrDeviceNotFound
	GetDevice(name string) (*DeviceInfo, error)

	// DeleteDevice deletes an existing block device with give name.
	// If 'flush' is 'true', then the device data is zeroed before deleting the device
	// Possible errors: ErrDeviceInUse, ErrPermission
	DeleteDevice(name string, flush bool) error

	// ListDevices returns all the block devices information that was created by this device manager
	ListDevices() (map[string]*DeviceInfo, error)
}
