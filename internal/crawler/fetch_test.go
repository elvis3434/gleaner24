package crawler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetSuccessAndUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<p>hi</p>"))
	}))
	defer srv.Close()

	f := New("TestAgent/1.0", 5*time.Second, 0, 0)
	resp, err := f.Get(context.Background(), srv.URL+"/a")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "<p>hi</p>" || resp.StatusCode != 200 {
		t.Errorf("resp = %d %q", resp.StatusCode, resp.Body)
	}
	if ua != "TestAgent/1.0" {
		t.Errorf("User-Agent = %q", ua)
	}
	if f.Requests() != 1 {
		t.Errorf("Requests = %d", f.Requests())
	}
}

func TestRetryOn500(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := New("t", 5*time.Second, 0, 2)
	f.Backoff = time.Millisecond
	resp, err := f.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "ok" || n.Load() != 3 {
		t.Errorf("body %q after %d requests", resp.Body, n.Load())
	}
	if f.Requests() != 1 {
		t.Errorf("Requests = %d, want 1 (retries not counted)", f.Requests())
	}
}

func TestNoRetryOn404And503(t *testing.T) {
	for _, code := range []int{404, 429, 503} {
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			w.WriteHeader(code)
		}))
		f := New("t", 5*time.Second, 0, 2)
		f.Backoff = time.Millisecond
		_, err := f.Get(context.Background(), srv.URL)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("code %d: err = %v", code, err)
		}
		if n.Load() != 1 {
			t.Errorf("code %d: %d requests, want 1", code, n.Load())
		}
		srv.Close()
	}
}

func TestRetriesExhausted(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(502)
	}))
	defer srv.Close()
	f := New("t", 5*time.Second, 0, 2)
	f.Backoff = time.Millisecond
	if _, err := f.Get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 3 {
		t.Errorf("%d requests, want 3", n.Load())
	}
}

func TestPerHostDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := New("t", 5*time.Second, 50*time.Millisecond, 0)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := f.Get(context.Background(), srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 100*time.Millisecond {
		t.Errorf("3 requests took %v, want >= 100ms", el)
	}
}

func TestContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := New("t", 5*time.Second, time.Hour, 0)
	if _, err := f.Get(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.Get(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
}

func TestMaxBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1000))
	}))
	defer srv.Close()
	f := New("t", 5*time.Second, 0, 0)
	f.MaxBody = 10
	if _, err := f.Get(context.Background(), srv.URL); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	f.MaxBody = 1000
	resp, err := f.Get(context.Background(), srv.URL)
	if err != nil || len(resp.Body) != 1000 {
		t.Errorf("body exactly at limit: %v, %d bytes", err, len(resp.Body))
	}
}

func TestPost(t *testing.T) {
	var method, ctype, got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ctype = r.Method, r.Header.Get("Content-Type")
		r.ParseForm()
		got = r.PostForm.Get("commodity_id") + "|" + r.PostForm.Get("queryStartDate")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := New("t", 5*time.Second, 0, 0)
	resp, err := f.Post(context.Background(), srv.URL, url.Values{"commodity_id": {"TX"}, "queryStartDate": {"2026/10/05"}})
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || ctype != "application/x-www-form-urlencoded" || got != "TX|2026/10/05" || string(resp.Body) != "ok" {
		t.Errorf("method=%s ctype=%s form=%s body=%s", method, ctype, got, resp.Body)
	}
}
