// -*- tab-width: 2 -*-
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/LiamHaworth/go-tproxy"
	count "github.com/jayalane/go-counter"
	pb "github.com/jayalane/go-relay/udpProxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type udpMsg struct {
	n    int
	conn net.PacketConn
	la   string // net.Addr // destination
	ra   string // net.Addr // source
	buf  []byte
}

func (m udpMsg) log() string {
	return fmt.Sprintf("%s/%s/%s",
		m.ra, m.la, string(m.buf[:m.n]))
}

/*
// New takes an incoming net.PacketConn and
// returns an initialized connection
func newUDP(in net.PacketConn) *udpMsg {
	c := udpMsg{conn: in}
	return &c
}
*/

// startupUDPHandler does the UDP listens, and setups up the gRPC stuff
// etc.
func startUDPHandler() {
	g.Ml.La("Staring UDP handler")
	// this has a lot of comments as this was my thinking process

	// start the go routines that establish connections to the uproxy
	// and funnel the msgs from the channel in bound msgs are just sent,
	// not too much blocking.
	for range (*g.Cfg)["numUdpMsgHandlers"].IntVal {
		go handleUDPProxy() // each one has 1 Proxy client
	}
	// start the go routines to listen to the network and
	// send the out bound msgs to the channel
	for _, p := range strings.Split((*g.Cfg)["udpPorts"].StrVal, ",") {
		udpAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:"+p)
		if err != nil {
			count.Incr("udp-listen-fmt-error")
			g.Ml.La("error: can't UDP listen to", p, err) // handle error

			continue
		}

		listener, err := tproxy.ListenUDP("udp", udpAddr) // like net but does the NAT stuff
		if err != nil {
			count.Incr("udp-listen-error")
			g.Ml.La("error: can't UDP listen to", p, err) // handle error

			continue
		}

		g.Ml.La("OK: UDP Listening to", udpAddr.String(), listener)

		// listen handler go routine
		go func() {
			buffer := make([]byte, (*g.Cfg)["udpBufferSize"].IntVal)

			for {
				g.Ml.La("Listener is now", listener)

				n, ra, la, err := tproxy.ReadFromUDP(listener, buffer)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() { //nolint:errorlint
						count.Incr("udp-read-out-error-temp")
						g.Ml.La("error: read UDP temp failed", listener, netErr)

						continue
					}

					count.Incr("udp-read-out-error-fatal")
					g.Ml.La("error: read UDP failed", listener, err)

					return // ?
				}

				g.Ml.Ln("Got an outgoing UDP msg", buffer[:n], listener, ra)
				count.Incr("udp-read-out-ok")
				count.Incr("udp-read-out-chan-add")

				host, port := hostPart(ra.String())

				g.Ml.Ls("Got UDP NAT Addr:", host, port)
				count.Incr("udp-route-out-nat")

				theCtx.udpMsgChan <- &udpMsg{n, listener, la.String(), ra.String(), buffer}
			}
		}()
	}
}

// getProxyClient sets the environment variable to squid and
// gets the gRPC client - because gRPC uses env vars.
func getProxyClient() (pb.ProxyClient, error) { //nolint:unparam
	squidProxyStr := fmt.Sprintf("%s:%s", (*g.Cfg)["squidHost"].StrVal,
		(*g.Cfg)["squidPort"].StrVal)

	os.Setenv("HTTP_PROXY", squidProxyStr)  // need to have PR to grpc for better config method
	os.Setenv("HTTPS_PROXY", squidProxyStr) // hopefully for now this is my only use of HTTP client
	os.Setenv("http_proxy", squidProxyStr)  // package in go
	os.Setenv("https_proxy", squidProxyStr)

	uproxyStr := fmt.Sprintf("%s:%s", (*g.Cfg)["uproxyHost"].StrVal,
		(*g.Cfg)["uproxyPort"].StrVal)

	backOffDial := 10 * time.Millisecond //nolint:mnd

	var cc grpc.ClientConnInterface

	var err error

	for {
		// open a connection to the UDP gRPC server
		// this is tricky because we need to use a Squid to reach the endpoint.
		g.Ml.Ls("About to dial", uproxyStr)
		//lint:ignore SA1019 we have a custom dialer
		cc, err = grpc.Dial(uproxyStr, //nolint:staticcheck
			//lint:ignore SA1019 need blocking to establish control over the connection state
			grpc.WithBlock(), //nolint:staticcheck
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				rH, rP, err := net.SplitHostPort(addr)
				if err != nil {
					g.Ml.Ls("splithostport error", err, addr)

					return nil, err
				}

				pH := (*g.Cfg)["squidHost"].StrVal
				pP := (*g.Cfg)["squidPort"].StrVal
				cancelTime, ok := ctx.Deadline()

				var timeout time.Duration // zero value ok here?

				if ok {
					timeout = time.Until(cancelTime)
				}

				conn, _, err := dialWithProxy(pH, pP, rH, rP, nil, timeout)
				if err != nil {
					return nil, err
				}

				var n int

				buffer := make([]byte, sixtyfourK)

				err = conn.SetReadDeadline(time.Now().Add(time.Minute * 15)) //nolint:mnd
				if err == nil {
					g.Ml.Ls("About to call first read for proxy")

					n, err = conn.Read(buffer)
					if err != nil {
						g.Ml.Ln("Got error ", err)

						return nil, errors.New("proxy read failed") //nolint:err113
					}
				}

				_, ok2 := checkFor200(n, buffer)
				// I don't know if in gRPC the proxy connect can return
				// extra data but we are ignoring it
				g.Ml.Ls("got 200 answer", ok2)

				if !ok2 {
					g.Ml.Ln("Got this instead", string(buffer[:n]))

					return nil, errors.New("rejected by proxy") //nolint:err113
				}

				return conn, nil
			}),
		)
		if err == nil {
			g.Ml.Ls("Got connection", cc)

			break
		}
		// sleep?  reconnect? log.Fatalf("fail to dial: %v", err)
		g.Ml.Ls("uproxy connect failed", err)

		if backOffDial < 60*time.Second {
			backOffDial *= 2
		}

		time.Sleep(backOffDial)
	}
	g.Ml.Ls("About to create new client")

	udpClient := pb.NewProxyClient(cc)

	g.Ml.Ls("Got new client", udpClient)

	return udpClient, nil
}

