package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
)

func TestClientPullRequestAndPublish(t *testing.T) {
	var comment string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(PullRequest{Number: 7, Title: "change"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			comment = payload["body"]
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient("token", server.URL)
	pr, err := client.GetPullRequest(context.Background(), "o", "r", 7)
	if err != nil || pr.Title != "change" {
		t.Fatalf("pr=%+v err=%v", pr, err)
	}
	err = client.PublishReview(context.Background(), "o", "r", 7, domain.Review{Summary: "summary", Findings: []domain.Finding{{Title: "bug", Description: "bad", Severity: domain.SeverityHigh}}})
	if err != nil || !strings.Contains(comment, "summary") || !strings.Contains(comment, "bug") {
		t.Fatalf("comment=%q err=%v", comment, err)
	}
}
