/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	grpcserver "github.com/intel/cdi/pkg/grpc-server"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// IdentityServer defines identity service structure
type IdentityServer struct {
	name       string
	version    string
	pluginCaps []*csi.PluginCapability
}

var _ grpcserver.Service = &IdentityServer{}

// NewIdentityServer creates IdentityServer object that provides
// CSI Identity service
func NewIdentityServer(name, version string) (*IdentityServer, error) {
	return &IdentityServer{
		name:    name,
		version: version,
		pluginCaps: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

// RegisterService registers IdentityServer on the registry server
func (ids *IdentityServer) RegisterService(rpcServer *grpc.Server) {
	csi.RegisterIdentityServer(rpcServer, ids)
}

// GetPluginInfo returns CSI plugin metadata
func (ids *IdentityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          ids.name,
		VendorVersion: ids.version,
	}, nil
}

// Probe returns the health and readiness of the CSI plugin
func (ids *IdentityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

// GetPluginCapabilities returns available CSI plugin capabilities
func (ids *IdentityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: ids.pluginCaps,
	}, nil
}
