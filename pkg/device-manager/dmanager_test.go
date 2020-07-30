/*
Copyright 2019  Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/
package dmanager

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"

	exec "github.com/intel/cdi/pkg/exec"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	losetup "gopkg.in/freddierice/go-losetup.v1"
)

const (
	ModeFPGA = "fpga"
	ModeGPU  = "gpu"
)

func TestMain(m *testing.M) {
	RegisterFailHandler(Fail)

	os.Exit(m.Run())
}

func TestPmd(t *testing.T) {
	RunSpecs(t, "PMEM Device manager Suite")
}

var _ = Describe("DeviceManager", func() {
	Context(ModeFPGA, func() { runTests(ModeFPGA) })
	Context(ModeGPU, func() { runTests(ModeGPU) })
})

func runTests(mode string) {
	var dm DeviceManager
	var vg *testVGS
	var cleanupList map[string]bool
	var err error

	BeforeEach(func() {
		precheck()

		cleanupList = map[string]bool{}

		if mode == ModeFPGA {
			dm, err = NewFpgaDman()
		}
		Expect(err).Should(BeNil(), "Failed to create FPGA device manager")

	})

	AfterEach(func() {
		for devName, ok := range cleanupList {
			if !ok {
				continue
			}
			By("Cleaning up device: " + devName)
			dm.DeleteDevice(devName, false)
		}
		if mode == ModeFPGA {
			err := vg.Clean()
			Expect(err).Should(BeNil(), "Failed to create LVM device manager")
		}
	})

	It("Should create a new device", func() {
		name := "test-dev-new"
		size := uint64(2) * 1024 * 1024 // 2Mb
		err := dm.CreateDevice(name, size)
		Expect(err).Should(BeNil(), "Failed to create new device")

		cleanupList[name] = true

		dev, err := dm.GetDevice(name)
		Expect(err).Should(BeNil(), "Failed to retrieve device info")
		Expect(dev.VolumeID).Should(Equal(name), "Name mismatch")
		Expect(dev.Size >= size).Should(BeTrue(), "Size mismatch")
		Expect(dev.Path).ShouldNot(BeNil(), "Null device path")
	})

	It("Should support recreating a device", func() {
		name := "test-dev"
		size := uint64(2) * 1024 * 1024 // 2Mb
		err := dm.CreateDevice(name, size)
		Expect(err).Should(BeNil(), "Failed to create new device")

		cleanupList[name] = true

		dev, err := dm.GetDevice(name)
		Expect(err).Should(BeNil(), "Failed to retrieve device info")
		Expect(dev.VolumeID).Should(Equal(name), "Name mismatch")
		Expect(dev.Size >= size).Should(BeTrue(), "Size mismatch")
		Expect(dev.Path).ShouldNot(BeNil(), "Null device path")

		err = dm.DeleteDevice(name, false)
		Expect(err).Should(BeNil(), "Failed to delete device")
		cleanupList[name] = false

		err = dm.CreateDevice(name, size)
		Expect(err).Should(BeNil(), "Failed to recreate the same device")
		cleanupList[name] = true
	})

	It("Should fail to retrieve non-existent device", func() {
		dev, err := dm.GetDevice("unknown")
		Expect(err).ShouldNot(BeNil(), "Error expected")
		Expect(errors.Is(err, ErrDeviceNotFound)).Should(BeTrue(), "expected error is device not found error")
		Expect(dev).Should(BeNil(), "returned device should be nil")
	})

	It("Should list devices", func() {
		maxDevices := 4
		maxDeletes := 2
		sizes := map[string]uint64{}

		// This test may run on a host which already has some volumes.
		list, err := dm.ListDevices()
		Expect(err).Should(BeNil(), "Failed to list devices")
		numExisting := len(list)
		for _, dev := range list {
			sizes[dev.VolumeID] = 0
		}

		for i := 1; i <= maxDevices; i++ {
			name := fmt.Sprintf("list-dev-%d", i)
			sizes[name] = uint64(rand.Intn(15)+1) * 1024 * 1024
			err := dm.CreateDevice(name, sizes[name])
			Expect(err).Should(BeNil(), "Failed to create new device")
			cleanupList[name] = true
		}
		list, err = dm.ListDevices()
		Expect(err).Should(BeNil(), "Failed to list devices")
		Expect(len(list)).Should(BeEquivalentTo(maxDevices+numExisting), "count mismatch")
		for _, dev := range list {
			size, ok := sizes[dev.VolumeID]
			Expect(ok).Should(BeTrue(), "Unexpected device name:"+dev.VolumeID)
			Expect(dev.Size).Should(BeNumerically(">=", size), "Device size mismatch for "+dev.VolumeID)
		}

		for i := 1; i <= maxDeletes; i++ {
			name := fmt.Sprintf("list-dev-%d", i)
			delete(sizes, name)
			err = dm.DeleteDevice(name, false)
			Expect(err).Should(BeNil(), "Error while deleting device '"+name+"'")
			cleanupList[name] = false
		}

		// List device after deleting a device
		list, err = dm.ListDevices()
		Expect(err).Should(BeNil(), "Failed to list devices")
		Expect(len(list)).Should(BeEquivalentTo(maxDevices-maxDeletes+numExisting), "count mismatch")
		for _, dev := range list {
			size, ok := sizes[dev.VolumeID]
			Expect(ok).Should(BeTrue(), "Unexpected device name:"+dev.VolumeID)
			// When testing in direct mode on a node which was set up for LVM
			// then we don't have unique
			// "volume IDs" for those existing namespaces (both have VolumeID = "pmem-csi")
			// and we only have one entry in the size hash for two volumes. We simply skip
			// the size check for existing volumes.
			// TODO: should those volumes be listed at all?
			if size > 0 {
				Expect(dev.Size).Should(BeNumerically(">=", size), "Device size mismatch")
			}
		}
	})

	It("Should delete devices", func() {
		name := "delete-dev"
		size := uint64(2) * 1024 * 1024 // 2Mb
		err := dm.CreateDevice(name, size)
		Expect(err).Should(BeNil(), "Failed to create new device")
		cleanupList[name] = true

		dev, err := dm.GetDevice(name)
		Expect(err).Should(BeNil(), "Failed to retrieve device info")
		Expect(dev.VolumeID).Should(Equal(name), "Name mismatch")
		Expect(dev.Size).Should(BeNumerically(">=", size), "Size mismatch")
		Expect(dev.Path).ShouldNot(BeNil(), "Null device path")

		mountPath, err := mountDevice(dev)
		Expect(err).Should(BeNil(), "Failed to create mount path: %s", mountPath)

		defer unmount(mountPath)

		// Delete should fail as the device is in use
		err = dm.DeleteDevice(name, true)
		Expect(err).ShouldNot(BeNil(), "Error expected when deleting device in use: %s", dev.VolumeID)
		Expect(errors.Is(err, ErrDeviceInUse)).Should(BeTrue(), "Expected device busy error: %s", dev.VolumeID)
		cleanupList[name] = false

		err = unmount(mountPath)
		Expect(err).Should(BeNil(), "Failed to unmount the device: %s", dev.VolumeID)

		// Delete should succeed
		err = dm.DeleteDevice(name, true)
		Expect(err).Should(BeNil(), "Failed to delete device")

		dev, err = dm.GetDevice(name)
		Expect(err).ShouldNot(BeNil(), "GetDevice() should fail on deleted device")
		Expect(errors.Is(err, ErrDeviceNotFound)).Should(BeTrue(), "expected error is os.ErrNotExist")
		Expect(dev).Should(BeNil(), "returned device should be nil")

		// Delete call should not return any error on non-existing device
		err = dm.DeleteDevice(name, true)
		Expect(err).Should(BeNil(), "DeleteDevice() is not idempotent")
	})
}

func precheck() {
	if os.Geteuid() != 0 {
		Skip("Root privileges are required to run these tests", 1)
	}

	info, err := os.Stat(losetup.LoopControlPath)
	if err != nil {
		Skip(fmt.Sprintf("Stat(%s) failure: %s", losetup.LoopControlPath, err.Error()), 1)
	}
	if isDev := info.Mode()&os.ModeDevice != 0; !isDev {
		Skip(fmt.Sprintf("%s is not a loop device file", losetup.LoopControlPath), 1)
	}
}

type testVGS struct {
	name       string
	loopDev    losetup.Device
	backedFile string
}

func createTestVGS(vgname string, size uint64) (*testVGS, error) {
	var err error
	var file *os.File
	var dev losetup.Device
	var out string

	By("Creating temporary file")
	if file, err = ioutil.TempFile("", "test-lvm-dev"); err != nil {
		By("Cleaning temporary file")
		return nil, fmt.Errorf("Fail to create temporary file : %s", err.Error())
	}

	defer func() {
		if err != nil && file != nil {
			By("Removing tmp file due to failure")
			os.Remove(file.Name())
		}
	}()

	By("Closing file")
	if err = file.Close(); err != nil {
		return nil, fmt.Errorf("Fail to close file: %s", err.Error())
	}

	By("File truncating")
	if err = os.Truncate(file.Name(), int64(size)); err != nil {
		return nil, fmt.Errorf("Fail to truncate file: %s", err.Error())
	}

	By("losetup.Attach")
	dev, err = losetup.Attach(file.Name(), 0, false)
	if err != nil {
		return nil, fmt.Errorf("losetup failure: %s", err.Error())
	}

	defer func() {
		if err != nil {
			By("losetup.Detach due to failure")
			dev.Detach() // nolint errcheck
		}
	}()

	if err = waitDeviceAppears(&DeviceInfo{Path: dev.Path()}); err != nil {
		return nil, fmt.Errorf("created loop device not appeared: %s", err.Error())
	}

	By("Creating physical volume")
	// TODO: reuse vgm code
	cmdArgs := []string{"--force", dev.Path()}
	if out, err = exec.RunCommand("pvcreate", cmdArgs...); err != nil { // nolint gosec
		return nil, fmt.Errorf("pvcreate failure(output:%s): %s", out, err.Error())
	}

	By("Creating volume group")
	cmdArgs = []string{"--force", vgname, dev.Path()}
	if out, err = exec.RunCommand("vgcreate", cmdArgs...); err != nil { // nolint gosec
		return nil, fmt.Errorf("vgcreate failure(output:%s): %s", out, err.Error())
	}

	defer func() {
		if err != nil {
			By("Removing volume group due to failure")
			exec.RunCommand("vgremove", "--force", vgname)
		}
	}()

	return &testVGS{
		name:       vgname,
		loopDev:    dev,
		backedFile: file.Name(),
	}, nil
}

func (vg *testVGS) Clean() error {
	By("Removing volume group")
	if out, err := exec.RunCommand("vgremove", "--force", vg.name); err != nil {
		return fmt.Errorf("Fail to remove volume group(output:%s): %s", out, err.Error())
	}

	By("losetup.Detach()")
	if err := vg.loopDev.Detach(); err != nil {
		return fmt.Errorf("Fail detatch loop device: %s", err.Error())
	}

	By("Removing temp file")
	if err := os.Remove(vg.backedFile); err != nil {
		return fmt.Errorf("Fail remove temporary file: %s", err.Error())
	}

	return nil
}

func mountDevice(device *DeviceInfo) (string, error) {
	targetPath, err := ioutil.TempDir("/tmp", "lmv-mnt-path-")
	if err != nil {
		return "", err
	}

	cmd := "mkfs.ext4"
	args := []string{"-b 4096", "-F", device.Path}

	if _, err := exec.RunCommand(cmd, args...); err != nil {
		os.Remove(targetPath)
		return "", err
	}

	cmd = "mount"
	args = []string{"-c", device.Path, targetPath}

	if _, err := exec.RunCommand(cmd, args...); err != nil {
		os.Remove(targetPath)
		return "", err
	}

	return targetPath, nil
}

func unmount(path string) error {
	args := []string{path}
	if _, err := exec.RunCommand("umount", args...); err != nil {
		return err
	}
	return os.Remove(path)
}
