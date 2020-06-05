// -*- tab-width: 2 -*-

package main

import (
	count "github.com/jayalane/go-counter"
	"github.com/jayalane/go-tinyconfig"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"
)

var theConfig config.Config
var defaultConfig = `#
ports = 5999
isNAT = true
squidHost = localhost
squidPort = 3128
destHostMethod = none
numConnectionHandlers = 10000
profListen = localhost:6060
# comments
`

// global state
type context struct {
	connChan chan *connection // fed by listener
	done     chan bool
}

var theCtx context

// then the listener go routine

// go routine to get connections from listener
func handleConn() {
	for {
		select {
		case c := <-theCtx.connChan:
			count.Incr("conn-chan-remove")
			c.run() // have to think the data flow here
		case <-time.After(60 * time.Second):
			count.Incr("conn-chan-idle")
		}
	}
}

func main() {
	// stats
	count.InitCounters()
	// config
	if len(os.Args) > 1 && os.Args[1] == "--dumpConfig" {
		log.Println(defaultConfig)
		return
	}
	// still config
	var err error
	theConfig, err = config.ReadConfig("config.txt", defaultConfig)
	log.Println("Config", theConfig)
	if err != nil {
		log.Println("Error opening config.txt", err.Error())
		if theConfig == nil {
			os.Exit(11)
		}
	}
	if err != nil {
		log.Println("Error opening config.txt", err.Error())
		if theConfig == nil {
			os.Exit(11)
		}
	}
	// init the globals
	theCtx.connChan = make(chan *connection, 1000000)
	theCtx.done = make(chan bool, 1)

	// start go routines
	for i := 0; i < theConfig["numConnectionHandlers"].IntVal; i++ {
		go handleConn()
	}

	// start the profiler
	go func() {
		if len(theConfig["profListen"].StrVal) > 0 {
			log.Println(http.ListenAndServe(theConfig["profListen"].StrVal, nil))
		}
	}()

	for _, p := range strings.Split(theConfig["ports"].StrVal, ",") {
		port, err := strconv.Atoi(p)
		if err != nil {
			count.Incr("listen-error")
			log.Println("ERROR: can't listen to", p, err) // handle error
			continue
		}
		tcp := net.TCPAddr{net.IPv4(0, 0, 0, 0), port, ""}
		ln, err := net.ListenTCP("tcp", &tcp)
		if err != nil {
			count.Incr("listen-error")
			log.Println("ERROR: can't listen to", p, err) // handle error
			continue
		}
		log.Println("OK: Listening to", p)
		go func() {
			for {
				conn, err := ln.AcceptTCP()
				if err != nil {
					count.Incr("accept-error")
					log.Println("ERROR: accept failed", ln, err)
				}
				count.Incr("accept-ok")
				count.Incr("conn-chan-add")
				theCtx.connChan <- initConn(*conn) // todo timeout
			}
		}()
	}
	// waiting till done - just wait forever I think
	<-theCtx.done
}
