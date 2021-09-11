.PHONY: all
all: manifest-server manifest-client

.PHONY: manifest-server
manifest-server: protos
	go build -o manifest-server cmd/manifest-server/main.go

.PHONY: manifest-client
manifest-client: protos
	go build -o manifest-client cmd/manifest-client/main.go

pkg/server/service.pb.go: pkg/server/service.proto Makefile
	protoc	-I=. \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		pkg/server/service.proto

.PHONY: protos
protos: pkg/server/service.pb.go

.PHONY: clean
clean:
	go clean

.PHONY: distclean
distclean: clean
	rm pkg/server/*.pb.go
