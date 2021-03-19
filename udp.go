// -*- tab-width: 2 -*-

package main

import (
	//	"context"
	"fmt"
	count "github.com/jayalane/go-counter"
	//	pb "github.com/jayalane/go-relay/udpProxy"
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

	// start the go routines that establish connections to the uproxy and
	// funnel the msgs from the channel
	// in bound msgs are just sent, not too much blocking.
	for i := 0; i < (*theConfig)["numUdpConnHandlers"].IntVal; i++ {
		go handleUDPProxy()
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
				count.Incr("udp-read-ok")
				count.Incr("udp-read-chan-add")
				theCtx.udpMsgChan <- &udpMsg{n, conn, la, ra, buffer}
			}
		}()
	}
}

// handleUDPProxy opens connection to uproxy, and sends
// UDP segments from the channel to the uproxy.
// it also stats a reader for the uproxy which sends the
// replies back to the network (no extra channel for that)
func handleUDPProxy() {

	go func() {
		for {
			select {
			case c := <-theCtx.udpMsgChan:
				count.Incr("read-udp-chan-remove")
				ml.Ln("Got a msg", c.n, c.la, c.ra, c.conn, c.buf[:c.n])

				//lHost, lPort := hostPart(c.la.String())
				//				rHost, rPort := hostPart(c.ra.String())

				/*				m := pb.UdpMsg{
								SrcIp:   rHost,
								SrcPort: rPort,
								DstIp:   lHost,
								DstPort: lPort,
								Msg:     c.buf[:c.n],
							}*/
				//_ = stream.Send(&m) // network send, need to check for error
				reply := udpMsg{
					n:   2, // len(hi)
					la:  c.ra,
					ra:  c.la,
					buf: []byte("hi")}
				sendRaw(reply) // no error checking - some logging and will retry

			case <-time.After(60 * time.Second):
				count.Incr("read-udp-chan-idle")
			}
		}
	}()

	squidProxyStr := fmt.Sprintf("%s:%s", (*theConfig)["squidHost"].StrVal,
		(*theConfig)["squidPort"].StrVal)

	os.Setenv("HTTP_PROXY", squidProxyStr)  // need to have PR to grpc for better config method
	os.Setenv("HTTPS_PROXY", squidProxyStr) // hopefully for now this is my only use of HTTP client
	os.Setenv("http_proxy", squidProxyStr)  // package in go
	os.Setenv("https_proxy", squidProxyStr)

	uproxyStr := fmt.Sprintf("%s:%s", (*theConfig)["uproxyHost"].StrVal,
		(*theConfig)["uproxyPort"].StrVal)

	backOffDial := 10 * time.Millisecond
	for {
		// open a connection to the UDP gRPC server
		// this is tricky because we need to use a Squid to reach the endpoint.

		_, err := grpc.Dial(uproxyStr)
		if err != nil {
			// sleep?  reconnect? log.Fatalf("fail to dial: %v", err)
			ml.Ls("uproxy connect failed", err)
			if backOffDial < 60*time.Second {
				backOffDial = backOffDial * 2
			}
			time.Sleep(backOffDial)
			continue
		}
		backOffDial = 10 * time.Millisecond
		//defer conn.Close()
		// udpClient := pb.NewProxyClient(conn)
		//stream, err := udpClient.SendMsgs(context.Background())
		//go func(s pb.Proxy_SendMsgsClient) {
		//			for {
		//				m, err := s.Recv()
		//if err != nil {
		//fmt.Print("Got this erro", err)
		//return // close?
		//}
		//fmt.Print("Got this thing", m)
		//}
		//}(stream)

	}
}
