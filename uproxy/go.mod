module github.com/jayalane/go-relay.git/uproxy

go 1.15

replace github.com/jayalane/go-relay/udpProxy => ../udpProxy
replace github.com/jayalane/go-counter => ../../go-counter
replace github.com/jayalane/go-tinyconfig => ../../go-tinyconfig/

require (
	github.com/golang/protobuf v1.5.1
	github.com/jayalane/go-counter v0.0.0-20210317224930-5dfe901a5bb2
	github.com/jayalane/go-lll v0.0.0-20210226204815-a749db371ada
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-20190322222019-6686e8e45220
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	google.golang.org/grpc v1.36.0
)
