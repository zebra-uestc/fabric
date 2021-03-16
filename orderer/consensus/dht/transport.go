package dht

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"log"
	"time"

	// "github.com/gogo/protobuf/proto"
	"github.com/zebra-uestc/chord/models/bridge"
	"google.golang.org/grpc"

	// cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/ledger/blkstorage"
	"github.com/hyperledger/fabric/common/ledger/blockledger/fileledger"
	"github.com/hyperledger/fabric/orderer/common/multichannel"
	"github.com/hyperledger/fabric/protoutil"
)

// service BlockTranser{
//     // 由发送方调用函数，接收方实现函数
//     rpc TransBlock(Block) returns (DhtStatus){};
//     rpc LoadConfig(DhtStatus) returns (Block){};
// }

// service MsgTranser{
//     rpc TransMsg(Msg) returns (DhtStatus){};
// }

func Dial(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial(addr, opts...)
}

type grpcConn struct {
	addr       string
	client     bridge.MsgTranserClient
	conn       *grpc.ClientConn
	lastActive time.Time
}

// server端 TODO
func (ch *chain) LoadConfig(ctx context.Context, s *bridge.DhtStatus) (*bridge.Config, error) {
	// 加载配置参数
	//var genesisblock *bridge.Block
	if chainSupport, ok := ch.support.(*multichannel.ChainSupport); ok {
		if ledger, ok := chainSupport.ReadWriter.(*fileledger.FileLedger); ok {
			if blockstore, ok := ledger.BlockStore().(*blkstorage.BlockStore); ok {
				if bcInfo, err := blockstore.GetBlockchainInfo(); err == nil {
					return &bridge.Config{
						PrevBlockHash: bcInfo.CurrentBlockHash,
						LastBlockNum:  bcInfo.Height,
					}, nil
				}
			}
		}
	}
	println("failed get config")
	return &bridge.Config{}, nil
}

// func (ch *chain) TransBlock(tx context.Context, blockByte *bridge.BlockBytes) (*bridge.DhtStatus, error) {
// 	block, err := protoutil.UnmarshalBlock(blockByte.BlockPayload)
// 	// 把收到的block送入channel。在dht.go里面从channel取出进行writeblock
// 	// println("trans block1", block.Header.Number)
// 	ch.receiveChan <- block
// 	// println("trans block2", block.Header.Number)

// 	return &bridge.DhtStatus{}, err
// }

func (ch *chain) TransBlock(receiver bridge.BlockTranser_TransBlockServer) error {
	for {
		blockByte, err := receiver.Recv()
		if err == io.EOF {
			return receiver.SendAndClose(&bridge.DhtStatus{})
		}
		if err != nil {
			log.Fatalln("Receive Msg from orderer err:", err)
			return err
		}
		block, err := protoutil.UnmarshalBlock(blockByte.BlockPayload)
		ch.receiveChan <- block

	}

	return nil
}

// client端，不采用transport.go原本实现的接口
func (ch *chain) TransMsgClient() error {
	client, err := ch.getConn(ch.cnf.Addr)
	if err != nil {
		log.Fatalf("Can't connect: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ch.cnf.Timeout)
	defer cancel()
	sender, err := client.TransMsg(ctx)
	if err != nil {
		return err
	}

	for msg := range ch.sendMsgChan {
		sender.Send(msg)
	}

	_, err = sender.CloseAndRecv()
	if err != nil {
		log.Fatalf("could not transcation MsgBytes: %v", err)
	}

	return err
}

func (g *grpcConn) Close() {
	g.conn.Close()
}

func (g *chain) StartReap() error {
	// Start RPC server
	// go g.listen()

	// Reap old connections
	go g.reapOld()

	return nil

}

// Closes old outbound connections
func (g *chain) reapOld() {
	ticker := time.NewTicker(60 * time.Second)

	for {
		if atomic.LoadInt32(&g.cnf.shutdown) == 1 {
			return
		}
		select {
		case <-ticker.C:
			g.reap()
		}

	}
}

func (g *chain) reap() {
	g.cnf.poolMtx.Lock()
	defer g.cnf.poolMtx.Unlock()
	for host, conn := range g.cnf.pool {
		if time.Since(conn.lastActive) > g.cnf.MaxIdle {
			conn.Close()
			delete(g.cnf.pool, host)
		}
	}
}

// Gets an outbound connection to a host
func (g *chain) getConn(
	addr string,
) (bridge.MsgTranserClient, error) {

	g.cnf.poolMtx.RLock()

	if atomic.LoadInt32(&g.cnf.shutdown) == 1 {
		g.cnf.poolMtx.Unlock()
		return nil, fmt.Errorf("TCP transport is shutdown")
	}

	cc, ok := g.cnf.pool[addr]
	g.cnf.poolMtx.RUnlock()
	if ok {
		return cc.client, nil
	}

	var conn *grpc.ClientConn
	var err error
	conn, err = Dial(addr, g.cnf.DialOpts...)
	if err != nil {
		return nil, err
	}

	client := bridge.NewMsgTranserClient(conn)
	cc = &grpcConn{addr, client, conn, time.Now()}
	g.cnf.poolMtx.Lock()
	if g.cnf.pool == nil {
		g.cnf.poolMtx.Unlock()
		return nil, errors.New("must instantiate node before using")
	}
	g.cnf.pool[addr] = cc
	g.cnf.poolMtx.Unlock()

	return client, nil
}

// Close all outbound connection in the pool
func (g *chain) Stop() error {
	atomic.StoreInt32(&g.cnf.shutdown, 1)

	// Close all the connections
	g.cnf.poolMtx.Lock()

	// g.server.Stop()
	for _, conn := range g.cnf.pool {
		conn.Close()
	}
	g.cnf.pool = nil

	g.cnf.poolMtx.Unlock()

	return nil
}
