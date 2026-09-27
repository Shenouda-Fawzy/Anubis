package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const objectOK = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`

func TestParseResponseObject(t *testing.T) {
	resp, err := ParseResponse([]byte(objectOK))
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("ParseResponse() = nil, want non-nil")
	}
	if resp.HasError() {
		t.Error("HasError() = true, want false")
	}
	if got := resp.TextContent(); got != "ok" {
		t.Errorf("TextContent() = %q, want %q", got, "ok")
	}
	if !resp.Finished() {
		t.Error("Finished() = false, want true")
	}
	if got := resp.FinishReason(); got != "stop" {
		t.Errorf("FinishReason() = %q, want %q", got, "stop")
	}
}

func TestParseResponseTrimsWhitespace(t *testing.T) {
	resp, err := ParseResponse([]byte("\n   \t" + objectOK + "\n"))
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp == nil || resp.TextContent() != "ok" {
		t.Errorf("TextContent() = %v, want %q", resp.TextContent(), "ok")
	}
}

func TestParseResponseEmpty(t *testing.T) {
	cases := [][]byte{nil, {}, []byte("   \n\t  ")}
	for _, in := range cases {
		if _, err := ParseResponse(in); err == nil || !strings.Contains(err.Error(), "empty response") {
			t.Errorf("ParseResponse(%q) error = %v, want %q", in, err, "empty response")
		}
	}
}

func TestParseResponseErrorField(t *testing.T) {
	data := []byte(`{"error":{"message":"boom","type":"server_error","param":"p","code":500}}`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if !resp.HasError() {
		t.Error("HasError() = false, want true")
	}
	if resp.TextContent() != "" {
		t.Errorf("TextContent() = %q, want empty", resp.TextContent())
	}
	if resp.Finished() {
		t.Error("Finished() = true, want false")
	}
	if got := resp.FinishReason(); got != "n/a" {
		t.Errorf("FinishReason() = %q, want %q", got, "n/a")
	}
}

func TestParseResponseNoChoices(t *testing.T) {
	resp, err := ParseResponse([]byte(`{"choices":[]}`))
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp.TextContent() != "" {
		t.Errorf("TextContent() = %q, want empty", resp.TextContent())
	}
	if resp.Finished() {
		t.Error("Finished() = true, want false")
	}
	if got := resp.FinishReason(); got != "n/a" {
		t.Errorf("FinishReason() = %q, want %q", got, "n/a")
	}
}

func TestParseResponseNilMessage(t *testing.T) {
	data := []byte(`{"choices":[{"message":null,"finish_reason":"stop"}]}`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp.TextContent() != "" {
		t.Errorf("TextContent() = %q, want empty", resp.TextContent())
	}
}

func TestParseResponseNonStopReason(t *testing.T) {
	data := []byte(`{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp.TextContent() != "" {
		t.Errorf("TextContent() = %q, want empty for non-stop finish", resp.TextContent())
	}
	if resp.Finished() {
		t.Error("Finished() = true, want false")
	}
}

func TestParseResponseArray(t *testing.T) {
	data := []byte(`[{"choices":[{"message":{"role":"assistant","content":"first"},"finish_reason":"stop"}]},{"error":{"message":"second"}}]`)
	resp, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("ParseResponse() = nil, want first array element")
	}
	if got := resp.TextContent(); got != "first" {
		t.Errorf("TextContent() = %q, want %q (first element)", got, "first")
	}
}

func TestParseResponseEmptyArray(t *testing.T) {
	if _, err := ParseResponse([]byte("[]")); err == nil || !strings.Contains(err.Error(), "empty array response") {
		t.Errorf("ParseResponse([]) error = %v, want %q", err, "empty array response")
	}
}

func TestParseResponseUnexpectedFormat(t *testing.T) {
	cases := []string{"hello", "<html>", "42"}
	for _, in := range cases {
		if _, err := ParseResponse([]byte(in)); err == nil || !strings.Contains(err.Error(), "unexpected response format") {
			t.Errorf("ParseResponse(%q) error = %v, want %q", in, err, "unexpected response format")
		}
	}
}

func TestParseResponseInvalidJSON(t *testing.T) {
	cases := []string{`{"oops`, `[,]`}
	for _, in := range cases {
		if _, err := ParseResponse([]byte(in)); err == nil {
			t.Errorf("ParseResponse(%q) error = nil, want JSON error", in)
		} else {
			var syntaxErr *json.SyntaxError
			if !errors.As(err, &syntaxErr) {
				t.Errorf("ParseResponse(%q) error = %v, want json.SyntaxError", in, err)
			}
		}
	}
}

const okCompletion = `{"choices":[{"message":{"role":"assistant","content":"all good"},"finish_reason":"stop"}],"usage":{}}`

func TestCompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want %q", got, "Bearer test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q, want application/json", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var req CompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request body is not a valid CompletionRequest: %v", err)
		}
		if req.Model != "test-model" || len(req.Messages) != 2 {
			t.Errorf("request = model %q, %d messages", req.Model, len(req.Messages))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okCompletion)
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	req := NewCompletionRequest("test-model", "instruct", "prompt")
	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "all good" {
		t.Errorf("Content = %q, want %q", resp.Content, "all good")
	}
	if resp.FinishReason != "stop" || !resp.Completed() {
		t.Errorf("expected finish_reason stop, got %q", resp.FinishReason)
	}
}

func TestCompleteKeepsExistingEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions (not double-appended)", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okCompletion)
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL+"/chat/completions", "test-model")
	client.HTTPClient = srv.Client()
	if _, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestCompleteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":{"message":"boom","type":"server_error","param":"","code":500}}`)
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	_, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Complete() error = %v, want it to contain %q", err, "boom")
	}
}

func TestCompleteMissingCredentials(t *testing.T) {
	client := NewOpenAIClient("", "", "test-model")
	if _, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"}); err == nil {
		t.Fatal("Complete() error = nil, want missing-credentials error")
	}
}

func TestCompleteNilReceiver(t *testing.T) {
	var client *OpenAIClient
	if _, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"}); err == nil {
		t.Fatal("Complete() error = nil, want nil-client error")
	}
}

func TestCompleteContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Complete(ctx, &CompletionRequest{Model: "test-model"}); err == nil {
		t.Fatal("Complete() error = nil, want context-cancelled error")
	}
}

// A rate limit is the most likely production failure. The status code has to
// reach the operator, otherwise the provider's error body is parsed as if it
// were a completion and reported as an unexplained format error.
func TestCompleteSurfacesHTTPStatus(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantParts  []string
		wantAbsent []string
	}{
		{
			name:      "rate limited with an OpenAI-shaped body",
			status:    http.StatusTooManyRequests,
			body:      `{"error":{"message":"Rate limit reached for big-pickle","type":"rate_limit_error"}}`,
			wantParts: []string{"429", "Rate limit reached for big-pickle"},
		},
		{
			name:      "gateway error with an HTML body",
			status:    http.StatusBadGateway,
			body:      "<html>\n<head><title>502</title></head>\n<body>upstream unavailable</body>\n</html>",
			wantParts: []string{"502", "upstream unavailable"},
			// Collapsed to one line and truncated, so an error page cannot
			// flood the review log.
			wantAbsent: []string{"\n"},
		},
		{
			name:      "server error with a JSON body that is not OpenAI-shaped",
			status:    http.StatusServiceUnavailable,
			body:      `{"error":{"status":"UNAVAILABLE","details":[{"reason":"backend"}]}}`,
			wantParts: []string{"503"},
		},
		{
			name:      "unauthorized",
			status:    http.StatusUnauthorized,
			body:      `{"error":{"message":"invalid api key"}}`,
			wantParts: []string{"401", "invalid api key"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			client := NewOpenAIClient("test-key", srv.URL, "test-model")
			client.HTTPClient = srv.Client()
			_, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"})
			if err == nil {
				t.Fatal("Complete() error = nil, want an error carrying the HTTP status")
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Complete() error = %v, want it to contain %q", err, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(err.Error(), absent) {
					t.Errorf("Complete() error = %v, want it to not contain %q", err, absent)
				}
			}
		})
	}
}

// Content that stopped early is not a review. Returning it silently would let a
// truncated answer read as a clean bill of health.
func TestCompleteRejectsTruncatedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"half a review"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	resp, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"})
	if err == nil {
		t.Fatal("Complete() error = nil, want an error for a truncated response")
	}
	if resp != nil {
		t.Errorf("Complete() = %+v, want nil so the partial content cannot be published", resp)
	}
	if !strings.Contains(err.Error(), "length") {
		t.Errorf("Complete() error = %v, want it to name the finish reason", err)
	}
}

func TestCompleteRejectsResponseWithNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"usage":{"total_tokens":7}}`)
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	if _, err := client.Complete(context.Background(), &CompletionRequest{Model: "test-model"}); err == nil {
		t.Fatal("Complete() error = nil, want an error when the provider returns no choices")
	}
}

func TestSnippet(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"collapses newlines and tabs", "a\nb\tc", "a b c"},
		{"collapses runs of whitespace", "  a   \n\n  b  ", "a b"},
		{"strips control characters", "a\x00b\x1fc", "abc"},
		{"empty body is labelled", "", "(empty body)"},
		{"whitespace-only body is labelled", "  \n ", "(empty body)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snippet([]byte(tc.in)); got != tc.want {
				t.Errorf("snippet(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSnippetTruncatesLongBodies(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := snippet([]byte(long))
	if len(got) > 210 {
		t.Errorf("len(snippet(...)) = %d, want it bounded near 200", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("snippet(...) = %q, want a truncation marker", got)
	}
}
