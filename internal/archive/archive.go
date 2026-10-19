// Package archive writes collected articles as Markdown files with a YAML
// front matter block.
package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvis3434/gleaner24/internal/normalize"
)

// MaxSlugRunes is the longest title part of a file name (SPEC F6).
const MaxSlugRunes = 80

// Doc is one archived article.
type Doc struct {
	Title       string
	SourceURL   string
	Site        string
	CollectedAt time.Time
	PublishedAt *time.Time
	Language    string
	Topics      []string
	Keywords    []string
	Version     int
	Images      []string
	Body        string // Markdown
}

// RelPath returns the file path, relative to the archive directory, for a
// new archive: YYYY/MM/DD/<topic>/<slug>.md, where the date is the
// collection date and slug is the title plus the first 8 hex digits of the
// URL's hash. Versions above 1 get a _v<N> suffix.
func RelPath(collected time.Time, topic, title, normURL string, version int) string {
	name := Slug(title, normURL)
	if version > 1 {
		name += fmt.Sprintf("_v%d", version)
	}
	return filepath.Join(collected.Format("2006"), collected.Format("01"), collected.Format("02"), topic, name+".md")
}

// Slug builds the file name stem for an article.
func Slug(title, normURL string) string {
	hash := normalize.Hash(normURL)[:8]
	if s := normalize.Slug(title, MaxSlugRunes); s != "" {
		return s + "-" + hash
	}
	return hash
}

// Render formats d as Markdown with front matter.
func Render(d Doc) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	field := func(k, v string) { fmt.Fprintf(&b, "%s: %s\n", k, v) }
	field("title", quote(d.Title))
	field("source_url", quote(d.SourceURL))
	field("site", quote(d.Site))
	field("collected_at", d.CollectedAt.Format(time.RFC3339))
	if d.PublishedAt != nil {
		field("published_at", d.PublishedAt.In(d.CollectedAt.Location()).Format("2006-01-02"))
	}
	if d.Language != "" {
		field("language", quote(d.Language))
	}
	field("topics", list(d.Topics))
	field("matched_keywords", list(d.Keywords))
	field("version", fmt.Sprint(d.Version))
	field("images", list(d.Images))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(d.Body))
	b.WriteString("\n")
	return b.Bytes()
}

// quote returns s as a double-quoted scalar. JSON strings are valid YAML
// double-quoted scalars, which keeps titles containing ':' or '#' safe.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func list(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = quote(x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// Write renders d and writes it to path atomically: the content goes to a
// temporary file in the same directory which is then renamed into place, so
// an interrupted run never leaves a half-written article.
func Write(path string, d Doc) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.md")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(Render(d)); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
