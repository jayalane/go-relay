// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	count "github.com/jayalane/go-counter"
	lll "github.com/jayalane/go-lll"
	pb "github.com/jayalane/go-relay/udpProxy"
	"github.com/jayalane/go-tinyconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var ml *lll.Lll
var theConfig *config.Config
var defaultConfig = `#
port = 8080
debugLevel = network
# network, state, always, or none
profListen = localhost:6060
# comments
`

// global state
type serverContext struct {
	reload chan os.Signal // to reload config
}

var theCtx serverContext

func reloadHandler() {
	for c := range theCtx.reload {
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

	// start the profiler
	go func() {
		if len((*theConfig)["profListen"].StrVal) > 0 {
			ml.La(http.ListenAndServe((*theConfig)["profListen"].StrVal, nil))
		}
	}()

	// now start the server (copied from gRPC example)
	port := (*theConfig)["port"].IntVal
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		panic(fmt.Sprintf("failed to listen: %v", err))
	}
	var opts []grpc.ServerOption
	tls := (*theConfig)["tls"].BoolVal
	certFile := (*theConfig)["certFile"].StrVal
	keyFile := (*theConfig)["keyFile"].StrVal
	if tls {
		if certFile == "" {
			certFile = "x509/server_cert.pem"
		}
		if keyFile == "" {
			keyFile = "x509/server_key.pem"
		}
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterProxyServer(grpcServer, newProxyServer())
	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)
	ml.La("About to start server", grpcServer, lis)
	err = grpcServer.Serve(lis)
	if err != nil {
		ml.La("Failed to start server", err)
		os.Exit(-2)
	}
}
