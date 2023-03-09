module github.com/jayalane/go-relay/uproxy

go 1.20

replace github.com/jayalane/go-relay/udpProxy => ../udpProxy

require (
	github.com/jayalane/go-counter v0.0.0-20230309045251-696a3bbcd44e
	github.com/jayalane/go-lll v0.0.0-20230309045553-4d9872d4d53e
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-20230309045147-8267dc4d6067
	google.golang.org/grpc v1.53.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.5.0 // indirect
	golang.org/x/sys v0.4.0 // indirect
	golang.org/x/text v0.6.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
