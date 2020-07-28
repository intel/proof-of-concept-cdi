/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"crypto/tls"
	"fmt"
	"sync"

	cdigrpc "github.com/intel/cdi/pkg/grpc"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

// Service interface proides RegisterService method that will be called
// by NonBlockingGRPCServer whenever its about to start a grpc server on an endpoint.
type Service interface {
	RegisterService(s *grpc.Server)
}

// NonBlockingGRPCServer structure
type NonBlockingGRPCServer struct {
	wg      sync.WaitGroup
	servers []*grpc.Server
}

// NewNonBlockingGRPCServer creates new NonBlockingGRPCServer object
func NewNonBlockingGRPCServer() *NonBlockingGRPCServer {
	return &NonBlockingGRPCServer{}
}

// Start starts nonblocking GRPC service
func (s *NonBlockingGRPCServer) Start(endpoint string, tlsConfig *tls.Config, services ...Service) error {
	if endpoint == "" {
		return fmt.Errorf("endpoint cannot be empty")
	}
	rpcServer, l, err := cdigrpc.NewServer(endpoint, tlsConfig)
	if err != nil {
		return nil
	}
	for _, service := range services {
		service.RegisterService(rpcServer)
	}
	s.servers = append(s.servers, rpcServer)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		klog.V(3).Infof("Listening for connections on address: %v", l.Addr())
		if err := rpcServer.Serve(l); err != nil {
			klog.Errorf("Server Listen failure: %s", err.Error())
		}
		klog.V(3).Infof("Server on '%s' stopped", endpoint)
	}()

	return nil
}

// Wait wraps WaitGrup.wait call
func (s *NonBlockingGRPCServer) Wait() {
	s.wg.Wait()
}

// Stop gracefully stops GRPC servers
func (s *NonBlockingGRPCServer) Stop() {
	for _, s := range s.servers {
		s.GracefulStop()
	}
}

// ForceStop force stops GRPC servers
func (s *NonBlockingGRPCServer) ForceStop() {
	for _, s := range s.servers {
		s.Stop()
	}
}
