package dht

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"./bridge"
	// "github.com/hyperledger/fabric/orderer/consensus/dht/bridge"
	"google.golang.org/grpc"
)

var (
	// emptyNode                = &bridge.Node{}
	emptyStatus      = &bridge.Status{}
	emptyConfig      = &bridge.Config{}
	emptyEnvelope    = &bridge.Envelope{}
	emptyMsg         = &bridge.Msg{}
	emptyBlockHeader = &bridge.BlockHeader{}
	emptyBlockData   = &bridge.BlockData{}
	emptyMetadata    = &bridge.BlockMetadata{}
	emptyBlock       = &bridge.Block{}
)

func Dial(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial(addr, opts...)
}

type Transport interface {
	Start() error
	Stop() error

	//RPC
	// GetSuccessor(*bridge.Node) (*bridge.Node, error)
	LoadConfig(*bridge.Status) (*bridge.Config, error)
	TransBlock(tx context.Context, block *bridge.Block) error
	TransMsgClient(msg *bridge.Msg) error
}

type GrpcTransport struct {
	config *TransportConfig // node0地址等信息
	*bridge.UnimplementedBridgeServer
	timeout time.Duration
	maxIdle time.Duration

	sock *net.TCPListener

	pool    map[string]*grpcConn
	poolMtx sync.RWMutex

	server *grpc.Server

	shutdown int32
}

func NewGrpcTransport(config *TransportConfig) (*GrpcTransport, error) {

	addr := config.Addr
	// Try to start the listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	pool := make(map[string]*grpcConn)

	// Setup the transport
	grp := &GrpcTransport{
		sock:    listener.(*net.TCPListener),
		timeout: config.Timeout,
		maxIdle: config.MaxIdle,
		pool:    pool,
		config:  config,
	}

	grp.server = grpc.NewServer(config.ServerOpts...)

	// Done
	return grp, nil
}

type grpcConn struct {
	addr       string
	client     bridge.BridgeClient
	conn       *grpc.ClientConn
	lastActive time.Time
}

func (g *grpcConn) Close() {
	g.conn.Close()
}

func (g *GrpcTransport) registerChain(chain *chain) {
	bridge.RegisterBridgeServer(g.server, chain)
}

func (g *GrpcTransport) GetServer() *grpc.Server {
	return g.server
}

// Gets an outbound connection to a host
func (g *GrpcTransport) getConn(
	addr string,
) (bridge.BridgeClient, error) {

	g.poolMtx.RLock()

	if atomic.LoadInt32(&g.shutdown) == 1 {
		g.poolMtx.Unlock()
		return nil, fmt.Errorf("TCP transport is shutdown")
	}

	cc, ok := g.pool[addr]
	g.poolMtx.RUnlock()
	if ok {
		return cc.client, nil
	}

	var conn *grpc.ClientConn
	var err error
	conn, err = Dial(addr, g.config.DialOpts...)
	if err != nil {
		return nil, err
	}

	client := bridge.NewBridgeClient(conn)
	cc = &grpcConn{addr, client, conn, time.Now()}
	g.poolMtx.Lock()
	if g.pool == nil {
		g.poolMtx.Unlock()
		return nil, errors.New("must instantiate node before using")
	}
	g.pool[addr] = cc
	g.poolMtx.Unlock()

	return client, nil
}

func (g *GrpcTransport) Start() error {
	// Start RPC server
	go g.listen()

	// Reap old connections
	go g.reapOld()

	return nil

}

// Returns an outbound TCP connection to the pool
func (g *GrpcTransport) returnConn(o *grpcConn) {
	// Update the last asctive time
	o.lastActive = time.Now()

	// Push back into the pool
	g.poolMtx.Lock()
	defer g.poolMtx.Unlock()
	if atomic.LoadInt32(&g.shutdown) == 1 {
		o.conn.Close()
		return
	}
	g.pool[o.addr] = o
}

// Shutdown the TCP transport
func (g *GrpcTransport) Stop() error {
	atomic.StoreInt32(&g.shutdown, 1)

	// Close all the connections
	g.poolMtx.Lock()

	g.server.Stop()
	for _, conn := range g.pool {
		conn.Close()
	}
	g.pool = nil

	g.poolMtx.Unlock()

	return nil
}

// Closes old outbound connections
func (g *GrpcTransport) reapOld() {
	ticker := time.NewTicker(60 * time.Second)

	for {
		if atomic.LoadInt32(&g.shutdown) == 1 {
			return
		}
		select {
		case <-ticker.C:
			g.reap()
		}

	}
}

func (g *GrpcTransport) reap() {
	g.poolMtx.Lock()
	defer g.poolMtx.Unlock()
	for host, conn := range g.pool {
		if time.Since(conn.lastActive) > g.maxIdle {
			conn.Close()
			delete(g.pool, host)
		}
	}
}

// Listens for inbound connections
func (g *GrpcTransport) listen() {
	g.server.Serve(g.sock)
}

// rpc TransMsg(Msg) returns (Status){};
// rpc TransBlock(Block) returns (Status){};
// // dht调用，orderer实现
// rpc LoadConfig(Status) returns (Config){};

// server端
func (c *GrpcTransport) LoadConfig(ctx context.Context, s *bridge.Status) (*bridge.Config, error) {
	s := emptyStatus
	cnf = emptyConfig
	var err error
	//加载配置参数！！！

	return cnf, s
}

func (g *GrpcTransport) TransBlock(tx context.Context, block *bridge.Block) error {
	s := emptyStatus
	var err error
	// 把收到的block送入channel。在dht.go里面从channel取出进行writeblock！！！
	return err
}

// client端
func (g *GrpcTransport) TransMsgClient(msg *bridge.Msg) error {
	client, err := g.getConn(g.config.Addr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()
	_, err = client.TransMsg(ctx, msg)
	return err
}
