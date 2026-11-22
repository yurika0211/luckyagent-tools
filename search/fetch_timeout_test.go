package search

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const fetchTimeoutTestURL = "http://93.184.216.34/page"

func TestCurlFetchDeadlineRetainsRealPartialResponseBytes(t *testing.T) {
	requireFetchTestCurl(t)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	const prefix = "<html><body><p>partial response 前缀 with enough readable words to remain useful before the request deadline"
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.String() != fetchTimeoutTestURL {
			t.Errorf("curl did not use the configured proxy for the validated public URL: %s", req.URL)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		select {
		case <-req.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := NewCurlEngine(server.URL).Fetch(ctx, fetchTimeoutTestURL, 1000)

	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("real curl body deadline must fail: err=%v result=%#v", err, result)
	}
	if result == nil || !result.Partial || result.Truncated {
		t.Fatalf("real curl discarded or mislabeled its partial response: %#v", result)
	}
	if result.BytesRead != len(prefix) || !strings.Contains(result.Content, "partial response 前缀") {
		t.Fatalf("raw bytes or partial extracted text lost: want %d bytes, result=%#v", len(prefix), result)
	}
}

func TestFetchDeadlineStopsFallbackAndDoesNotCachePartialContent(t *testing.T) {
	manager := NewManager(DefaultSearchConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	fallbackCalls := 0
	manager.fetchEngines = []FetchEngine{
		fetchTimeoutTestEngine{name: "partial", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			<-ctx.Done()
			return &FetchResult{URL: rawURL, Content: "received prefix", BytesRead: 15, Partial: true}, ctx.Err()
		}},
		fetchTimeoutTestEngine{name: "fallback", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			fallbackCalls++
			return &FetchResult{URL: rawURL, Content: "complete fallback"}, nil
		}},
	}

	result, attempts, err := manager.FetchURLWithDiagnostics(ctx, fetchTimeoutTestURL, 1000)

	if !errors.Is(err, context.DeadlineExceeded) || result == nil || !result.Partial || result.Content != "received prefix" || result.BytesRead != 15 {
		t.Fatalf("deadline result lost partial content or error: result=%#v err=%v", result, err)
	}
	if fallbackCalls != 0 || len(attempts) != 1 || attempts[0].Engine != "partial" {
		t.Fatalf("deadline started another engine or lost diagnostics: calls=%d attempts=%#v", fallbackCalls, attempts)
	}
	if cached, ok := manager.fetchCache.Get(fetchTimeoutTestURL, 1000); ok {
		t.Fatalf("partial response was cached as a complete page: %#v", cached)
	}
}

func TestFetchPartialReadErrorCanFallbackAndCacheOnlyTheCompletePage(t *testing.T) {
	manager := NewManager(DefaultSearchConfig())
	partialCalls, completeCalls := 0, 0
	manager.fetchEngines = []FetchEngine{
		fetchTimeoutTestEngine{name: "partial", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			partialCalls++
			return &FetchResult{URL: rawURL, Content: "cut-off page", BytesRead: 12, Partial: true}, io.ErrUnexpectedEOF
		}},
		fetchTimeoutTestEngine{name: "complete", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			completeCalls++
			return &FetchResult{URL: rawURL, Content: "complete replacement page", BytesRead: 25}, nil
		}},
	}

	result, attempts, err := manager.FetchURLWithDiagnostics(context.Background(), fetchTimeoutTestURL, 1000)
	if err != nil || result == nil || result.Partial || result.Content != "complete replacement page" || result.Source != "complete" {
		t.Fatalf("a complete fallback must replace the incomplete page: result=%#v err=%v", result, err)
	}
	if len(attempts) != 1 || attempts[0].Engine != "partial" || !strings.Contains(attempts[0].Error, io.ErrUnexpectedEOF.Error()) {
		t.Fatalf("partial attempt diagnostics lost: %#v", attempts)
	}
	cached, ok := manager.fetchCache.Get(fetchTimeoutTestURL, 1000)
	if !ok || cached.Partial || cached.Content != result.Content || cached.Source != "complete" {
		t.Fatalf("only the complete fallback may be cached: %#v (present=%v)", cached, ok)
	}
	result, _, err = manager.FetchURLWithDiagnostics(context.Background(), fetchTimeoutTestURL, 1000)
	if err != nil || result == nil || result.Content != "complete replacement page" || partialCalls != 1 || completeCalls != 1 {
		t.Fatalf("second call did not reuse the complete page: result=%#v err=%v calls=%d/%d", result, err, partialCalls, completeCalls)
	}
}