func netUDPAddr(ip string, port string) (*net.UDPAddr, error) {
	addr, err := net.ResolveUDPAddr("udp", ip+":"+port)

	return addr, err
}

func sendRaw(m *pb.UdpMsg) (int, error) {
	dstAddr, err := netUDPAddr(m.GetDstIp(), m.GetDstPort())
	if err != nil {
		g.Ml.Ls("Addr error", m.GetDstIp(), m.GetDstPort(), err)

		return 0, err
	}

	srcAddr, err := netUDPAddr(m.GetSrcIp(), m.GetSrcPort())
	if err != nil {
		g.Ml.Ls("Addr error", m.GetSrcIp(), m.GetSrcPort(), err)

		return 0, err
	}

	inConn, err := tproxy.DialUDP("udp", dstAddr, srcAddr)
	if err != nil {
		g.Ml.Ls("Failed to connect to original UDP source", srcAddr.String(), err)
		count.Incr("udp-send-in-conn-err")

		return 0, err
	}

	defer inConn.Close()

	bytesWritten, err := inConn.Write(m.GetMsg())
	if err != nil {
		g.Ml.Ls("Encountered error while writing to local", inConn.RemoteAddr(), err)
		count.Incr("udp-send-in-write-err")

		return 0, err
	} else if bytesWritten < len(m.GetMsg()) {
		g.Ml.Ls("Not all bytes in buffer written", bytesWritten,
			len(m.GetMsg()),
			inConn.RemoteAddr())
	}

	count.Incr("udp-send-in-ok")
	count.IncrDelta("udp-send-in-len", int64(bytesWritten))

	return bytesWritten, nil
}

// handleUDPProxy opens connection to uproxy, and sends
// UDP segments from the channel to the uproxy.
// it also stats a reader for the uproxy which sends the
// replies back to the network (no extra channel for that).
func handleUDPProxy() {
	done := make(chan bool, 1)

	var udpClient pb.ProxyClient

	var err error

	for {
		udpClient, err = getProxyClient() // apparently once this exists it is robust
		if err == nil {
			break
		}

		g.Ml.La("Error reaching uproxy gRPC", err)
		time.Sleep(10 * time.Second) //nolint:mnd
	}
	g.Ml.La("Got a gRPC client", udpClient)
	stream, err := udpClient.SendMsgs(context.Background())
	g.Ml.La("Got here got a stream", stream, err)
	// start up a go routine to read outbound msgs from channel
	// and send to the udpClient proxy
	go func() {
		g.Ml.Ls("Started a listener for a gRPC proxy connection")

		for {
			select {
			case c := <-theCtx.udpMsgChan:
				count.Incr("udp-read-out-chan-remove")
				g.Ml.Ln("Got an outbound msg", c.log())

				lHost, lPort := hostPart(c.la)
				rHost, rPort := hostPart(c.ra)
				m := pb.UdpMsg{
					SrcIp:   rHost,
					SrcPort: rPort,
					DstIp:   lHost,
					DstPort: lPort,
					Msg:     c.buf[:c.n],
				}

				err = stream.Send(&m) // network send, need to check for error
				if err != nil {
					g.Ml.La("Send failed", err) // more?
				} else {
					g.Ml.La("Sent ok to proxy")
				}
			case <-time.After(60 * time.Second * five):
				count.Incr("udp-read-out-chan-idle")
			}
		}
	}()
	// start up a routine to read from the stream
	go func(s pb.Proxy_SendMsgsClient) {
		for {
			m, err := s.Recv()
			if err != nil {
				g.Ml.Ls("Proxy recv got this err", err)
				time.Sleep(30 * time.Second) //nolint:mnd
				// return // close?

				continue // ? break  or new client?
			}

			count.Incr("udp-read-in-msg-gprc")
			g.Ml.Ln("proxy recv go thing", m.String())

			nn, err2 := sendRaw(m) // no error checking - some logging and something external will retry
			if err2 != nil {
				g.Ml.Ln("Send got err", err)

				continue
			}

			g.Ml.Ln("Send went ok", nn)
		}
	}(stream)
	<-done
}
