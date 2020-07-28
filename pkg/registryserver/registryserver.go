package registryserver

import (
	"crypto/tls"
	"fmt"
	"sync"

	cdigrpc "github.com/intel/cdi/pkg/grpc"
	registry "github.com/intel/cdi/pkg/registry"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"k8s.io/utils/keymutex"
)

// RegistryListener is an interface for registry server change listeners
// All the callbacks are called once after updating the in-memory
// registry data.
type RegistryListener interface {
	// OnNodeAdded is called by RegistryServer whenever a new node controller is registered
	// or node controller updated its endpoint.
	// In case of error, the node registration would fail and removed from registry.
	OnNodeAdded(ctx context.Context, node *NodeInfo) error
	// OnNodeDeleted is called by RegistryServer whenever a node controller unregistered.
	// Callback implementations has to note that by the time this method is called,
	// the NodeInfo for that node have already removed from in-memory registry.
	OnNodeDeleted(ctx context.Context, node *NodeInfo)
}

// RegistryServer defines a structure for registry server
type RegistryServer struct {
	// mutex is used to protect concurrent access of RegistryServer's
	// data(nodeClients)
	mutex sync.Mutex
	// rpcMutex is used to avoid concurrent RPC(RegisterController, UnregisterController)
	// requests from the same node
	rpcMutex        keymutex.KeyMutex
	clientTLSConfig *tls.Config
	nodeClients     map[string]*NodeInfo
	listeners       map[RegistryListener]struct{}
}

// NodeInfo structure holds node information
type NodeInfo struct {
	//NodeID controller node id
	NodeID string
	//Endpoint node controller endpoint
	Endpoint string
}

// New creates new registry server
func New(tlsConfig *tls.Config) *RegistryServer {
	return &RegistryServer{
		rpcMutex:        keymutex.NewHashed(-1),
		clientTLSConfig: tlsConfig,
		nodeClients:     map[string]*NodeInfo{},
		listeners:       map[RegistryListener]struct{}{},
	}
}

// RegisterService registers grpc server on the registry service
func (rs *RegistryServer) RegisterService(rpcServer *grpc.Server) {
	registry.RegisterRegistryServer(rpcServer, rs)
}

// GetNodeController returns the node controller info for given nodeID, error if not found
func (rs *RegistryServer) GetNodeController(nodeID string) (NodeInfo, error) {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()

	if node, ok := rs.nodeClients[nodeID]; ok {
		return *node, nil
	}

	return NodeInfo{}, fmt.Errorf("No node registered with id: %v", nodeID)
}

// ConnectToNodeController initiates a connection to controller running at nodeId
func (rs *RegistryServer) ConnectToNodeController(nodeID string) (*grpc.ClientConn, error) {
	nodeInfo, err := rs.GetNodeController(nodeID)
	if err != nil {
		return nil, err
	}

	klog.V(3).Infof("Connecting to node controller: %s", nodeInfo.Endpoint)

	return cdigrpc.Connect(nodeInfo.Endpoint, rs.clientTLSConfig)
}

// AddListener adds new listener to a registry server
func (rs *RegistryServer) AddListener(l RegistryListener) {
	rs.listeners[l] = struct{}{}
}

// RegisterController registers new controller
func (rs *RegistryServer) RegisterController(ctx context.Context, req *registry.RegisterControllerRequest) (*registry.RegisterControllerReply, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing NodeId parameter")
	}

	if req.GetEndpoint() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing endpoint address")
	}

	rs.rpcMutex.LockKey(req.NodeId)
	defer rs.rpcMutex.UnlockKey(req.NodeId)

	klog.V(3).Infof("Registering node: %s, endpoint: %s", req.NodeId, req.Endpoint)

	node := &NodeInfo{
		NodeID:   req.NodeId,
		Endpoint: req.Endpoint,
	}

	rs.mutex.Lock()
	n, found := rs.nodeClients[req.NodeId]
	if found {
		if n.Endpoint != req.Endpoint {
			found = false
		}
	}
	rs.nodeClients[req.NodeId] = node
	rs.mutex.Unlock()

	if !found {
		for l := range rs.listeners {
			if err := l.OnNodeAdded(ctx, node); err != nil {
				rs.mutex.Lock()
				delete(rs.nodeClients, req.NodeId)
				rs.mutex.Unlock()
				return nil, errors.Wrap(err, "failed to register node")
			}
		}
	}

	return &registry.RegisterControllerReply{}, nil
}

// UnregisterController unregisters controller
func (rs *RegistryServer) UnregisterController(ctx context.Context, req *registry.UnregisterControllerRequest) (*registry.UnregisterControllerReply, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing NodeId parameter")
	}

	rs.rpcMutex.LockKey(req.NodeId)
	defer rs.rpcMutex.UnlockKey(req.NodeId)

	rs.mutex.Lock()
	node, ok := rs.nodeClients[req.NodeId]
	delete(rs.nodeClients, req.NodeId)
	rs.mutex.Unlock()

	if ok {
		for l := range rs.listeners {
			l.OnNodeDeleted(ctx, node)
		}
		klog.V(3).Infof("Unregistered node: %s", req.NodeId)
	} else {
		klog.V(3).Infof("No node registered with id '%s'", req.NodeId)
	}

	return &registry.UnregisterControllerReply{}, nil
}

// NodeClients returns a new map which contains a copy of all currently known node clients.
// It is safe to use concurrently with the other methods.
func (rs *RegistryServer) NodeClients() map[string]*NodeInfo {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()

	copy := map[string]*NodeInfo{}
	for key, value := range rs.nodeClients {
		copy[key] = value
	}
	return copy
}
