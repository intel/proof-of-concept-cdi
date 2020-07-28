/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultControllerServer is a base structure
// for master and node controller servers
type DefaultControllerServer struct {
	serviceCaps []*csi.ControllerServiceCapability
}

// NewDefaultControllerServer creates new DefaulControllerServer object with a set of CSI capabilities
func NewDefaultControllerServer(caps []csi.ControllerServiceCapability_RPC_Type) *DefaultControllerServer {
	serviceCaps := []*csi.ControllerServiceCapability{}
	for _, cap := range caps {
		serviceCaps = append(serviceCaps, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return &DefaultControllerServer{
		serviceCaps: serviceCaps,
	}
}

// CreateVolume is a CSI API call that creates new CSI volume
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_VOLUME controller capability.
// This RPC will be called by the CO to provision a new volume on behalf of a user
// (to be consumed as either a block device or a mounted filesystem).
//
// This operation MUST be idempotent. If a volume corresponding to the specified volume name already exists,
// is accessible from accessibility_requirements, and is compatible with the specified capacity_range,
// volume_capabilities and parameters in the CreateVolumeRequest, the Plugin MUST reply 0 OK with the corresponding CreateVolumeResponse.
func (cs *DefaultControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteVolume is a CSI API call that deletes CSI volume
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_VOLUME capability.
// This RPC will be called by the CO to deprovision a volume.
//
// This operation MUST be idempotent. If a volume corresponding to the specified volume_id does not exist
// or the artifacts associated with the volume do not exist anymore, the Plugin MUST reply 0 OK.
func (cs *DefaultControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerPublishVolume is a CSI API call that makes volume to be available on the node
// A Controller Plugin MUST implement this RPC call if it has PUBLISH_UNPUBLISH_VOLUME controller capability.
// This RPC will be called by the CO when it wants to place a workload that uses the volume onto a node.
// The Plugin SHOULD perform the work that is necessary for making the volume available on the given node.
// The Plugin MUST NOT assume that this RPC will be executed on the node where the volume will be used.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id has already been published
// at the node corresponding to the node_id, and is compatible with the specified volume_capability and readonly
// flag, the Plugin MUST reply 0 OK.
//
// If the operation failed or the CO does not know if the operation has failed or not, it MAY choose to call
// ControllerPublishVolume again or choose to call ControllerUnpublishVolume.
//
// The CO MAY call this RPC for publishing a volume to multiple nodes if the volume has MULTI_NODE capability
// (i.e., MULTI_NODE_READER_ONLY, MULTI_NODE_SINGLE_WRITER or MULTI_NODE_MULTI_WRITER).
func (cs *DefaultControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerUnpublishVolume is a reverse operation of ControllerPublishVolume
// Controller Plugin MUST implement this RPC call if it has PUBLISH_UNPUBLISH_VOLUME controller capability.
// This RPC is a reverse operation of ControllerPublishVolume. It MUST be called after all NodeUnstageVolume
// and NodeUnpublishVolume on the volume are called and succeed. The Plugin SHOULD perform the work that is
// necessary for making the volume ready to be consumed by a different node. The Plugin MUST NOT assume that
// this RPC will be executed on the node where the volume was previously used.
//
// This RPC is typically called by the CO when the workload using the volume is being moved to a different node,
// or all the workload using the volume on a node has finished.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id is not attached to the node
// corresponding to the node_id, the Plugin MUST reply 0 OK. If the volume corresponding to the volume_id or the
// node corresponding to node_id cannot be found by the Plugin and the volume can be safely regarded as
// ControllerUnpublished from the node, the plugin SHOULD return 0 OK. If this operation failed, or the CO
// does not know if the operation failed or not, it can choose to call ControllerUnpublishVolume again.
func (cs *DefaultControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListVolumes is a CSI API call that lists available volumes
// A Controller Plugin MUST implement this RPC call if it has LIST_VOLUMES capability.
// The Plugin SHALL return the information about all the volumes that it knows about.
// If volumes are created and/or deleted while the CO is concurrently paging through
// ListVolumes results then it is possible that the CO MAY either witness duplicate
// volumes in the list, not witness existing volumes, or both.
// The CO SHALL NOT expect a consistent "view" of all volumes when paging through the
// volume list via multiple calls to ListVolumes.
func (cs *DefaultControllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// GetCapacity is a CSI API call that reports capacity of the storage pool
// A Controller Plugin MUST implement this RPC call if it has GET_CAPACITY controller capability.
// The RPC allows the CO to query the capacity of the storage pool from which the controller provisions volumes.
func (cs *DefaultControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities is a CSI API call that returns controller service capabilities
// A Controller Plugin MUST implement this RPC call.
// This RPC allows the CO to check the supported capabilities of controller service provided by the Plugin.
func (cs *DefaultControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.serviceCaps,
	}, nil
}

// CreateSnapshot is a CSI API call that creates a new volume snapshot
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_SNAPSHOT controller capability.
// This RPC will be called by the CO to create a new snapshot from a source volume on behalf of a user.
//
// This operation MUST be idempotent. If a snapshot corresponding to the specified snapshot name is
// successfully cut and ready to use (meaning it MAY be specified as a volume_content_source in a
// CreateVolumeRequest), the Plugin MUST reply 0 OK with the corresponding CreateSnapshotResponse.
//
// If an error occurs before a snapshot is cut, CreateSnapshot SHOULD return a corresponding
// gRPC error code that reflects the error condition.
func (cs *DefaultControllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteSnapshot is a CSI API call that deletes a volume snapshot
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_SNAPSHOT capability.
// This RPC will be called by the CO to delete a snapshot.
//
// This operation MUST be idempotent. If a snapshot corresponding to the specified snapshot_id does not
// exist or the artifacts associated with the snapshot do not exist anymore, the Plugin MUST reply 0 OK.
func (cs *DefaultControllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListSnapshots is a CSI API call that lists available snapshots
// A Controller Plugin MUST implement this RPC call if it has LIST_SNAPSHOTS capability.
// The Plugin SHALL return the information about all snapshots on the storage system within the given parameters
// regardless of how they were created. ListSnapshots SHALL NOT list a snapshot that is being created but has
// not been cut successfully yet. If snapshots are created and/or deleted while the CO is concurrently paging
// through ListSnapshots results then it is possible that the CO MAY either witness duplicate snapshots in the
// list, not witness existing snapshots, or both. The CO SHALL NOT expect a consistent "view" of all snapshots
// when paging through the snapshot list via multiple calls to ListSnapshots.
func (cs *DefaultControllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ValidateControllerServiceRequest checks if controller has valid CSI service capabilities
func (cs *DefaultControllerServer) ValidateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range cs.serviceCaps {
		if c == cap.GetRpc().GetType() {
			return nil
		}
	}
	return status.Error(codes.InvalidArgument, string(c))
}

// ControllerExpandVolume is a CSI API call that expands existing volume
// A Controller plugin MUST implement this RPC call if plugin has EXPAND_VOLUME controller capability.
// This RPC allows the CO to expand the size of a volume.
//
// This operation MUST be idempotent. If a volume corresponding to the specified volume ID is already larger
// than or equal to the target capacity of the expansion request, the plugin SHOULD reply 0 OK.
func (cs *DefaultControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
