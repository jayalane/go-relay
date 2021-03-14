// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	"net"
	"strings"
	"time"
)

type udpConn struct {
	n    int
	conn net.PacketConn
	addr net.Addr
	buf  []byte
}

// New takes an incoming net.PacketConn and
// returns an initialized connection
func newUDP(in net.PacketConn) *udpConn {
	c := udpConn{conn: in}
	return &c
}

func udpHandler() {
	// start go routines to handle msgs
	for i := 0; i < (*theConfig)["numUdpConnHandlers"].IntVal; i++ {
		go handleUDPConn()
	}
	// this needs thinking - this is wrong as is
	for _, p := range strings.Split((*theConfig)["udpPorts"].StrVal, ",") {
		udpAddr := fmt.Sprintf("0.0.0.0:%s", p)
		for {
			conn, err := net.ListenPacket("udp", udpAddr)
			if err != nil {
				count.Incr("listen-udp-error")
				ml.La("ERROR: can't UDP listen to", p, err) // handle error
				return
			}
			ml.La("OK: UDP Listening to", p)
			// listen handler go routine
			go func() {
				buffer := make([]byte, (*theConfig)["udpBufferSize"].IntVal)
				for {
					n, address, err := conn.ReadFrom(buffer)
					if err != nil {
						count.Incr("udp-read-error")
						ml.La("ERROR: read UDP failed", conn, err)
						count.Incr("udp-read-bad")
						return // Not sure this is correct
					}
					count.Incr("udp-read-ok")
					count.Incr("udp-read-chan-add")
					theCtx.udpConnChan <- &udpConn{n, conn, address, buffer}
				}
			}()
		}
	}

}

func handleUDPConn() {
	// taking from main loop - starts a go routine per connection
	// open a connection to the UDP gRPC server.

	for {
		select {
		case c := <-theCtx.udpConnChan:
			count.Incr("read-udp-chan-remove")
			fmt.Println("Got a msg", c.n, c.addr, c.conn, c.buf[:c.n])
			// go c.run()
		case <-time.After(60 * time.Second):
			count.Incr("read-udp-chan-idle")
		}
	}
}
