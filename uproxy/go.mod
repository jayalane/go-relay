module github.com/jayalane/go-relay.git/uproxy

go 1.15

replace github.com/jayalane/go-relay/udpProxy => ../udpProxy

replace github.com/jayalane/go-counter => ../../go-counter

replace github.com/jayalane/go-tinyconfig => ../../go-tinyconfig/

require (
	github.com/jayalane/go-counter v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-lll v0.0.0-20210226204815-a749db371ada
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-00010101000000-000000000000
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	google.golang.org/grpc v1.36.0
)
