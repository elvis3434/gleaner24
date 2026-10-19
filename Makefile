# gleaner build tasks. On Windows without make, see README.md.
BIN := gleaner
PKG := ./cmd/gleaner

.PHONY: build test vet windows clean

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

# Cross-compile the Windows executable (pure Go, no cgo needed).
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(BIN).exe $(PKG)

clean:
	rm -f $(BIN) $(BIN).exe
