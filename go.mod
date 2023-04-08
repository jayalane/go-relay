module github.com/jayalane/go-relay

go 1.20

replace github.com/jayalane/go-relay/udpProxy => ./udpProxy

require (
	github.com/LiamHaworth/go-tproxy v0.0.0-20190726054950-ef7efd7f24ed
	github.com/jayalane/go-counter v0.0.0-20230405044057-110fc30883c0
	github.com/jayalane/go-lll v0.0.0-20230319184427-bcaed09a676c
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-20230406214908-d011322222a8
	github.com/paultag/sniff v0.0.0-20200207005214-cf7e4d167732
	google.golang.org/grpc v1.54.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
