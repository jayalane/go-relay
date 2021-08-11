module github.com/jayalane/go-relay

go 1.16

replace github.com/jayalane/go-relay/udpProxy => ./udpProxy

replace github.com/jayalane/go-counter => ../go-counter/

replace github.com/jayalane/go-lll => ../go-lll/

replace github.com/jayalane/go-tinyconfig => ../go-tinyconfig/

require (
	github.com/jayalane/go-counter v0.0.0-20210327181920-3beb6a93b7e9 // indirect
	github.com/jayalane/go-lll v0.0.0-20210514151941-58c7355c631d // indirect
	github.com/jayalane/go-relay/udpProxy v0.0.0-20210426064240-bf6b4f8d1d05 // indirect
	github.com/jayalane/go-tinyconfig v0.0.0-20210321232054-140a43c2e65c // indirect
	github.com/paultag/sniff v0.0.0-20200207005214-cf7e4d167732 // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	google.golang.org/grpc v1.39.0 // indirect
)
