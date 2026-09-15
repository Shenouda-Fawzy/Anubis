package main

import (
	"encoding/json"
	"errors"
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
