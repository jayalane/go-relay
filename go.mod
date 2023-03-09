module github.com/jayalane/go-relay

replace github.com/jayalane/go-relay/udpProxy => ./udpProxy

go 1.20

require (
	github.com/LiamHaworth/go-tproxy v0.0.0-20190726054950-ef7efd7f24ed
	github.com/jayalane/go-counter v0.0.0-20230309045251-696a3bbcd44e
	github.com/jayalane/go-lll v0.0.0-20230309045553-4d9872d4d53e
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-20230309045147-8267dc4d6067
	github.com/paultag/sniff v0.0.0-20200207005214-cf7e4d167732
	google.golang.org/grpc v1.53.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.29.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
