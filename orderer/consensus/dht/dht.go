/*
	以dht为模板。
	Order()以及Configure()等主要逻辑已经实现
	未实现orderer与dht节点的通信（用###标出），标！！！处需要验证是否能跑通
*/

package dht

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"

	"github.com/hyperledger/fabric/orderer/common/msgprocessor"
	"github.com/hyperledger/fabric/orderer/consensus"

	"github.com/hyperledger/fabric/protoutil"
	"github.com/zebra-uestc/chord/models/bridge"

	"google.golang.org/grpc"
	"github.com/zebra-uestc/chord/config"
)

type TransportConfig struct {
	Addr string // node0

	DialOpts []grpc.DialOption
	Timeout  time.Duration // 作为WithTimeout()函数参数
	MaxIdle  time.Duration // 超过maxidle自动关闭连接

	pool    map[string]*grpcConn
	poolMtx sync.RWMutex

	shutdown int32 // 关闭所有连接的标志位
}

var logger = flogging.MustGetLogger("orderer.consensus.dht")

type consenter struct{}

type chain struct {
	*bridge.UnimplementedBlockTranserServer

	support consensus.ConsenterSupport

	sendChan    chan *message
	sendMsgChan chan *bridge.MsgBytes
	receiveChan chan *cb.Block
	exitChan    chan struct{}

	cnf *TransportConfig // 存储node0的地址
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
	return NewChain(support), nil
}

func (dht *consenter) JoinChain(support consensus.ConsenterSupport, joinBlock *cb.Block) (consensus.Chain, error) {
	return nil, errors.New("the Solo orderer does not support JoinChain")
}

func NewChain(support consensus.ConsenterSupport) *chain {
	return &chain{
		support:     support,
		sendChan:    make(chan *message, 10),
		sendMsgChan: make(chan *bridge.MsgBytes, 10),
		receiveChan: make(chan *cb.Block, 10),
		exitChan:    make(chan struct{}),
		cnf:         &TransportConfig{
						Addr: config.MainNodeAddressMsg,
						DialOpts: []grpc.DialOption{
							grpc.WithBlock(),
							grpc.FailOnNonTempDialError(true),
							grpc.WithInsecure(),
						},
						Timeout:  config.GrpcTimeout,
						MaxIdle:  100 * config.GrpcTimeout,
						pool:    make(map[string]*grpcConn),
					},
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
	// e, _ := protoutil.Marshal(env)
	// var tmp cb.Envelope
	// protoutil.UnmarshalEnvelopeOfType(e,_, tmp)
	// go func() error {
	// 	select {
	// 	case ch.sendChan <- &message{
	// 		configSeq: configSeq,
	// 		normalMsg: env,
	// 	}:
	// 		return nil
	// 	case <-ch.exitChan:
	// 		return fmt.Errorf("Exiting")
	// 	}
	// }()
	// return nil
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
	ch.StartTransBlockServer(config.OrdererAddress)
	count := 0;
	// 把message发送给dhtto
	go func() {
		for {
			seq := ch.support.Sequence()
			err = nil
			select {
			case msg, ok := <-ch.sendChan:
				//初始化执行一次
				if !ok {
					println("channel sendChan is closed!")
				}
				if msg.configMsg == nil {
					// NormalMsg
					if msg.configSeq < seq {
						_, err = ch.support.ProcessNormalMsg(msg.normalMsg)
						if err != nil {
							logger.Warningf("Discarding bad normal message: %s", err)
							continue
						}
					}
					// 发送msg
					mc, _ := protoutil.Marshal(msg.configMsg)
					mn, _ := protoutil.Marshal(msg.normalMsg)
					ch.sendMsgChan <- &bridge.MsgBytes{
						ConfigSeq: msg.configSeq,
						ConfigMsg: mc,
						NormalMsg: mn,
					}

				} else {
					// ConfigMsg
					if msg.configSeq < seq {
						msg.configMsg, _, err = ch.support.ProcessConfigMsg(msg.configMsg)
						if err != nil {
							logger.Warningf("Discarding bad config message: %s", err)
							continue
						}
					}
					// 发送msg
					mc, _ := protoutil.Marshal(msg.configMsg)
					mn, _ := protoutil.Marshal(msg.normalMsg)
					ch.sendMsgChan <- &bridge.MsgBytes{
						ConfigSeq: msg.configSeq,
						ConfigMsg: mc,
						NormalMsg: mn,
					}
				}
				count = count +1;
				if count == cap(ch.sendMsgChan){
					go ch.TransMsgClient()
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
			block, ok := <-ch.receiveChan
			if !ok {
				println("channel receiveChan is closed!")
			}
			// write block
			// multichannel/blockwriter.go/WriteConfigBlock
			// msg, _ := protoutil.ExtractEnvelope(block, 0)
			// broadcast/broadcast.go/ProcessMessage
			// 想办法代替这个函数，或者找到这个函数的实现并import
			// multichannel/registerar.go
			// 返回消息的通道头，检查是否是配置交易BroadcastChannelSupport(msg *cb.Envelope)
			// isConfig := ch.IsConfig(msg)
			// if !isConfig {
			ch.support.WriteBlock(block, nil)
			// } else {
			// 	ch.support.WriteConfigBlock(block, nil)
			// }
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

// 实现方法参考broadcast.go/ProcessMessage()中调用的BroadcastChannelSupport(msg *cb.Envelope)函数（multichannel/register.go/Register的方法）,但能否得到预期结果存疑 TODO
func (ch *chain) IsConfig(msg *cb.Envelope) bool {
	chdr, _ := protoutil.ChannelHeader(msg)

	isConfig := false
	switch ch.support.ClassifyMsg(chdr) {
	case msgprocessor.ConfigUpdateMsg:
		isConfig = true
	case msgprocessor.ConfigMsg:
		isConfig = false
	default:
	}
	return isConfig
}
