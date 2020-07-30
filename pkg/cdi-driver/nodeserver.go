/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"k8s.io/utils/mount"

	"github.com/intel/cdi/pkg/cdi-driver/parameters"
	dmanager "github.com/intel/cdi/pkg/device-manager"
	exec "github.com/intel/cdi/pkg/exec"
	grpcserver "github.com/intel/cdi/pkg/grpc-server"
)

const (
	// ProvisionerIdentity is supposed to pass to NodePublish
	// only in case of Persistent volumes that were provisioned by
	// the driver
	volumeProvisionerIdentity = "storage.kubernetes.io/csiProvisionerIdentity"
	defaultFilesystem         = "ext4"
)

type volumeInfo struct {
	readOnly   bool
	targetPath string
	mountFlags string // used mount flags, sorted and joined to single string
}

type nodeServer struct {
	nodeCaps []*csi.NodeServiceCapability
	cs       *nodeControllerServer
	// Driver deployed to provision only ephemeral volumes(only for Kubernetes v1.15)
	mounter mount.Interface

	// A directory for additional mount points.
	mountDirectory string
}

var _ csi.NodeServer = &nodeServer{}
var _ grpcserver.Service = &nodeServer{}

func NewNodeServer(cs *nodeControllerServer, mountDirectory string) *nodeServer {
	return &nodeServer{
		nodeCaps: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
		cs:             cs,
		mounter:        mount.New(""),
		mountDirectory: mountDirectory,
	}
}

