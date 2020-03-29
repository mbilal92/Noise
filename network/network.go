package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mbilal92/noise"
	"github.com/mbilal92/noise/kademlia"
	"github.com/mbilal92/noise/payload"
	"go.uber.org/zap"
	// "github.com/mbilal92/noise/broadcast"
	// "github.com/mbilal92/noise/relay"
)

const (
	// DefaultBootstrapTimeout is the default timeout for bootstrapping with each peer.
	DefaultBootstrapTimeout = time.Second * 10
	// DefaultPeerThreshold is the default threshold above which bootstrapping is considered successful
	DefaultPeerThreshold = 8
)

// Network encapsulates the communication in a noise p2p network.
type Network struct {
	node    *noise.Node
	overlay *kademlia.Protocol
	// relayChan     chan relay.Message
	// broadcastChan chan broadcast.Message
}

type Message struct {
	From   noise.ID
	Code   byte // 0 for relay, 1 for broadcast
	Data   []byte
	SeqNum byte
}

func (msg Message) Marshal() []byte {
	writer := payload.NewWriter(nil)
	writer.Write(msg.From.Marshal())
	writer.WriteByte(msg.Code)
	writer.WriteByte(msg.SeqNum)
	writer.WriteUint32(uint32(len(msg.Data)))
	writer.Write(msg.Data)
	return writer.Bytes()
}

func (m Message) String() string {
	return m.From.String() + " Code: " + strconv.Itoa(int(m.Code)) + " SeqNum: " + strconv.Itoa(int(m.SeqNum)) + " msg: " + string(m.Data)
}

func unmarshalChatMessage(buf []byte) (Message, error) {
	msg := Message{}
	msg.From, _ = noise.UnmarshalID(buf)

	buf = buf[msg.From.Size():]
	reader := payload.NewReader(buf)
	code, err := reader.ReadByte()
	if err != nil {
		panic(err)
	}
	msg.Code = code

	seqNum, err := reader.ReadByte()
	if err != nil {
		panic(err)
	}
	msg.SeqNum = seqNum

	data, err := reader.ReadBytes()
	if err != nil {
		panic(err)
	}
	msg.Data = data
	return msg, nil
}

// //HandlePeerDisconnection registers the dinconnection callback with the interface
// func (ntw *Network) HandlePeerDisconnection(peerCleanup disconCleanInterface) {

// 	node := ntw.node

// 	node.OnPeerDisconnected(func(node *noise.Node, peer *noise.Peer) error {
// 		fmt.Println("PEER HAS BEEN DISCONNECTED: ", (protocol.PeerID(peer).(kad.ID)).Address())
// 		// fmt.Println(kad.Table(node).GetPeers())
// 		kad.Table(node).Delete(protocol.PeerID(peer)) //Deleting it from the table where it is used to send messages e.g broadcasting
// 		// fmt.Println("After delete: ", kad.Table(node).GetPeers())
// 		peerCleanup.OnDisconnectCleanup(protocol.PeerID(peer).(kad.ID))
// 		return nil
// 	})

// }

// check panics if err is not nil.
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// printedLength is the total prefix length of a public key associated to a chat users ID.
const printedLength = 8

// New creates and returns a new network instance.
func New(host net.IP, port uint16, privatekey noise.PrivateKey, logger *zap.Logger) (*Network, error) {
	// Set up node and policy.

	node, err := noise.NewNode(
		noise.WithNodeBindHost(host), //net.ParseIP(host)),
		noise.WithNodeBindPort(port),
		noise.WithNodeLogger(logger),
		noise.WithNodePrivateKey(privatekey),
		// noise.WithNodeAddress(*addressFlag),
	)

	check(err)
	// r := relay.New()
	// relayChan := r.GetRelayChan()
	// bc := broadcast.New()
	// broadcastChan := bc.GetBroadcastChan()
	node.RegisterMessage(Message{}, unmarshalChatMessage)
	node.Handle(handle)
	events := kademlia.Events{
		OnPeerAdmitted: func(id noise.ID) {
			fmt.Printf("Learned about a new peer %s(%s).\n", id.Address, id.ID.String()[:printedLength])
		},
		OnPeerEvicted: func(id noise.ID) {
			fmt.Printf("Forgotten a peer %s(%s).\n", id.Address, id.ID.String()[:printedLength])
		},
	}

	overlay := kademlia.New(kademlia.WithProtocolEvents(events))

	// Bind Kademlia to the node.
	node.Bind(overlay.Protocol())

	go node.Listen()
	return &Network{
		node:    node,
		overlay: overlay,
		// relayChan:     relayChan,
		// broadcastChan: broadcastChan,
	}, nil
}

func handle(ctx noise.HandlerContext) error {
	obj, err := ctx.DecodeMessage()
	if err != nil {
		return nil
	}

	msg, ok := obj.(Message)
	if !ok {
		return nil
	}

	if len(msg.Data) == 0 {
		return nil
	}

	fmt.Printf("%s(%s)> %s\n", ctx.ID().Address, ctx.ID().ID.String()[:printedLength], msg.String())

	if ctx.IsRequest() {
		msg2 := Message{}
		msg2.From = ctx.ID()
		msg2.Data = []byte("GOT: " + string(msg.Data))
		msg2.Code = 2
		msg2.SeqNum = 1
		return ctx.SendMessage(msg2)
	}

	return nil
}

