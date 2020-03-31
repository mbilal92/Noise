package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mbilal92/noise"
	"github.com/mbilal92/noise/broadcast"
	"github.com/mbilal92/noise/network"
	"github.com/mbilal92/noise/relay"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

var (
	hostFlag    = pflag.IPP("host", "h", nil, "binding host")
	portFlag    = pflag.Uint16P("port", "p", 0, "binding port")
	addressFlag = pflag.StringP("address", "a", "", "publicly reachable network address")
)

// check panics if err is not nil.
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// printedLength is the total prefix length of a public key associated to a chat users ID.
const printedLength = 8

func GetLocalIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP
			}
		}
	}
	return nil
}

// An example chat application on Noise.
func main() {
	// Parse flags/options.
	pflag.Parse()

	logger, err := zap.NewDevelopment(zap.AddStacktrace(zap.PanicLevel))
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Create a new configured node.
	localIP := GetLocalIP()
	ntw, err := network.New(localIP, *portFlag, noise.ZeroPrivateKey, logger)
	check(err)
	defer ntw.Close()

	ntw.Bootstrap(pflag.Args(), 3*time.Second, 8)

	go ntw.Process()
	// Accept chat message inputs and handle chat commands in a separate goroutine.
	go input(func(line string) {
		chat(ntw, line)
	})

	// Wait until Ctrl+C or a termination call is done.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	// Close stdin to kill the input goroutine.
	check(os.Stdin.Close())

	// Empty println.
	println()
}

// input handles inputs from stdin.
func input(callback func(string)) {
	r := bufio.NewReader(os.Stdin)

	for {
		buf, _, err := r.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}

			check(err)
		}

		line := string(buf)
		if len(line) == 0 {
			continue
		}

		callback(line)
	}
}

// help prints out the users ID and commands available.
func help(node *noise.Node) {
	fmt.Printf("Your ID is %s(%s). Type '/discover' to attempt to discover new "+
		"peers, or '/peers' to list out all peers you are connected to.\n",
		node.ID().Address,
		node.ID().ID.String()[:printedLength],
	)
}

// chat handles sending chat messages and handling chat commands.
func chat(ntw *network.Network, line string) {
	switch line {
	// case "/discover":
	// 	ntw.Discover()
	// 	return
	case "/peers":
		ids := ntw.Peers()
		var str []string
		for _, id := range ids {
			str = append(str, fmt.Sprintf("%s(%s)", id.Address, id.ID.String()[:printedLength]))
		}

		fmt.Printf("I know %d peer(s): [%v]\n", len(ids), strings.Join(str, ", "))
		return
	case "/fp":
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		hexPbKey := strings.TrimSpace(input)
		decoded, _ := hex.DecodeString(hexPbKey)
		var publicKey noise.PublicKey
		copy(publicKey[:], decoded)
		fmt.Println("Decoded publicKey: %v", publicKey.String())
		fmt.Println("%v", ntw.FindPeer(publicKey))
	case "/sp":
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		hexPbKey := strings.TrimSpace(input)
		decoded, _ := hex.DecodeString(hexPbKey)
		var publicKey noise.PublicKey
		copy(publicKey[:], decoded)
		fmt.Println("Decoded publicKey: %v", publicKey.String())
		line2, _ := reader.ReadString('\n')
		msgTosend := strings.TrimSpace(line2)
		msg := relay.Message{}
		msg.From = ntw.Node().ID()
		msg.Data = []byte(msgTosend)
		msg.To = publicKey
		ntw.RelayMsg(msg)
		return
	case "/request":
		for _, id := range ntw.Peers() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			msg := network.Message{}
			msg.From = ntw.Node().ID()
			msg.SeqNum = byte(int(0))
			msg.Code = byte(int(1))
			msg.Data = []byte(line)
			msg.To = ntw.Node().ID().ID
			msg2, err := ntw.Node().RequestMessage(ctx, id.Address, msg)
			if err != nil {
				fmt.Printf("Failed to send message to %s(%s). Skipping... [error: %s]\n",
					id.Address,
					id.ID.String()[:printedLength],
					err,
				)
				continue
			} else {
				fmt.Printf("GOR RESPONSE for Request %v", msg2.(network.Message).String())
			}
			cancel()
		}
		return
	default:
	}

	if strings.HasPrefix(line, "/") {
		help(ntw.Node())
		return
	}

	msg := broadcast.Message{}
	msg.From = ntw.Node().ID()
	msg.Data = []byte(line)
	// fmt.Printf("msg %v", msg.String())
	ntw.Broadcast(msg)
}
