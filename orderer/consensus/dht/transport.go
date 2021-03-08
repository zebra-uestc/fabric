package dht

import (
	"context"
	"log"
	"time"

	"./bridge"
	"google.golang.org/grpc"
	// "github.com/hyperledger/fabric/orderer/consensus/dht/bridge"
)

// service BlockTranser{
//     // 由发送方调用函数，接收方实现函数
//     rpc TransBlock(Block) returns (DhtStatus){};
//     rpc LoadConfig(DhtStatus) returns (Block){};
// }

// service MsgTranser{
//     rpc TransMsg(Msg) returns (DhtStatus){};
// }

// server端
func (ch *chain) LoadConfig(ctx context.Context, s *bridge.DhtStatus) (*bridge.Block, error) {
	var err error
	// //加载配置参数！！！
	// 加载创世区块的hash
	var genesisblock *bridge.Block

	return genesisblock, nil
}

func (ch *chain) TransBlock(tx context.Context, block *bridge.Block) (*bridge.DhtStatus, error) {
	var s *bridge.DhtStatus
	var err error
	// 把收到的block送入channel。在dht.go里面从channel取出进行writeblock
	ch.receiveChan <- block

	return s, err
}

// client端，不采用transport.go原本实现的接口
func (ch *chain) TransMsgClient(msg *bridge.Msg) error {
	conn, err := grpc.Dial(ch.cnf.Addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	client := bridge.NewMsgTranserClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.TransMsg(ctx, msg)

	if err != nil {
		log.Fatalf("could not transcation Msg: %v", err)
	}
	return err
}
