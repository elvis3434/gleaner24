package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elvis3434/gleaner24/internal/archive"
	"github.com/elvis3434/gleaner24/internal/config"
	"github.com/elvis3434/gleaner24/internal/crawler"
	"github.com/elvis3434/gleaner24/internal/extract"
	"github.com/elvis3434/gleaner24/internal/lang"
	"github.com/elvis3434/gleaner24/internal/normalize"
	"github.com/elvis3434/gleaner24/internal/runlog"
	"github.com/elvis3434/gleaner24/internal/source"
	"github.com/elvis3434/gleaner24/internal/store"
)

type runOptions struct {
	DryRun bool
	Out    io.Writer // console output; also receives the log lines
	Loc    *time.Location
	// Now overrides the clock in tests.
	Now func() time.Time
}

// errLimit stops processing once crawl.max_pages_per_run is reached.
var errLimit = errors.New("max_pages_per_run reached")

type runner struct {
	cfg   *config.Config
	opts  runOptions
	fetch *crawler.Fetcher
	st    *store.Store // nil in a dry run without an existing database
	log   *runlog.Log
	runID int64
	now   time.Time
	// seen holds the normalised URLs handled in this run, so an article
	// listed by two feeds is processed once.
	seen map[string]bool
}

// run performs one collection run over the configured feeds. The returned
// error is for failures that stop the whole run (database, log file); page
// and feed failures are only counted.
func run(ctx context.Context, cfg *config.Config, opts runOptions) (runlog.Stats, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	r := &runner{
		cfg:   cfg,
		opts:  opts,
		fetch: crawler.New(cfg.Crawl.UserAgent, cfg.Crawl.Timeout.Duration, cfg.Crawl.PerHostDelay.Duration, cfg.Crawl.Retries),
		now:   opts.Now().In(opts.Loc),
		seen:  map[string]bool{},
	}

	var err error
	if opts.DryRun {
		r.st, err = store.OpenReadOnly(cfg.Output.DB)
		if errors.Is(err, os.ErrNotExist) {
			r.st, err = nil, nil
		}
		r.log = runlog.New(opts.Out, r.now)
	} else {
		r.st, err = store.Open(cfg.Output.DB)
		if err == nil {
			r.log, err = runlog.Create(cfg.Output.LogDir, r.now, opts.Out)
		}
	}
	if err != nil {
		if r.st != nil {
			r.st.Close()
		}
		return runlog.Stats{}, err
	}
	if r.st != nil {
		defer r.st.Close()
	}
	defer r.log.Close()

	if !opts.DryRun {
		if r.runID, err = r.st.StartRun(r.now.Format(time.RFC3339), filepath.ToSlash(r.log.Path())); err != nil {
			return runlog.Stats{}, err
		}
		r.log.Printf("run %d started, log %s", r.runID, r.log.Path())
	} else {
		r.log.Printf("dry run: nothing will be written")
	}

	reason := runlog.Completed
	var fatal error
	for _, feed := range cfg.Sources.Feeds {
		err := r.processFeed(ctx, feed)
		switch {
		case errors.Is(err, errLimit):
			reason = runlog.LimitReached
			r.log.Printf("stopping: %v (%d)", err, cfg.Crawl.MaxPagesPerRun)
		case ctx.Err() != nil:
			reason = runlog.Aborted
			r.log.Printf("stopping: interrupted")
		case err != nil:
			reason, fatal = runlog.Aborted, err
			r.log.Printf("stopping: %v", err)
		}
		if reason != runlog.Completed {
			break
		}
	}
	if len(cfg.Sources.Sites) > 0 {
		r.log.Printf("note: sources.sites is not processed yet (stage 2)")
	}

	r.log.Fetched(r.fetch.Requests())
	stats := r.log.Finish(reason)
	r.log.WriteSummary(stats)
	if !opts.DryRun {
		if err := r.st.FinishRun(r.runID, stats.FinishedAt, stats.EndReason, stats.JSON()); err != nil && fatal == nil {
			fatal = err
		}
	}
	return stats, fatal
}

