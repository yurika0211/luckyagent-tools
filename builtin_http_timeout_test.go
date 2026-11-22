package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const httpTimeoutTestURL = "http://93.184.216.34/timeout-test"

type httpTimeoutTestResult struct {
	Status         string `json:"status"`
	StatusCode     int    `json:"status_code"`
	RequestStatus  string `json:"request_status"`
	Body           string `json:"body"`
	BytesRead      int    `json:"bytes_read"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Elapsed        string `json:"elapsed"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	Partial        bool   `json:"partial"`
	TimedOut       bool   `json:"timed_out"`
	Truncated      bool   `json:"truncated"`
}

func TestHTTPRequestTimeoutDefaultsAndMaximumReachTheRequest(t *testing.T) {
	if got := HTTPRequestTool().Parameters["timeout"].Default; got != 30 {
		t.Fatalf("model-visible default timeout = %v, want 30", got)
	}

	for _, tc := range []struct {
		name    string
		timeout any
		want    int
	}{
		{name: "default", want: 30},
		{name: "explicit", timeout: 90, want: 90},
		{name: "maximum", timeout: 999, want: 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var remaining time.Duration
			var hasDeadline bool
			previous := http.DefaultTransport
			http.DefaultTransport = httpTimeoutRoundTripper(func(req *http.Request) (*http.Response, error) {
				deadline, ok := req.Context().Deadline()
				hasDeadline = ok
				remaining = time.Until(deadline)
				return &http.Response{
					Status:     "200 OK",
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("complete body")),
					Request:    req,
				}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			args := map[string]any{"url": httpTimeoutTestURL, "format": "json"}
			if tc.timeout != nil {
				args["timeout"] = tc.timeout
			}

			output, err := handleHTTPRequest(args)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if !hasDeadline || remaining < time.Duration(tc.want-2)*time.Second || remaining > time.Duration(tc.want)*time.Second {
				t.Fatalf("actual request deadline = %s (present=%v), want about %ds", remaining, hasDeadline, tc.want)
			}
			result := decodeHTTPTimeoutResult(t, output)
			if result.TimeoutSeconds != tc.want || result.Status != "200 OK" || result.StatusCode != 200 || result.Body != "complete body" {
				t.Fatalf("unexpected successful response: %s", output)
			}
			if result.Partial || result.TimedOut || result.RequestStatus != "success" {
				t.Fatalf("complete response must remain successful: %s", output)
			}
		})
	}
}

func TestHTTPRequestBodyTimeoutRetainsPartialBytesAndFails(t *testing.T) {
	// Spaces and a multi-byte rune ensure bytes_read describes the received
	// payload, not the trimmed display body or its Unicode character count.
	const prefix = " \npartial response 前缀 that arrived before the deadline\n "
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = io.WriteString(w, prefix)
				w.(http.Flusher).Flush()
				<-req.Context().Done()
			}))
			t.Cleanup(server.Close)
			routeHTTPTimeoutTestTo(t, server)

			output, err := handleHTTPRequest(map[string]any{
				"url": httpTimeoutTestURL, "timeout": 1, "format": format,
			})
			if err == nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("body deadline must fail while preserving output: err=%v output=%q", err, output)
			}
			if !strings.Contains(output, strings.TrimSpace(prefix)) {
				t.Fatalf("partial body was discarded: %q", output)
			}
			if !strings.Contains(err.Error(), "timeout_seconds=1") || !strings.Contains(err.Error(), "elapsed=") {
				t.Fatalf("timeout error lost budget or elapsed time: %v", err)
			}
			if format == "json" {
				result := decodeHTTPTimeoutResult(t, output)
				if result.RequestStatus != "partial" || !result.Partial || !result.TimedOut || result.Truncated {
					t.Fatalf("deadline body was mislabeled: %s", output)
				}
				if result.BytesRead != len(prefix) || result.TimeoutSeconds != 1 || result.Status != "200 OK" {
					t.Fatalf("partial metadata lost actual received bytes or HTTP status: %s", output)
				}
				assertHTTPTimeoutElapsed(t, result)
			} else {
				for _, want := range []string{
					"Request-Status: partial", "Timeout-Seconds: 1", "Elapsed:",
					fmt.Sprintf("Bytes-Read: %d", len(prefix)), "Partial: true", "Timed-Out: true",
				} {
					if !strings.Contains(output, want) {
						t.Fatalf("text diagnostics lack %q: %s", want, output)
					}
				}
			}
		})
	}
}

