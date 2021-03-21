// -*- tab-width: 2 -*-

package main

import (
	"context"
	"fmt"
	count "github.com/jayalane/go-counter"
	pb "github.com/jayalane/go-relay/udpProxy"
	"google.golang.org/grpc"
	"net"
	"os"
	"strings"
	"time"
)

type udpMsg struct {
	n    int
	conn net.PacketConn
	la   net.Addr // destination
	ra   net.Addr // source
	buf  []byte
}

func (m udpMsg) log() string {
	return fmt.Sprintf("%s/%s/%s",
		m.ra.String(), m.la.String(), string(m.buf[:m.n]))
}

// New takes an incoming net.PacketConn and
// returns an initialized connection
func newUDP(in net.PacketConn) *udpMsg {
	c := udpMsg{conn: in}
	return &c
}

//
func startUDPHandler() {

	// this has a lot of comments as this was my thinking process

	// start the go routines that establish connections to the uproxy
	// and funnel the msgs from the channel in bound msgs are just sent,
	// not too much blocking.
	for i := 0; i < (*theConfig)["numUdpMsgHandlers"].IntVal; i++ {
		go handleUDPProxy() // each one has 1 Proxy client
	}
	// start the go routines to listen to the network and
	// send the out bound msgs to the channel
	for _, p := range strings.Split((*theConfig)["udpPorts"].StrVal, ",") {
		udpAddr := fmt.Sprintf("0.0.0.0:%s", p)
		conn, err := net.ListenPacket("udp", udpAddr)
		if err != nil {
			count.Incr("listen-udp-error")
			ml.La("ERROR: can't UDP listen to", p, err) // handle error
			return
		}
		la := conn.LocalAddr()
		ml.La("OK: UDP Listening to", la.String())

		// listen handler go routine
		go func() {
			buffer := make([]byte, (*theConfig)["udpBufferSize"].IntVal)
			for {
				n, ra, err := conn.ReadFrom(buffer)
				if err != nil {
					count.Incr("udp-read-error")
					ml.La("ERROR: read UDP failed", conn, err)
					count.Incr("udp-read-bad")
					return // Not sure this is correct
				}
				ml.Ln("Got an outgoing UDP msg", buffer[:n])
				count.Incr("udp-read-ok")
				count.Incr("udp-read-chan-add")
				theCtx.udpMsgChan <- &udpMsg{n, conn, la, ra, buffer}
			}
		}()
	}
}

// getProxyClient sets the environment variable to squid and
// gets the gRPC client.
func getProxyClient() (pb.ProxyClient, error) {
	squidProxyStr := fmt.Sprintf("%s:%s", (*theConfig)["squidHost"].StrVal,
		(*theConfig)["squidPort"].StrVal)

	os.Setenv("HTTP_PROXY", squidProxyStr)  // need to have PR to grpc for better config method
	os.Setenv("HTTPS_PROXY", squidProxyStr) // hopefully for now this is my only use of HTTP client
	os.Setenv("http_proxy", squidProxyStr)  // package in go
	os.Setenv("https_proxy", squidProxyStr)

	uproxyStr := fmt.Sprintf("%s:%s", (*theConfig)["uproxyHost"].StrVal,
		(*theConfig)["uproxyPort"].StrVal)

	backOffDial := 10 * time.Millisecond
	var cc grpc.ClientConnInterface
	var err error

	for {
		// open a connection to the UDP gRPC server
		// this is tricky because we need to use a Squid to reach the endpoint.
		ml.Ls("About to dial", uproxyStr)
		cc, err = grpc.Dial(uproxyStr, grpc.WithInsecure())
		if err == nil {
			ml.Ls("Got connection", cc)
			break
		}
		// sleep?  reconnect? log.Fatalf("fail to dial: %v", err)
		ml.Ls("uproxy connect failed", err)
		if backOffDial < 60*time.Second {
			backOffDial = backOffDial * 2
		}
		time.Sleep(backOffDial)
	}
	ml.Ls("About to create new client")
	udpClient := pb.NewProxyClient(cc)
	ml.Ls("Got new client", udpClient)

	return udpClient, nil
}

// handleUDPProxy opens connection to uproxy, and sends
// UDP segments from the channel to the uproxy.
// it also stats a reader for the uproxy which sends the
// replies back to the network (no extra channel for that)
func handleUDPProxy() {

	done := make(chan bool, 1)

	udpClient, err := getProxyClient() // apparently once this exists
	// it lasts robustly
	ml.La("Got here 0 client proxy", udpClient)
	stream, err := udpClient.SendMsgs(context.Background())
	ml.La("Got here got a stream", stream, err)
	// start up a go routine to read outbound msgs from channel
	// and send to the udpClient proxy
	go func() {
		ml.Ls("Started a listener for a gRPC proxy connection")
		for {
			select {
			case c := <-theCtx.udpMsgChan:
				count.Incr("read-udp-chan-remove")
				ml.Ln("Got an outbound msg", c.log())

				lHost, lPort := hostPart(c.la.String())
				rHost, rPort := hostPart(c.ra.String())

				m := pb.UdpMsg{
					SrcIp:   rHost,
					SrcPort: rPort,
					DstIp:   lHost,
					DstPort: lPort,
					Msg:     c.buf[:c.n],
				}
				err = stream.Send(&m) // network send, need to check for error
				if err != nil {
					ml.La("Send failed", err)
					// and ... ?
				}
				ml.La("Sent ok to proxy")
			case <-time.After(60 * time.Second * 5):
				count.Incr("read-udp-chan-idle")
			}
		}
	}()
	// start up a routine to read from the
	go func(s pb.Proxy_SendMsgsClient) {
		for {
			m, err := s.Recv()
			if err != nil {
				ml.Ls("Proxy recv got this err", err)
				//return // close?
				continue // ? break  or new client?
			}
			fmt.Print("proxy recv go thing", m)
			sendRaw(m) // no error checking - some logging and something external will retry
		}
	}(stream)
	<-done
}
