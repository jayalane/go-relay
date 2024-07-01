// -*- tab-width: 2 -*-

package main

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	count "github.com/jayalane/go-counter"
	"github.com/paultag/sniff/parser"
)

type connState int

const (
	waitingFor200 = iota
	up
	closed
)

const (
	two        = 2
	three      = 3
	five       = 5
	oneHundred = 100
	oneK       = 1024

	sixtyfourK = 65535
)

// info about a TCP Connection.
type tcpConn struct {
	lock       sync.RWMutex
	state      connState
	inConn     net.TCPConn
	outConn    *net.TCPConn
	remoteHost string
	remotePort string
	writesDone chan bool
	outBound   chan []byte
	inBound    chan []byte
}

const (
	headerForHost     = "Host: "
	httpNewLine       = "\r\n"
	requestForConnect = "CONNECT %s:%s HTTP/1.0\r\nHost: %s\r\nUser-Agent: %s\r\nX-Forwarded-For: %s\r\n\r\n"
)

// utility functions then go routines start then class methods for
// tcpConn

// dialWithProxy dials out to the endpoint with a given proxy
// returns connection, connection String, and error.
func dialWithProxy(pH string, pP string,
	rH string, rP string,
	remoteAddr *net.Addr,
	timeout time.Duration,
) (net.Conn, string, error) {
	cc, err := net.DialTimeout("tcp", pH+":"+pP, timeout)
	tcpConn, ok := cc.(*net.TCPConn)

	var conn *net.TCPConn

	if ok {
		conn = tcpConn
	} else {
		g.Ml.Ls("error: connect out got wrong type", err)

		return nil, "", errors.New("connect out got wrong type") //nolint:err113
	}

	if err != nil {
		g.Ml.Ls("error: connect out got err", err)

		if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
			count.Incr("connect-out-timeout")
			count.Incr("proxy-connect-timeout")
			count.Incr("proxy-connect-timeout-" + pH)
		} else {
			count.Incr("connect-out-error")
			count.Incr("proxy-connect-error")
			count.Incr("proxy-connect-error" + pH)
		}

		if conn != nil {
			conn.Close()
		}

		g.Ml.Ls("Closed inConn")

		return nil, "", err
	}

	count.Incr("connect-out-good")
	count.Incr("proxy-connect-good")
	count.Incr("proxy-connect-good-" + pH)

	ra := ""

	if remoteAddr == nil {
		ra = "127.0.0.1"
	} else {
		ra = (*remoteAddr).String()
	}

	connS := fmt.Sprintf(
		requestForConnect,
		rH,
		rP,
		rH,
		(*g.Cfg)["requestHeaderAgentForConnect"].StrVal,
		ra,
	)

	if (*g.Cfg)["sendConnectLines"].BoolVal {
		g.Ml.Ln("Sending", connS)

		_, err = conn.Write([]byte(connS)) // check for error?
		if err != nil {
			count.Incr("proxy-connect-write-error")
			count.Incr("proxy-connect-write-error" + pH)
			conn.Close()

			return nil, "", err
		}
	}

	return conn, connS, nil
}

// constHttp200Header returns a constant list of possible 200 status headers.
func constHTTP200Header() []string {
	return []string{
		"HTTP/1.0 200 Connection",
		"HTTP/1.1 200 Connection",
	}
}

// checkFor200 takes a buffer and sees if it was a successful
// connect reply.  Returns the remainder (if any) and ok set to
// true on success or ”, false on failure.
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
		res := []byte("")
		if rest < len(a) {
			res = []byte(strings.Join(a[rest:], httpNewLine))
		}

		return res, ok
	}

	return []byte(""), false
}

// hostPart returns just the host part of a host:port string.
func hostPart(ad string) (string, string) {
	s := strings.Split(ad, ":")
	if len(s) < two {
		return "", s[len(s)-1]
	}

	return strings.Join(s[:len(s)-1], ""), s[len(s)-1]
}

