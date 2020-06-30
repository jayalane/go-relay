// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	"github.com/jayalane/go-tinyconfig"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
)

var ml lll
var theConfig config.Config
var defaultConfig = `#
ports = 5999
isNAT = true
debugLevel = debug
# debug, all or none
squidHost = localhost
squidPort = 3128
destPortOverride = 
destHostOverride = 
destCidrUseConnect = 0.0.0.0/0
numConnectionHandlers = 10000
profListen = localhost:6060
srcCidrBan = 127.0.0.0/8
# comments
`

// global state
type context struct {
	connChan  chan *connection // fed by listener
	done      chan bool
	relayCidr *net.IPNet // in the cidr gets tunnel, out gets direct connect
	banCidr   *net.IPNet // blocks from this cidr to avoid routing calling loops
}

var theCtx context

func main() {

	// config
	if len(os.Args) > 1 && os.Args[1] == "--dumpConfig" {
		fmt.Println(defaultConfig)
		return
	}
	// still config
	var err error
	theConfig, err = config.ReadConfig("config.txt", defaultConfig)
	fmt.Println("Config", theConfig) // lll isn't up yet
	if err != nil {
		fmt.Println("Error opening config.txt", err.Error())
		if theConfig == nil {
			os.Exit(11)
		}
	}
	// low level logging (first so everything rotates)
	ml = initLll("PROXY", theConfig["debugLevel"].StrVal)

	// stats
	count.InitCounters()

	// init the globals
	theCtx.connChan = make(chan *connection, 1000000)
	theCtx.done = make(chan bool, 1)
	fmt.Println("Cidrs", theConfig["destCidrUseConnect"].StrVal)
	fmt.Println("Cidrs", theConfig["srcCidrBan"].StrVal)
	initConnCtx()

	// start go routines
	for i := 0; i < theConfig["numConnectionHandlers"].IntVal; i++ {
		go handleConn()
	}

	// start the profiler
	go func() {
		if len(theConfig["profListen"].StrVal) > 0 {
			ml.la(http.ListenAndServe(theConfig["profListen"].StrVal, nil))
		}
	}()

	// listen
	oneListen := false
	for _, p := range strings.Split(theConfig["ports"].StrVal, ",") {
		port, err := strconv.Atoi(p)
		if err != nil {
			count.Incr("listen-error")
			ml.la("ERROR: can't listen to", p, err) // handle error
			continue
		}
		tcp := net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port}
		ln, err := net.ListenTCP("tcp", &tcp)
		if err != nil {
			count.Incr("listen-error")
			ml.la("ERROR: can't listen to", p, err) // handle error
			continue
		}
		oneListen = true
		ml.la("OK: Listening to", p)
		// listen handler go routine
		go func() {
			for {
				conn, err := ln.AcceptTCP()
				if err != nil {
					count.Incr("accept-error")
					ml.la("ERROR: accept failed", ln, err)
				}
				count.Incr("accept-ok")
				count.Incr("conn-chan-add")
				theCtx.connChan <- initConn(*conn) // todo timeout
			}
		}()
	}
	if !oneListen {
		panic("No listens succeeded")
	}
	// waiting till done - just wait forever I think
	ml.la("Waiting...")
	<-theCtx.done
}
