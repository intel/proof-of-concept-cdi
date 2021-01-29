/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"

	dmanager "github.com/intel/cdi/pkg/device-manager"
	"github.com/intel/cdi/pkg/registryserver"
)

// MasterController defines master controller structure
type MasterController struct {
	*DefaultControllerServer
	rs                  *registryserver.RegistryServer
	driverTopologyKey   string
	devicesByVolumeName map[string]*dmanager.DeviceInfo   // map volumeName:DeviceInfo
	devicesByIDs        map[string]*dmanager.DeviceInfo   // map deviceID:DeviceInfo
	devicesByNodes      map[string][]*dmanager.DeviceInfo // map NodeID:DeviceInfos
	mutex               sync.Mutex                        // mutex for devices*
}

// NewMasterControllerServer creates MasterController object with a set of CSI capabilities
// and starts monitoring node additions and deletions to manage list of devices
func NewMasterControllerServer(rs *registryserver.RegistryServer, driverTopologyKey string) *MasterController {
	serverCaps := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
	}
	cs := &MasterController{
		DefaultControllerServer: NewDefaultControllerServer(serverCaps),
		rs:                      rs,
		driverTopologyKey:       driverTopologyKey,
		devicesByVolumeName:     map[string]*dmanager.DeviceInfo{},
		devicesByIDs:            map[string]*dmanager.DeviceInfo{},
		devicesByNodes:          map[string][]*dmanager.DeviceInfo{},
	}

	rs.AddListener(cs)

	return cs
}

// RegisterService registers master controller on the registry server
func (cs *MasterController) RegisterService(rpcServer *grpc.Server) {
	csi.RegisterControllerServer(rpcServer, cs)
}

// OnNodeAdded retrieves the existing devices at recently added Node.
// It uses ControllerServer.ListVolume() CSI call to retrieve devices.
func (cs *MasterController) OnNodeAdded(ctx context.Context, node *registryserver.NodeInfo) error {
	klog.V(5).Infof("OnNodeAdded: node: %+v", *node)
	conn, err := cs.rs.ConnectToNodeController(node.NodeID)
	if err != nil {
		return fmt.Errorf("Connection failure on given endpoint %s : %s", node.Endpoint, err.Error())
	}
	defer conn.Close()

	csiClient := csi.NewControllerClient(conn)
	resp, err := csiClient.ListVolumes(ctx, &csi.ListVolumesRequest{})
	if err != nil {
		return fmt.Errorf("Node failed to report devices: %s", err.Error())
	}

	klog.V(5).Infof("OnNodeAdded: Found Devices at %s: %v", node.NodeID, resp.Entries)

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// remove all node device ids from cs.devicesByIDs to ensure
	// that not reported devices will not stay in cs.devicesByIDs
	for _, device := range cs.devicesByNodes[node.NodeID] {
		delete(cs.devicesByIDs, device.ID)
	}

	// Reset masterController.devices* with received device info
	cs.devicesByNodes[node.NodeID] = []*dmanager.DeviceInfo{}
	for _, entry := range resp.Entries {
		vol := entry.GetVolume()
		if vol == nil { /* this shouldn't happen */
			continue
		}
		deviceInfo := &dmanager.DeviceInfo{
			ID:         vol.VolumeId,
			Size:       vol.CapacityBytes,
			Parameters: vol.VolumeContext,
		}
		cs.devicesByNodes[node.NodeID] = append(cs.devicesByNodes[node.NodeID], deviceInfo)
		cs.devicesByIDs[vol.VolumeId] = deviceInfo
		klog.V(5).Infof("OnNodeAdded: added Device %v", *deviceInfo)
	}
	return nil
}

// OnNodeDeleted deletes node devices when node is deleted
func (cs *MasterController) OnNodeDeleted(ctx context.Context, node *registryserver.NodeInfo) {
	klog.V(5).Infof("OnNodeDeleted: node: %s", node.NodeID)
	if devices, ok := cs.devicesByNodes[node.NodeID]; ok {
		klog.V(5).Infof("OnNodeDeleted: node %s: removing devices: %+v", node.NodeID, devices)
		delete(cs.devicesByNodes, node.NodeID)
	} else {
		klog.V(5).Infof("OnNodeDeleted: no devices registered for node id %s", node.NodeID)
	}
	// TODO: remove all devices with this NodeID from cs.devicesByID
}

func (cs *MasterController) findDeviceNode(volumeID string) (string, error) {
	for node, devices := range cs.devicesByNodes {
		for _, device := range devices {
			if device.ID == volumeID {
				return node, nil
			}
		}
	}
	return "", fmt.Errorf("volume id %s doesn't belong to any node", volumeID)

}

