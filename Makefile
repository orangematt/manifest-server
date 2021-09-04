.PHONY: all
all: manifest-server

.PHONY: manifest-server
manifest-server:
	go build -o manifest-server cmd/main.go
