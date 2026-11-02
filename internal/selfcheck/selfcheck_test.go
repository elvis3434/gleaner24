package selfcheck

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildGleaner compiles cmd/gleaner, the kind of executable the vulncheck
// command scans in practice.
func buildGleaner(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "gleaner")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	// go test puts the go command it belongs to first on PATH.
	cmd := exec.Command("go", "build", "-o", exe, "github.com/elvis3434/gleaner24/cmd/gleaner")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return exe
}

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an executable")
	}
	exe := buildGleaner(t)

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{Exe: exe, DB: filepath.Join("testdata", "vulndb", "affected")}, &out)
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if res != Found {
		t.Errorf("result = %v, want Found\n%s", res, out.String())
	}
	for _, want := range []string{"GO-0000-0001", "github.com/BurntSushi/toml", "Fixed in", "發現已知漏洞", "Go 版本：go1.", "github.com/mmcdole/gofeed v"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	res, err = Run(context.Background(), Options{Exe: exe, DB: filepath.Join("testdata", "vulndb", "clean")}, &out)
	if err != nil || res != Clean {
		t.Errorf("clean db: %v, %v\n%s", res, err, out.String())
	}
	if !strings.Contains(out.String(), "未發現已知漏洞") {
		t.Errorf("clean report:\n%s", out.String())
	}
}

func TestRunErrors(t *testing.T) {
	var out bytes.Buffer
	if _, err := Run(context.Background(), Options{Exe: "x", DB: t.TempDir()}, &out); err == nil || !strings.Contains(err.Error(), "not a vulnerability database") {
		t.Errorf("bad db dir: %v", err)
	}
	notExe := filepath.Join(t.TempDir(), "not-an-executable")
	os.WriteFile(notExe, []byte("hello"), 0o644)
	if _, err := Run(context.Background(), Options{Exe: notExe, DB: filepath.Join("testdata", "vulndb", "clean")}, &out); err == nil {
		t.Error("scanning a non-executable should fail")
	}
}

func TestDBURL(t *testing.T) {
	if u, _ := dbURL("https://vuln.go.dev"); u != "https://vuln.go.dev" {
		t.Errorf("https = %s", u)
	}
	u, err := dbURL(filepath.Join("testdata", "vulndb", "clean"))
	if err != nil || !strings.HasPrefix(u, "file:///") || !strings.HasSuffix(u, "/testdata/vulndb/clean") {
		t.Errorf("dir = %s, %v", u, err)
	}
}