// getProxyAddr will return the squid endpoint.
func getProxyAddr(na net.Addr, dst string, isSNI bool) (string, string) {
	if isSNI {
		return (*g.Cfg)["squidHost"].StrVal, (*g.Cfg)["squidPort"].StrVal
	}

	ip := net.ParseIP(dst)

	g.Ml.Ls("Checking for squid", na, dst, ip, theCtx.relayCidr)

	if theCtx.relayCidr == nil || theCtx.relayCidr.Contains(ip) {
		// if no config, always use squid
		g.Ml.Ls("Checks ok use squid ", ip, theCtx.relayCidr)

		return (*g.Cfg)["squidHost"].StrVal, (*g.Cfg)["squidPort"].StrVal
	}

	if isSNI && (*g.Cfg)["squidForSNI"].BoolVal {
		g.Ml.Ls("SNI using squid", ip, isSNI)

		return (*g.Cfg)["squidHost"].StrVal, (*g.Cfg)["squidPort"].StrVal
	}

	return "", ""
}

// getRealAddr will look up the true hostname for the CONNECT call
// - looking at NAT, SNI and Host: line - overrideable via config.
func getRealAddr(na net.Addr, sni string, sniPort string) (string, string) {
	var port string

	h, _ := hostPart(na.String())

	g.Ml.Ls("Checking for real addr", na, sni, sniPort)

	if (*g.Cfg)["destPortOverride"].StrVal != "" {
		port = (*g.Cfg)["destPortOverride"].StrVal
	} else {
		port = sniPort
	}

	if (*g.Cfg)["destHostOverride"].StrVal != "" {
		a := strings.Split((*g.Cfg)["destHostOverride"].StrVal, ",")
		b := a[rand.Intn(len(a))] //nolint:gosec
		g.Ml.Ls("Using random override", b)

		return b, port
	}

	if sni != "" {
		return sni, port
	}

	g.Ml.Ls("Checking for real addr going with ", na, sni, sniPort, h, port)

	return h, port
}

// checkBan will check a net.Addr for
// being in the ban CIDR.
func checkBan(la net.Addr) bool {
	if theCtx.banCidr == nil {
		return false
	}

	h, _ := hostPart(la.String())
	ip := net.ParseIP(h)

	g.Ml.Ls("Checking ip, cidr", ip, theCtx.banCidr, theCtx.banCidr.Contains(ip))

	return theCtx.banCidr.Contains(ip)
}

// parseCidr checks cidrStr and does error handling/logging.
func parseCidr(cidr **net.IPNet, cidrStr string) {
	_, aCidr, err := net.ParseCIDR(cidrStr)

	g.Ml.La("Parsing", cidrStr, aCidr, err)

	if err == nil {
		*cidr = aCidr
	} else {
		*cidr = nil

		g.Ml.La("Failed to parse Cidr", cidrStr, err)
	}
}

// initConnCtx parses the configs for this file for TCP.
func initConnCtx() {
	// setup CIDR to use tunnel/direct NAT decision
	parseCidr(&theCtx.relayCidr, (*g.Cfg)["destCidrUseSquid"].StrVal)
	// setup CIDR to block local calls
	parseCidr(&theCtx.banCidr, (*g.Cfg)["srcCidrBan"].StrVal)
}

// getHttpHost checks a buffer for an HTTP 1 style Host line.
func getHTTPHost(data []byte) (string, error) {
	a := strings.Split(string(data), httpNewLine)

	for _, s := range a {
		if len(s) >= len(headerForHost) && s[0:len(headerForHost)] == headerForHost {
			b := strings.Split(s, " ")
			if len(b) >= 1 {
				return b[1], nil
			}

			return "", errors.New("conn line short" + s) //nolint:err113
		}
	}

	return "", errors.New("no Host header") //nolint:err113
}

// tcpHandler sets up the listens and a goroutine for accepting.
func startTCPHandler() {
	oneListen := false

	for _, p := range strings.Split((*g.Cfg)["ports"].StrVal, ",") {
		port, err := strconv.Atoi(p)
		if err != nil {
			count.Incr("listen-error")
			g.Ml.La("ERROR: can't listen to", p, err) // handle error

			continue
		}

		tcp := net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port}

		ln, err := net.ListenTCP("tcp", &tcp)
		if err != nil {
			count.Incr("listen-error")
			g.Ml.La("ERROR: can't listen to", p, err) // handle error

			continue
		}

		oneListen = true

		g.Ml.La("OK: Listening to", p)

		// listen handler go routine
		go func() {
			for {
				conn, err := ln.AcceptTCP()
				if err != nil {
					count.Incr("accept-error")
					g.Ml.La("ERROR: accept failed", ln, err)

					panic("Can't accpet -probably out of FDs")
				}

				count.Incr("accept-ok")
				count.Incr("conn-chan-add")
				theCtx.tcpConnChan <- newTCP(*conn) // later timeout
			}
		}()
	}

	if !oneListen {
		panic("No listens succeeded")
	}

	// start go routines
	for range (*g.Cfg)["numTcpConnHandlers"].IntVal {
		g.Ml.La("Starting handleConn")

		go handleConn()
	}
}

