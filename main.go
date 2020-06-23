// -*- tab-width: 2 -*-

package main

import (
	count "github.com/jayalane/go-counter"
	"github.com/jayalane/go-tinyconfig"
	"github.com/lestrrat-go/file-rotatelogs"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
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
# comments
`

// global state
type context struct {
	connChan  chan *connection // fed by listener
	done      chan bool
	relayCidr *net.IPNet
}

var theCtx context

func main() {

	logPathTemplate := "/var/log/proxy.log.%Y%m%d"
	u, err := user.Current()
	if err != nil {
		log.Panic("Can't check user id")
	}
	if u.Uid != "0" {
		logPathTemplate = "./proxy.log.%Y%m%d"
	}
	// init rotating logs
	r1, err := rotatelogs.New(
		logPathTemplate,
		rotatelogs.WithMaxAge(time.Hour*168),
	)
	if err != nil {
		log.Panic("Can't open rotating logs")
	}
	log.SetOutput(r1)

	// stats
	count.InitCounters()

	// config
	if len(os.Args) > 1 && os.Args[1] == "--dumpConfig" {
		log.Println(defaultConfig)
		return
	}
	// still config
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
	ml = initLll("PROXY", theConfig["debugLevel"].StrVal)

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

	// listen
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
		// listen handler go routine
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
	log.Println("Waiting...")
	<-theCtx.done
}
