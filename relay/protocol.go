// Package gossip is a simple implementation of a gossip protocol for noise. It keeps track of a cache of messages
// sent/received to/from peers to avoid re-gossiping particular messages to specific peers.
package relay

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/mbilal92/noise"
	"github.com/mbilal92/noise/kademlia"
)

const (
	DefaultPeerToSendRelay = 16
	broadcastChanSize      = 64 // default broadcast channel buffer size
)

// Protocol implements a simple gossiping protocol that avoids resending messages to peers that it already believes
// is aware of particular messages that are being gossiped.
type Protocol struct {
	node           *noise.Node
	overlay        *kademlia.Protocol
	events         Events
	relayChan      chan Message
	msgSentCounter uint32
	Logging        bool
	seen           *fastcache.Cache
}

// New returns a new instance of a gossip protocol with 32MB of in-memory cache instantiated.
func New(overlay *kademlia.Protocol, log bool, opts ...Option) *Protocol {
	p := &Protocol{
		overlay:        overlay,
		seen:           fastcache.New(32 << 20),
		relayChan:      make(chan Message, broadcastChanSize),
		msgSentCounter: 0,
		Logging:        log,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Protocol returns a noise.Protocol that may registered to a node via (*noise.Node).Bind.
func (p *Protocol) Protocol() noise.Protocol {
	return noise.Protocol{
		VersionMajor: 0,
		VersionMinor: 0,
		VersionPatch: 0,
		Bind:         p.Bind,
	}
}

// Bind registers a single message gossip.Message, and handles them by registering the (*Protocol).Handle Handler.
func (p *Protocol) Bind(node *noise.Node) error {
	p.node = node

	node.RegisterMessage(Message{}, UnmarshalMessage)
	node.Handle(p.Handle)

	return nil
}

// Push gossips a single message concurrently to all peers this node is aware of, on the condition that this node
// believes that the aforementioned peer has not received data before. A context may be provided to cancel Push, as it
// blocks the current goroutine until the gossiping of a single message is done. Any errors pushing a message to a
// particular peer is ignored.
func (p *Protocol) Relay(ctx context.Context, msg Message, changeRandomN bool, Fromid noise.ID, chainID string) {
	// fmt.Println("Relay 1")
	if changeRandomN {
		msg.randomN = p.msgSentCounter
		atomic.AddUint32(&p.msgSentCounter, 1)
		if p.Logging {
			fmt.Printf("Sending Relay Msg at Node %v - %v\n", p.node.Addr(), msg.String())
		}
	}
	msg.ChainID = chainID
	data := msg.Marshal()
	dataHash := p.hash(data)
	if !p.seen.Has(dataHash) {
		p.seen.SetBig(dataHash, nil)
	}

	localPeerAddress := p.overlay.Table().AddressFromPK(msg.To)
	if localPeerAddress != "" {
		if err := p.node.SendMessage(ctx, localPeerAddress, msg); err != nil {
			// fmt.Printf("Relay send msg Fucked %v\n", err)
		}
		return
	}

	// peers := p.overlay.Find(msg.To)
	peers := p.overlay.Table().FindClosest(msg.To, DefaultPeerToSendRelay)

	// fmt.Printf("Relay Peers %v\n", peers)
	// var wg sync.WaitGroup
	// wg.Add(len(peers))

	for _, id := range peers {
		id, key := id, dataHash
		// key := p.hash(id, data)
		if Fromid.ID.String() == id.ID.String() {
			continue
		}

		go func() {
			// defer wg.Done()

			if p.seen.Has(key) {
				// fmt.Printf("Relay ID %v Alread seen Msg %v\n", id, hex.EncodeToString(key))
				return
			}

			// fmt.Printf("Relay ID %v hash %v\n", id, hex.EncodeToString(key))
			if err := p.node.SendMessage(ctx, id.Address, msg); err != nil {
				// fmt.Printf("Relay send msg Fucked %v\n", err)
				return
			}

			p.seen.SetBig(key, nil)
		}()
	}

	// wg.Wait()
}

// Handle implements noise.Protocol and handles gossip.Message messages.
func (p *Protocol) Handle(ctx noise.HandlerContext) error {
	// fmt.Printf("Relay Handle Enter, ctx.ID %v msg: %v\n", ctx.ID(), ctx.Message().String())
	if ctx.IsRequest() {
		return nil
	}

	obj, err := ctx.DecodeMessage()
	if err != nil {
		return nil
	}

	msg, ok := obj.(Message)
	if !ok {
		return nil
	}

	// fmt.Printf("Handle received msg %v\n", msg.String())
	data := msg.Marshal()
	dataHash := p.hash(data)
	if p.seen.Has(dataHash) {
		return nil
	}

	// p.seen.SetBig(p.hash(ctx.ID(), data), nil) // Mark that the sender already has this data.
	p.seen.SetBig(dataHash, nil) // Mark that the sender already has this data.
	// fmt.Printf("Seen Hash set in Handle  for ID %v and data %v  %v\n", ctx.ID(), hex.EncodeToString(p.hash(ctx.ID(), data)))

	// self := p.hash(p.node.ID(), data)

	// if p.seen.Has(self) {
	// 	return nil
	// }

	// p.seen.SetBig(self, nil) // Mark that we already have this data.
	if !p.checkChaidID(msg.ChainID) {
		return nil
	}
	if msg.To == p.node.ID().ID {
		if p.Logging {
			fmt.Printf("Relay Msg Received at Node %v From Peer %v - %v\n", p.node.Addr(), ctx.ID(), msg.String())
		}
		p.relayChan <- msg
	} else {
		// fmt.Println("Relay Handle Relaying further")
		go p.Relay(context.TODO(), msg, false, ctx.ID(), msg.ChainID)
	}

	// if p.events.OnGossipReceived != nil {
	// 	if err := p.events.OnGossipReceived(ctx.ID(), msg); err != nil {
	// 		return err
	// 	}
	// }

	return nil
}

// func (p *Protocol) hash(id noise.ID, data []byte) []byte {
func (p *Protocol) hash(data []byte) []byte {
	hasher := sha256.New()
	hasher.Write(data)
	return hasher.Sum(nil)
	// return append(id.ID[:], data...)
}

func (p *Protocol) GetRelayChan() chan Message {
	return p.relayChan
}

func (p *Protocol) checkChaidID(chainID string) bool {
	if p.node.ChainID == chainID {
		return true
	}

	if p.node.AcceptMsgFromParentChainID {
		myChaidSplit := strings.Split(p.node.ChainID, ".")
		incomingChaidSplit := strings.Split(chainID, ".")

		if len(incomingChaidSplit) == len(myChaidSplit)-1 {
			for i := 0; i < len(incomingChaidSplit); i++ {
				if incomingChaidSplit[i] != myChaidSplit[i] {
					return false
				}
			}
			return true
		}
	}
	return false
}