// findDevice finds devices satisfying CreateVolumeRequest and its topology info
func (cs *MasterController) findDevice(req *csi.CreateVolumeRequest) (*dmanager.DeviceInfo, []*csi.Topology, *string, error) {
	klog.V(5).Infof("masterController.findDevice: request: %+v", req)
	// Collect topology requests
	reqTopology := []*csi.Topology{}
	if reqTop := req.GetAccessibilityRequirements(); reqTop != nil {
		reqTopology = reqTop.Preferred
		if reqTopology == nil {
			reqTopology = reqTop.Requisite
		}
	}

	// Find device satisfying request parameters
	chosenDevices := map[string]*dmanager.DeviceInfo{}
	for nodeID, devices := range cs.devicesByNodes {
		for _, device := range devices {
			if device.Match(req.Parameters) {
				// if there is no topology constraint requested
				// first found device is OK
				if len(reqTopology) == 0 {
					klog.V(5).Infof("masterController.findDevice: request: %+v: found device: %v", req, device)
					return device, []*csi.Topology{
						&csi.Topology{
							Segments: map[string]string{
								cs.driverTopologyKey: nodeID,
							},
						},
					}, &nodeID, nil
				}
				chosenDevices[nodeID] = device
			}
		}
	}

	// chose device satisfying topology request
	for _, topology := range reqTopology {
		node := topology.Segments[cs.driverTopologyKey]
		if device, ok := chosenDevices[node]; ok {
			klog.V(5).Infof("masterController.findDevice: request: %+v: found device sutisfying topology request: %v", req, device)
			return device, []*csi.Topology{
				&csi.Topology{
					Segments: map[string]string{
						cs.driverTopologyKey: node,
					},
				},
			}, &node, nil
		}
	}

	klog.V(5).Infof("masterController.findDevice: request: %+v: device not found", req)
	return nil, nil, nil, fmt.Errorf("no device found")
}

// CreateVolume is a CSI API call that creates new CSI volume
// It finds node with a device satisfying requested parameters and informs node controller about device allocation
// It sets topology info to ensure that workload will be scheduled to the found node
func (cs *MasterController) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(5).Infof("masterController.CreateVolume: request: %+v", req)

	// Validate CreateVolumeRequest
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.Errorf("masterController.CreateVolume: invalid create volume req: %v: err: %v", req, err)
		return nil, err
	}

	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}

	capRange := req.GetCapacityRange()
	reqBytes := capRange.GetRequiredBytes()
	klog.V(3).Infof("masterController.CreateVolume: Name:%v required_bytes:%v limit_bytes:%v", req.Name, reqBytes, capRange.GetLimitBytes())

	device, topology, node, err := cs.findDevice(req)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           device.ID,
			CapacityBytes:      reqBytes,
			AccessibleTopology: topology,
			VolumeContext:      req.Parameters,
		},
	}

	req.Parameters["ID"] = device.ID

	// Inform Node Controller about new volume
	conn, err := cs.rs.ConnectToNodeController(*node)
	if err != nil {
		klog.Warningf("masterController.CreateVolume: failed to connect to node %s: %s", *node, err.Error())
		return nil, status.Errorf(codes.Internal, "failed to connect to node %s: %s", *node, err.Error())
	}

	defer conn.Close()

	csiClient := csi.NewControllerClient(conn)

	req.Parameters["ID"] = device.ID

	if _, err := csiClient.CreateVolume(ctx, req); err != nil {
		klog.Warningf("masterController.CreateVolume: failed to inform node about volume name:%s id:%s on %s: %s", *node, req.Name, device.ID, err.Error())
		return nil, status.Errorf(codes.Internal, "failed to inform node about volume name:%s id:%s on %s: %s", *node, req.Name, device.ID, err.Error())
	}

	klog.V(5).Infof("masterController.CreateVolume: response: %+v", resp)
	return resp, nil
}

