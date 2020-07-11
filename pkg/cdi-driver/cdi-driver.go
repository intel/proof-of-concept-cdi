/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package cdidriver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	api "github.com/intel/cdi/pkg/apis/cdi/v1alpha1"
	dmanager "github.com/intel/cdi/pkg/device-manager"
	cdigrpc "github.com/intel/cdi/pkg/grpc"
	grpcserver "github.com/intel/cdi/pkg/grpc-server"
	registry "github.com/intel/cdi/pkg/registry"
	"github.com/intel/cdi/pkg/registryserver"
	"github.com/intel/cdi/pkg/scheduler"
	state "github.com/intel/cdi/pkg/state"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	connectionTimeout time.Duration = 10 * time.Second
	retryTimeout      time.Duration = 10 * time.Second
	requestTimeout    time.Duration = 10 * time.Second
)

type DriverMode string

func (mode *DriverMode) Set(value string) error {
	switch value {
	case string(Controller), string(Node):
		*mode = DriverMode(value)
	default:
		// The flag package will add the value to the final output, no need to do it here.
		return errors.New("invalid driver mode")
	}
	return nil
}

func (mode *DriverMode) String() string {
	return string(*mode)
}

const (
	//Controller definition for controller driver mode
	Controller DriverMode = "controller"
	//Node definition for noder driver mode
	Node DriverMode = "node"
)

var (
	//PmemDriverTopologyKey key to use for topology constraint
	DriverTopologyKey = ""
)

//Config type for driver configuration
type Config struct {
	//DriverName name of the csi driver
	DriverName string
	//NodeID node id on which this csi driver is running
	NodeID string
	//Endpoint exported csi driver endpoint
	Endpoint string
	//TestEndpoint adds the controller service to the server listening on Endpoint.
	//Only needed for testing.
	TestEndpoint bool
	//Mode mode fo the driver
	Mode DriverMode
	//RegistryName exported registry peer name
	RegistryName string
	//RegistryEndpoint exported registry server endpoint
	RegistryEndpoint string
	//CAFile Root certificate authority certificate file
	CAFile string
	//CertFile certificate for server authentication
	CertFile string
	//KeyFile server private key file
	KeyFile string
	//ClientCertFile certificate for client side authentication
	ClientCertFile string
	//ClientKeyFile client private key
	ClientKeyFile string
	//Controller exported node controller peer name
	ControllerName string
	//ControllerEndpoint exported node controller endpoint
	ControllerEndpoint string
	//DeviceManager device manager to use
	DeviceManager api.DeviceMode
	//Directory where to persist the node driver state
	StateBasePath string
	//Version driver release version
	Version string

	// parameters for Kubernetes scheduler extender
	schedulerListen string
	client          kubernetes.Interface
}

type csiDriver struct {
	cfg             Config
	serverTLSConfig *tls.Config
	clientTLSConfig *tls.Config
}

func GetCSIDriver(cfg Config) (*csiDriver, error) {
	validModes := map[DriverMode]struct{}{
		Controller: struct{}{},
		Node:       struct{}{},
	}
	var serverConfig *tls.Config
	var clientConfig *tls.Config
	var err error

	if _, ok := validModes[cfg.Mode]; !ok {
		return nil, fmt.Errorf("Invalid driver mode: %s", string(cfg.Mode))
	}
	if cfg.DriverName == "" || cfg.NodeID == "" || cfg.Endpoint == "" {
		return nil, fmt.Errorf("One of mandatory(Drivername Node id or Endpoint) configuration option missing")
	}
	if cfg.RegistryEndpoint == "" {
		cfg.RegistryEndpoint = cfg.Endpoint
	}
	if cfg.ControllerEndpoint == "" {
		cfg.ControllerEndpoint = cfg.Endpoint
	}

	if cfg.Mode == Node && cfg.StateBasePath == "" {
		cfg.StateBasePath = "/var/lib/" + cfg.DriverName
	}

	peerName := cfg.RegistryName
	if cfg.Mode == Controller {
		//When driver running in Controller mode, we connect to node controllers
		//so use appropriate peer name
		peerName = cfg.ControllerName
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		serverConfig, err = cdigrpc.LoadServerTLS(cfg.CAFile, cfg.CertFile, cfg.KeyFile, peerName)
		if err != nil {
			return nil, err
		}
	}

	/* if no client certificate details provided use same server certificate to connect to peer server */
	if cfg.ClientCertFile == "" {
		cfg.ClientCertFile = cfg.CertFile
		cfg.ClientKeyFile = cfg.KeyFile
	}

	if cfg.ClientCertFile != "" && cfg.ClientKeyFile != "" {
		clientConfig, err = cdigrpc.LoadClientTLS(cfg.CAFile, cfg.ClientCertFile, cfg.ClientKeyFile, peerName)
		if err != nil {
			return nil, err
		}
	}

	DriverTopologyKey = cfg.DriverName + "/node"

	return &csiDriver{
		cfg:             cfg,
		serverTLSConfig: serverConfig,
		clientTLSConfig: clientConfig,
	}, nil
}

