// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	"net"
	_ "net/http/pprof" //nolint:gosec

	globals "github.com/jayalane/go-globals"
)

var (
	g             globals.Global
	defaultConfig = `#
ports = 5999
udpPorts = 5999
udpBufferSize = 8192
udpOobBufferSize = 8192
isNAT = true
debugLevel = network
# network, state, always, or none
sendConnectLines = true
squidHost = localhost
squidPort = 4128
squidForSNI = true
uproxyHost = localhost
uproxyPort = 8080
destPortOverride = 
destHostOverride = 
destCidrUseSquid = 0.0.0.0/0
numTcpConnHandlers = 3
numUdpMsgHandlers = 3
numBuffers = 100
profListen = localhost:6060
srcCidrBan = 127.0.0.0/8
requestHeaderAgentForConnect = Go-http-client/1.0
# comments
`
)

// global state.
type serverContext struct {
	tcpConnChan chan *tcpConn // fed by listener
	udpMsgChan  chan *udpMsg  // fed by reader
	done        chan bool
	relayCidr   *net.IPNet // in the cidr gets tunnel, out gets direct connect
	banCidr     *net.IPNet // blocks from this cidr to avoid routing calling loops
}

var theCtx serverContext

func main() {
	g = globals.NewGlobal(defaultConfig, true)

	// init the globals
	numTCPConnHand := (*g.Cfg)["numTcpConnHandlers"].IntVal
	numUDPConnHand := (*g.Cfg)["numUdpConnHandlers"].IntVal

	// this is illogical coupling between # go routines and buffer size
	theCtx.tcpConnChan = make(chan *tcpConn, numTCPConnHand)
	theCtx.udpMsgChan = make(chan *udpMsg, numUDPConnHand)
	theCtx.done = make(chan bool, 1)

	fmt.Println("Cidrs", (*g.Cfg)["destCidrUseSquid"].StrVal)
	fmt.Println("Cidrs", (*g.Cfg)["srcCidrBan"].StrVal)
	initConnCtx()

	// tcp listen
	startTCPHandler()

	// udp too now
	startUDPHandler()

	// waiting till done - just wait forever I think
	g.Ml.La("Waiting...")
	<-theCtx.done
}
