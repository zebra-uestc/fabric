/*
	以dht为模板。
	Order()以及Configure()等主要逻辑已经实现
	未实现orderer与dht节点的通信（用###标出），标！！！处需要验证是否能跑通
*/

package dht

import (
	"fmt"
	"sync"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/dht/bridge"
	"github.com/hyperledger/fabric/protoutil"

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
	*bridge.UnimplementedBridgeServer

	support  consensus.ConsenterSupport
	sendChan chan *message
	exitChan chan struct{}

	cnf       *TransportConfig // 存储node0的地址
	transport Transport        // LoadConfig函数实现以及grpc封装函数
	tsMtx     sync.RWMutex
}

type message struct {
	configSeq uint64
	normalMsg *cb.Envelope
	configMsg *cb.Envelope
}

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
		support:  support,
		sendChan: make(chan *message),
		exitChan: make(chan struct{}),
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
	case ch.sendChan <- &message{
		configSeq: configSeq,
		normalMsg: env,
	}:
		return nil
	case <-ch.exitChan:
		return fmt.Errorf("Exiting")
	}
}

// Configure accepts configuration update messages for ordering
func (ch *chain) Configure(config *cb.Envelope, configSeq uint64) error {
	select {
	case ch.sendChan <- &message{
		configSeq: configSeq,
		configMsg: config,
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
	go func() {
		cnf := DefaultTransportConfig()
		transport, err := NewGrpcTransport(cnf)
		if err != nil {
			return
		}

		ch.transport = transport // 为什么会报错！！！这里和node.go:124行完全一样

		bridge.RegisterBridgeServer(transport.server, ch)

		bridge.transport.Start()
	}()

	// 把message发送给dht
	go func() {
		for {
			seq := ch.support.Sequence()
			err = nil
			select {
			case msg := <-ch.sendChan:
				if msg.configMsg == nil {
					// NormalMsg
					if msg.configSeq < seq {
						_, err = ch.support.ProcessNormalMsg(msg.normalMsg)
						if err != nil {
							logger.Warningf("Discarding bad normal message: %s", err)
							continue
						}
					}
					// 发送msg###
					ch.transport.TransMsgClient(msg)

				} else {
					// ConfigMsg
					if msg.configSeq < seq {
						msg.configMsg, _, err = ch.support.ProcessConfigMsg(msg.configMsg)
						if err != nil {
							logger.Warningf("Discarding bad config message: %s", err)
							continue
						}
					}
					// 发送msg###
					ch.transport.TransMsgClient(msg)
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

			// 从node0处获得block###

			// write block！！！
			// multichannel/blockwriter.go/WriteConfigBlock
			msg, _ := protoutil.ExtractEnvelope(block, 0)
			// broadcast/broadcast.go/ProcessMessage
			// 想办法代替这个函数，或者找到这个函数的实现并import！！！
			_, isConfig, _, _ := ch.SupportRegistrar.BroadcastChannelSupport(msg)
			if !isConfig {
				ch.support.WriteBlock(block, nil)
			} else {
				ch.support.WriteConfigBlock(block, nil)
			}
			return
		}
	}()
}