func (csid *csiDriver) Run() error {
	// Create GRPC servers
	ids, err := NewIdentityServer(csid.cfg.DriverName, csid.cfg.Version)
	if err != nil {
		return err
	}

	s := grpcserver.NewNonBlockingGRPCServer()
	// Ensure that the server is stopped before we return.
	defer func() {
		s.ForceStop()
		s.Wait()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if csid.cfg.Mode == Controller {
		rs := registryserver.New(csid.clientTLSConfig)
		cs := NewMasterControllerServer(rs)

		if csid.cfg.Endpoint != csid.cfg.RegistryEndpoint {
			if err := s.Start(csid.cfg.Endpoint, nil, ids, cs); err != nil {
				return err
			}
			if err := s.Start(csid.cfg.RegistryEndpoint, csid.serverTLSConfig, rs); err != nil {
				return err
			}
		} else {
			if err := s.Start(csid.cfg.Endpoint, csid.serverTLSConfig, ids, cs, rs); err != nil {
				return err
			}
		}

		// Also run scheduler extender?
		if _, err := csid.startScheduler(ctx, cancel, rs); err != nil {
			return err
		}
	} else if csid.cfg.Mode == Node {
		dm, err := newDeviceManager(csid.cfg.DeviceManager)
		if err != nil {
			return err
		}
		sm, err := state.NewFileState(csid.cfg.StateBasePath)
		if err != nil {
			return err
		}
		cs := NewNodeControllerServer(csid.cfg.NodeID, dm, sm)
		ns := NewNodeServer(cs, filepath.Clean(csid.cfg.StateBasePath)+"/mount")

		if csid.cfg.Endpoint != csid.cfg.ControllerEndpoint {
			if err := s.Start(csid.cfg.ControllerEndpoint, csid.serverTLSConfig, cs); err != nil {
				return err
			}
			if err := csid.registerNodeController(); err != nil {
				return err
			}
			services := []grpcserver.Service{ids, ns}
			if csid.cfg.TestEndpoint {
				services = append(services, cs)
			}
			if err := s.Start(csid.cfg.Endpoint, nil, services...); err != nil {
				return err
			}
		} else {
			if err := s.Start(csid.cfg.Endpoint, nil, ids, cs, ns); err != nil {
				return err
			}
			if err := csid.registerNodeController(); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("Unsupported device mode '%v", csid.cfg.Mode)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-c:
		// Here we want to shut down cleanly, i.e. let running
		// gRPC calls complete.
		klog.V(3).Infof("Caught signal %s, terminating.", sig)
		if csid.cfg.Mode == Node {
			klog.V(3).Info("Unregistering node...")
			if err := csid.unregisterNodeController(); err != nil {
				klog.Errorf("Failed to unregister node: %v", err)
				return err
			}
		}

	case <-ctx.Done():
		// The scheduler HTTP server must have failed (to start).
		// We quit in that case.
	}
	s.Stop()
	s.Wait()

	return nil
}

func (csid *csiDriver) registerNodeController() error {
	var err error
	var conn *grpc.ClientConn

	for {
		klog.V(3).Infof("Connecting to registry server at: %s\n", csid.cfg.RegistryEndpoint)
		conn, err = cdigrpc.Connect(csid.cfg.RegistryEndpoint, csid.clientTLSConfig)
		if err == nil {
			break
		}
		klog.Warningf("Failed to connect registry server: %s, retrying after %v seconds...", err.Error(), retryTimeout.Seconds())
		time.Sleep(retryTimeout)
	}

	req := &registry.RegisterControllerRequest{
		NodeId:   csid.cfg.NodeID,
		Endpoint: csid.cfg.ControllerEndpoint,
	}

	if err := register(context.Background(), conn, req); err != nil {
		return err
	}
	go waitAndWatchConnection(conn, req)

	return nil
}

func (csid *csiDriver) unregisterNodeController() error {
	req := &registry.UnregisterControllerRequest{
		NodeId: csid.cfg.NodeID,
	}
	conn, err := cdigrpc.Connect(csid.cfg.RegistryEndpoint, csid.clientTLSConfig)
	if err != nil {
		return err
	}

	client := registry.NewRegistryClient(conn)
	_, err = client.UnregisterController(context.Background(), req)

	return err
}

// startScheduler starts the scheduler extender if it is enabled. It
// logs errors and cancels the context when it runs into a problem,
// either during the startup phase (blocking) or later at runtime (in
// a go routine).
func (csid *csiDriver) startScheduler(ctx context.Context, cancel func(), rs *registryserver.RegistryServer) (string, error) {
	if csid.cfg.schedulerListen == "" {
		return "", nil
	}

	resyncPeriod := 1 * time.Hour
	factory := informers.NewSharedInformerFactory(csid.cfg.client, resyncPeriod)
	pvcLister := factory.Core().V1().PersistentVolumeClaims().Lister()
	scLister := factory.Storage().V1().StorageClasses().Lister()
	sched, err := scheduler.NewScheduler(
		csid.cfg.DriverName,
		scheduler.CapacityViaRegistry(rs),
		csid.cfg.client,
		pvcLister,
		scLister,
	)
	if err != nil {
		return "", fmt.Errorf("create scheduler: %v", err)
	}
	factory.Start(ctx.Done())
	cacheSyncResult := factory.WaitForCacheSync(ctx.Done())
	klog.V(5).Infof("synchronized caches: %+v", cacheSyncResult)
	for t, v := range cacheSyncResult {
		if !v {
			return "", fmt.Errorf("failed to sync informer for type %v", t)
		}
	}
	return csid.startHTTPSServer(ctx, cancel, csid.cfg.schedulerListen, sched)
}

// startHTTPSServer contains the common logic for starting and
// stopping an HTTPS server.  Returns an error or the address that can
// be used in Dial("tcp") to reach the server (useful for testing when
// "listen" does not include a port).
func (csid *csiDriver) startHTTPSServer(ctx context.Context, cancel func(), listen string, handler http.Handler) (string, error) {
	config, err := cdigrpc.LoadServerTLS(csid.cfg.CAFile, csid.cfg.CertFile, csid.cfg.KeyFile, "")
	if err != nil {
		return "", fmt.Errorf("initialize HTTPS config: %v", err)
	}
	server := http.Server{
		Addr: listen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			klog.V(5).Infof("HTTP request: %s %q from %s %s", r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
			handler.ServeHTTP(w, r)
		}),
		TLSConfig: config,
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return "", fmt.Errorf("listen on TCP address %q: %v", listen, err)
	}
	tcpListener := listener.(*net.TCPListener)
	go func() {
		defer tcpListener.Close()

		err := server.ServeTLS(listener, csid.cfg.CertFile, csid.cfg.KeyFile)
		if err != http.ErrServerClosed {
			klog.Errorf("%s HTTPS server error: %v", listen, err)
		}
		// Also stop main thread.
		cancel()
	}()
	go func() {
		// Block until the context is done, then immediately
		// close the server.
		<-ctx.Done()
		server.Close()
	}()

	return tcpListener.Addr().String(), nil
}

// waitAndWatchConnection Keeps watching for connection changes, and whenever the
// connection state changed from lost to ready, it re-register the node controller with registry server.
func waitAndWatchConnection(conn *grpc.ClientConn, req *registry.RegisterControllerRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionLost := false

	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			if connectionLost {
				klog.V(4).Info("ReConnected.")
				if err := register(ctx, conn, req); err != nil {
					klog.Warning(err)
				}
			}
		} else {
			connectionLost = true
			klog.V(4).Info("Connection state: ", s)
		}
		conn.WaitForStateChange(ctx, s)
	}
}

// register Tries to register with RegistryServer in endless loop till,
// either the registration succeeds or RegisterController() returns only possible InvalidArgument error.
func register(ctx context.Context, conn *grpc.ClientConn, req *registry.RegisterControllerRequest) error {
	client := registry.NewRegistryClient(conn)
	for {
		klog.V(3).Info("Registering controller...")
		if _, err := client.RegisterController(ctx, req); err != nil {
			if s, ok := status.FromError(err); ok && s.Code() == codes.InvalidArgument {
				return fmt.Errorf("Registration failed: %s", s.Message())
			}
			klog.Warningf("Failed to register: %s, retrying after %v seconds...", err.Error(), retryTimeout.Seconds())
			time.Sleep(retryTimeout)
		} else {
			break
		}
	}
	klog.V(4).Info("Registration success")

	return nil
}

func newDeviceManager(dmType api.DeviceMode) (dmanager.DeviceManager, error) {
	if dmType == api.DeviceModeFPGA {
		return dmanager.NewFpgaDman()
	} /*elif dmType == api.DeviceModeGPU {
		return dmanager.NewGpuDman()
	}*/
	return nil, fmt.Errorf("Unsupported device manager type '%s'", dmType)
}
