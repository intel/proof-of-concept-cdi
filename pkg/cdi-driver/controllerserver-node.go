/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"

	"github.com/container-storage-interface/spec/lib/go/csi"

	dmanager "github.com/intel/cdi/pkg/device-manager"
	state "github.com/intel/cdi/pkg/state"
)

type nodeControllerServer struct {
	*DefaultControllerServer
	nodeID string
	dm     *dmanager.DeviceManager
}

// newNodeControllerServer creates nodeControllerServer
func newNodeControllerServer(nodeID string, dm *dmanager.DeviceManager, sm state.StateManager) (*nodeControllerServer, error) {
	serverCaps := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
	}

	return &nodeControllerServer{
		DefaultControllerServer: NewDefaultControllerServer(serverCaps),
		nodeID:                  nodeID,
		dm:                      dm,
	}, nil
}

func (cs *nodeControllerServer) RegisterService(rpcServer *grpc.Server) {
	csi.RegisterControllerServer(rpcServer, cs)
}

func (cs *nodeControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(5).Infof("nodeControllerServer.CreateVolume: request: %+v", req)
	// Validate request
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.Errorf("nodeControllerServer.CreateVolume: invalid create volume request: %v", req)
		return nil, err
	}
	volumeName := req.GetName()
	if len(volumeName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}

	deviceID, ok := req.Parameters["ID"]
	if !ok {
		return nil, status.Errorf(codes.Internal, "device ID is missing in request")
	}

	// Allocate the device for the volume
	if err := cs.dm.Allocate(deviceID, volumeName); err != nil {
		return nil, status.Errorf(codes.Internal, "can't allocate volume %s to device id %s: error: %+v", volumeName, deviceID, err)
	}

	resp := &csi.CreateVolumeResponse{Volume: &csi.Volume{}}
	klog.V(5).Infof("nodeControllerServer.CreateVolume: response: %+v", resp)

	return resp, nil
}

func (cs *nodeControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	// Validate request
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
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

func (cs *nodeControllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(5).Infof("nodeControllerServer.ListVolumes: request: %v", req)
	if err := cs.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_LIST_VOLUMES); err != nil {
		klog.Errorf("nodeControllerServer.ListVolumes: invalid list volumes request: %v", req)
		return nil, err
	}
	// List devices
	var entries []*csi.ListVolumesResponse_Entry
	for _, dev := range cs.dm.ListDevices() {
		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      dev.ID,
				CapacityBytes: dev.Size,
				VolumeContext: dev.Parameters,
			},
		})
	}

	resp := &csi.ListVolumesResponse{Entries: entries}

	klog.V(5).Infof("nodeControllerServer.ListVolumes: response: %+v", resp)
	return resp, nil
}

func (cs *nodeControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	var capacity int64

	/*cap, err := cs.dm.GetCapacity()
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}*/

	capacity = int64(100)

	return &csi.GetCapacityResponse{
		AvailableCapacity: capacity,
	}, nil
}

func (cs *nodeControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
