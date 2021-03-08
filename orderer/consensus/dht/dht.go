/*
	以dht为模板。
	Order()以及Configure()等主要逻辑已经实现
	未实现orderer与dht节点的通信（用###标出），标！！！处需要验证是否能跑通
*/

package dht

import (
	"fmt"
	"log"
	"net"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"

	mc "github.com/hyperledger/fabric/orderer/common/multichannel"
	"github.com/hyperledger/fabric/orderer/consensus"

	// "github.com/hyperledger/fabric/orderer/consensus/dht/bridge"
	"github.com/hyperledger/fabric/protoutil"

	"./bridge"

	"google.golang.org/grpc"
)

func DefaultTransportConfig() *TransportConfig {
	n := &TransportConfig{
		DialOpts: make([]grpc.DialOption, 0, 5),
	}

	n.DialOpts = append(n.DialOpts,
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
		grpc.FailOnNonTempDialError(true),
		grpc.WithInsecure(),
	)
	return n
}

type TransportConfig struct {
	Id   string // node0的id
	Addr string // node0

	ServerOpts []grpc.ServerOption
	DialOpts   []grpc.DialOption

	Timeout time.Duration
	MaxIdle time.Duration
}

var logger = flogging.MustGetLogger("orderer.consensus.dht")

type consenter struct{}

type chain struct {
	*bridge.UnimplementedBlockTranserServer
	// *bridge.UnimplementedMsgTranserServer

	support consensus.ConsenterSupport

	sendChan    chan *bridge.Msg
	receiveChan chan *bridge.Block
	exitChan    chan struct{}

	cnf *TransportConfig // 存储node0的地址

}

// type message struct {
// 	configSeq uint64
// 	normalMsg *cb.Envelope
// 	configMsg *cb.Envelope
// }

// New creates a new consenter for the dht consensus scheme.
// The dht consensus scheme is very simple, and allows only one consenter for a given chain (this process).
// It accepts messages being delivered via Order/Configure, orders them, and then uses the blockcutter to form the messages
// into blocks before writing to the given ledger
func New() consensus.Consenter {
	return &consenter{}
}

func (dht *consenter) HandleChain(support consensus.ConsenterSupport, metadata *cb.Metadata) (consensus.Chain, error) {
	logger.Warningf("Use of the Solo orderer is deprecated and remains only for use in test environments but may be removed in the future.")
	return newChain(support), nil
}

func newChain(support consensus.ConsenterSupport) *chain {
	return &chain{
		support:     support,
		sendChan:    make(chan *bridge.Msg),
		receiveChan: make(chan *bridge.Block),
		exitChan:    make(chan struct{}),
	}
}

// 外部函数自动调用Start()
func (ch *chain) Start() {
	go ch.main()
}

func (ch *chain) Halt() {
	select {
	case <-ch.exitChan:
		// Allow multiple halts without panic
	default:
		close(ch.exitChan)
	}
}

func (ch *chain) WaitReady() error {
	return nil
}

// Order accepts normal messages for ordering
func (ch *chain) Order(env *cb.Envelope, configSeq uint64) error {
	select {
	case ch.sendChan <- &bridge.Msg{
		ConfigSeq: configSeq,
		NormalMsg: env,
	}:
		return nil
	case <-ch.exitChan:
		return fmt.Errorf("Exiting")
	}
}

// Configure accepts configuration update messages for ordering
func (ch *chain) Configure(config *cb.Envelope, configSeq uint64) error {
	select {
	case ch.sendChan <- &bridge.Msg{
		ConfigSeq: configSeq,
		ConfigMsg: cfg,
	}:
		return nil
	case <-ch.exitChan:
		return fmt.Errorf("Exiting")
	}
}

// Errored only closes on exit
func (ch *chain) Errored() <-chan struct{} {
	return ch.exitChan
}

func (ch *chain) main() {
	// var timer <-chan time.Time
	var err error
	// Start RPC server

	// 把message发送给dht
	go func() {
		for {
			seq := ch.support.Sequence()
			err = nil
			select {
			case msg := <-ch.sendChan:
				if msg.ConfigMsg == nil {
					// NormalMsg
					if msg.ConfigSeq < seq {
						_, err = ch.support.ProcessNormalMsg(msg.NormalMsg)
						if err != nil {
							logger.Warningf("Discarding bad normal message: %s", err)
							continue
						}
					}
					// 发送msg###
					ch.TransMsgClient(msg)

				} else {
					// ConfigMsg
					if msg.ConfigSeq < seq {
						msg.ConfigMsg, _, err = ch.support.ProcessConfigMsg(msg.ConfigMsg)
						if err != nil {
							logger.Warningf("Discarding bad config message: %s", err)
							continue
						}
					}
					// 发送msg###
					ch.TransMsgClient(msg)
				}
			case <-ch.exitChan:
				logger.Debugf("Exiting")
				return
			}
		}
	}()

	// 把从dht接受的block写入账本
	go func() {
		for {
			// 从node0处获得block
			block := <-ch.receiveChan
			// write block
			// multichannel/blockwriter.go/WriteConfigBlock
			msg, _ := protoutil.ExtractEnvelope(block, 0)
			// broadcast/broadcast.go/ProcessMessage
			// 想办法代替这个函数，或者找到这个函数的实现并import！！！
			// multichannel/registerar.go
			// 返回消息的通道头，检查是否是配置交易BroadcastChannelSupport(msg *cb.Envelope)
			_, isConfig, _, _ := ch.IsConfig(msg)
			if !isConfig {
				ch.support.WriteBlock(block, nil)
			} else {
				ch.support.WriteConfigBlock(block, nil)
			}
			return
		}
	}()
}

func (ch *chain) StartTransBlockServer(address string) {
	go ch.startTransBlockServer(address)
}

func (ch *chain) startTransBlockServer(address string) {
	println("TransBlockServer listen:", address)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal("failed to listen: ", err)
	}
	s := grpc.NewServer()
	bridge.RegisterBlockTranserServer(s, ch)
	println("TransBlockServer start serve")
	if err := s.Serve(lis); err != nil {
		log.Fatal("fail to  serve:", err)
	}
	println("TransBlockServer serve end")
}

func (ch *chain) IsConfig(msg *cb.Envelope) bool {
	_, isConfig, _, _ := mc.BroadcastChannelSupport(msg)
	return isConfig
}
