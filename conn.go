// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	"github.com/paultag/sniff/parser"
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
	inConn   net.TCPConn
	outConn  net.Conn
	outBound chan []byte
	inBound  chan []byte
	// may need state later on
}

const (
	headerFor200      = "HTTP/1.1 200 Connection"
	httpNewLine       = "\r\n"
	requestForConnect = "CONNECT %s:%s HTTP/1.0\r\nHost: %s\r\nUser-Agent: Go-http-client/1.0\r\nX-Forwarded-For: %s\r\n\r\n"
)

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// utility functions

// checkFor200 takes a buffer and sees if it was a successful
// connect reply.  Returns the remainder (if any) and ok set to
// true on success or '', false on failure
func checkFor200(n int, buf []byte) ([]byte, bool) {
	a := strings.Split(string(buf[:n]), httpNewLine)
	ok := false
	rest := -1
	for i, s := range a {
		if i == 0 {
			if len(s) >= len(headerFor200) && s[0:len(headerFor200)] == headerFor200 {
				ok = true
			}
		}
		if len(s) == 0 {
			rest = i + 1
		}
	}
	if ok {
		var res []byte = []byte("")
		if rest < len(a) {
			res = []byte(strings.Join(a[rest:], httpNewLine))
		}
		return res, ok
	}
	return []byte(""), false
}

func hostPart(ad string) (string, string) {
	s := strings.Split(ad, ":")
	if len(s) < 2 {
		return "", s[len(s)-1]
	}
	return strings.Join(s[:len(s)-1], ""), s[len(s)-1]
}

// getProxyAddr will return the squid endpoint
func getProxyAddr(na net.Addr, sni string) (string, string) {
	return theConfig["squidHost"].StrVal, theConfig["squidPort"].StrVal
}

// getRealAddr will look up the true hostname
// for the CONNECT call - maybe via zone xfer?
func getRealAddr(na net.Addr, sni string, sniPort string) (string, string) {
	h, port := hostPart(na.String())
	if theConfig["destPortOverride"].StrVal != "" {
		port = theConfig["destPortOverride"].StrVal
	} else {
		port = sniPort
	}
	if sni != "" {
		return sni, port
	}
	if theConfig["destHostMethod"].StrVal == "incoming" {
	}
	if theConfig["destHostOverride"].StrVal != "" {
		return theConfig["destHostOverride"].StrVal, port
	}
	return h, port

}

// initConn takes an incoming net.Conn and
// returns an initialized connection
func initConn(in net.TCPConn) *connection {
	c := connection{inConn: in}
	c.outBound = make(chan []byte, 10000)
	c.inBound = make(chan []byte, 10000)
	c.lock = sync.RWMutex{}
	c.state = waitingFor200
	return &c
}

// doneWithConn closes everything safely when done
// can be called multiple times
func (c *connection) doneWithConn() {
	// log.Println("Closing connections")
	time.Sleep(3 * time.Second) // drain time
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.state == closed {
		// log.Println("Skipping due to already closed")
		count.Incr("connection-pairing-dup-close")
		return
	}
	count.Incr("connection-pairing-closed")
	count.Decr("connection-pairing")
	c.state = closed
	c.inConn.Close()
	c.outConn.Close()
	// I was closign the channel but that panicked go
}

// next four goroutines per connection

// inWriteLoop writes date from the far end
// back to the caller
func (c *connection) inWriteLoop() {
	//log.Println("Starting inWriteLoop for", c)
	//defer log.Println("exiting inWriteLoop for", c)
	//  reads stuff from channel and writes to socket till fd is dead
	defer c.doneWithConn()
	for {
		select {
		case buffer, ok := <-c.inBound:
			if !ok {
				count.Incr("write-in-zero")
			}
			//log.Println("Going to write inbound", buffer)
			total := len(buffer)
			pos := 0
			for pos < total {
				len, err := c.inConn.Write(buffer[pos:total])
				if len == 0 {
					count.Incr("write-in-zero")
					return
				}
				if err != nil {
					count.Incr("write-in-err")
					return
				}
				pos += len
			}
			count.IncrDelta("write-in-len", int64(total))
			count.Incr("write-in-ok")
			//log.Printf("OK: sent in data %d {%s} %d\n", total, string(buffer),
			//	c.state)
		}
	}
}

// outWriteLoop reads from the channel and writes to the far end
func (c *connection) outWriteLoop() {
	//  reads stuff from channel and writes to out socket
	//log.Println("Starting outWriteLoop for", c)
	//+++++defer log.Println("exiting outWriteLoop for", c)
	defer c.doneWithConn()
	for {
		select {
		case buffer, ok := <-c.outBound:
			if !ok {
				return
			}
			total := len(buffer)
			// log.Printf("Got a buffer for writing %d {%s}\n",
			// 	total, buffer[0:total])
			pos := 0
			for pos < total {
				n, err := c.outConn.Write(buffer[pos:total])
				if n == 0 {
					count.Incr("write-out-zero")
					return
				}
				if err != nil {
					count.Incr("write-out-err")
					return
				}
				pos += n
			}
			count.IncrDelta("write-out-len", int64(total))
			count.Incr("write-out-ok")
			//log.Printf("OK: sent out data %d {%s} state %d\n", total, string(buffer),
			//	c.state)
		}
	}
}

