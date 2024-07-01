// -*- tab-width: 2 -*-

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	count "github.com/jayalane/go-counter"
	pb "github.com/jayalane/go-relay/udpProxy"
)

const (
	udpBufferSize   = 10000 // later config
	fourK           = 4096
	gRPCInTimoutMin = 5
)

type udpProxyServer struct {
	pb.UnimplementedProxyServer
	// anything?  port -> chan map
	portListeners    map[string]chan *pb.UdpMsg
	portListenerLock sync.RWMutex
	inChan           chan *pb.UdpMsg // UDP from network to gRPC
}

type tempError interface {
	Temporary() bool
}

func newProxyServer() *udpProxyServer {
	s := udpProxyServer{}

	s.portListeners = make(map[string]chan *pb.UdpMsg)
	s.inChan = make(chan *pb.UdpMsg, udpBufferSize)
	s.portListenerLock = sync.RWMutex{}

	return &s
}

func (s *udpProxyServer) handleOutgoingMsg(m *pb.UdpMsg) { //nolint:gocognit,cyclop
	g.Ml.La("Got a msg from gRPC", m)
	count.Incr("grpc_out_process")
	s.portListenerLock.RLock()

	inAddr := fmt.Sprintf("%s:%s", m.GetSrcIp(), m.GetSrcPort())
	outAddr := fmt.Sprintf("%s:%s", m.GetDstIp(), m.GetDstPort())

	if ch, ok := s.portListeners[inAddr]; ok {
		g.Ml.La("found inAddr", inAddr, ok)
		count.Incr("grpc_out_listener_reuse")
		s.portListenerLock.RUnlock()
		g.Ml.La("putting msg in channel", m)

		ch <- m

		return
	}
	s.portListenerLock.RUnlock()
	// update the listeners
	s.portListenerLock.Lock()

	val := make(chan *pb.UdpMsg, udpBufferSize) // config

	count.Incr("grpc_out_listener_make")

	s.portListeners[inAddr] = val

	s.portListenerLock.Unlock()
	// new go routine to handle the channel sends
	go func(ch chan *pb.UdpMsg, outAddr string, inAddr string) {
		g.Ml.La("Got here outbound msg sends", ch, outAddr)

		udpAddr, err := net.ResolveUDPAddr("udp", outAddr)
		if err != nil {
			g.Ml.La("error resolving", outAddr, err)
		}

		var conn *net.UDPConn

		delay := 50 * time.Millisecond //nolint:mnd

		for {
			conn, err = net.DialUDP("udp", nil, udpAddr)
			if err != nil {
				if terr, ok := err.(tempError); ok && terr.Temporary() {
					time.Sleep(delay)

					if delay < 500*time.Millisecond {
						delay *= 2
					}

					count.Incr("udp_out_dial_temp_err")

					continue // retry
				}

				g.Ml.La("Error Dialing", outAddr, err)
				count.Incr("udp_out_dial_err")

				return
			}

			break // not temp error fail
		}

		if err != nil {
			return
		}

		count.Incr("udp_out_dial_ok")
		g.Ml.La("Dial succeeded", udpAddr, conn.LocalAddr())

		wg := sync.WaitGroup{}

		defer conn.Close()

		wg.Add(1)

		go func(conn *net.UDPConn, _ string, inAddr string) { // why not used?
			defer wg.Done()
			// and channel to handle the listen segments
			for {
				buffer := make([]byte, fourK)

				g.Ml.Ls("about to read conn from", conn)
				count.Incr("udp_in_read")

				n, ra, err := conn.ReadFrom(buffer)
				if err != nil {
					g.Ml.Ls("got error on udp read", err)
					count.Incr("udp_in_err")

					return
				}

				count.Incr("udp_in_ok")
				g.Ml.Ln("Read UDP msg", n, buffer[:n])

				srcIP, srcPort, err := net.SplitHostPort(ra.String())
				if err != nil {
					g.Ml.Ls("got error on split host port", ra)

					continue
				}

				dstIP, dstPort, err := net.SplitHostPort(inAddr)
				if err != nil {
					g.Ml.Ls("got error on split host port", inAddr)
					count.Incr("udp_in_msg_err")

					return
				}

				count.Incr("udp_in_msg_ok")

				m := &pb.UdpMsg{
					SrcIp:   srcIP,
					SrcPort: srcPort,
					DstIp:   dstIP,
					DstPort: dstPort,
					Msg:     buffer[:n],
				}

				g.Ml.Ln("sending inbound", m)

				s.inChan <- m
			}
		}(conn, outAddr, inAddr)

		defer g.Ml.La("End of listen for Udp incoming", outAddr, inAddr)

		for {
			select {
			case mm := <-ch:
				count.Incr("grpc_in_send")

				n, err := conn.Write(mm.GetMsg())
				if err != nil {
					g.Ml.Ls("Write error", err)
					wg.Wait()
					count.Incr("grpc_in_send_err")

					return
				}

				if n != len(mm.GetMsg()) {
					g.Ml.Ls("Short write", n)
					wg.Wait()
					count.Incr("grpc_in_send_err_short")

					return
				}

				count.Incr("grpc_in_send_ok")
				g.Ml.Ln("Message sent", mm)
			case <-time.After(60 * time.Second * gRPCInTimoutMin): // config
				count.Incr("grpc_in_timeout")
				g.Ml.Ln("Timeout after 5 minutes on outbound")
				wg.Wait()

				return
			}
		}
	}(val, outAddr, inAddr)
	val <- m

	g.Ml.La("End of handle outgoing msg")
}

// SendMsgs receives a stream msgs which it puts on the wire and
// also sends a stream of messages from the network.
func (s *udpProxyServer) SendMsgs(stream pb.Proxy_SendMsgsServer) error {
	done := make(chan bool, 1)

	go func(s *udpProxyServer, ss pb.Proxy_SendMsgsServer) {
		for {
			select {
			case note := <-s.inChan:
				g.Ml.La("Sending a msg in", note)
				count.Incr("grpc_in_send")

				if err := ss.Send(note); err != nil {
					done <- true

					g.Ml.La("client send got error", err)
					count.Incr("grpc_in_send_err")

					return
				}

				count.Incr("grpc_in_send_ok")

			case <-time.After(60 * time.Second * gRPCInTimoutMin):
				g.Ml.La("No inbound msgs for five minutes")
				done <- true

				count.Incr("grpc_in_send_timeout")

				return
			}
		}
	}(s, stream)

	for {
		count.Incr("grpc_out_recv")

		out, err := stream.Recv()

		g.Ml.La("gRPC got a message for out", out, err)

		if errors.Is(err, io.EOF) {
			// done with reading
			g.Ml.Ls("EOF on gRPC read out")
			count.Incr("grpc_out_eof")

			break
		}

		if err != nil {
			g.Ml.Ls("on gRPC read out", err)
			count.Incr("grpc_out_err")

			return err
		}
		// handle msg
		count.Incr("grpc_out_ok")
		s.handleOutgoingMsg(out)
	}
	<-done

	return nil
}
