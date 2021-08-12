module github.com/jayalane/go-relay

replace github.com/jayalane/go-relay/udpProxy => ./udpProxy

replace github.com/jayalane/go-counter => ../go-counter/

replace github.com/jayalane/go-lll => ../go-lll/

replace github.com/jayalane/go-tinyconfig => ../go-tinyconfig/

go 1.16

require (
	github.com/LiamHaworth/go-tproxy v0.0.0-20190726054950-ef7efd7f24ed
	github.com/jayalane/go-counter v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-lll v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-relay/udpProxy v0.0.0-00010101000000-000000000000
	github.com/jayalane/go-tinyconfig v0.0.0-00010101000000-000000000000
	github.com/paultag/sniff v0.0.0-20200207005214-cf7e4d167732
	golang.org/x/sys v0.0.0-20210809222454-d867a43fc93e // indirect
	google.golang.org/grpc v1.40.0
)
