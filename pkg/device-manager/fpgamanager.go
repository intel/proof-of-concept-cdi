package dmanager

import (
	"io/ioutil"
	"os"
	"path"
	"regexp"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/intel/cdi/pkg/common"
	"github.com/intel/intel-device-plugins-for-kubernetes/pkg/fpga"
	"github.com/pkg/errors"
	"k8s.io/klog"
)

var (
	// FPGARequiredParameters is a list of FPGA specific mandatory device parameters
	FPGARequiredParameters = []string{"afuID", "interfaceID"}
)

const (
	fpgaDeviceType = "fpga"

	// device-specific paths
	devfsDirectory     = "/dev"
	sysfsDirectoryOPAE = "/sys/class/fpga"
	sysfsDirectoryDFL  = "/sys/class/fpga_region"

	// DFL regexps
	dflDeviceRE = `^region[0-9]+$`
	dflPortRE   = `^dfl-port\.[0-9]+$`

	// OPAE regexps
	opaeDeviceRE = `^intel-fpga-dev.[0-9]+$`
	opaePortRE   = `^intel-fpga-port.[0-9]+$`
)

// FPGAManager manages FPGA devices
type FPGAManager struct {
	name string

	sysfsDir string
	devfsDir string

	deviceReg *regexp.Regexp
	portReg   *regexp.Regexp
}

// NewFPGAManager returns a proper FPGA Manager
func NewFPGAManager() *FPGAManager {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}

	if _, err := os.Stat(sysfsDirectoryDFL); !os.IsNotExist(err) {
		return &FPGAManager{
			name: "DFL",

			sysfsDir: sysfsDirectoryDFL,
			devfsDir: devfsDirectory,

			deviceReg: regexp.MustCompile(dflDeviceRE),
			portReg:   regexp.MustCompile(dflPortRE),
		}
	}

	return &FPGAManager{
		name: "OPAE",

		sysfsDir: sysfsDirectoryOPAE,
		devfsDir: devfsDirectory,

		deviceReg: regexp.MustCompile(opaeDeviceRE),
		portReg:   regexp.MustCompile(opaePortRE),
	}
}

func (man *FPGAManager) checkParams(di *DeviceInfo, params map[string]string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return di.checkParams(params, FPGARequiredParameters)
}

// Discover FPGA devices
func (man *FPGAManager) discoverDevices() ([]*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}

	files, err := ioutil.ReadDir(man.sysfsDir)
	if err != nil {
		klog.Warningf("Can't read folder %s. Kernel driver not loaded?", man.sysfsDir)
		return nil, nil
	}

	devices := []*DeviceInfo{}
	for _, file := range files {
		devName := file.Name()

		if !man.deviceReg.MatchString(devName) {
			continue
		}

		deviceFiles, err := ioutil.ReadDir(path.Join(man.sysfsDir, devName))
		if err != nil {
			return nil, errors.WithStack(err)
		}

		for _, deviceFile := range deviceFiles {
			name := deviceFile.Name()
			if man.portReg.MatchString(name) {
				port, err := fpga.NewPort(name)
				if err != nil {
					return nil, errors.Wrapf(err, "can't get port info for %s", name)
				}
				fme, err := port.GetFME()
				if err != nil {
					return nil, errors.Wrapf(err, "can't get FME info for %s", name)
				}

				devices = append(devices, &DeviceInfo{
					Paths: []string{port.GetDevPath()},
					Size:  100,
					Parameters: map[string]string{
						"vendor":      intelVendor,
						"deviceType":  fpgaDeviceType,
						"interfaceID": fme.GetInterfaceUUID(),
						"afuID":       port.GetAcceleratorTypeUUID(),
					},
					Volumes: map[string]*csi.Volume{},
				})
			}
		}
	}

	klog.Infof("devices: %+v", devices)

	return devices, nil
}
