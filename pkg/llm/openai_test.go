package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClient(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":4}}`))
	}))
	defer server.Close()
	client := NewOpenAIClient("secret", server.URL+"/v1", "test-model")
	result, err := client.Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil || strings.TrimSpace(result.Content) != "ok" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(gotBody, `"response_format":{"type":"json_object"}`) {
		t.Fatalf("request did not force json_object response format: %s", gotBody)
	}
}

func TestOpenAIClientHonorsExplicitResponseFormat(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}}`))
	}))
	defer server.Close()
	client := NewOpenAIClient("secret", server.URL+"/v1", "test-model")
	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Type: "json_schema"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotBody, `"response_format":{"type":"json_schema"}`) {
		t.Fatalf("explicit response format not honored: %s", gotBody)
	}
}
