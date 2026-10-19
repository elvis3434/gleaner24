package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/elvis3434/gleaner24/internal/config"
	"github.com/elvis3434/gleaner24/internal/crawler"
	"github.com/elvis3434/gleaner24/internal/extract"
	"github.com/elvis3434/gleaner24/internal/query"
)

func cmdTestQuery(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("test-query", stderr)
	caseSensitive := fs.Bool("case-sensitive", false, "compare case-sensitively")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "usage: gleaner test-query [--case-sensitive] \"<expression>\" <file-or-URL>\n")
		return exitConfig
	}
	expr, target := fs.Arg(0), fs.Arg(1)

	q, err := query.Parse(expr, query.Options{CaseSensitive: *caseSensitive})
	if err != nil {
		fmt.Fprintf(stderr, "query: %v\n", err)
		return exitConfig
	}

	title, body, err := loadTarget(target)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", target, err)
		return exitAborted
	}
	ok, kws := q.Match(title + "\n" + body)
	if title != "" {
		fmt.Fprintf(stdout, "標題: %s\n", title)
	}
	if ok {
		fmt.Fprintf(stdout, "結果: 命中\n命中關鍵字: %s\n", strings.Join(kws, ", "))
	} else {
		fmt.Fprintln(stdout, "結果: 未命中")
	}
	return exitOK
}

// loadTarget returns the title and text of a URL or local file. HTML is run
// through the same extraction as a real collection run.
func loadTarget(target string) (title, body string, err error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		def := config.Default()
		f := crawler.New(def.Crawl.UserAgent, def.Crawl.Timeout.Duration, 0, def.Crawl.Retries)
		resp, err := f.Get(context.Background(), target)
		if err != nil {
			return "", "", err
		}
		u, _ := url.Parse(resp.URL)
		return extractHTML(resp.Body, resp.Header.Get("Content-Type"), u)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", "", err
	}
	ext := strings.ToLower(filepath.Ext(target))
	if ext == ".html" || ext == ".htm" || strings.Contains(strings.ToLower(string(data[:min(len(data), 1024)])), "<html") {
		return extractHTML(data, "", nil)
	}
	return "", string(data), nil
}

func extractHTML(data []byte, contentType string, u *url.URL) (string, string, error) {
	art, err := extract.Page(data, contentType, u)
	if err != nil {
		return "", "", err
	}
	return art.Title, art.Text, nil
}
