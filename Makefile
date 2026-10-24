# Build tasks for gleaner (article archive) and quotes (market data).
# On Windows without make, see README.md.

.PHONY: build test vet windows clean

build:
	go build -o gleaner ./cmd/gleaner
	go build -o quotes ./cmd/quotes

test:
	go test ./...

vet:
	go vet ./...

# Cross-compile the Windows executables (pure Go, no cgo needed).
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o gleaner.exe ./cmd/gleaner
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o quotes.exe ./cmd/quotes

clean:
	rm -f gleaner gleaner.exe quotes quotes.exe
