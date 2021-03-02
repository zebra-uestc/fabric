/*
	以solo为模板。
	Order()以及Configure()等主要逻辑已经实现
	未实现orderer与dht节点的通信（用###标出），标！！！处需要验证是否能跑通
*/

package dht

import (
	"fmt"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/orderer/consensus"
)

var logger = flogging.MustGetLogger("orderer.consensus.solo")

type consenter struct{}

type chain struct {
	support  consensus.ConsenterSupport
	sendChan chan *message
	exitChan chan struct{}
}

type message struct {
	configSeq uint64
	normalMsg *cb.Envelope
	configMsg *cb.Envelope
}

// New creates a new consenter for the solo consensus scheme.
// The solo consensus scheme is very simple, and allows only one consenter for a given chain (this process).
// It accepts messages being delivered via Order/Configure, orders them, and then uses the blockcutter to form the messages
// into blocks before writing to the given ledger
func New() consensus.Consenter {
	return &consenter{}
}

func (solo *consenter) HandleChain(support consensus.ConsenterSupport, metadata *cb.Metadata) (consensus.Chain, error) {
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
	// 把message发送给dht
	go func sendMsg(){
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
					
				}
			case <-ch.exitChan:
				logger.Debugf("Exiting")
				return
			}
		}
	}()

	// 把从dht接受的block写入账本
	go func commitBlock()error{
		for {
			// 从node0处获得block###

			// write block！！！
			// multichannel/blockwriter.go/WriteConfigBlock
			msg, err := protoutil.ExtractEnvelope(block, 0)
			// broadcast/broadcast.go/ProcessMessage
			chdr, isConfig, processor, err := bh.SupportRegistrar.BroadcastChannelSupport(msg)
			if !isConfig{
				ch.support.WriteBlock(block, nil)
			}else{
				ch.support.WriteConfigBlock(block, nil)
			}
			return err
		}
	}()
}