// Bootstrap bootstraps a network using a list of peer addresses and returns whether bootstrap finished before timeout.
func (ntw *Network) Bootstrap(peerAddrs []string, timeout time.Duration, peerThreshold int) bool {
	if len(peerAddrs) == 0 {
		return false
	}

	for _, addr := range peerAddrs {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err := ntw.node.Ping(ctx, addr)
		cancel()

		if err != nil {
			fmt.Printf("Failed to ping bootstrap node (%s). Skipping... [error: %s]\n", addr, err)
			continue
		}

		if ntw.GetNumPeers() >= peerThreshold {
			return true
		}

	}

	if ntw.GetNumPeers() <= peerThreshold {
		ntw.Discover()
	}

	return true
}

func (ntw *Network) Discover() {
	ids := ntw.overlay.Discover()

	var str []string
	for _, id := range ids {
		str = append(str, fmt.Sprintf("%s(%s)", id.Address, id.ID.String()[:printedLength]))
	}

	if len(ids) > 0 {
		fmt.Printf("Discovered %d peer(s): [%v]\n", len(ids), strings.Join(str, ", "))
	} else {
		fmt.Printf("Did not discover any peers.\n")
	}
}

// peers prints out all peers we are already aware of.
func (ntw *Network) Peers() []noise.ID {
	ids := ntw.overlay.Table().Peers()

	// var str []string
	// for _, id := range ids {
	// 	str = append(str, fmt.Sprintf("%s(%s)", id.Address, id.ID.String()[:printedLength]))
	// }

	// fmt.Printf("I know %d peer(s): [%v]\n", len(ids), strings.Join(str, ", "))
	return ids
}

// BootstrapDefault runs Bootstrap with default parameters.
func (ntw *Network) BootstrapDefault(peerAddrs []string) bool {
	return ntw.Bootstrap(peerAddrs, DefaultBootstrapTimeout, DefaultPeerThreshold)
}

// GetRelayChan returns the channel for relay messages.
// func (ntw *Network) GetRelayChan() chan relay.Message {
// 	return ntw.relayChan
// }

// // GetBroadcastChan returns the channel for broadcast messages.
// func (ntw *Network) GetBroadcastChan() chan broadcast.Message {
// 	return ntw.broadcastChan
// }

// GetNodeID returns the network node's skademlia ID.
func (ntw *Network) GetNodeID() noise.ID {
	return ntw.node.ID()
}

//AddressFromPK returns the address associated with the public key
func (ntw *Network) AddressFromPK(publicKey []byte) string {
	return ntw.overlay.Table().AddressFromPK(publicKey)
}

// GetNumPeers returns the number of peers the network node has.
func (ntw *Network) GetNumPeers() int {
	return len(ntw.overlay.Table().Peers())
}

// Relay relays data to peer with given ID.
// func (ntw *Network) Relay(peerID noise.ID, code byte, data []byte) error {
// 	nodeID := ntw.GetNodeID()
// 	nodeAddr := nodeID.Address()
// 	peerAddr := peerID.Address()
// 	msg := relay.NewMessage(nodeID, peerID, code, data)
// 	err := relay.ToPeer(ntw.node, msg, false, true)
// 	if err != nil {
// 		log.Warn().Msgf("%v to %v relay failed without lookup: %v", nodeAddr, peerAddr, err)
// 		err = relay.ToPeer(ntw.node, msg, true, true)
// 	}
// 	return err
// }

// // Broadcast broadcasts data to the entire p2p network.
// func (ntw *Network) Broadcast(code byte, data []byte) {
// 	minBucketID := 0
// 	maxBucketID := kad.Table(ntw.node).GetNumOfBuckets() - 1
// 	broadcast.Send(ntw.node, ntw.GetNodeID(), code, data, minBucketID, maxBucketID, 0, true)
// }

func (ntw *Network) Close() {
	ntw.node.Close()
}

func (ntw *Network) Node() *noise.Node {
	return ntw.node
}

// func (ntw *Network) RequestMsgToPeers() {
// 	for _, id := range ntw.overlay.Table().Peers() {
// 		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
// 		msg := Message{}
// 		msg.From = node.ID()
// 		msg.SeqNum = byte(int(0))
// 		msg.Code = byte(int(1))
// 		msg.Data = []byte(line)
// 		msg2, err := node.RequestMessage(ctx, id.Address, msg)
// 		if err != nil {
// 			fmt.Printf("Failed to send message to %s(%s). Skipping... [error: %s]\n",
// 				id.Address,
// 				id.ID.String()[:printedLength],
// 				err,
// 			)
// 			continue
// 		} else {
// 			fmt.Printf("GOR RESPONSE for Request %v", msg2.(Message).String())
// 		}
// 		cancel()
// 	}
// }
