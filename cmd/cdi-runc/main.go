package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"syscall"

	"github.com/intel/cdi/pkg/cdispec"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var (
	configJSON = "config.json"
	cdiJSON    = "CDI.json"

	logPath = "/var/log/cdi-runc.log"

	pvDir        = "/var/lib/kubelet/plugins/kubernetes.io/csi/pv"
	pvcDirRegexp = regexp.MustCompile(`.*/(pvc-[-0-9a-f]+)/mount`)
)

// loadJSON loads JSON file into the data struct
func loadJSON(path string, result interface{}) (interface{}, error) {
	jsonFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	if err = json.NewDecoder(jsonFile).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// findCDI returns path to CDI.json if it exists
func findCDI(source string) string {
	// find pvc id
	match := pvcDirRegexp.FindStringSubmatch(source)
	if match == nil {
		return ""
	}
	// Check if CDI.json exists in globalmount for this pvc
	pathCDI := path.Join(pvDir, match[1], "globalmount", "CDI.json")
	if _, err := os.Stat(pathCDI); !os.IsNotExist(err) {
		return pathCDI
	}
	return ""
}

// getLinuxDevice returns OCI LinuxDevice structure with filled
// device mode, major and minor properties obtained from syscall.Stat call
func getLinuxDevice(device *cdispec.Device) (*specs.LinuxDevice, error) {
	stat := syscall.Stat_t{}
	err := syscall.Stat(device.HostPath, &stat)
	if err != nil {
		return nil, fmt.Errorf("%s: stat error: %+v", device.HostPath, err)
	}

	modeFmt := stat.Mode & syscall.S_IFMT
	if modeFmt == syscall.S_IFBLK || modeFmt == syscall.S_IFCHR || modeFmt == syscall.S_IFIFO {
		var devType string
		switch modeFmt {
		case syscall.S_IFBLK:
			devType = "b"
		case syscall.S_IFCHR:
			devType = "c"
		case syscall.S_IFIFO:
			devType = "p"
		}
		return &specs.LinuxDevice{
			Path:  device.ContainerPath,
			Type:  devType,
			Major: int64(stat.Rdev / 256),
			Minor: int64(stat.Rdev % 256)}, nil
	}
	return nil, fmt.Errorf("%s is not a device", device.HostPath)
}

func updateSpec(cdiSpec *cdispec.CDISpec, spec *specs.Spec, outputFile string) error {
	if len(cdiSpec.ContainerSpec.Devices) == 0 {
		// no devices - nothing to update
		return nil
	}
	for _, device := range cdiSpec.ContainerSpec.Devices {
		// Add device to the spec
		linuxDevice, err := getLinuxDevice(device)
		if err != nil {
			return err
		}

		if spec.Linux == nil {
			spec.Linux = &specs.Linux{}
		}
		if spec.Linux.Devices == nil {
			spec.Linux.Devices = []specs.LinuxDevice{}
		}
		spec.Linux.Devices = append(spec.Linux.Devices, *linuxDevice)

		specDeviceCgroup := specs.LinuxDeviceCgroup{
			Allow:  true,
			Type:   linuxDevice.Type,
			Major:  &linuxDevice.Major,
			Minor:  &linuxDevice.Minor,
			Access: "rwm",
		}

		if spec.Linux.Resources == nil {
			spec.Linux.Resources = &specs.LinuxResources{}
		}
		if spec.Linux.Resources.Devices == nil {
			spec.Linux.Resources.Devices = []specs.LinuxDeviceCgroup{}
		}
		spec.Linux.Resources.Devices = append(spec.Linux.Resources.Devices, specDeviceCgroup)
		log.Printf("cdi-runc: added device %s(%s %d:%d), to the container\n", device.HostPath, specDeviceCgroup.Type, linuxDevice.Major, linuxDevice.Minor)
	}

	// Encode OCI spec structure into JSON.
	result, err := json.Marshal(spec)
	if err != nil {
		return err
	}

	// Rewrite config.json
	symLink, err := isSymLink(outputFile)
	if err != nil {
		return err
	}
	if symLink {
		return fmt.Errorf("%s is a symlink", outputFile)
	}
	err = ioutil.WriteFile(outputFile, result, 0600)
	if err != nil {
		return fmt.Errorf("error writing %s: %+v", outputFile, err)
	}

	return nil
}

// Run runc.orig
func runOrigRunc() error {
	// Verify runc.orig binary exists.
	runcPath, err := exec.LookPath("runc.orig")
	if err != nil {
		return fmt.Errorf("runc.orig not found: %+v", err)
	}

	err = syscall.Exec(runcPath, append([]string{runcPath}, os.Args[1:]...), os.Environ())
	if err != nil {
		return fmt.Errorf("can't run runc.orig: %+v", err)
	}

	return nil
}

func isSymLink(filename string) (bool, error) {
	stat, err := os.Lstat(filename)
	if err != nil {
		return false, err
	}
	if stat.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	return false, nil
}

func main() {
	// setup logging
	symLink, err := isSymLink(logPath)
	if err != nil {
		log.Fatal(err)
	}
	if symLink {
		log.Fatal(fmt.Errorf("%s is a symlink", logPath))
	}

	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logf.Close()
	log.SetOutput(logf)

	// Run original runc if not in create mode
	if !func() bool {
		for _, opt := range os.Args[1:] {
			if opt == "create" {
				return true
			}
		}
		return false
	}() {
		err := runOrigRunc()
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	// get bundle directory
	bundleDir, err := func() (string, error) {
		for i, opt := range os.Args[1:] {
			if opt == "--bundle" {
				return os.Args[i+2], nil
			}
		}
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd, nil
	}()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("cdi-runc: bundle: %s, args: %+v\n", bundleDir, os.Args[1:])

	pathConfig := path.Join(bundleDir, configJSON)

	// Update device list in config.json from CDI.json
	if _, err := os.Stat(pathConfig); !os.IsNotExist(err) {
		// load spec from config.json
		log.Printf("loading %s", pathConfig)
		result, err := loadJSON(pathConfig, &specs.Spec{})
		if err != nil {
			log.Fatal(err)
		}

		spec := result.(*specs.Spec)

		for _, mount := range spec.Mounts {
			// load CDI.json if exists
			pathCDI := findCDI(mount.Source)
			if pathCDI != "" {
				log.Printf("loading CDI config %s", pathCDI)
				result, err := loadJSON(pathCDI, &cdispec.CDISpec{})
				if err != nil {
					log.Fatal(err)
				}

				// Update OCI runtime spec (config.json)
				log.Printf("updating spec %s from CDI config %s", pathConfig, pathCDI)
				err = updateSpec(result.(*cdispec.CDISpec), spec, pathConfig)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}

	err = runOrigRunc()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("success")
}