// DeleteVolume is a CSI API call that deletes CSI volume
// It detaches volume from the device and informs node controller about the volume deletion
func (cs *MasterController) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.Errorf("invalid delete volume req: %v", req)
		return nil, err
	}

	// Get volume id
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	klog.V(4).Infof("DeleteVolume: requested volumeID: %v", volumeID)

	// Find device by ID
	device, ok := cs.devicesByIDs[volumeID]
	if ok {
		cs.mutex.Lock()
		defer cs.mutex.Unlock()
		delete(cs.devicesByVolumeName, device.VolumeName)
		volumeName := device.VolumeName
		device.VolumeName = ""

		node, err := cs.findDeviceNode(volumeID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to find node for volume id %s: %s", volumeID, err.Error())
		}

		// Call DeleteVolume on node controller
		conn, err := cs.rs.ConnectToNodeController(node)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to connect to node %s: %s", node, err.Error())
		}
		defer conn.Close()
		klog.V(4).Infof("Asking node %s to delete volume name:%s id:%s req:%v", node, volumeName, volumeID, req)
		if _, err := csi.NewControllerClient(conn).DeleteVolume(ctx, req); err != nil {
			return nil, err
		}
		klog.V(4).Infof("Controller DeleteVolume: volume name:%s id:%s deleted", volumeName, volumeID)
	} else {
		klog.Warningf("Volume %s not created by this controller", volumeID)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

// ValidateVolumeCapabilities is a CSI call that checks if a pre-provisioned volume
// has all the capabilities that the Container Orchestration system wants
func (cs *MasterController) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	_, found := cs.devicesByIDs[req.VolumeId]
	if !found {
		return nil, status.Error(codes.NotFound, "No volume found with id "+req.VolumeId)
	}

	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request")
	}

	for _, cap := range req.VolumeCapabilities {
		if cap.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Confirmed: nil,
				Message:   "Driver does not support '" + cap.AccessMode.Mode.String() + "' mode",
			}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
			VolumeContext:      req.GetVolumeContext(),
		},
	}, nil
}

// ListVolumes is a CSI call that returns the information about all the volumes that it knows about
// as CDI volumes map 1->1 to devices this call lists available devices
func (cs *MasterController) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(5).Info("ListVolumes")
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_LIST_VOLUMES); err != nil {
		klog.Errorf("invalid list volumes req: %v", req)
		return nil, err
	}

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// Copy from map into array for pagination.
	devices := make([]*dmanager.DeviceInfo, 0, len(cs.devicesByVolumeName))
	for _, device := range cs.devicesByVolumeName {
		devices = append(devices, device)
	}

	// Code originally copied from https://github.com/kubernetes-csi/csi-test/blob/f14e3d32125274e0c3a3a5df380e1f89ff7c132b/mock/service/controller.go#L309-L365

	var (
		ulenVols      = int32(len(devices))
		maxEntries    = req.MaxEntries
		startingToken int32
	)

	if v := req.StartingToken; v != "" {
		i, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"startingToken=%d !< int32=%d",
				startingToken, math.MaxUint32)
		}
		startingToken = int32(i)
	}

	if startingToken > ulenVols {
		return nil, status.Errorf(
			codes.Aborted,
			"startingToken=%d > len(vols)=%d",
			startingToken, ulenVols)
	}

	// Discern the number of remaining entries.
	rem := ulenVols - startingToken

	// If maxEntries is 0 or greater than the number of remaining entries then
	// set maxEntries to the number of remaining entries.
	if maxEntries == 0 || maxEntries > rem {
		maxEntries = rem
	}

	var (
		i       int
		j       = startingToken
		entries = make(
			[]*csi.ListVolumesResponse_Entry,
			maxEntries)
	)

	for i = 0; i < len(entries); i++ {
		device := devices[j]
		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      device.ID,
				CapacityBytes: device.Size,
			},
		}
		j++
	}

	var nextToken string
	if n := startingToken + int32(i); n < ulenVols {
		nextToken = fmt.Sprintf("%d", n)
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil
}

// GetCapacity is a CSI call that allows the Container Orchestration system to query the capacity of the
// storage pool from which the controller provisions volumes.
func (cs *MasterController) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	var capacity int64
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_GET_CAPACITY); err != nil {
		return nil, err
	}

	if top := req.GetAccessibleTopology(); top != nil {
		node, err := cs.rs.GetNodeController(top.Segments[cs.driverTopologyKey])
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
		cap, err := cs.getNodeCapacity(ctx, node, req)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get node(%s) capacity: %s", node, err.Error())
		}
		capacity = cap
	} else {
		for _, node := range cs.rs.NodeClients() {
			cap, err := cs.getNodeCapacity(ctx, *node, req)
			if err != nil {
				klog.Warningf("Error while fetching '%s' node capacity: %s", node.NodeID, err.Error())
				continue
			}
			capacity += cap
		}
	}

	return &csi.GetCapacityResponse{
		AvailableCapacity: capacity,
	}, nil
}

func (cs *MasterController) getNodeCapacity(ctx context.Context, node registryserver.NodeInfo, req *csi.GetCapacityRequest) (int64, error) {
	conn, err := cs.rs.ConnectToNodeController(node.NodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to node %s: %s", node.NodeID, err.Error())
	}

	defer conn.Close()

	csiClient := csi.NewControllerClient(conn)
	resp, err := csiClient.GetCapacity(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("Error while fetching '%s' node capacity: %s", node.NodeID, err.Error())
	}

	return resp.AvailableCapacity, nil
}
