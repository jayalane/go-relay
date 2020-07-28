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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

var ml lll
var theConfig *config.Config
var defaultConfig = `#
ports = 5999
isNAT = true
debugLevel = debug
# debug, all or none
sendConnectLines = true
squidHost = localhost
squidPort = 3128
destPortOverride = 
destHostOverride = 
destCidrUseSquid = 0.0.0.0/0
numConnectionHandlers = 3
numBuffers = 100
profListen = localhost:6060
srcCidrBan = 127.0.0.0/8
# comments
`

// global state
type context struct {
	connChan  chan *connection // fed by listener
	done      chan bool
	reload    chan os.Signal // to reload config
	relayCidr *net.IPNet     // in the cidr gets tunnel, out gets direct connect
	banCidr   *net.IPNet     // blocks from this cidr to avoid routing calling loops
}

var theCtx context

func reloadHandler() {
	for {
		select {
		case c := <-theCtx.reload:
			ml.la("OK: Got a signal, reloading config", c)

			(*theConfig) = nil
			t, err := config.ReadConfig("config.txt", defaultConfig)
			if err != nil {
				fmt.Println("Error opening config.txt", err.Error())
				return
			}
			theConfig = &t                          // is this atomic?
			fmt.Println("New Config", (*theConfig)) // lll isn't up yet
			lllSetLevel(&ml, (*theConfig)["debugLevel"].StrVal)
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
	ml = initLll("PROXY", (*theConfig)["debugLevel"].StrVal)

	// config sig handlers - to enable log levels
	theCtx.reload = make(chan os.Signal, 2)
	signal.Notify(theCtx.reload, syscall.SIGHUP)
	go reloadHandler() // to listen to the signal

	// stats
	count.InitCounters()

	// init the globals
	numConnHand := (*theConfig)["numConnectionHandler"].IntVal
	theCtx.connChan = make(chan *connection, numConnHand)
	theCtx.done = make(chan bool, 1)
	fmt.Println("Cidrs", (*theConfig)["destCidrUseSquid"].StrVal)
	fmt.Println("Cidrs", (*theConfig)["srcCidrBan"].StrVal)
	initConnCtx()

	// start go routines
	for i := 0; i < (*theConfig)["numConnectionHandlers"].IntVal; i++ {
		go handleConn()
	}

	// start the profiler
	go func() {
		if len((*theConfig)["profListen"].StrVal) > 0 {
			ml.la(http.ListenAndServe((*theConfig)["profListen"].StrVal, nil))
		}
	}()

	// listen
	oneListen := false
	for _, p := range strings.Split((*theConfig)["ports"].StrVal, ",") {
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
					panic("Can't accpet -probably out of FDs")
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
