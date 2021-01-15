package dmanager

import (
	"github.com/intel/cdi/pkg/common"
	"k8s.io/klog"
)

var (
	// FPGARequiredParameters is a list of FPGA specific mandatory device parameters
	FPGARequiredParameters = []string{"afuID", "interfaceID"}
)

const (
	fpgaDeviceType = "fpga"
)

// FPGAManager manages FPGA devices
type FPGAManager struct {
}

// NewFPGAManager returns a proper FPGA Manager
func NewFPGAManager() *FPGAManager {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return &FPGAManager{}
}

// FIXME: discover real devices
func (gm *FPGAManager) discoverDevices() ([]*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return []*DeviceInfo{
		&DeviceInfo{
			Paths: []string{"/dev/loop0"},
			Size:  100,
			Parameters: map[string]string{
				"vendor":      intelVendor,
				"deviceType":  fpgaDeviceType,
				"interfaceID": "69528db6eb31577a8c3668f9faa081f6",
				"afuID":       "d8424dc4a4a3c413f89e433683f9040b",
			},
		},
		&DeviceInfo{
			Paths: []string{"/dev/loop1"},
			Size:  100,
			Parameters: map[string]string{
				"vendor":      intelVendor,
				"deviceType":  fpgaDeviceType,
				"interfaceID": "bfac4d851ee856fe8c95865ce1bbaa2d",
				"afuID":       "f7df405cbd7acf7222f144b0b93acd18",
			},
		},
	}, nil
}
