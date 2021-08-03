// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	pb "github.com/jayalane/go-relay/udpProxy"
	"io"
	"net"
	"sync"
	"time"
)

type udpProxyServer struct {
	pb.UnimplementedProxyServer
	// anything?  port -> chan map
	portListeners    map[string]chan *pb.UdpMsg
	portListenerLock sync.RWMutex
	inChan           chan *pb.UdpMsg // UDP from network to gRPC
}

func newProxyServer() *udpProxyServer {
	s := udpProxyServer{}

	s.portListeners = make(map[string]chan *pb.UdpMsg)
	s.inChan = make(chan *pb.UdpMsg, 10000) // TODO config
	s.portListenerLock = sync.RWMutex{}
	return &s
}

func (s *udpProxyServer) handleOutgoingMsg(m *pb.UdpMsg) {
	ml.La("Got a msg from gRPC", m)
	count.Incr("grpc_out_process")
	s.portListenerLock.RLock()
	inAddr := fmt.Sprintf("%s:%s", m.SrcIp, m.SrcPort)
	outAddr := fmt.Sprintf("%s:%s", m.DstIp, m.DstPort)
	if ch, ok := s.portListeners[inAddr]; ok {
		ml.La("found inAddr", inAddr, ok)
		count.Incr("grpc_out_listener_reuse")
		s.portListenerLock.RUnlock()
		ml.La("putting msg in channel", m)
		ch <- m
		return
	}
	s.portListenerLock.RUnlock()
	// update the listeners
	s.portListenerLock.Lock()
	val := make(chan *pb.UdpMsg, 10000) // config
	count.Incr("grpc_out_listener_make")
	s.portListeners[inAddr] = val
	s.portListenerLock.Unlock()
	// new go routine to handle the channel sends
	go func(ch chan *pb.UdpMsg, outAddr string, inAddr string) {
		ml.La("Got here outbound msg sends", ch, outAddr)
		udpAddr, err := net.ResolveUDPAddr("udp", outAddr)
		if err != nil {
			ml.La("error resolving", outAddr, err)
		}
		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			ml.La("Error Dialing", outAddr, err)
			count.Incr("udp_out_dial_bad")
			return
		}
		count.Incr("udp_out_dial_ok")
		ml.La("Dial succeeded", udpAddr, conn.LocalAddr())
		wg := sync.WaitGroup{}
		defer conn.Close()
		go func(conn *net.UDPConn, outAddr string, inAddr string) {
			wg.Add(1)
			defer wg.Done()
			// and channel to handle the listen segments
			for {
				buffer := make([]byte, 4096)
				ml.Ls("about to read from conn", conn)
				count.Incr("udp_in_read")
				n, ra, err := conn.ReadFrom(buffer)
				if err != nil {
					ml.Ls("got error on udp read", err)
					count.Incr("udp_in_err")
					return
				}
				count.Incr("udp_in_ok")
				ml.Ln("Read UDP msg", n, buffer[:n])
				srcIP, srcPort, err := net.SplitHostPort(ra.String())
				if err != nil {
					ml.Ls("got error on split host port", ra)
					continue
				}
				dstIP, dstPort, err := net.SplitHostPort(inAddr)
				if err != nil {
					ml.Ls("got error on split host port", inAddr)
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
				ml.Ln("sending inbound", m)
				s.inChan <- m
			}
		}(conn, outAddr, inAddr)

		defer ml.La("End of listen for Udp incoming", outAddr, inAddr)

		for {
			select {
			case mm := <-ch:
				count.Incr("grpc_in_send")
				n, err := conn.Write(mm.Msg)
				if err != nil {
					ml.Ls("Write error", err)
					wg.Wait()
					count.Incr("grpc_in_send_err")
					return
				}
				if n != len(mm.Msg) {
					ml.Ls("Short write", n)
					wg.Wait()
					count.Incr("grpc_in_send_err_short")
					return
				}
				count.Incr("grpc_in_send_ok")
				ml.Ln("Message sent", mm)
			case <-time.After(60 * time.Second * 5): // config
				count.Incr("grpc_in_timeout")
				ml.Ln("Timeout after 5 minutes on outbound")
				wg.Wait()
				return
			}
		}
	}(val, outAddr, inAddr)
	val <- m
	ml.La("End of handle outgoing msg")
}

// SendMsgs receives a stream msgs which it puts on the wire and
// also sends a stream of messages from the network
func (s *udpProxyServer) SendMsgs(stream pb.Proxy_SendMsgsServer) error {
	done := make(chan bool, 1)
	go func(s *udpProxyServer, ss pb.Proxy_SendMsgsServer) {
		for {
			select {
			case note := <-s.inChan:
				ml.La("Sending a msg in", note)
				count.Incr("grpc_in_send")
				if err := ss.Send(note); err != nil {
					done <- true
					ml.La("client send got error", err)
					count.Incr("grpc_in_send_err")
					return
				}
				count.Incr("grpc_in_send_ok")

			case <-time.After(60 * time.Second * 5):
				ml.La("No inbound msgs for a minute")
				done <- true
				count.Incr("grpc_in_send_timeout")

				return
			}
		}
	}(s, stream)
	for {
		count.Incr("grpc_out_recv")
		out, err := stream.Recv()
		ml.La("gRPC got a message for out", out, err)
		if err == io.EOF {
			// done with reading
			ml.Ls("EOF on gRPC read out")
			count.Incr("grpc_out_eof")
			break
		}
		if err != nil {
			ml.Ls("on gRPC read out", err)
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
