// -*- tab-width: 2 -*-
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	count "github.com/jayalane/go-counter"
	pb "github.com/jayalane/go-relay/udpProxy"
	"google.golang.org/grpc"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type udpMsg struct {
	n    int
	conn net.PacketConn
	la   string // net.Addr // destination
	ra   string // net.Addr // source
	na   string // net.Addr // NAT destination
	buf  []byte
}

func (m udpMsg) log() string {
	return fmt.Sprintf("%s/%s/%s/%s",
		m.ra, m.la, m.na, string(m.buf[:m.n]))
}

// New takes an incoming net.PacketConn and
// returns an initialized connection
func newUDP(in net.PacketConn) *udpMsg {
	c := udpMsg{conn: in}
	return &c
}

func readUDPMsg(c *net.UDPConn, buf []byte) (int, *net.UDPAddr, *net.UDPAddr, error) {

	oob := make([]byte, (*theConfig)["udpOobBufferSize"].IntVal)
	n, oobn, _, addr, err := c.ReadMsgUDP(buf, oob)
	if err != nil {
		return 0, nil, nil, err
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return 0, nil, nil, fmt.Errorf("parsing socket control message: %s", err)
	}
	var originalDst *net.UDPAddr
	for _, msg := range msgs {
		if msg.Header.Level == syscall.SOL_IP && msg.Header.Type == syscall.IP_RECVORIGDSTADDR {
			originalDstRaw := &syscall.RawSockaddrInet4{}
			if err = binary.Read(bytes.NewReader(msg.Data), binary.LittleEndian, originalDstRaw); err != nil {
				return 0, nil, nil, fmt.Errorf("read original destination address: %s", err)
			}
			switch originalDstRaw.Family {
			case syscall.AF_INET:
				pp := (*syscall.RawSockaddrInet4)(unsafe.Pointer(originalDstRaw))
				p := (*[2]byte)(unsafe.Pointer(&pp.Port))
				originalDst = &net.UDPAddr{
					IP:   net.IPv4(pp.Addr[0], pp.Addr[1], pp.Addr[2], pp.Addr[3]),
					Port: int(p[0])<<8 + int(p[1]),
				}

			case syscall.AF_INET6:
				pp := (*syscall.RawSockaddrInet6)(unsafe.Pointer(originalDstRaw))
				p := (*[2]byte)(unsafe.Pointer(&pp.Port))
				originalDst = &net.UDPAddr{
					IP:   net.IP(pp.Addr[:]),
					Port: int(p[0])<<8 + int(p[1]),
					Zone: strconv.Itoa(int(pp.Scope_id)),
				}

			default:
				return 0, nil, nil, fmt.Errorf("original destination is an unsupported network family")
			}
		}
	}

	if originalDst == nil {
		return 0, nil, nil, fmt.Errorf("unable to obtain original destination: %s", err)
	}

	return n, addr, originalDst, nil
}

// setTproxy2Listen does the set sock opts to enable TProxy
func setTproxy2Listen(conn *net.UDPConn) {
	fdFile, err := conn.File()
	fd := int(fdFile.Fd())
	if err != nil {
		return
	}
	defer fdFile.Close()
	err = syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
	if err != nil {
		return
	}
	syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
}

// startupUDPHandler does the UDP listens, and setups up the gRPC stuff
// etc.
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
		udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%s", p))
		if err != nil {
			count.Incr("listen-udp-fmt-error")
			ml.La("ERROR: can't UDP listen to", p, err) // handle error
			continue
		}
		conn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			count.Incr("listen-udp-error")
			ml.La("ERROR: can't UDP listen to", p, err) // handle error
			continue
		}
		setTproxy2Listen(conn) // don't care about errors here
		la := conn.LocalAddr()
		ml.La("OK: UDP Listening to", la.String(), conn)

		// listen handler go routine
		go func() {
			buffer := make([]byte, (*theConfig)["udpBufferSize"].IntVal)
			for {
				ml.La("Conn is now", conn)
				n, ra, la, err := readUDPMsg(conn, buffer)
				if err != nil {
					count.Incr("udp-read-error")
					ml.La("ERROR: read UDP failed", conn, err)
					count.Incr("udp-read-bad")
					return // Not sure this is correct
				}
				ml.Ln("Got an outgoing UDP msg", buffer[:n], conn, ra)
				count.Incr("udp-read-ok")
				count.Incr("udp-read-chan-add")
				_, na, newConn, err := getOriginalDstUDP(conn) // read NAT stuff
				if newConn != nil {
					conn = newConn
				}
				if err == nil {
					host, port := hostPart(na)
					ml.Ls("Got UDP NAT Addr:", host, port)
					count.Incr("UDP_NAT")
				} else {
					ml.Ls("NAT call got err", err)
				}
				theCtx.udpMsgChan <- &udpMsg{n, conn, la.String(), ra.String(), na, buffer}
			}
		}()
	}
}

// getProxyClient sets the environment variable to squid and
// gets the gRPC client - because gRPC uses env vars
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
		cc, err = grpc.Dial(uproxyStr,
			grpc.WithBlock(),
			grpc.WithInsecure(),
			grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
				rH, rP, err := net.SplitHostPort(addr)
				if err != nil {
					ml.Ls("splithostport error", err, addr)
					return nil, err
				}
				pH := (*theConfig)["squidHost"].StrVal
				pP := (*theConfig)["squidPort"].StrVal
				conn, _, err := dialWithProxy(pH, pP, rH, rP, nil, timeout)
				if err != nil {
					return nil, err
				}
				var n int
				var buffer = make([]byte, 65536)
				err = conn.SetReadDeadline(time.Now().Add(time.Minute * 15))
				if err == nil {
					ml.Ls("About to call first read for proxy")
					n, err = conn.Read(buffer)
				}
				_, ok2 := checkFor200(n, buffer)
				// I don't know if in gRPC the proxy connect can return
				// extra data but we are ignoring it
				ml.Ls("got 200 answer", ok2)
				if !ok2 {
					ml.Ln("Got this instead", string(buffer[:n]))
					return nil, errors.New("rejected by proxy")
				}
				return conn, nil
			}),
		)
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

	var udpClient pb.ProxyClient
	var err error
	for {
		udpClient, err = getProxyClient() // apparently once this exists
		if err == nil {
			break
		}
		ml.La("Error reaching uproxy gRPC", err)
		time.Sleep(10 * time.Second)
	}
	ml.La("Got a gRPC client", udpClient)
	// it lasts robustly
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
				time.Sleep(30 * time.Second)
				//return // close?
				continue // ? break  or new client?
			}
			ml.Ln("proxy recv go thing", m)
			sendRaw(m) // no error checking - some logging and something external will retry
		}
	}(stream)
	<-done
}