// inReadLoop (for inboud) reads from the far end and sticks into a
// channel
func (c *connection) inReadLoop() {
	//	log.Println("Starting inReadLoop for", c)
	//defer log.Println("exiting inReadLoop for", c)

	//  reads stuff from out and writes to channel till fd is dead
	defer c.doneWithConn()
	for {
		var n int
		var buffer []byte = make([]byte, 65536)
		err := c.outConn.SetReadDeadline(time.Now().Add(time.Minute * 15))
		if err == nil {
			// log.Println("About to call read for inReadLoop state", c.state)
			n, err = c.outConn.Read(buffer)
		}
		//log.Println("inReadLoop got", n, err)
		if err != nil { // including read timeouts
			count.Incr("read-in-err")
			// log.Println("ERROR: inReadLoop got error", err)
			return
		}
		if n == 0 {
			count.Incr("read-in-zero")
			// log.Println("ERROR: inReadLoop got null read")
			continue
		}
		//log.Printf("state %d Got data in %d {%s}\n%d\n", c.state,
		//	n,
		//	string(buffer[0:n]),
		//	c.state)
		c.lock.Lock()
		if c.state == waitingFor200 {
			// this will fail for fragmented reads of
			// first line
			newBuf, ok2 := checkFor200(n, buffer)
			// log.Println("Got answer", ok2, newBuf)
			if !ok2 {
				count.Incr("read-in-bad-header")
				c.inConn.Close()
				c.outConn.Close()
				c.lock.Unlock()
				return
			}
			count.Incr("read-in-header")
			c.state = up
			c.lock.Unlock()
			if len(newBuf) > 0 {
				c.inBound <- newBuf
				count.Incr("read-in-ok")
				count.IncrDelta("read-in-len", int64(n))
			}
			go c.outReadLoop() // now start the whole thing
			go c.inWriteLoop()
			go c.outWriteLoop()
			continue
		}
		c.lock.Unlock()
		count.Incr("read-in-ok")
		count.IncrDelta("read-in-len", int64(n))
		c.inBound <- buffer[0:n] //  to do non-blocking?
	}
}

// outReadLoop reads from the caller and writes to channel
// for outbound data flow
func (c *connection) outReadLoop() {
	//  reads stuff from in and writes to channel till fd is dead
	//log.Println("Starting in outReadLoop for", c)
	//defer log.Println("exiting outReadLoop for", c)
	defer c.doneWithConn()
	for {
		var n int
		var buffer []byte = make([]byte, 65536)
		err := c.inConn.SetReadDeadline(time.Now().Add(time.Minute * 15))
		if err == nil {
			n, err = c.inConn.Read(buffer)
		}
		//log.Println("Read outReadLoop got", n, err)
		if err != nil {
			if err, ok := err.(net.Error); ok && !err.Timeout() {
				count.Incr("read-out-timeout")
			} else {
				count.Incr("read-out-read-err")
				return
			}
		}
		if n == 0 {
			count.Incr("read-out-zero")
			continue
		}
		//log.Printf("Got data out %d {%s}\n", n, string(buffer[0:n]))
		count.Incr("read-out-ok")
		count.Incr("read-out-len")
		c.outBound <- buffer[0:n] // to do non-blocking?
	}
}

// run starts up the work on a new connection
func (c *connection) run() {

	// use local address
	la := c.inConn.LocalAddr()
	ra := c.inConn.RemoteAddr()
	//	log.Print("LA", la, "\nRA", ra)
	h, _ := hostPart(ra.String())
	count.Incr("conn-from-" + h)
	firstRead := make([]byte, 1024)
	err := c.inConn.SetReadDeadline(time.Now().Add(time.Second * 1))
	var n int
	if err == nil {
		n, err = c.inConn.Read(firstRead)
	}
	//	log.Println("Got first read", string(firstRead[:n]))
	if err != nil {
		if err, ok := err.(net.Error); ok && !err.Timeout() {
			// log.Println("ERROR: First read on inbound got error or timeout", err)
			count.Incr("read-out-err")
			return
		}
		count.Incr("read-out-timeout-first")
	}
	dstHost, err := parser.GetHostname(firstRead[:n])
	if err != nil {
		dstHost = ""
	} else {
		count.Incr("Sni")
	}
	// first get NAT address
	dstPort := ""
	if dstHost == "" && theConfig["isNAT"].BoolVal {
		c.lock.Lock()
		_, addr, newConn, err := getOriginalDst(&c.inConn)
		if err == nil {
			host, port := hostPart(addr)
			// log.Println("Got NAT Addr:", host, port)
			dstHost = host
			dstPort = port
			count.Incr("NAT")
		}
		c.inConn = *newConn
		c.lock.Unlock()
	}
	hP, pP := getProxyAddr(la, dstHost)
	rH, rP := getRealAddr(la, dstHost, dstPort)
	count.Incr("connect-out-remote-" + rH)
	//log.Println("Dial to", hP, pP)
	c.outConn, err = net.DialTimeout("tcp", hP+":"+pP, 15*time.Second)
	if err != nil {
		log.Println("ERROR: connect out got err", err)
		if err, ok := err.(net.Error); ok && err.Timeout() {
			count.Incr("connect-out-timeout")
		}
		count.Incr("connect-out-error")
		c.inConn.Close()
		c.outConn.Close()
		log.Println("Closed inConn")
		return
	}
	connS := fmt.Sprintf(
		requestForConnect,
		rH,
		rP,
		rH,
		ra.String(),
	)
	c.outConn.Write([]byte(connS))
	log.Println("Handling a connection", c.inConn.RemoteAddr(), connS)

	// and stage the fristRead data
	c.outBound <- firstRead[:n]

	// now a goroutine per connection (in and out)
	// data flow thru a per connection
	count.Incr("connection-pairing")
	go c.inReadLoop()

}