// handleConn is a long lived go routine to get connections from listener.
func handleConn() {
	// taking from main loop - starts a go routine per connection
	for {
		select {
		case c := <-theCtx.tcpConnChan:
			fmt.Print("Read one accepted", c)
			count.Incr("conn-chan-remove")

			go c.run()
		case <-time.After(time.Minute):
			count.Incr("conn-chan-idle")
		}
	}
}

// newTCP takes an incoming net.Conn and
// returns an initialized connection.
func newTCP(in net.TCPConn) *tcpConn {
	c := tcpConn{inConn: in}

	numBuffers := (*g.Cfg)["numBuffers"].IntVal

	c.outBound = make(chan []byte, numBuffers)
	c.inBound = make(chan []byte, numBuffers)
	c.writesDone = make(chan bool, five)
	c.lock = sync.RWMutex{}
	c.state = waitingFor200

	_ = c.inConn.SetKeepAlive(true)
	_ = c.inConn.SetLinger(five)
	_ = c.inConn.SetNoDelay(true)

	return &c
}

// now the tcpConn class methods

// doneWithConn closes everything safely when done
// can be called multiple times.
func (c *tcpConn) doneWithConn() {
	g.Ml.Ls("Closing connections")
	time.Sleep(three * time.Second) // drain time
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.state == closed {
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
}

// next four goroutines per tcpConn

// inWriteLoop writes data from the far end
// back to the caller.
func (c *tcpConn) inWriteLoop() {
	//  reads stuff from channel and writes to socket till fd is dead
	defer c.doneWithConn()

	for {
		select {
		case done, ok := <-c.writesDone:
			g.Ml.Ls("Writes done", done, ok)
			count.Incr("write-in-done")

			return
		case buffer, ok := <-c.inBound:
			if !ok {
				g.Ml.Ls("Write in continuing not ok", ok)
				count.Incr("write-in-not-ok")

				continue
			}

			g.Ml.Ln("Going to write inbound", len(buffer))

			total := len(buffer)
			pos := 0

			for pos < total {
				theLen, err := c.inConn.Write(buffer[pos:total])
				if err != nil {
					count.Incr("write-in-err")
					count.Incr("write-in-err-" + c.remoteHost + ":" + c.remotePort)
					g.Ml.Ls("Write in erro", err)

					return
				}

				if theLen == 0 {
					count.Incr("write-in-zero")
					count.Incr("write-in-zero-" + c.remoteHost + ":" + c.remotePort)
					g.Ml.Ls("Write in zero", err)

					return // this has to be return twice I've made it continue and it loops infinitely
				}

				pos += theLen
			}
			count.IncrDelta("write-in-len", int64(total))
			count.IncrDelta("write-in-len-"+c.remoteHost+":"+c.remotePort, int64(total))
			count.Incr("write-in-ok")
			count.Incr("write-in-ok-" + c.remoteHost + ":" + c.remotePort)

			g.Ml.Ln(
				"OK: sent in data",
				total,
				string(buffer[:total]),
				c.state,
			)
		}
	}
}

// outWriteLoop reads from the channel and writes to the far end.
func (c *tcpConn) outWriteLoop() {
	//  reads stuff from channel and writes to out socket
	// log.Println("Starting outWriteLoop for", c)
	// +++++defer log.Println("exiting outWriteLoop for", c)
	defer c.doneWithConn()

	for {
		select {
		case done, ok := <-c.writesDone:
			g.Ml.Ls("Writes done", done, ok)
			count.Incr("write-out-done")

			return
		case buffer, ok := <-c.outBound:
			if !ok {
				g.Ml.Ls("Returning from outWriteLoop", ok)

				return
			}

			total := len(buffer)

			g.Ml.Ln(
				"Got a buffer for out writing",
				total,
			)

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

			g.Ml.Ln(
				"OK: sent out data",
				total,
				string(buffer[:total]),
				c.state,
			)
		}
	}
}

// inReadLoop (for inboud) reads from the far end and sticks into a
// channel.
func (c *tcpConn) inReadLoop() {
	g.Ml.Ls("Starting inReadLoop for", c)
	defer g.Ml.Ls("exiting inReadLoop for", c)

	//  reads stuff from out and writes to channel till fd is dead
	defer c.doneWithConn()

	for {
		var n int

		buffer := make([]byte, sixtyfourK)
		err := c.outConn.SetReadDeadline(time.Now().Add(time.Minute * 15)) //nolint:mnd

		if err == nil {
			g.Ml.Ls("About to call read for inReadLoop state", c.state, c)
			n, err = c.outConn.Read(buffer)
		}

		g.Ml.Ln("inReadLoop got", n, err)

		if err != nil { // including read timeouts
			if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
				count.Incr("read-in-timeout")
				count.Incr("read-in-timeout-" + c.remoteHost + ":" + c.remotePort)

				continue
			}

			count.Incr("read-in-read-err")
			count.Incr("read-in-read-err-" + c.remoteHost + ":" + c.remotePort)
			g.Ml.Ls("ERROR: inReadLoop got error", err)

			return
		}

		if n == 0 {
			count.Incr("read-in-zero")
			count.Incr("read-in-zero-" + c.remoteHost + ":" + c.remotePort)
			g.Ml.Ls("ERROR: inReadLoop got null read")

			continue
		}

		g.Ml.Ln(
			"Got data in",
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

			g.Ml.Ls("Got 200 answer", ok2)

			if !ok2 {
				count.Incr("read-in-bad-header")
				count.Incr("read-in-bad-header-" + c.remoteHost + ":" + c.remotePort)
				c.inConn.Close()
				c.outConn.Close()
				c.lock.Unlock()

				return
			}

			count.Incr("read-in-header")
			g.Ml.Ls("Got 200 header")

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
		g.Ml.Ln("Putting the buffer into inboud")

		c.inBound <- buffer[0:n] //  to do non-blocking?
	}
}

// outReadLoop reads from the caller and writes to channel
// for outbound data flow.
func (c *tcpConn) outReadLoop() {
	//  reads stuff from in and writes to channel till fd is dead
	g.Ml.Ls("Starting in outReadLoop for", c)

	defer g.Ml.Ls("exiting outReadLoop for", c)
	defer c.doneWithConn()

	for {
		var n int

		buffer := make([]byte, sixtyfourK)

		err := c.inConn.SetReadDeadline(time.Now().Add(time.Minute * 15)) //nolint:mnd
		if err == nil {
			n, err = c.inConn.Read(buffer)
		}

		g.Ml.Ln("Read outReadLoop got", n, err)

		if err != nil {
			if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
				count.Incr("read-out-timeout")
				count.Incr("read-out-timeout-" + c.remoteHost + ":" + c.remotePort)

				continue
			}

			count.Incr("read-out-read-err")
			count.Incr("read-out-read-err-" + c.remoteHost + ":" + c.remotePort)

			return
		}

		if n == 0 {
			count.Incr("read-out-zero")
			count.Incr("read-out-zero-" + c.remoteHost + ":" + c.remotePort)

			continue
		}

		g.Ml.Ln("Got data out",
			n,
			string(buffer[0:n]),
		)

		count.Incr("read-out-ok")
		count.Incr("read-out-ok-" + c.remoteHost + ":" + c.remotePort)
		count.IncrDelta("read-out-len", int64(n))
		count.IncrDelta("read-out-len-"+c.remoteHost+":"+c.remotePort, int64(n))
		c.outBound <- buffer[0:n] // to do non-blocking?
	}
}

