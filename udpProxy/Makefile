
PROTOC=protoc


all: udp.pb.go udp_grpc.pb.go

udp.pb.go: udp.proto
	protoc --proto_path=. --go_out=. --go_opt=paths=source_relative $<

udp_grpc.pb.go: udp.proto
	protoc --proto_path=. --go-grpc_out=. --go-grpc_opt=paths=source_relative $<
