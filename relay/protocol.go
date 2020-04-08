// Package gossip is a simple implementation of a gossip protocol for noise. It keeps track of a cache of messages
// sent/received to/from peers to avoid re-gossiping particular messages to specific peers.
package relay

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

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

	seen *fastcache.Cache
}

// New returns a new instance of a gossip protocol with 32MB of in-memory cache instantiated.
func New(overlay *kademlia.Protocol, opts ...Option) *Protocol {
	p := &Protocol{
		overlay:        overlay,
		seen:           fastcache.New(32 << 20),
		relayChan:      make(chan Message, broadcastChanSize),
		msgSentCounter: 0,
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
func (p *Protocol) Relay(ctx context.Context, msg Message, changeRandomN bool) {
	// fmt.Println("Relay 1")
	if changeRandomN {
		msg.randomN = p.msgSentCounter
		atomic.AddUint32(&p.msgSentCounter, 1)
	}

	data := msg.Marshal()
	p.seen.Set(p.hash(p.node.ID(), data), nil)

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
		id, key := id, p.hash(id, data)
		// key := p.hash(id, data)
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

			p.seen.Set(key, nil)
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

	time.Sleep(6)
	msg, ok := obj.(Message)
	if !ok {
		return nil
	}

	// fmt.Printf("Handle received msg %v\n", msg.String())
	data := msg.Marshal()
	p.seen.Set(p.hash(ctx.ID(), data), nil) // Mark that the sender already has this data.
	// fmt.Printf("Seen Hash set in Handle  for ID %v and data %v  %v\n", ctx.ID(), hex.EncodeToString(p.hash(ctx.ID(), data)))

	self := p.hash(p.node.ID(), data)

	if p.seen.Has(self) {
		return nil
	}

	p.seen.Set(self, nil) // Mark that we already have this data.

	if msg.To == p.node.ID().ID {
		fmt.Printf("Relay Msg Received at Node %v - %v\n", p.node.Addr(), msg.String())
		p.relayChan <- msg
	} else {
		// fmt.Println("Relay Handle Relaying further")
		go p.Relay(context.TODO(), msg, false)
	}

	// if p.events.OnGossipReceived != nil {
	// 	if err := p.events.OnGossipReceived(ctx.ID(), msg); err != nil {
	// 		return err
	// 	}
	// }

	return nil
}

func (p *Protocol) hash(id noise.ID, data []byte) []byte {
	return append(id.ID[:], data...)
}

func (p *Protocol) GetRelayChan() chan Message {
	return p.relayChan
}