// get fetches u unless the per-run page limit has been reached.
func (r *runner) get(ctx context.Context, u string) (*crawler.Response, error) {
	if r.fetch.Requests() >= r.cfg.Crawl.MaxPagesPerRun {
		return nil, errLimit
	}
	return r.fetch.Get(ctx, u)
}

// processFeed handles one feed. It returns errLimit or a context error when
// the run must stop; other failures are logged and swallowed.
func (r *runner) processFeed(ctx context.Context, feed config.Feed) error {
	resp, err := r.get(ctx, feed.URL)
	if err != nil {
		return r.failOrStop(ctx, feed.URL, err)
	}
	items, err := source.ParseFeed(resp.Body, resp.URL)
	if err != nil {
		r.log.Fail(feed.URL, err)
		return nil
	}
	r.log.Printf("feed %s: %d items", feed.Name, len(items))
	for _, it := range items {
		if err := r.processItem(ctx, feed, it); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) failOrStop(ctx context.Context, u string, err error) error {
	if errors.Is(err, errLimit) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.log.Fail(u, err)
	return nil
}

// candidate is a page ready for matching.
type candidate struct {
	url       string // normalised
	title     string
	published *time.Time
	language  string
	art       *extract.Article
}

func (r *runner) processItem(ctx context.Context, feed config.Feed, it source.Item) error {
	u, err := normalize.URL(it.Link)
	if err != nil {
		r.log.Fail(it.Link, err)
		return nil
	}
	if r.seen[u] {
		r.log.Skip(runlog.SkipDuplicate, u)
		return nil
	}
	r.seen[u] = true

	c := candidate{url: u, title: it.Title, published: it.Published}
	if feed.FetchFullText {
		resp, err := r.get(ctx, u)
		if err != nil {
			return r.failOrStop(ctx, u, err)
		}
		final, _ := url.Parse(resp.URL)
		c.art, err = extract.Page(resp.Body, resp.Header.Get("Content-Type"), final)
		if err != nil {
			r.log.Fail(u, err)
			return nil
		}
		if cu := r.canonical(c.art.Canonical, final); cu != "" && cu != u {
			if r.seen[cu] {
				r.log.Skip(runlog.SkipDuplicate, cu)
				return nil
			}
			r.seen[cu] = true
			c.url = cu
		}
		c.language = lang.Code(c.art.Lang)
	} else {
		base, _ := url.Parse(u)
		c.art, err = extract.Fragment(it.Content, base)
		if err != nil {
			r.log.Fail(u, fmt.Errorf("feed item content: %w", err))
			return nil
		}
	}
	if c.title == "" {
		c.title = c.art.Title
	}
	if c.published == nil {
		c.published = c.art.Published
	}
	if c.language == "" {
		c.language = lang.Code(it.Language)
	}
	if c.language == "" && len(feed.Languages) == 1 {
		c.language = feed.Languages[0]
	}
	return r.matchAndArchive(feed.Name, c)
}

// canonical returns the normalised canonical URL when it is on the same
// host as the fetched page; a canonical pointing elsewhere is ignored.
func (r *runner) canonical(raw string, page *url.URL) string {
	if raw == "" || page == nil {
		return ""
	}
	cu, err := normalize.URL(raw)
	if err != nil {
		return ""
	}
	parsed, err := url.Parse(cu)
	if err != nil || !strings.EqualFold(parsed.Hostname(), page.Hostname()) {
		return ""
	}
	return cu
}

func (r *runner) matchAndArchive(site string, c candidate) error {
	var topics, keywords []string
	for i := range r.cfg.Topics {
		t := &r.cfg.Topics[i]
		if !t.AppliesTo(site) {
			continue
		}
		var parts []string
		for _, f := range t.MatchIn {
			switch f {
			case "title":
				parts = append(parts, c.title)
			case "body":
				parts = append(parts, c.art.Text)
			}
		}
		ok, kws := t.Compiled.Match(strings.Join(parts, "\n"))
		if !ok {
			continue
		}
		topics = append(topics, t.Name)
		for _, k := range kws {
			if !slices.Contains(keywords, k) {
				keywords = append(keywords, k)
			}
		}
	}

	hash := normalize.Hash(normalize.Text(c.art.Text))
	ts := r.now.Format(time.RFC3339)
	var existing *store.Page
	if r.st != nil {
		var err error
		if existing, err = r.st.Page(c.url); err != nil {
			return fmt.Errorf("read page %s: %w", c.url, err)
		}
	}
	archived := existing != nil && existing.Status == store.StatusArchived

	page := &store.Page{
		URL: c.url, Site: site, Title: c.title, Language: c.language,
		ContentHash: hash, FirstSeenAt: ts, LastFetchAt: ts,
	}
	if c.published != nil {
		page.PublishedAt = c.published.In(r.opts.Loc).Format(time.RFC3339)
	}
	if existing != nil {
		page.FirstSeenAt = existing.FirstSeenAt
		page.LastChangeAt = existing.LastChangeAt
	}

	if len(topics) == 0 {
		r.log.Skip(runlog.SkipNoMatch, c.url)
		if !r.opts.DryRun && !archived {
			page.Status = store.StatusSkipped
			if err := r.st.SavePage(page); err != nil {
				return fmt.Errorf("save page %s: %w", c.url, err)
			}
		}
		return nil
	}

	if archived && existing.ContentHash == hash {
		r.log.Skip(runlog.SkipDuplicate, c.url)
		if !r.opts.DryRun {
			page.Status = store.StatusArchived
			if err := r.st.SavePage(page); err != nil {
				return fmt.Errorf("save page %s: %w", c.url, err)
			}
		}
		return nil
	}

	version, rel := 1, ""
	if archived {
		latest, err := r.st.LatestArchive(existing.ID)
		if err != nil {
			return fmt.Errorf("read archive of %s: %w", c.url, err)
		}
		if latest != nil {
			version = latest.Version + 1
			if r.cfg.Output.OnUpdate == config.Overwrite {
				rel = filepath.FromSlash(latest.FilePath)
			}
		}
	}
	if rel == "" {
		rel = archive.RelPath(r.now, topics[0], c.title, c.url, version)
	}

	action := "NEW"
	if archived {
		action = "UPDATE"
	}
	if r.opts.DryRun {
		r.log.Printf("WOULD %s %s <- %s topics=%v keywords=%v", action, rel, c.url, topics, keywords)
		r.log.Archived(archived, site, topics)
		return nil
	}

	doc := archive.Doc{
		Title: c.title, SourceURL: c.url, Site: site, CollectedAt: r.now,
		PublishedAt: c.published, Language: c.language, Topics: topics,
		Keywords: keywords, Version: version, Body: c.art.Markdown,
	}
	if err := archive.Write(filepath.Join(r.cfg.Output.ArchiveDir, rel), doc); err != nil {
		r.log.Fail(c.url, fmt.Errorf("write archive: %w", err))
		return nil
	}
	page.Status = store.StatusArchived
	page.LastChangeAt = ts
	if err := r.st.SavePage(page); err != nil {
		return fmt.Errorf("save page %s: %w", c.url, err)
	}
	if err := r.st.AddArchive(&store.Archive{
		PageID: page.ID, Version: version, FilePath: filepath.ToSlash(rel),
		Topics: topics, Keywords: keywords, ArchivedAt: ts, RunID: r.runID,
	}); err != nil {
		return fmt.Errorf("save archive %s: %w", c.url, err)
	}
	r.log.Archived(archived, site, topics)
	r.log.Printf("%s %s <- %s", action, rel, c.url)
	return nil
}
