package dmanager

import (
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/intel/cdi/pkg/common"
	"github.com/pkg/errors"
	"k8s.io/klog"
)

const (
	sysfsDrmDirectory    = "/sys/class/drm"
	devfsDriDirectory    = "/dev/dri"
	gpuDeviceRE          = `^card[0-9]+$`
	controlDeviceRE      = `^controlD[0-9]+$`
	gpuDeviceType        = "gpu"
	gpuDefaultMemory     = "4000000000"
	gpuDefaultMillicores = "1000"

	memoryParamName    = "memory"
	millicoreParamName = "millicores"
)

var (
	// GPURequiredParameters is a list of GPU specific mandatory device parameters
	GPURequiredParameters = []string{memoryParamName, millicoreParamName}
)

// GPUManager manages gpu devices
type GPUManager struct {
	gpuDeviceReg     *regexp.Regexp
	controlDeviceReg *regexp.Regexp
}

// NewGPUManager returns a proper GPU Manager
func NewGPUManager() *GPUManager {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	return &GPUManager{
		gpuDeviceReg:     regexp.MustCompile(gpuDeviceRE),
		controlDeviceReg: regexp.MustCompile(controlDeviceRE),
	}
}

func (gm *GPUManager) checkParams(di *DeviceInfo, params map[string]string) bool {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}

	return di.checkParamFits(params, memoryParamName) &&
		di.checkParamFits(params, millicoreParamName)
}

func (gm *GPUManager) discoverDevices() ([]*DeviceInfo, error) {
	if klog.V(5) {
		defer klog.Info(common.Etrace("-> ") + " ->")
	}
	deviceInfos := []*DeviceInfo{}
	files, err := ioutil.ReadDir(sysfsDrmDirectory)
	if err != nil {
		return nil, errors.Wrap(err, "Can't read sysfs folder")
	}

	for _, f := range files {
		if !gm.gpuDeviceReg.MatchString(f.Name()) {
			klog.V(4).Info("Not compatible device", f.Name())
			continue
		}

		dat, err := ioutil.ReadFile(path.Join(sysfsDrmDirectory, f.Name(), "device/vendor"))
		if err != nil {
			klog.Warning("Skipping. Can't read vendor file: ", err)
			continue
		}

		if strings.TrimSpace(string(dat)) != intelVendor {
			klog.V(4).Info("Non-Intel GPU", f.Name())
			continue
		}

		drmFiles, err := ioutil.ReadDir(path.Join(sysfsDrmDirectory, f.Name(), "device/drm"))
		if err != nil {
			return nil, errors.Wrap(err, "Can't read device folder")
		}

		devicePaths := []string{}
		for _, drmFile := range drmFiles {
			if gm.controlDeviceReg.MatchString(drmFile.Name()) {
				//Skipping possible drm control node
				continue
			}
			devPath := path.Join(devfsDriDirectory, drmFile.Name())
			if _, err := os.Stat(devPath); err != nil {
				continue
			}

			klog.V(4).Infof("Adding %s to GPU %s", devPath, f.Name())
			devicePaths = append(devicePaths, devPath)
		}

		if len(devicePaths) > 0 {
			info := DeviceInfo{
				Paths: devicePaths,
				Size:  100,
				Parameters: map[string]string{
					vendorParamName:     intelVendor,
					deviceTypeParamName: gpuDeviceType,
					memoryParamName:     gpuDefaultMemory,
					millicoreParamName:  gpuDefaultMillicores,
				},
				Volumes: map[string]*csi.Volume{},
			}
			deviceInfos = append(deviceInfos, &info)
		}
	}

	return deviceInfos, nil
}
