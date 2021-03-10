package dht

import (
	"context"

	"log"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/zebra-uestc/chord/models/bridge"
	"google.golang.org/grpc"

	cb "github.com/hyperledger/fabric-protos-go/common"
)

// service BlockTranser{
//     // 由发送方调用函数，接收方实现函数
//     rpc TransBlock(Block) returns (DhtStatus){};
//     rpc LoadConfig(DhtStatus) returns (Block){};
// }

// service MsgTranser{
//     rpc TransMsg(Msg) returns (DhtStatus){};
// }

// server端 TODO
func (ch *chain) LoadConfig(ctx context.Context, s *bridge.DhtStatus) (*bridge.Config, error) {
	// var err error
	// //加载配置参数！！！
	// 加载创世区块的hash
	//var genesisblock *bridge.Block

	return nil, nil
}

func (ch *chain) TransBlock(tx context.Context, blockByte *bridge.BlockBytes) (*bridge.DhtStatus, error) {
	var s *bridge.DhtStatus
	var err error
	var block *cb.Block
	err = proto.Unmarshal(blockByte.BlockPayload,block)
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
