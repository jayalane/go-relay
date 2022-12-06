module github.com/jayalane/go-relay/uproxy

go 1.18

replace github.com/jayalane/go-relay/udpProxy => ../udpProxy

require (
	github.com/jayalane/go-counter v0.0.0-20221206021103-daad75b2de86
	github.com/jayalane/go-lll v0.0.0-20221117191206-0dc3b9c0210c
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-20221117172320-01f26dc93835
	google.golang.org/grpc v1.51.0
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.0.0-20220722155237-a158d28d115b // indirect
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f // indirect
	golang.org/x/text v0.4.0 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
