// Package source turns configured sources into candidate URLs and content.
package source

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// Item is one entry of an RSS or Atom feed.
type Item struct {
	Title     string
	Link      string     // absolute URL as found in the feed (not normalised)
	Published *time.Time // nil when the feed gives no date
	Content   string     // HTML content, or the summary when there is none
	Language  string     // feed-level language, as declared by the feed
}

// ParseFeed parses an RSS or Atom document fetched from feedURL. Relative
// item links are resolved against feedURL; items without a link are dropped.
func ParseFeed(data []byte, feedURL string) ([]Item, error) {
	base, err := url.Parse(feedURL)
	if err != nil {
		return nil, err
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	items := make([]Item, 0, len(feed.Items))
	for _, it := range feed.Items {
		link := strings.TrimSpace(it.Link)
		if link == "" && len(it.Links) > 0 {
			link = strings.TrimSpace(it.Links[0])
		}
		if link == "" {
			continue
		}
		ref, err := url.Parse(link)
		if err != nil {
			continue
		}
		content := it.Content
		if strings.TrimSpace(content) == "" {
			content = it.Description
		}
		pub := it.PublishedParsed
		if pub == nil {
			pub = it.UpdatedParsed
		}
		items = append(items, Item{
			Title:     strings.TrimSpace(it.Title),
			Link:      base.ResolveReference(ref).String(),
			Published: pub,
			Content:   content,
			Language:  feed.Language,
		})
	}
	return items, nil
}
