// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	lll "github.com/jayalane/go-lll"
	"github.com/jayalane/go-tinyconfig"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var ml lll.Lll
var theConfig *config.Config
var defaultConfig = `#
ports = 5999
udpPorts = 53
isNAT = true
debugLevel = debug
# all for only "All" level logging, network for alll network traffic,
# debug for debugging handling the network traffic
sendConnectLines = true
squidHost = localhost
squidPort = 3128
destPortOverride = 
destHostOverride = 
destCidrUseSquid = 0.0.0.0/0
numTcpConnHandlers = 3
numUdpConnHandlers = 3
numBuffers = 100
profListen = localhost:6060
srcCidrBan = 127.0.0.0/8
requestHeaderAgentForConnect = Go-http-client/1.0
udpBufferSize = 8192
numUdpMsgHandlers = 10  // really # of connections to gRPC proxies
udpPorts = 5999
# comments
`

// global state
type context struct {
	tcpConnChan chan *tcpConn // fed by listener
	udpConnChan chan *udpConn // fed by reader
	done        chan bool
	reload      chan os.Signal // to reload config
	relayCidr   *net.IPNet     // in the cidr gets tunnel, out gets direct connect
	banCidr     *net.IPNet     // blocks from this cidr to avoid routing calling loops
}

var theCtx context

func reloadHandler() {
	for {
		select {
		case c := <-theCtx.reload:
			ml.La("OK: Got a signal, reloading config", c)

			t, err := config.ReadConfig("config.txt", defaultConfig)
			if err != nil {
				fmt.Println("Error opening config.txt", err.Error())
				return
			}
			st := unsafe.Pointer(theConfig)
			atomic.StorePointer(&st, unsafe.Pointer(&t))
			fmt.Println("New Config", (*theConfig)) // lll isn't up yet
			lll.SetLevel(&ml, (*theConfig)["debugLevel"].StrVal)
			initConnCtx() // to allow reloading the CIDRs
		}
	}
}

func main() {

	// config
	if len(os.Args) > 1 && os.Args[1] == "--dumpConfig" {
		fmt.Println(defaultConfig)
		return
	}
	// still config
	theConfig = nil
	t, err := config.ReadConfig("config.txt", defaultConfig)
	if err != nil {
		fmt.Println("Error opening config.txt", err.Error())
		if theConfig == nil {
			os.Exit(11)
		}
	}
	theConfig = &t
	fmt.Println("Config", (*theConfig)) // lll isn't up yet

	// low level logging (first so everything rotates)
	ml = lll.Init("PROXY", (*theConfig)["debugLevel"].StrVal)

	// config sig handlers - to enable log levels
	theCtx.reload = make(chan os.Signal, 2)
	signal.Notify(theCtx.reload, syscall.SIGHUP)
	go reloadHandler() // to listen to the signal

	// stats
	count.InitCounters()

	// init the globals
	numTCPConnHand := (*theConfig)["numTcpConnHandlers"].IntVal
	numUDPConnHand := (*theConfig)["numUdpConnHandlers"].IntVal
	// this is illogical coupling between # go routines and buffer size
	theCtx.tcpConnChan = make(chan *tcpConn, numTCPConnHand)
	theCtx.udpConnChan = make(chan *udpConn, numUDPConnHand)
	theCtx.done = make(chan bool, 1)
	fmt.Println("Cidrs", (*theConfig)["destCidrUseSquid"].StrVal)
	fmt.Println("Cidrs", (*theConfig)["srcCidrBan"].StrVal)
	initConnCtx()

	// start the profiler
	go func() {
		if len((*theConfig)["profListen"].StrVal) > 0 {
			ml.La(http.ListenAndServe((*theConfig)["profListen"].StrVal, nil))
		}
	}()

	// tcp listen
	tcpHandler()

	// udp too now
	udpHandler()

	// waiting till done - just wait forever I think
	ml.La("Waiting...")
	<-theCtx.done
}