/*
// setIPTransparent makes the syscall to
// make localaddr be the original NATted
// destination
func (c *tcpConn) setIPTransparent() {

	rs, err := c.inConn.SyscallConn()
	if err != nil {
		tl.La("ERROR: can't get syscall conn", err)
		return
	}

	_ = rs.Control(func(fd uintptr) {
		var err error

		err = syscall.SetsockoptInt(int(fd),
			syscall.SOL_IP,
			syscall.IP_TRANSPARENT,
			1)
		if err != nil {
			tl.La("can't set sock opt", err)
		}
		return
	})
}
*/

// run starts up the work on a new connection.
func (c *tcpConn) run() { //nolint:gocognit,cyclop
	// get local address
	la := c.inConn.LocalAddr()
	ra := c.inConn.RemoteAddr()

	g.Ml.Ls("LA", la, "RA", ra)

	if checkBan(ra) {
		g.Ml.La("ERROR: LocalAddr banned", ra)
		c.inConn.Close()
		count.Incr("conn-ban")

		return
	}

	h, _ := hostPart(ra.String())

	count.Incr("conn-from-" + h)

	firstRead := make([]byte, oneK)

	err := c.inConn.SetReadDeadline(time.Now().Add(time.Millisecond * oneHundred)) // later tunable

	var n int

	if err == nil {
		n, err = c.inConn.Read(firstRead)
	}

	g.Ml.Ln("Got first read", string(firstRead[:n]))

	if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
		g.Ml.Ls("error: First read on inbound got timeout", err)
		count.Incr("read-in-first-timeout")

		n = 0
	} else if err != nil {
		g.Ml.Ls("error: First read error", err)
		count.Incr("read-in-first-error")

		return
	}

	hasSNI := false

	dstHost, err := parser.GetHostname(firstRead[:n]) // peak for SNI in https
	if err != nil {
		// check for HTTP Host: header in http
		dstHost, err = getHTTPHost(firstRead[:n])
		if err != nil {
			g.Ml.Ls("No Host Header:", err)

			dstHost = ""
		} else {
			count.Incr("Host")

			hasSNI = true // not technically but Host is like SNI

			g.Ml.Ls("Got Host Header", dstHost)
		}
	} else {
		count.Incr("Sni")

		hasSNI = true

		g.Ml.Ls("Got SNI", dstHost)
	}

	// first get NAT address
	dstPort := ""
	host := ""
	port := ""

	c.lock.Lock()

	_, addr, newConn, err := getOriginalDst(&c.inConn) // read NAT stuff
	if err == nil {
		host, port = hostPart(addr)

		g.Ml.Ls("Got NAT Addr:", host, port)
		count.Incr("NAT")
	}

	c.inConn = *newConn // even in err case
	c.lock.Unlock()

	if dstHost == "" && (*g.Cfg)["isNAT"].BoolVal {
		dstHost = host
	}

	dstPort = port

	rH, rP := getRealAddr(la, dstHost, dstPort)
	pH, pP := getProxyAddr(la, rH, hasSNI)

	c.remoteHost = rH
	c.remotePort = rP

	count.Incr("connect-out-remote-" + rH)

	if pH == "" { //nolint:nestif
		count.Incr("direct-connect")
		count.Incr("direct-connect-" + rH)

		cc, err := net.DialTimeout("tcp", rH+":"+rP, 15*time.Second) //nolint:mnd

		tcpConn, ok := cc.(*net.TCPConn)
		if ok {
			c.outConn = tcpConn
		} else {
			g.Ml.Ls("ERROR: connect out got wrong type", err, fmt.Sprintf("%T", cc))
			c.inConn.Close()

			return
		}

		if err != nil {
			g.Ml.Ls("ERROR: connect out got err", err)

			if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
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

			g.Ml.Ls("Closed inConn")

			return
		}

		_ = c.outConn.SetKeepAlive(true)
		_ = c.outConn.SetLinger(five)
		_ = c.outConn.SetNoDelay(true)
		c.state = up
		g.Ml.La("Handling a direct connection",
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
		g.Ml.Ls("Dial to", pH, pP)

		count.Incr("proxy-connect")
		count.Incr("proxy-connect-" + pH)

		cc, ConnS, err := dialWithProxy(pH, pP, rH, rP, &ra, 15*time.Second) //nolint:mnd
		if len(ConnS) > 0 {
			g.Ml.Ls("Setting conn into waiting for 200")

			c.state = waitingFor200
		} else {
			g.Ml.Ls("Setting conn into up")

			c.state = up
		}

		tcpConn, ok := cc.(*net.TCPConn)
		if ok {
			c.outConn = tcpConn
		} else {
			g.Ml.Ls("ERROR: connect out got wrong type", err, fmt.Sprintf("%T", cc))
			c.inConn.Close()
			c.state = closed

			return
		}

		if err != nil {
			c.inConn.Close()

			g.Ml.Ls("Closed inConn")

			if c.outConn != nil {
				c.outConn.Close()
				c.state = closed
			}

			g.Ml.Ls("Closed inConn")

			return
		}

		g.Ml.La("Handling a connection", c.state, c.inConn.RemoteAddr(), ConnS)
	}
	// and stage the fristRead data
	c.outBound <- firstRead[:n]
	g.Ml.La("About to start in read loop, state", c.state)
	// now a goroutine per connection (in and out)
	// data flow thru a per connection
	count.Incr("connection-pairing")

	go c.inReadLoop()
}
