// -*- tab-width: 2 -*-

package main

import (
	"errors"
	"fmt"
	count "github.com/jayalane/go-counter"
	"github.com/paultag/sniff/parser"
	"math/rand"
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
	lock       sync.RWMutex
	state      connState
	inConn     net.TCPConn
	outConn    *net.TCPConn
	remoteHost string
	remotePort string
	writesDone chan bool
	outBound   chan []byte
	inBound    chan []byte
	// may need state later on
}

const (
	headerForHost     = "Host: "
	httpNewLine       = "\r\n"
	requestForConnect = "CONNECT %s:%s HTTP/1.0\r\nHost: %s\r\nUser-Agent: %s\r\nX-Forwarded-For: %s\r\n\r\n"
)

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// utility functions

// constHttp200Header returns a constant list of possible 200 status headers
func constHTTP200Header() []string {
	return []string{
		"HTTP/1.0 200 Connection",
		"HTTP/1.1 200 Connection",
	}
}

// checkFor200 takes a buffer and sees if it was a successful
// connect reply.  Returns the remainder (if any) and ok set to
// true on success or '', false on failure
func checkFor200(n int, buf []byte) ([]byte, bool) {
	a := strings.Split(string(buf[:n]), httpNewLine)
	ok := false
	rest := -1
	for i, s := range a {
		if i == 0 {
			for _, head := range constHTTP200Header() {
				if len(s) >= len(head) && s[0:len(head)] == head {
					ok = true
				}
			}
		}
		if len(s) == 0 {
			rest = i + 1
		}
	}
	if ok {
		var res = []byte("")
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
func getProxyAddr(na net.Addr, dst string, isSNI bool) (string, string) {
	if isSNI {
		return (*theConfig)["squidHost"].StrVal, (*theConfig)["squidPort"].StrVal
	}
	ip := net.ParseIP(dst)
	ml.ls("Checking for squid", na, dst, ip, theCtx.relayCidr)
	if theCtx.relayCidr == nil || theCtx.relayCidr.Contains(ip) {
		// if no config, always use squid
		ml.ls("Checks ok use squid ", ip, theCtx.relayCidr)
		return (*theConfig)["squidHost"].StrVal, (*theConfig)["squidPort"].StrVal
	}
	return "", ""
}

// getRealAddr will look up the true hostname
// for the CONNECT call - maybe via zone xfer?
func getRealAddr(na net.Addr, sni string, sniPort string) (string, string) {
	h, port := hostPart(na.String())
	ml.ls("Checking for real addr", na, sni, sniPort)
	if (*theConfig)["destPortOverride"].StrVal != "" {
		port = (*theConfig)["destPortOverride"].StrVal
	} else {
		port = sniPort
	}
	if sni != "" {
		return sni, port
	}
	if (*theConfig)["destHostOverride"].StrVal != "" {
		a := strings.Split((*theConfig)["destHostOverride"].StrVal, ",")
		b := a[rand.Intn(len(a))]
		ml.ls("Using random override", b)
		return b, port
	}
	ml.ls("Checking for real addr going with ", na, sni, sniPort, h, port)
	return h, port

}

// checkBan will check a net.Addr for
// being in the ban CIDR
func checkBan(la net.Addr) bool {
	if theCtx.banCidr == nil {
		return false
	}
	h, _ := hostPart(la.String())
	ip := net.ParseIP(h)
	ml.ls("Checking ip, cidr", ip, theCtx.banCidr, theCtx.banCidr.Contains(ip))
	return theCtx.banCidr.Contains(ip)
}

// parseCidr checks cidrStr and does error handling/logging
func parseCidr(cidr **net.IPNet, cidrStr string) {
	_, aCidr, err := net.ParseCIDR(cidrStr)
	ml.la("Parsing", cidrStr, aCidr, err)
	if err == nil {
		*cidr = aCidr
	} else {
		*cidr = nil
		ml.la("Failed to parse Cidr", cidrStr, err)
	}
}

// initConnCtx parses the config for this file
func initConnCtx() {

	// setup CIDR to use tunnel/direct NAT decision
	parseCidr(&theCtx.relayCidr, (*theConfig)["destCidrUseSquid"].StrVal)
	// setup CIDR to block local calls
	parseCidr(&theCtx.banCidr, (*theConfig)["srcCidrBan"].StrVal)

}

// getHttpHost checks a buffer for an HTTP 1 style Host line
func getHTTPHost(data []byte) (string, error) {
	a := strings.Split(string(data), httpNewLine)
	for _, s := range a {
		if len(s) >= len(headerForHost) && s[0:len(headerForHost)] == headerForHost {
			b := strings.Split(s, " ")
			if len(b) >= 1 {
				return b[1], nil
			}
			return "", errors.New("Conn line short" + string(s))
		}
	}
	return "", errors.New("No Host header")
}

// handleConn is a long lived go routine to get connections from listener
func handleConn() {

	// taking from main loop - starts a go routine per connection
	for {
		select {
		case c := <-theCtx.connChan:
			count.Incr("conn-chan-remove")
			go c.run()
		case <-time.After(60 * time.Second):
			count.Incr("conn-chan-idle")
		}
	}
}

// initConn takes an incoming net.Conn and
// returns an initialized connection
func initConn(in net.TCPConn) *connection {
	c := connection{inConn: in}
	numBuffers := (*theConfig)["numBuffers"].IntVal
	c.outBound = make(chan []byte, numBuffers)
	c.inBound = make(chan []byte, numBuffers)
	c.writesDone = make(chan bool, 5)
	c.lock = sync.RWMutex{}
	c.state = waitingFor200
	c.inConn.SetKeepAlive(true)
	c.inConn.SetLinger(5)
	c.inConn.SetNoDelay(true)
	return &c
}

// doneWithConn closes everything safely when done
// can be called multiple times
func (c *connection) doneWithConn() {
	ml.ls("Closing connections")
	time.Sleep(3 * time.Second) // drain time
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.state == closed {
		// log.Println("Skipping due to already closed")
		count.Incr("connection-pairing-dup-close")
		return
	}
	c.writesDone <- true
	c.writesDone <- true
	count.Incr("connection-pairing-closed")
	count.Decr("connection-pairing")
	c.state = closed
	c.inConn.Close()
	c.outConn.Close()
	// I was closign the channel but that panicked go
}

// next four goroutines per connection

// inWriteLoop writes data from the far end
// back to the caller
func (c *connection) inWriteLoop() {
	//log.Println("Starting inWriteLoop for", c)
	//defer log.Println("exiting inWriteLoop for", c)
	//  reads stuff from channel and writes to socket till fd is dead
	defer c.doneWithConn()
	for {
		select {
		case done, ok := <-c.writesDone:
			ml.ls("Writes done", done, ok)
			count.Incr("write-in-done")
			return
		case buffer, ok := <-c.inBound:
			if !ok {
				ml.ls("Write in continuing not ok", ok)
				count.Incr("write-in-not-ok")
				continue
			}
			ml.ln("Going to write inbound", len(buffer))
			total := len(buffer)
			pos := 0
			for pos < total {
				len, err := c.inConn.Write(buffer[pos:total])
				if err != nil {
					count.Incr("write-in-err")
					count.Incr("write-in-err-" + c.remoteHost + ":" + c.remotePort)
					ml.ls("Write in erro", err)
					return
				}
				if len == 0 {
					count.Incr("write-in-zero")
					count.Incr("write-in-zero-" + c.remoteHost + ":" + c.remotePort)
					ml.ls("Write in zero", err)
					return // this has to be return twice I've made it continue and it loops infinitely
				}
				pos += len
			}
			count.IncrDelta("write-in-len", int64(total))
			count.IncrDelta("write-in-len-"+c.remoteHost+":"+c.remotePort, int64(total))
			count.Incr("write-in-ok")
			count.Incr("write-in-ok-" + c.remoteHost + ":" + c.remotePort)
			ml.ln("OK: sent in data",
				total,
				string(buffer[:total]),
				c.state)
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
		case done, ok := <-c.writesDone:
			ml.ls("Writes done", done, ok)
			count.Incr("write-out-done")
			return
		case buffer, ok := <-c.outBound:
			if !ok {
				ml.ls("Returning from outWriteLoop", ok)
				return
			}
			total := len(buffer)
			ml.ln("Got a buffer for out writing",
				total)
			pos := 0
			for pos < total {
				n, err := c.outConn.Write(buffer[pos:total])
				if err != nil {
					count.Incr("write-out-err")
					count.Incr("write-out-err-" + c.remoteHost + ":" + c.remotePort)
					return
				}
				if n == 0 {
					count.Incr("write-out-zero")
					count.Incr("write-out-zero-" + c.remoteHost + ":" + c.remotePort)
					return // keep as return continue breaks things
				}
				pos += n
			}
			count.IncrDelta("write-out-len", int64(total))
			count.IncrDelta("write-out-len-"+c.remoteHost+":"+c.remotePort, int64(total))
			count.Incr("write-out-ok")
			count.Incr("write-out-ok-" + c.remoteHost + ":" + c.remotePort)
			ml.ln("OK: sent out data",
				total,
				string(buffer[:total]),
				c.state)
		}
	}
}

// inReadLoop (for inboud) reads from the far end and sticks into a
// channel
func (c *connection) inReadLoop() {
	ml.ls("Starting inReadLoop for", c)
	defer ml.ls("exiting inReadLoop for", c)

	//  reads stuff from out and writes to channel till fd is dead
	defer c.doneWithConn()
	for {
		var n int
		var buffer = make([]byte, 65536)
		err := c.outConn.SetReadDeadline(time.Now().Add(time.Minute * 15))
		if err == nil {
			ml.ls("About to call read for inReadLoop state", c.state, c)
			n, err = c.outConn.Read(buffer)
		}
		ml.ln("inReadLoop got", n, err)
		if err != nil { // including read timeouts
			if err, ok := err.(net.Error); ok && err.Timeout() {
				count.Incr("read-in-timeout")
				count.Incr("read-in-timeout-" + c.remoteHost + ":" + c.remotePort)
				continue
			} else {
				count.Incr("read-in-read-err")
				count.Incr("read-in-read-err-" + c.remoteHost + ":" + c.remotePort)
				ml.ls("ERROR: inReadLoop got error", err)
				return
			}
		}
		if n == 0 {
			count.Incr("read-in-zero")
			count.Incr("read-in-zero-" + c.remoteHost + ":" + c.remotePort)
			ml.ls("ERROR: inReadLoop got null read")
			continue
		}

		ml.ln("Got data in",
			c,
			c.state,
			n,
			string(buffer[0:n]),
		)
		c.lock.Lock()
		if c.state == waitingFor200 {
			// this will fail for fragmented reads of
			// first line
			newBuf, ok2 := checkFor200(n, buffer)
			ml.ls("Got 200 answer", ok2)
			if !ok2 {
				count.Incr("read-in-bad-header")
				count.Incr("read-in-bad-header-" + c.remoteHost + ":" + c.remotePort)
				c.inConn.Close()
				c.outConn.Close()
				c.lock.Unlock()
				return
			}
			count.Incr("read-in-header")
			ml.ls("Got 200 header")
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
		count.Incr("read-in-ok-" + c.remoteHost + ":" + c.remotePort)
		count.IncrDelta("read-in-len", int64(n))
		count.IncrDelta("read-in-len-"+c.remoteHost+":"+c.remotePort, int64(n))
		ml.ln("Putting the buffer into inboud")
		c.inBound <- buffer[0:n] //  to do non-blocking?
	}
}

// outReadLoop reads from the caller and writes to channel
// for outbound data flow
func (c *connection) outReadLoop() {
	//  reads stuff from in and writes to channel till fd is dead
	ml.ls("Starting in outReadLoop for", c)
	defer ml.ls("exiting outReadLoop for", c)
	defer c.doneWithConn()
	for {
		var n int
		var buffer = make([]byte, 65536)
		err := c.inConn.SetReadDeadline(time.Now().Add(time.Minute * 15))
		if err == nil {
			n, err = c.inConn.Read(buffer)
		}
		ml.ln("Read outReadLoop got", n, err)
		if err != nil {
			if err, ok := err.(net.Error); ok && err.Timeout() {
				count.Incr("read-out-timeout")
				count.Incr("read-out-timeout-" + c.remoteHost + ":" + c.remotePort)
				continue
			} else {
				count.Incr("read-out-read-err")
				count.Incr("read-out-read-err-" + c.remoteHost + ":" + c.remotePort)
				return
			}
		}
		if n == 0 {
			count.Incr("read-out-zero")
			count.Incr("read-out-zero-" + c.remoteHost + ":" + c.remotePort)
			continue
		}
		ml.ln("Got data out",
			n,
			string(buffer[0:n]),
		)
		count.Incr("read-out-ok")
		count.Incr("read-out-ok-" + c.remoteHost + ":" + c.remotePort)
		count.IncrDelta("read-out-len", int64(n))
		count.IncrDelta("read-out-len"+c.remoteHost+":"+c.remotePort, int64(n))
		c.outBound <- buffer[0:n] // to do non-blocking?
	}
}

// run starts up the work on a new connection
func (c *connection) run() {

	// get local address
	la := c.inConn.LocalAddr()
	ra := c.inConn.RemoteAddr()
	ml.ls("LA", la, "RA", ra)
	if checkBan(ra) {
		ml.la("ERROR: LocalAddr banned", ra)
		c.inConn.Close()
		count.Incr("conn-ban")
		return
	}
	h, _ := hostPart(ra.String())
	count.Incr("conn-from-" + h)
	firstRead := make([]byte, 1024)
	err := c.inConn.SetReadDeadline(time.Now().Add(time.Millisecond * 100)) // todo tunable
	var n int
	if err == nil {
		n, err = c.inConn.Read(firstRead)
	}
	ml.ln("Got first read", string(firstRead[:n]))
	if err, ok := err.(net.Error); ok && err.Timeout() {
		ml.ls("ERROR: First read on inbound got timeout", err)
		count.Incr("read-in-first-timeout")
		n = 0
	} else if err != nil {
		ml.ls("ERROR: First read error", err)
		count.Incr("read-in-first-error")
		return
	}
	hasSNI := false
	dstHost, err := parser.GetHostname(firstRead[:n]) // peak for SNI in https
	if err != nil {
		// check for HTTP Host: header in http
		dstHost, err = getHTTPHost(firstRead[:n])
		if err != nil {
			ml.ls("No Host Header:", err)
			dstHost = ""
		} else {
			count.Incr("Host")
			hasSNI = true // not technically but Host is like SNI
			ml.ls("Got Host Header", dstHost)
		}

	} else {
		count.Incr("Sni")
		hasSNI = true
		ml.ls("Got SNI", dstHost)
	}
	// first get NAT address
	dstPort := ""
	host := ""
	port := ""
	c.lock.Lock()
	_, addr, newConn, err := getOriginalDst(&c.inConn) // read NAT stuff
	if err == nil {
		host, port = hostPart(addr)
		ml.ls("Got NAT Addr:", host, port)
		count.Incr("NAT")
	}
	c.inConn = *newConn // even in err case
	c.lock.Unlock()
	if dstHost == "" && (*theConfig)["isNAT"].BoolVal {
		dstPort = port
		dstHost = host
	} else {
		dstPort = "443" // hmm what should this be?
	}
	dstPort = port
	rH, rP := getRealAddr(la, dstHost, dstPort)
	pH, pP := getProxyAddr(la, rH, hasSNI)
	c.remoteHost = rH
	c.remotePort = rP
	count.Incr("connect-out-remote-" + rH)
	if pH == "" {
		count.Incr("direct-connect")
		count.Incr("direct-connect-" + rH)
		cc, err := net.DialTimeout("tcp", rH+":"+rP, 15*time.Second)
		tcpConn, ok := cc.(*net.TCPConn)
		if ok {
			c.outConn = tcpConn
		} else {
			ml.ls("ERROR: connect out got wrong type", err)
			c.inConn.Close()
			return
		}

		if err != nil {
			ml.ls("ERROR: connect out got err", err)
			if err, ok := err.(net.Error); ok && err.Timeout() {
				count.Incr("connect-out-timeout")
				count.Incr("direct-connect-timeout")
				count.Incr("direct-connect-timeout-" + rH)
			}
			count.Incr("connect-out-error")
			count.Incr("direct-connect-error")
			count.Incr("direct-connect-error-" + rH)
			c.inConn.Close()
			if c.outConn != nil {
				c.outConn.Close()
			}
			ml.ls("Closed inConn")
			return
		}
		c.outConn.SetKeepAlive(true)
		c.outConn.SetLinger(5)
		c.outConn.SetNoDelay(true)
		c.state = up
		ml.la("Handling a direct connection",
			c.inConn.RemoteAddr(),
			c.outConn.RemoteAddr(),
		)
		count.Incr("connect-out-good")
		count.Incr("direct-connect-good")
		defer func() {
			go c.outReadLoop() // now start the whole thing
			go c.inWriteLoop()
			go c.outWriteLoop()
		}()

	} else {
		ml.ls("Dial to", pH, pP)
		count.Incr("proxy-connect")
		count.Incr("proxy-connect-" + pH)
		cc, err := net.DialTimeout("tcp", pH+":"+pP, 15*time.Second)
		tcpConn, ok := cc.(*net.TCPConn)
		if ok {
			c.outConn = tcpConn
		} else {
			ml.ls("ERROR: connect out got wrong type", err)
			c.inConn.Close()
			return
		}
		if err != nil {
			ml.ls("ERROR: connect out got err", err)
			if err, ok := err.(net.Error); ok && err.Timeout() {
				count.Incr("connect-out-timeout")
				count.Incr("proxy-connect-timeout")
				count.Incr("proxy-connect-timeout-" + pH)
			} else {
				count.Incr("connect-out-error")
				count.Incr("proxy-connect-error")
				count.Incr("proxy-connect-error" + pH)
			}
			c.inConn.Close()
			if c.outConn != nil {
				c.outConn.Close()
			}
			ml.ls("Closed inConn")
			return
		}
		count.Incr("connect-out-good")
		count.Incr("proxy-connect-good")
		count.Incr("proxy-connect-good-" + pH)
		connS := fmt.Sprintf(
			requestForConnect,
			rH,
			rP,
			rH,
			(*theConfig)["requestHeaderAgentForConnect"].StrVal,
			ra.String(),
		)
		if (*theConfig)["sendConnectLines"].BoolVal == true {
			c.outConn.Write([]byte(connS))
		} else {
			c.state = up
		}
		ml.la("Handling a connection", c.inConn.RemoteAddr(), connS)
	}
	// and stage the fristRead data
	c.outBound <- firstRead[:n]

	// now a goroutine per connection (in and out)
	// data flow thru a per connection
	count.Incr("connection-pairing")
	go c.inReadLoop()

}
