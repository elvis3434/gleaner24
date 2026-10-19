// Package crawler performs polite HTTP fetching: a fixed User-Agent, a
// minimum delay between requests to the same host and retries with
// increasing back-off for timeouts and server errors.
package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultMaxBody caps how much of a response body is read.
const DefaultMaxBody = 10 << 20

// Response is a fetched document.
type Response struct {
	URL        string // final URL after redirects
	StatusCode int
	Header     http.Header
	Body       []byte
}

// StatusError is returned for non-2xx responses.
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.Code)
}

// Fetcher fetches URLs. The zero value is not usable; use New.
type Fetcher struct {
	Client    *http.Client
	UserAgent string
	// Delay is the minimum interval between two requests to the same host.
	Delay time.Duration
	// Retries is how many times a timeout or 5xx response is retried.
	Retries int
	// Backoff is the wait before the first retry; it grows linearly.
	Backoff time.Duration
	MaxBody int64

	mu   sync.Mutex
	next map[string]time.Time // host -> earliest time of the next request
	// Requests counts HTTP requests actually sent, including retries.
	requests int
}

// New returns a Fetcher with the given settings.
func New(userAgent string, timeout, delay time.Duration, retries int) *Fetcher {
	return &Fetcher{
		Client:    &http.Client{Timeout: timeout},
		UserAgent: userAgent,
		Delay:     delay,
		Retries:   retries,
		Backoff:   time.Second,
		MaxBody:   DefaultMaxBody,
	}
}

// Requests returns the number of HTTP requests sent so far.
func (f *Fetcher) Requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// Get fetches rawURL, waiting for the per-host delay and retrying transient
// failures. Non-2xx responses are returned as *StatusError.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt <= f.Retries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, time.Duration(attempt)*f.Backoff); err != nil {
				return nil, err
			}
		}
		if err := f.wait(ctx, u.Host); err != nil {
			return nil, err
		}
		resp, err := f.do(ctx, rawURL)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable(err) || ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func (f *Fetcher) do(ctx context.Context, rawURL string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, &StatusError{URL: rawURL, Code: resp.StatusCode}
	}
	limit := f.MaxBody
	if limit <= 0 {
		limit = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	return &Response{
		URL:        resp.Request.URL.String(),
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}, nil
}

// wait blocks until a request to host is allowed and reserves the slot.
func (f *Fetcher) wait(ctx context.Context, host string) error {
	host = strings.ToLower(host)
	f.mu.Lock()
	if f.next == nil {
		f.next = map[string]time.Time{}
	}
	now := time.Now()
	at := f.next[host]
	if at.Before(now) {
		at = now
	}
	f.next[host] = at.Add(f.Delay)
	f.mu.Unlock()
	return sleep(ctx, time.Until(at))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryable reports whether err is a timeout or a 5xx other than 503.
// 429 and 503 ask the client to back off and are not retried.
func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code >= 500 && se.Code != http.StatusServiceUnavailable
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Connection resets and similar transport errors are worth one more try.
	var ue *url.Error
	return errors.As(err, &ue)
}
