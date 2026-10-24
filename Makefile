# Build and maintenance tasks for gleaner (article archive) and quotes
# (market data). On Windows without make, see README.md and
# docs/DEPENDENCIES.md for the equivalent go commands.

.PHONY: build test vet windows clean tools lint vulncheck outdated check update-lwc

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

# Analysis tools at the versions pinned in tools/go.mod, built into bin/.
tools:
	go build -C tools -o ../bin/ golang.org/x/vuln/cmd/govulncheck honnef.co/go/tools/cmd/staticcheck

lint: tools
	bin/staticcheck ./...

# Known vulnerabilities reachable from our code, in dependencies and in the
# Go standard library of the toolchain in use. VULNDB=file:///path uses an
# offline copy of the database instead of https://vuln.go.dev.
vulncheck: tools
	bin/govulncheck $(if $(VULNDB),-db $(VULNDB)) ./...
	bin/govulncheck $(if $(VULNDB),-db $(VULNDB)) -mode binary bin/govulncheck
	bin/govulncheck $(if $(VULNDB),-db $(VULNDB)) -mode binary bin/staticcheck

# Dependencies with newer versions (informational).
outdated:
	@go list -m -u -f '{{if and .Update (not .Indirect)}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all
	@go list -C tools -m -u -f '{{if .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}} (tools){{end}}' all | grep -E 'golang.org/x/vuln|honnef.co/go/tools' || true

# Everything CI runs.
check: vet test lint vulncheck windows

# Re-vendor lightweight-charts at the version in
# internal/market/chart/assets/package.json (needs node and npm).
update-lwc:
	sh scripts/update-lightweight-charts.sh

clean:
	rm -f gleaner gleaner.exe quotes quotes.exe
	rm -rf bin
