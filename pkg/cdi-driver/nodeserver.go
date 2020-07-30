/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"os"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"

	dmanager "github.com/intel/cdi/pkg/device-manager"
)

var (
	jsonCDI = "CDI.json"
)

type nodeServer struct {
	nodeID            string
	driverTopologyKey string
	dm                *dmanager.DeviceManager
	nodeCaps          []*csi.NodeServiceCapability
}

func newNodeServer(nodeID, driverTopologyKey string, dm *dmanager.DeviceManager) *nodeServer {
	return &nodeServer{
		nodeID:            nodeID,
		driverTopologyKey: driverTopologyKey,
		dm:                dm,
		nodeCaps: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}
}

func (ns *nodeServer) RegisterService(rpcServer *grpc.Server) {
	csi.RegisterNodeServer(rpcServer, ns)
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.nodeID,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				ns.driverTopologyKey: ns.nodeID,
			},
		},
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.nodeCaps,
	}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(5).Infof("NodeStageVolume: request: %+v", req)

	// Check arguments
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	targetPath := req.GetStagingTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if volCap.GetBlock() != nil {
		return nil, status.Error(codes.InvalidArgument, "Block volumes not supported")
	}
	mnt := volCap.GetMount()
	if mnt == nil {
		return nil, status.Error(codes.InvalidArgument, "Only mounted volumes supported")
	}

	// Create target path
	if err := os.MkdirAll(targetPath, 0750); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to create target path %s: %v", targetPath, err)
	}

	device, err := ns.dm.GetDevice(volumeID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get device for the volume %s: %s", volumeID, err.Error())
	}

	// Write device info in CDI JSON format
	targetFile := filepath.Join(targetPath, jsonCDI)
	err = device.Marshall(targetFile)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshall device info to %s: %s", targetFile, err.Error())
	}

	klog.V(5).Infof("NodeStageVolume: volume %s has been staged on the path %s", volumeID, targetFile)

	resp := &csi.NodeStageVolumeResponse{}
	klog.V(5).Infof("NodeStageVolume: response: %+v", resp)

	return resp, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(5).Infof("NodeUnStageVolume: request: %+v", req)

	// Check arguments
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	targetPath := req.GetStagingTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Delete JSON file created by NodeStageVolume
	targetFile := filepath.Join(targetPath, jsonCDI)
	if _, err := os.Stat(targetFile); err == nil {
		os.Remove(targetFile)
		klog.V(5).Infof("NodeUnStageVolume: device %s: %s has been removed", volumeID, targetFile)
	}

	resp := &csi.NodeUnstageVolumeResponse{}
	klog.V(5).Infof("NodeUnStageVolume: response: %+v", resp)

	return resp, nil
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
