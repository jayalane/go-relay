// -*- tab-width: 2 -*-

package main

import (
	pb "github.com/jayalane/go-relay/udpProxy"
	"io"
	"sync"
	"time"
)

type udpProxyServer struct {
	pb.UnimplementedProxyServer
	// anything?  port -> chan map
	portListeners    map[int]chan pb.UdpMsg
	portListenerLock sync.RWMutex
	inChan           chan *pb.UdpMsg // UDP from network to gRPC
}

func newProxyServer() *udpProxyServer {
	s := udpProxyServer{}
	return &s
}

func (s *udpProxyServer) handleOutgoingMsg(m *pb.UdpMsg) {
	ml.La("Got a msg", m)
}

// SendMsgs receives a stream msgs which it puts on the wire and
// also sends a stream of messages from the network
func (s *udpProxyServer) SendMsgs(stream pb.Proxy_SendMsgsServer) error {
	done := make(chan bool, 1)
	go func(s *udpProxyServer, ss pb.Proxy_SendMsgsServer) {
		for {
			select {
			case note := <-s.inChan:
				ml.La("Woot! Sending a msg in", note)
				if err := ss.Send(note); err != nil {
					done <- true
					return
				}
			case <-time.After(60 * time.Second):
				ml.La("No inbound msgs for a minute")
				done <- true
				return
			}
		}
	}(s, stream)
	for {
		out, err := stream.Recv()
		ml.La("Woot! got a message for out", out, err)
		if err == io.EOF {
			// done with reading
			break
		}
		if err != nil {
			return err
		}
		// handle msg
		s.handleOutgoingMsg(out)
	}
	<-done
	return nil
}