func TestHTTPRequestTimeoutBeforeHeadersHasNoPartialBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	t.Cleanup(server.Close)
	routeHTTPTimeoutTestTo(t, server)

	output, err := handleHTTPRequest(map[string]any{"url": httpTimeoutTestURL, "timeout": 1, "format": "json"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request deadline must remain an error: err=%v output=%q", err, output)
	}
	result := decodeHTTPTimeoutResult(t, output)
	if result.RequestStatus != "timeout" || !result.TimedOut || result.Partial || result.BytesRead != 0 || result.Body != "" {
		t.Fatalf("no-response timeout must not invent a partial body: %s", output)
	}
	if result.TimeoutSeconds != 1 {
		t.Fatalf("timeout budget missing: %s", output)
	}
	assertHTTPTimeoutElapsed(t, result)
}

func TestHTTPRequestResponseLimitIsSuccessfulTruncationNotTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "abcdefgh")
	}))
	t.Cleanup(server.Close)
	routeHTTPTimeoutTestTo(t, server)

	output, err := handleHTTPRequest(map[string]any{
		"url": httpTimeoutTestURL, "max_response_bytes": 4, "format": "json",
	})
	if err != nil {
		t.Fatalf("an intentional response limit must not fail: %v", err)
	}
	result := decodeHTTPTimeoutResult(t, output)
	if !result.Truncated || result.Partial || result.TimedOut || result.RequestStatus != "success" || result.Body != "abcd" {
		t.Fatalf("intentional truncation was mislabeled: %s", output)
	}
	if result.Status != "200 OK" || result.BytesRead < 4 || result.BytesRead > 5 {
		t.Fatalf("response limit or HTTP status changed: %s", output)
	}
}

func TestHTTPRequestIncompleteBodyIsPartialButNotTimeout(t *testing.T) {
	const prefix = "a response that ended before its declared content length"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(prefix)+30))
		_, _ = io.WriteString(w, prefix)
	}))
	t.Cleanup(server.Close)
	routeHTTPTimeoutTestTo(t, server)

	output, err := handleHTTPRequest(map[string]any{"url": httpTimeoutTestURL, "format": "json"})
	if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short Content-Length body must preserve its real error: err=%v output=%q", err, output)
	}
	result := decodeHTTPTimeoutResult(t, output)
	if result.RequestStatus != "partial" || !result.Partial || result.TimedOut || result.Truncated || result.Body != prefix || result.BytesRead != len(prefix) {
		t.Fatalf("non-timeout body failure was mislabeled or lost content: %s", output)
	}
}

type httpTimeoutRoundTripper func(*http.Request) (*http.Response, error)

func (f httpTimeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// The production URL remains a public address and still passes its normal SSRF
// validation. Only the test transport's TCP destination is redirected to the
// local fixture; no production validation bypass is introduced.
func routeHTTPTimeoutTestTo(t *testing.T, server *httptest.Server) {
	t.Helper()
	previous := http.DefaultTransport
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
	}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = previous
		transport.CloseIdleConnections()
	})
}

func decodeHTTPTimeoutResult(t *testing.T, output string) httpTimeoutTestResult {
	t.Helper()
	var result httpTimeoutTestResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode HTTP diagnostics: %v, output=%q", err, output)
	}
	return result
}

func assertHTTPTimeoutElapsed(t *testing.T, result httpTimeoutTestResult) {
	t.Helper()
	elapsed, err := time.ParseDuration(result.Elapsed)
	if err != nil || elapsed <= 0 || result.ElapsedMS <= 0 {
		t.Fatalf("missing or invalid elapsed diagnostics: %#v (parse error %v)", result, err)
	}
	if delta := elapsed - time.Duration(result.ElapsedMS)*time.Millisecond; delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("elapsed fields disagree: elapsed=%s elapsed_ms=%d", elapsed, result.ElapsedMS)
	}
}
