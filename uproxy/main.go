// -*- tab-width: 2 -*-

package main

import (
	"fmt"
	"log"
	"net"
	_ "net/http/pprof" //nolint:gosec
	"os"

	globals "github.com/jayalane/go-globals"
	pb "github.com/jayalane/go-relay/udpProxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

var (
	g             globals.Global
	defaultConfig = `#
port = 8080
debugLevel = network
# network, state, always, or none
profListen = localhost:6060
# comments
`
)

func main() {
	g = globals.NewGlobal(defaultConfig, true)

	// now start the server (copied from gRPC example)
	port := (*g.Cfg)["port"].IntVal

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		panic(fmt.Sprintf("failed to listen: %v", err))
	}

	var opts []grpc.ServerOption

	tls := (*g.Cfg)["tls"].BoolVal
	certFile := (*g.Cfg)["certFile"].StrVal
	keyFile := (*g.Cfg)["keyFile"].StrVal

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
	g.Ml.La("About to start server", grpcServer, lis)

	err = grpcServer.Serve(lis)
	if err != nil {
		g.Ml.La("Failed to start server", err)
		os.Exit(-2)
	}
}
