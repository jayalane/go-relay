// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type connState int

const (
	waitingFor200 = iota
	up
	closed
)

// info about an object to check
type connection struct {
	lock     sync.RWMutex
	state    connState
	inConn   net.Conn
	outConn  net.Conn
	outBound chan []byte
	inBound  chan []byte
	// may need state later on
}

const (
	headerFor200      = "HTTP/1.1 200 Connection"
	httpNewLine       = "\r\n"
	requestForConnect = "CONNECT %s:%s HTTP/1.0\r\nHost: %s\r\nUser-Agent: Go-http-client/1.0\r\n\r\n"
)

func checkFor200(n int, buf []byte) ([]byte, bool) {
	a := strings.Split(string(buf[:n]), httpNewLine)
	if len(a) >= 1 && a[0][:len(headerFor200)] == headerFor200 {
		return []byte(strings.Join(a[2:], httpNewLine)), true
	}
	return []byte(""), false
}

func getProxyAddr(na net.Addr) (string, string) {
	return "localhost", "3128"
}

func getRealAddr(na net.Addr) (string, string) {
	return "ns.lane-jayasinha.com", "8022"
}

func (c *connection) inWriteLoop() {
	//  reads stuff from channel and writes to socket till fd is dead
	defer c.doneWithConn()
	for {
		select {
		case buffer, ok := <-c.inBound:
			if !ok {
				log.Println("CLOSING: channel closed", c.inConn)
				return
			}
			total := len(buffer)
			pos := 0
			for pos < total {
				len, err := c.inConn.Write(buffer[pos:total])
				if err != nil {
					count.Incr("conn-in-writes-err")
					log.Println("ERROR: write errored", c.inConn, err)
					return
				}
				pos += len
			}
			count.IncrDelta("conn-in-write-len", int64(total))
			count.Incr("conn-in-writes-ok")
			//log.Printf("OK: sent in data %d {%s} %d\n", total, string(buffer),
			//	c.state)
		}
	}
}

func (c *connection) doneWithConn() {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.state == closed {
		fmt.Println("Skipping close, already closed")
		return
	}
	fmt.Println("Closing from state", c.state)
	count.Incr("connection-pairing-closed")
	count.Decr("connection-pairing")
	c.state = closed
	c.inConn.Close()
	c.outConn.Close()
	close(c.outBound)
	close(c.inBound)
}

func (c *connection) outWriteLoop() {
	//  reads stuff from channel and writes to out socket
	defer c.doneWithConn()
	for {
		select {
		case buffer, ok := <-c.outBound:
			if !ok {
				fmt.Println("CLOSING: outBound channel closed")
				return
			}
			total := len(buffer)
			//fmt.Printf("Got a buffer for writing %d {%s}\n",
			//	total, buffer[0:total])
			pos := 0
			for pos < total {
				n, err := c.outConn.Write(buffer[pos:total])
				if n == 0 {
					count.Incr("conn-out-writes-zero")
					log.Println("ZERO-WRITE: write zero bytes", c.outConn, err)
					return
				}
				if err != nil {
					count.Incr("conn-out-writes-err")
					log.Println("ERROR: write errored", c.outConn, err)
					return
				}
				pos += n
			}
			count.IncrDelta("conn-out-write-len", int64(total))
			count.Incr("conn-out-writes-ok")
			//log.Printf("OK: sent out data %d {%s} state %d\n", total, string(buffer),
			//	c.state)
		}
	}
}

func (c *connection) inReadLoop() {

	//  reads stuff from out and writes to channel till fd is dead
	defer c.doneWithConn()
	for {
		var buffer []byte = make([]byte, 65536)
		n, err := c.outConn.Read(buffer)
		if n == 0 {
			count.Incr("read-in-closed")
			log.Println("CLOSED: read EOF", c.inConn, err)
			return
		}
		if err != nil {
			count.Incr("read-in-err")
			log.Println("ERROR: read errored", c.inConn, err)
			return
		}
		//fmt.Printf("Got data in %d {%s}\n", n, string(
		//	buffer[0:n]))
		c.lock.Lock()
		if c.state == waitingFor200 {
			// this will fail for fragmented reads of
			// first line
			newBuf, ok2 := checkFor200(n, buffer)
			if ok2 {
				c.state = up
				c.lock.Unlock()
				c.inBound <- newBuf
				continue
			}
			c.lock.Unlock()
			return
		}
		c.lock.Unlock()
		c.inBound <- buffer[0:n] //  to do non-blocking?
	}
}

func (c *connection) outReadLoop() {
	//  reads stuff from in and writes to channel till fd is dead
	defer c.doneWithConn()
	for {
		var buffer []byte = make([]byte, 65536)
		n, err := c.inConn.Read(buffer)
		if n == 0 {
			count.Incr("read-out-closed")
			log.Println("CLOSED: read EOF", c.inConn, err)
			return
		}
		if err != nil {
			count.Incr("read-out-read-err")
			log.Println("ERROR: read errored", c.outConn, err)
			return
		}
		// fmt.Printf("Got data out %d {%s}\n", n, string(buffer[0:n]))
		c.outBound <- buffer[0:n] // to do non-blocking?
	}
}

// first a few utilities
func (c *connection) handleOneConn() {
	ra := c.inConn.RemoteAddr()
	h, p := getProxyAddr(ra)
	rH, rP := getRealAddr(ra)
	var err error
	c.outConn, err = net.DialTimeout("tcp", h+":"+p, 15*time.Second)
	if err != nil {
		log.Println("ERROR: connect out got err", err)
		if err, ok := err.(net.Error); ok && err.Timeout() {
			count.Incr("connect-out-timeout")
		}
		count.Incr("connect-out-error")
		return // TODO
	}
	connS := fmt.Sprintf(
		requestForConnect,
		rH,
		rP,
		rH,
	)
	c.outConn.Write([]byte(connS))
	log.Println("Handling a connection", c.inConn.RemoteAddr())
	// now a goroutine per connection (in and out)
	// data flow thru a per connection
	count.Incr("connection-pairing")
	go c.inReadLoop()
	go c.outReadLoop()
	go c.inWriteLoop()
	go c.outWriteLoop()
}

func initConn(in net.Conn) *connection {
	c := connection{inConn: in}
	c.outBound = make(chan []byte, 10000)
	c.inBound = make(chan []byte, 10000)
	c.lock = sync.RWMutex{}
	c.state = waitingFor200
	return &c
}