func TestFetchLateSuccessAfterDeadlineCannotBecomeSuccessfulOrCached(t *testing.T) {
	manager := NewManager(DefaultSearchConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	fallbackCalls := 0
	manager.fetchEngines = []FetchEngine{
		fetchTimeoutTestEngine{name: "late", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			<-ctx.Done()
			// A misbehaving engine may return data and nil after cancellation.
			return &FetchResult{URL: rawURL, Content: "late page", BytesRead: 9}, nil
		}},
		fetchTimeoutTestEngine{name: "fallback", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
			fallbackCalls++
			return &FetchResult{URL: rawURL, Content: "another page"}, nil
		}},
	}

	result, attempts, err := manager.FetchURLWithDiagnostics(ctx, fetchTimeoutTestURL, 1000)

	if !errors.Is(err, context.DeadlineExceeded) || result == nil || !result.Partial || result.Content != "late page" {
		t.Fatalf("late data incorrectly became a successful page: result=%#v err=%v", result, err)
	}
	if fallbackCalls != 0 || len(attempts) != 1 || attempts[0].Error == "" {
		t.Fatalf("late success lost deadline diagnostics or started fallback: calls=%d attempts=%#v", fallbackCalls, attempts)
	}
	if cached, ok := manager.fetchCache.Get(fetchTimeoutTestURL, 1000); ok {
		t.Fatalf("late success was cached: %#v", cached)
	}
}

func TestFetchCancelledContextDoesNotStartEnginesOrReturnCachedSuccess(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty-cache", true: "cached-page"}[cached], func(t *testing.T) {
			manager := NewManager(DefaultSearchConfig())
			if cached {
				manager.fetchCache.Set(fetchTimeoutTestURL, 1000, &FetchResult{URL: fetchTimeoutTestURL, Content: "cached complete page"})
			}
			calls := 0
			manager.fetchEngines = []FetchEngine{
				fetchTimeoutTestEngine{name: "unexpected", fetch: func(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
					calls++
					return &FetchResult{URL: rawURL, Content: "must not run"}, nil
				}},
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			result, attempts, err := manager.FetchURLWithDiagnostics(ctx, fetchTimeoutTestURL, 1000)

			if !errors.Is(err, context.Canceled) || result != nil || len(attempts) != 0 || calls != 0 {
				t.Fatalf("cancelled fetch performed work: result=%#v attempts=%#v err=%v calls=%d", result, attempts, err, calls)
			}
			_, present := manager.fetchCache.Get(fetchTimeoutTestURL, 1000)
			if present != cached {
				t.Fatal("cancelled fetch changed the cache")
			}
		})
	}
}

func TestCurlFetchMaxCharsIsSuccessfulTruncationAndCanBeCached(t *testing.T) {
	requireFetchTestCurl(t)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	const body = "<html><body><p>A complete article with enough readable text to meet extraction limits and demonstrate deliberate display truncation.</p></body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	manager := NewManager(DefaultSearchConfig())
	manager.fetchEngines = []FetchEngine{NewCurlEngine(server.URL)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := manager.FetchURLWithDiagnostics(ctx, fetchTimeoutTestURL, 24)

	if err != nil || result == nil || !result.Truncated || result.Partial || result.BytesRead != len(body) {
		t.Fatalf("intentional display limit became a partial/error: result=%#v err=%v", result, err)
	}
	if !strings.HasPrefix(result.Content, "A complete article with ") {
		t.Fatalf("display prefix changed: %q", result.Content)
	}
	cached, ok := manager.fetchCache.Get(fetchTimeoutTestURL, 24)
	if !ok || !cached.Truncated || cached.Partial || cached.Content != result.Content {
		t.Fatalf("a complete but display-limited page should be cached: %#v (present=%v)", cached, ok)
	}
}

type fetchTimeoutTestEngine struct {
	name  string
	fetch func(context.Context, string, int) (*FetchResult, error)
}

func (e fetchTimeoutTestEngine) Name() string { return e.name }

func (e fetchTimeoutTestEngine) Fetch(ctx context.Context, rawURL string, maxChars int) (*FetchResult, error) {
	return e.fetch(ctx, rawURL, maxChars)
}

func requireFetchTestCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Fatalf("real fetch timeout regression requires the tool's curl dependency: %v", err)
	}
}
