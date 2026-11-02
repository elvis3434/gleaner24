// Package selfcheck implements the vulncheck command of gleaner and quotes:
// the running executable checks the Go packages compiled into it against
// the Go vulnerability database (https://vuln.go.dev), on demand.
//
// It runs govulncheck in binary mode, so it needs neither the source code
// nor a Go installation, and it reports only vulnerable functions that are
// actually linked into the executable.
package selfcheck

import (
	"context"
	"debug/buildinfo"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/vuln/scan"
)

// Result of a scan.
type Result int

const (
	Clean Result = iota // no known vulnerability
	Found               // at least one known vulnerability is reachable
)

// govulncheck's exit code when it finds vulnerabilities (text output).
const exitVulnsFound = 3

// Options configures a scan.
type Options struct {
	// Exe is the executable to scan; empty means the running one.
	Exe string
	// DB is the vulnerability database URL; empty means
	// https://vuln.go.dev. A local directory path is accepted as well and
	// turned into a file:// URL.
	DB string
	// Verbose also lists vulnerabilities in packages the executable
	// imports or requires but does not call.
	Verbose bool
}

// Run scans the executable and writes the report to w.
func Run(ctx context.Context, opts Options, w io.Writer) (Result, error) {
	exe := opts.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return Clean, fmt.Errorf("locate own executable: %w", err)
		}
	}
	args := []string{"-mode", "binary"}
	if opts.DB != "" {
		db, err := dbURL(opts.DB)
		if err != nil {
			return Clean, err
		}
		args = append(args, "-db", db)
	}
	if opts.Verbose {
		args = append(args, "-show", "verbose")
	}
	args = append(args, exe)

	writeBuildInfo(w, exe)
	fmt.Fprintln(w)

	var stderr strings.Builder
	cmd := scan.Command(ctx, args...)
	cmd.Stdout = w
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader("")
	if err := cmd.Start(); err != nil {
		return Clean, err
	}
	err := cmd.Wait()
	var exit interface{ ExitCode() int }
	switch {
	case err == nil:
		fmt.Fprintln(w, "\n結果：未發現已知漏洞。")
		return Clean, nil
	case errors.As(err, &exit) && exit.ExitCode() == exitVulnsFound:
		fmt.Fprintln(w, "\n結果：發現已知漏洞。請依上方「Fixed in」的版本升級後重新建置，步驟見 docs/DEPENDENCIES.md。")
		return Found, nil
	default:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Clean, fmt.Errorf("vulnerability scan failed: %s", msg)
	}
}

// dbURL accepts a URL (https://, file://) or a local directory.
func dbURL(db string) (string, error) {
	if strings.Contains(db, "://") {
		return db, nil
	}
	abs, err := filepath.Abs(db)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "index", "db.json")); err != nil {
		return "", fmt.Errorf("%s is not a vulnerability database (no index/db.json)", db)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if filepath.VolumeName(abs) != "" {
		u.Path = "/" + u.Path // file:///C:/...
	}
	return u.String(), nil
}

// writeBuildInfo lists the Go version and modules compiled into exe, so the
// report shows what would need upgrading.
func writeBuildInfo(w io.Writer, exe string) {
	bi, err := buildinfo.ReadFile(exe)
	if err != nil {
		return // govulncheck reports the problem with the file
	}
	fmt.Fprintf(w, "檢查對象：%s\n", exe)
	fmt.Fprintf(w, "Go 版本：%s\n", bi.GoVersion)
	fmt.Fprintln(w, "編入的套件：")
	for _, m := range bi.Deps {
		v := m.Version
		if m.Replace != nil {
			v += " => " + m.Replace.Path + " " + m.Replace.Version
		}
		fmt.Fprintf(w, "  %s %s\n", m.Path, v)
	}
}

// Exit codes of the vulncheck command.
const (
	ExitClean  = 0
	ExitUsage  = 1
	ExitFailed = 2 // the scan could not run (no network, bad database)
	ExitFound  = 3 // same as govulncheck
)

// Command implements `<tool> vulncheck [-db URL|DIR] [-v]` for both tools
// and returns the process exit code.
func Command(ctx context.Context, tool string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(tool+" vulncheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	db := fs.String("db", "", "vulnerability database URL or directory (default https://vuln.go.dev)")
	verbose := fs.Bool("v", false, "also list vulnerabilities in packages that are not called")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: %s vulncheck [-db URL|DIR] [-v]\n\n"+
			"Checks the packages compiled into this executable against the Go\n"+
			"vulnerability database. Exit codes: 0 none found, 3 found, 2 scan failed.\n\n", tool)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	res, err := Run(ctx, Options{DB: *db, Verbose: *verbose}, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "%s vulncheck: %v\n", tool, err)
		return ExitFailed
	}
	if res == Found {
		return ExitFound
	}
	return ExitClean
}
