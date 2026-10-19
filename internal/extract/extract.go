// Package extract pulls the main article out of an HTML page (dropping
// menus, ads and footers) and converts it to Markdown and plain text.
package extract

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/go-shiori/go-readability"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

// ErrNoContent is returned when no readable article text is found.
var ErrNoContent = errors.New("no readable content found")

// Article is the extracted content of a page.
type Article struct {
	Title     string
	Published *time.Time
	// Lang is the page's declared language (<html lang>), unnormalised.
	Lang string
	// Canonical is the absolute <link rel="canonical"> URL, if any.
	Canonical string
	Markdown  string
	// Text is the plain text of the article, used for keyword matching
	// and fingerprinting.
	Text string
}

// Page extracts the main article from a full HTML document. contentType is
// the HTTP Content-Type header, used to detect the character set.
func Page(body []byte, contentType string, pageURL *url.URL) (*Article, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, fmt.Errorf("decode charset: %w", err)
	}
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	// Read metadata before readability rewrites the tree.
	canonical, lang := pageMeta(doc, pageURL)

	art, err := readability.FromDocument(doc, pageURL)
	if err != nil {
		return nil, fmt.Errorf("extract article: %w", err)
	}
	text := strings.TrimSpace(art.TextContent)
	if text == "" {
		return nil, ErrNoContent
	}
	md, err := toMarkdown(art.Content, pageURL)
	if err != nil {
		return nil, err
	}
	if lang == "" {
		lang = art.Language
	}
	return &Article{
		Title:     strings.TrimSpace(art.Title),
		Published: art.PublishedTime,
		Lang:      lang,
		Canonical: canonical,
		Markdown:  md,
		Text:      text,
	}, nil
}

// Fragment converts an HTML snippet such as a feed item's content. No
// boilerplate removal is done since feed content is already the article.
func Fragment(fragment string, base *url.URL) (*Article, error) {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	var sb strings.Builder
	for _, n := range nodes {
		writeText(&sb, n)
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return nil, ErrNoContent
	}
	md, err := toMarkdown(fragment, base)
	if err != nil {
		return nil, err
	}
	return &Article{Markdown: md, Text: text}, nil
}

func toMarkdown(content string, base *url.URL) (string, error) {
	var opts []converter.ConvertOptionFunc
	if base != nil {
		opts = append(opts, converter.WithDomain(base.Scheme+"://"+base.Host))
	}
	md, err := htmltomarkdown.ConvertString(content, opts...)
	if err != nil {
		return "", fmt.Errorf("convert to Markdown: %w", err)
	}
	return strings.TrimSpace(md), nil
}

// pageMeta returns the canonical URL and the <html lang> attribute.
func pageMeta(doc *html.Node, pageURL *url.URL) (canonical, lang string) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "html":
				if lang == "" {
					lang = strings.TrimSpace(attr(n, "lang"))
				}
			case "link":
				if canonical == "" && hasToken(attr(n, "rel"), "canonical") {
					if ref, err := url.Parse(strings.TrimSpace(attr(n, "href"))); err == nil && attr(n, "href") != "" {
						if pageURL != nil {
							ref = pageURL.ResolveReference(ref)
						}
						if ref.IsAbs() {
							canonical = ref.String()
						}
					}
				}
			case "body":
				return // canonical and lang live in <html>/<head>
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return canonical, lang
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasToken(list, tok string) bool {
	for _, f := range strings.Fields(list) {
		if strings.EqualFold(f, tok) {
			return true
		}
	}
	return false
}

// blockElements get a line break around their text so that words from
// adjacent paragraphs are not glued together.
var blockElements = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true,
	"section": true, "article": true, "table": true, "ul": true, "ol": true,
}

func writeText(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(n.Data)
		return
	case html.ElementNode:
		if n.Data == "script" || n.Data == "style" || n.Data == "noscript" {
			return
		}
	}
	block := n.Type == html.ElementNode && blockElements[n.Data]
	if block {
		sb.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeText(sb, c)
	}
	if block {
		sb.WriteByte('\n')
	}
}