func (ns *nodeServer) RegisterService(rpcServer *grpc.Server) {
	csi.RegisterNodeServer(rpcServer, ns)
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.cs.nodeID,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				DriverTopologyKey: ns.cs.nodeID,
			},
		},
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.nodeCaps,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Serialize by VolumeId
	volumeMutex.LockKey(req.GetVolumeId())
	defer volumeMutex.UnlockKey(req.GetVolumeId())

	var ephemeral bool
	var device *dmanager.DeviceInfo
	var err error

	srcPath := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()
	readOnly := req.GetReadonly()
	fsType := req.GetVolumeCapability().GetMount().GetFsType()
	volumeContext := req.GetVolumeContext()
	// volumeContext contains the original volume name for persistent volumes.
	klog.V(3).Infof("NodePublishVolume request: VolID:%s targetpath:%v sourcepath:%v readonly:%v mount flags:%v fsType:%v context:%q",
		req.GetVolumeId(), targetPath, srcPath, readOnly, mountFlags, fsType, volumeContext)

	// Kubernetes v1.16+ would request ephemeral volumes via VolumeContext
	val, ok := req.GetVolumeContext()[parameters.Ephemeral]
	if ok {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		ephemeral = b
	} else {
		// If met all below conditions we still treat as a ephemeral volume request:
		// 1) No Volume found with given volume id
		// 2) No provisioner info found in VolumeContext "storage.kubernetes.io/csiProvisionerIdentity"
		// 3) No StagingPath in the request
		if device, err = ns.cs.dm.GetDevice(req.VolumeId); err != nil && !errors.Is(err, dmanager.ErrDeviceNotFound) {
			return nil, status.Errorf(codes.Internal, "failed to get device details for volume id '%s': %v", req.VolumeId, err)
		}
		_, ok := req.GetVolumeContext()[volumeProvisionerIdentity]
		ephemeral = device == nil && !ok && len(srcPath) == 0
	}

	var volumeParameters parameters.Volume
	if ephemeral {
		v, err := parameters.Parse(parameters.EphemeralVolumeOrigin, req.GetVolumeContext())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "ephemeral inline volume parameters: "+err.Error())
		}
		volumeParameters = v

		device, err := ns.createEphemeralDevice(ctx, req, volumeParameters)
		if err != nil {
			// createEphemeralDevice() returns status.Error, so safe to return
			return nil, err
		}
		srcPath = device.Path
		mountFlags = append(mountFlags, "dax")
	} else {
		// Validate parameters.
		v, err := parameters.Parse(parameters.PersistentVolumeOrigin, req.GetVolumeContext())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "persistent volume context: "+err.Error())
		}
		volumeParameters = v

		if device, err = ns.cs.dm.GetDevice(req.VolumeId); err != nil {
			if errors.Is(err, dmanager.ErrDeviceNotFound) {
				return nil, status.Errorf(codes.NotFound, "no device found with volume id %q: %v", req.VolumeId, err)
			}
			return nil, status.Errorf(codes.Internal, "failed to get device details for volume id %q: %v", req.VolumeId, err)
		}
		mountFlags = append(mountFlags, "bind")
	}

	if readOnly {
		mountFlags = append(mountFlags, "ro")
	}

	rawBlock := false
	switch req.VolumeCapability.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		rawBlock = true
		// For block volumes, source path is the actual Device path
		srcPath = device.Path
	case *csi.VolumeCapability_Mount:
		if !ephemeral && len(srcPath) == 0 {
			return nil, status.Error(codes.FailedPrecondition, "Staging target path missing in request")
		}

		notMnt, err := mount.IsNotMountPoint(ns.mounter, targetPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, status.Error(codes.Internal, "validate target path: "+err.Error())
		}
		if !notMnt {
			// Check if mount is compatible. Return OK if these match:
			// 1) Requested target path MUST match the published path of that volume ID
			// 2) VolumeCapability MUST match
			//    VolumeCapability/Mountflags must match used flags.
			//    VolumeCapability/fsType (if present in request) must match used fsType.
			// 3) Readonly MUST match
			// If there is mismatch of any of above, we return ALREADY_EXISTS error.
			mpList, err := ns.mounter.List()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "Failed to fetch existing mount details while checking %q: %v", targetPath, err)
			}
			for i := len(mpList) - 1; i >= 0; i-- {
				if mpList[i].Path == targetPath {
					klog.V(5).Infof("NodePublishVolume[%s]: Existing mountFlags:'%s', fsTyp: '%s'", req.VolumeId, mpList[i].Opts, mpList[i].Type)
					if (fsType == "" || mpList[i].Type == fsType) && findMountFlags(mountFlags, mpList[i].Opts) {
						klog.V(5).Infof("NodePublishVolume[%s]: parameters match existing, return OK", req.VolumeId)
						return &csi.NodePublishVolumeResponse{}, nil
					}
					break
				}
			}
			klog.V(5).Infof("NodePublishVolume[%s]: parameters do not match existing volume, return ALREADY_EXISTS", req.VolumeId)
			return nil, status.Error(codes.AlreadyExists, "Volume published but is incompatible")
		}
	}

	// We always (bind) mount. This is not strictly necessary for
	// Kata Containers and persistent volumes because we could use
	// the mounted filesystem at the staging path, but done anyway
	// for two reason:
	// - avoids special cases
	// - we don't have the staging path in NodeUnpublishVolume
	//   and can't undo some of the operations there if they refer to
	//   the staging path
	hostMount := targetPath
	if err := ns.mount(srcPath, hostMount, mountFlags, rawBlock); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Serialize by VolumeId
	volumeMutex.LockKey(req.VolumeId)
	defer volumeMutex.UnlockKey(req.VolumeId)

	var vol *nodeVolume
	if vol = ns.cs.getVolumeByID(req.VolumeId); vol == nil {
		// For ephemeral volumes we use req.VolumeId as volume name.
		vol = ns.cs.getVolumeByName(req.VolumeId)
	}

	// The CSI spec 1.2 requires that the SP returns NOT_FOUND
	// when the volume is not known.  This is problematic for
	// idempotent calls, in particular for ephemeral volumes,
	// because the first call will remove the volume and then the
	// second would fail. Even for persistent volumes this may be
	// problematic and therefore it was already changed for
	// ControllerUnpublishVolume
	// (https://github.com/container-storage-interface/spec/pull/375).
	//
	// For NodeUnpublishVolume, a bug is currently pending in
	// https://github.com/kubernetes/kubernetes/issues/90752.

	// Check if the target path is really a mount point. If it's not a mount point *and* we don't
	// have such a volume, then we are done.
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(targetPath)
	if (notMnt || err != nil && !os.IsNotExist(err)) && vol == nil {
		klog.V(5).Infof("NodeUnpublishVolume: %s is not mount point, no such volume -> done", targetPath)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// If we don't have volume information, we can't proceed. But
	// what we return depends on the circumstances.
	if vol == nil {
		if err == nil && !notMnt {
			// It is a mount point and we don't know the volume. Don't
			// do anything because the call is invalid. We return
			// NOT_FOUND as required by the spec.
			return nil, status.Errorf(codes.NotFound, "no volume found with volume id %q", req.VolumeId)
		}
		// No volume, no mount point. Looks like an
		// idempotent call for an operation that was
		// completed earlier, so don't return an
		// error.
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	p, err := parameters.Parse(parameters.NodeVolumeOrigin, vol.Params)
	if err != nil {
		// This should never happen because PMEM-CSI itself created these parameters.
		// But if it happens, better fail and force an admin to recover instead of
		// potentially destroying data.
		return nil, status.Errorf(codes.Internal, "previously stored volume parameters for volume with ID %q: %v", req.VolumeId, err)
	}

	// Unmounting the image if still mounted. It might have been unmounted before if
	// a previous NodeUnpublishVolume call was interrupted.
	if err == nil && !notMnt {
		klog.V(3).Infof("NodeUnpublishVolume: unmount %s", targetPath)
		if err := ns.mounter.Unmount(targetPath); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		klog.V(5).Infof("NodeUnpublishVolume: volume id:%s targetpath:%s has been unmounted", req.VolumeId, targetPath)
	}

	err = os.Remove(targetPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, status.Error(codes.Internal, "unexpected error while removing target path: "+err.Error())
	}
	klog.V(5).Infof("NodeUnpublishVolume: volume id:%s targetpath:%s has been removed with error: %v", req.VolumeId, targetPath, err)

	if p.GetPersistency() == parameters.PersistencyEphemeral {
		if _, err := ns.cs.DeleteVolume(ctx, &csi.DeleteVolumeRequest{VolumeId: vol.ID}); err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to delete ephemeral volume %s: %s", req.VolumeId, err.Error()))
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	stagingtargetPath := req.GetStagingTargetPath()
	if len(stagingtargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}

	// We should do nothing for block device usage
	switch req.VolumeCapability.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		return &csi.NodeStageVolumeResponse{}, nil
	}

	requestedFsType := req.GetVolumeCapability().GetMount().GetFsType()
	if requestedFsType == "" {
		// Default to ext4 filesystem
		requestedFsType = defaultFilesystem
	}

	// Serialize by VolumeId
	volumeMutex.LockKey(req.GetVolumeId())
	defer volumeMutex.UnlockKey(req.GetVolumeId())

	mountOptions := req.GetVolumeCapability().GetMount().GetMountFlags()
	klog.V(4).Infof("NodeStageVolume: VolumeID:%v Staging target path:%v Requested fsType:%v Requested mount options:%v",
		req.GetVolumeId(), stagingtargetPath, requestedFsType, mountOptions)

	device, err := ns.cs.dm.GetDevice(req.VolumeId)
	if err != nil {
		if errors.Is(err, dmanager.ErrDeviceNotFound) {
			return nil, status.Errorf(codes.NotFound, "no device found with volume id %q: %v", req.VolumeId, err)
		}
		return nil, status.Errorf(codes.Internal, "failed to get device details for volume id %q: %v", req.VolumeId, err)
	}

	// Check does devicepath already contain a filesystem?
	existingFsType, err := determineFilesystemType(device.Path)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// what to do if existing file system is detected;
	if existingFsType != "" {
		// Is existing filesystem type same as requested?
		if existingFsType == requestedFsType {
			klog.V(4).Infof("Skip mkfs as %v file system already exists on %v", existingFsType, device.Path)
		} else {
			return nil, status.Error(codes.AlreadyExists, "File system with different type exists")
		}
	} else {
		if err = ns.provisionDevice(device, requestedFsType); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	mountOptions = append(mountOptions, "dax")

	if err = ns.mount(device.Path, stagingtargetPath, mountOptions, false /* raw block */); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	stagingtargetPath := req.GetStagingTargetPath()
	if len(stagingtargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Serialize by VolumeId
	volumeMutex.LockKey(req.GetVolumeId())
	defer volumeMutex.UnlockKey(req.GetVolumeId())

	klog.V(4).Infof("NodeUnStageVolume: VolumeID:%v Staging target path:%v",
		req.GetVolumeId(), stagingtargetPath)

	// by spec, we have to return OK if asked volume is not mounted on asked path,
	// so we look up the current device by volumeID and see is that device
	// mounted on staging target path
	if _, err := ns.cs.dm.GetDevice(req.VolumeId); err != nil {
		if errors.Is(err, dmanager.ErrDeviceNotFound) {
			return nil, status.Errorf(codes.NotFound, "no device found with volume id '%s': %s", req.VolumeId, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to get device details for volume id '%s': %s", req.VolumeId, err.Error())
	}

	// Find out device name for mounted path
	mountedDev, _, err := mount.GetDeviceNameFromMount(ns.mounter, stagingtargetPath)
	if err != nil {
		return nil, err
	}
	if mountedDev == "" {
		klog.Warningf("NodeUnstageVolume: No device name for mount point '%v'", stagingtargetPath)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}
	klog.V(4).Infof("NodeUnstageVolume: detected mountedDev: %v", mountedDev)
	klog.V(3).Infof("NodeUnStageVolume: umount %s", stagingtargetPath)
	if err := ns.mounter.Unmount(stagingtargetPath); err != nil {
		klog.Errorf("NodeUnstageVolume: Umount failed: %v", err)
		return nil, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// createEphemeralDevice creates new device for given req.
// On failure it returns one of status errors.
func (ns *nodeServer) createEphemeralDevice(ctx context.Context, req *csi.NodePublishVolumeRequest, p parameters.Volume) (*dmanager.DeviceInfo, error) {
	// If the caller has use the heuristic for detecting ephemeral volumes, the flag won't
	// be set. Fix that here.
	ephemeral := parameters.PersistencyEphemeral
	p.Persistency = &ephemeral

	// Create new device, using the same code that the normal CreateVolume also uses,
	// so internally this volume will be tracked like persistent volumes.
	volumeID, _, err := ns.cs.createVolumeInternal(ctx, p, req.VolumeId,
		[]*csi.VolumeCapability{req.VolumeCapability},
		&csi.CapacityRange{RequiredBytes: p.GetSize()},
	)
	if err != nil {
		// This is already a status error.
		return nil, err
	}

	device, err := ns.cs.dm.GetDevice(volumeID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("ephemeral inline volume: device not found after creating volume %q: %v", volumeID, err))
	}

	// Create filesystem
	if err := ns.provisionDevice(device, req.GetVolumeCapability().GetMount().GetFsType()); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("ephemeral inline volume: failed to create filesystem: %v", err))
	}

	return device, nil
}

// provisionDevice initializes the device with requested filesystem.
// It can be called multiple times for the same device (idempotent).
func (ns *nodeServer) provisionDevice(device *dmanager.DeviceInfo, fsType string) error {
	if fsType == "" {
		// Empty FsType means "unspecified" and we pick default, currently hard-coded to ext4
		fsType = defaultFilesystem
	}

	cmd := ""
	var args []string
	// hard-code block size to 4k to avoid smaller values and trouble to dax mount option
	if fsType == "ext4" {
		cmd = "mkfs.ext4"
		args = []string{"-b 4096", "-F", device.Path}
	} else if fsType == "xfs" {
		cmd = "mkfs.xfs"
		// reflink and DAX are mutually exclusive
		// (http://man7.org/linux/man-pages/man8/mkfs.xfs.8.html).
		args = []string{"-b", "size=4096", "-m", "reflink=0", "-f", device.Path}
	} else {
		return fmt.Errorf("Unsupported filesystem '%s'. Supported filesystems types: 'xfs', 'ext4'", fsType)
	}
	output, err := exec.RunCommand(cmd, args...)
	if err != nil {
		// If the filesystem is already mounted, then
		// formatting it fails.  In that case we don't need to
		// format and can ignore that error.  We could check
		// for that ourselves in advance, but that would be
		// extra code.
		if strings.Contains(output, "contains a mounted filesystem") {
			return nil
		}
		return fmt.Errorf("mkfs failed: %s", output)
	}

	return nil
}

// mount creates the target path (parent must exist) and mounts the source there. It is idempotent.
func (ns *nodeServer) mount(sourcePath, targetPath string, mountOptions []string, rawBlock bool) error {
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to determain if '%s' is a valid mount point: %s", targetPath, err.Error())
	}
	if !notMnt {
		return nil
	}

	// Create target path, using a file for raw block bind mounts
	// or a directory for filesystems. Might already exist from a
	// previous call or because Kubernetes erroneously created it
	// for us.
	if rawBlock {
		f, err := os.OpenFile(targetPath, os.O_CREATE, os.FileMode(0644))
		if err == nil {
			defer f.Close()
		} else if !os.IsExist(err) {
			return fmt.Errorf("create target device file: %w", err)
		}
	} else {
		if err := os.Mkdir(targetPath, os.FileMode(0755)); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create target directory: %w", err)
		}
	}

	// We supposed to use "mount" package - ns.mounter.Mount()
	// but it seems not supporting -c "canonical" option, so do it with exec()
	// added -c makes canonical mount, resulting in mounted path matching what LV thinks is lvpath.
	args := []string{"-c"}
	if len(mountOptions) != 0 {
		args = append(args, "-o", strings.Join(mountOptions, ","))
	}

	args = append(args, sourcePath, targetPath)
	if _, err := exec.RunCommand("mount", args...); err != nil {
		return fmt.Errorf("mount filesystem failed: %s", err.Error())
	}

	return nil
}

// This is based on function used in LV-CSI driver
func determineFilesystemType(devicePath string) (string, error) {
	if devicePath == "" {
		return "", fmt.Errorf("null device path")
	}
	// Use `file -bsL` to determine whether any filesystem type is detected.
	// If a filesystem is detected (ie., the output is not "data", we use
	// `blkid` to determine what the filesystem is. We use `blkid` as `file`
	// has inconvenient output.
	// We do *not* use `lsblk` as that requires udev to be up-to-date which
	// is often not the case when a device is erased using `dd`.
	output, err := exec.RunCommand("file", "-bsL", devicePath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "data" {
		// No filesystem detected.
		return "", nil
	}
	// Some filesystem was detected, use blkid to figure out what it is.
	output, err = exec.RunCommand("blkid", "-c", "/dev/null", "-o", "full", devicePath)
	if err != nil {
		return "", err
	}
	if len(output) == 0 {
		return "", fmt.Errorf("no device information for %s", devicePath)
	}

	// expected output format from blkid:
	// devicepath: UUID="<uuid>" TYPE="<filesystem type>"
	attrs := strings.Split(string(output), ":")
	if len(attrs) != 2 {
		return "", fmt.Errorf("Can not parse blkid output: %s", output)
	}
	for _, field := range strings.Fields(attrs[1]) {
		attr := strings.Split(field, "=")
		if len(attr) == 2 && attr[0] == "TYPE" {
			return strings.Trim(attr[1], "\""), nil
		}
	}
	return "", fmt.Errorf("no filesystem type detected for %s", devicePath)
}

// ensureDirectory checks if the given directory is existing, if not attempts to create it.
// retruns true, if directory pre-exists, otherwise false.
func ensureDirectory(mounter mount.Interface, dir string) (bool, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, status.Errorf(codes.Internal, "Failed to check existence of directory %q: %s", dir, err.Error())
	}

	if err := os.MkdirAll(dir, os.FileMode(0755)); err != nil && !os.IsExist(err) {
		return false, status.Errorf(codes.Internal, "Could not create dir %q: %v", dir, err)
	}

	return false, nil
}

// findMountFlags finds existence of all flags in findIn array
func findMountFlags(flags []string, findIn []string) bool {
	for _, f := range flags {
		// bind mounts are not visible in mount options
		// so ignore the flag
		if f == "bind" {
			continue
		}
		found := false
		for _, fIn := range findIn {
			if f == fIn {
				found = true
				break
			}
		}
		if !found {
			klog.V(5).Infof("Mount flag '%s' not found in %v", f, findIn)
			return false
		}
	}

	return true
}
