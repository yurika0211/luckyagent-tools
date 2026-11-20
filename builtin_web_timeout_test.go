package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	searchpkg "github.com/yurika0211/luckyagent-tools/search"
)

func TestWebFetchBodyDeadlineReportsPartialAndFails(t *testing.T) {
	const body = "<html><body>" + "This partial page arrived before the deadline; do not claim it is complete." + "</body></html>"
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			useOnlyCurlForWebFetch(t)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodConnect {
					http.Error(w, "reader proxy unavailable", http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, body)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer proxy.Close()
			t.Setenv("LH_SEARCH_PROXY", proxy.URL)

			out, err := handleWebFetch(&WebSearchConfig{Proxy: proxy.URL}, map[string]any{
				"url": "http://93.184.216.34/page", "timeout": 1, "format": format,
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline with preserved output, got %v: %s", err, out)
			}
			if !strings.Contains(out, "This partial page arrived before the deadline") {
				t.Fatalf("partial readable body was discarded: %s", out)
			}
			if !strings.Contains(err.Error(), "timeout_seconds=1") || !strings.Contains(err.Error(), "elapsed=") {
				t.Fatalf("missing timeout diagnostics: %v", err)
			}
			if format == "json" {
				var result struct {
					Status         string `json:"status"`
					Partial        bool   `json:"partial"`
					TimedOut       bool   `json:"timed_out"`
					BytesRead      int    `json:"bytes_read"`
					TimeoutSeconds int    `json:"timeout_seconds"`
					Elapsed        string `json:"elapsed"`
					ElapsedMS      int64  `json:"elapsed_ms"`
				}
				if json.Unmarshal([]byte(out), &result) != nil || result.Status != "partial" || !result.Partial || !result.TimedOut || result.BytesRead != len(body) || result.TimeoutSeconds != 1 || result.Elapsed == "" || result.ElapsedMS < 500 {
					t.Fatalf("partial metadata is incorrect: %s", out)
				}
			} else {
				for _, expected := range []string{"Status: partial", "Partial: true", "Timed-Out: true", "Bytes-Read:", "Timeout-Seconds: 1", "Elapsed:"} {
					if !strings.Contains(out, expected) {
						t.Fatalf("missing %q: %s", expected, out)
					}
				}
			}
		})
	}
}

func TestWebFetchIntentionalDisplayLimitRemainsSuccessful(t *testing.T) {
	useOnlyCurlForWebFetch(t)
	const body = "<html><body>" + "A complete article with sufficient content to pass extraction, ending normally." + "</body></html>"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			http.Error(w, "reader unavailable", http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer proxy.Close()
	t.Setenv("LH_SEARCH_PROXY", proxy.URL)
	out, err := handleWebFetch(&WebSearchConfig{Proxy: proxy.URL}, map[string]any{
		"url": "http://93.184.216.34/page", "max_chars": 20, "format": "json",
	})
	if err != nil {
		t.Fatalf("complete limited page failed: %v: %s", err, out)
	}
	var result struct {
		Status         string `json:"status"`
		Partial        bool   `json:"partial"`
		TimedOut       bool   `json:"timed_out"`
		Truncated      bool   `json:"truncated"`
		BytesRead      int    `json:"bytes_read"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || result.Status != "success" || result.Partial || result.TimedOut || !result.Truncated || result.BytesRead != len(body) || result.TimeoutSeconds != 30 {
		t.Fatalf("normal truncation was mislabeled: %s", out)
	}
}

func TestWebFetchTimeoutParameterAndEmptySearchContract(t *testing.T) {
	if got := WebFetchTool(nil).Parameters["timeout"].Default; got != 30 {
		t.Fatalf("fetch schema default = %v", got)
	}
	for _, tc := range []struct {
		value int
		want  int
	}{{90, 90}, {999, 120}} {
		opts, err := parseWebFetchOptions(map[string]any{"url": "http://93.184.216.34/page", "timeout": tc.value})
		if err != nil || opts.TimeoutSeconds != tc.want {
			t.Fatalf("timeout %d: options=%+v err=%v", tc.value, opts, err)
		}
	}
	if webSearchTimeout != 8*time.Second {
		t.Fatalf("web_search budget changed: %v", webSearchTimeout)
	}
	out, err := formatWebSearchJSON(webSearchOptions{Query: "missing", Count: 5}, "brave", &searchpkg.QuickSearchResult{Errors: []string{"no results"}})
	if err != nil || strings.Contains(out, "timeout") || !strings.Contains(out, "no results") {
		t.Fatalf("empty search was mislabeled: %v: %s", err, out)
	}
}

func useOnlyCurlForWebFetch(t *testing.T) {
	t.Helper()
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatal("curl is required for the actual fetch integration test")
	}
	bin := t.TempDir()
	target := filepath.Join(bin, filepath.Base(curl))
	data, err := os.ReadFile(curl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0755); err != nil {
		t.Fatal(err)
	}
	// Only curl is present: optional defuddle cannot contact an external site.
	t.Setenv("PATH", bin)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
}
