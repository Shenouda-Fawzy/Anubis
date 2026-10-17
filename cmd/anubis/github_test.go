package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The GitHub REST API is hosted at a different path on GitHub Enterprise
// (https://host/api/v3) than on github.com (https://api.github.com). These
// tests pin that path prefix so a change to the base-URL handling cannot
// silently send Enterprise requests to github.com.
//
// The end-to-end tests use a bare httptest host with no path prefix, so they
// cannot catch this class of bug.

// NewGithubClient builds a client whose go-github HTTP client already has the
// 120s timeout. That is fine against a local test server, so the tests below
// deliberately use the client as constructed rather than swapping the transport.

func TestNewGithubClientDefaultsToDotCom(t *testing.T) {
	c, err := NewGithubClient("test-token", "")
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	if c.BaseURL != defaultGitHubBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, defaultGitHubBaseURL)
	}
}

func TestNewGithubClientTrimsTrailingSlash(t *testing.T) {
	c, err := NewGithubClient("test-token", "https://github.example.com/api/v3/")
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	if c.BaseURL != "https://github.example.com/api/v3" {
		t.Errorf("BaseURL = %q, want the trailing slash trimmed", c.BaseURL)
	}
}

func TestNewGithubClientRejectsInvalidBaseURL(t *testing.T) {
	if _, err := NewGithubClient("test-token", "://not-a-url"); err == nil {
		t.Fatal("NewGithubClient() error = nil, want an error for an unparseable base URL")
	}
}

func TestEnterprisePullRequestPathKeepsPrefix(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":18,"title":"Add /api/v1/users","body":"desc","html_url":"https://github.example.com/octo/repo/pull/18"}`))
	}))
	defer srv.Close()

	c, err := NewGithubClient("test-token", srv.URL+"/api/v3")
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	pr, err := c.GetPullRequest(context.Background(), "octo", "repo", 18)
	if err != nil {
		t.Fatalf("GetPullRequest() error = %v", err)
	}
	if want := "/api/v3/repos/octo/repo/pulls/18"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if want := "Bearer test-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if pr.Number != 18 || pr.Title != "Add /api/v1/users" {
		t.Errorf("GetPullRequest() = %+v, want number 18 and the mocked title", pr)
	}
	if pr.Description != "desc" {
		t.Errorf("Description = %q, want %q", pr.Description, "desc")
	}
}

// The raw diff download goes through a different go-github entry point than the
// JSON API, so it needs its own prefix assertion.
func TestEnterpriseDiffPathKeepsPrefix(t *testing.T) {
	var gotPath, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte("diff --git a/main.go b/main.go\n+added\n"))
	}))
	defer srv.Close()

	c, err := NewGithubClient("test-token", srv.URL+"/api/v3")
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	diff, err := c.GetDiff(context.Background(), "octo", "repo", 18)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if want := "/api/v3/repos/octo/repo/pulls/18"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if strings.Contains(gotAccept, "diff") == false {
		t.Errorf("Accept = %q, want it to request the diff media type", gotAccept)
	}
	if strings.HasPrefix(diff, "diff --git") == false {
		t.Errorf("GetDiff() = %q, want the raw diff", diff)
	}
}

// The head branch and head repository are what the custom-agent loader gates
// on, and the base branch is what allowed_target_branch matches, so all three
// must survive parsing. The null head repo is the deleted-fork case: it must
// come back empty rather than panic the getter chain.
func TestGetPullRequestReadsHeadBranchAndRepo(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantRef  string
		wantRepo string
		wantBase string
	}{
		{
			name:     "same repository",
			body:     `{"number":1,"head":{"ref":"feature/agents","repo":{"full_name":"octo/repo"}},"base":{"ref":"main"}}`,
			wantRef:  "feature/agents",
			wantRepo: "octo/repo",
			wantBase: "main",
		},
		{
			name:     "fork into development",
			body:     `{"number":1,"head":{"ref":"patch-1","repo":{"full_name":"someone/repo"}},"base":{"ref":"development"}}`,
			wantRef:  "patch-1",
			wantRepo: "someone/repo",
			wantBase: "development",
		},
		{
			name:     "deleted fork",
			body:     `{"number":1,"head":{"ref":"patch-1","repo":null},"base":{"ref":"main"}}`,
			wantRef:  "patch-1",
			wantRepo: "",
			wantBase: "main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c, err := NewGithubClient("test-token", srv.URL)
			if err != nil {
				t.Fatalf("NewGithubClient() error = %v", err)
			}
			pr, err := c.GetPullRequest(context.Background(), "octo", "repo", 1)
			if err != nil {
				t.Fatalf("GetPullRequest() error = %v", err)
			}
			if pr.SourceBranch != tc.wantRef {
				t.Errorf("SourceBranch = %q, want %q", pr.SourceBranch, tc.wantRef)
			}
			if pr.Repository != tc.wantRepo {
				t.Errorf("Repository = %q, want %q", pr.Repository, tc.wantRepo)
			}
			if pr.TargetBranch != tc.wantBase {
				t.Errorf("TargetBranch = %q, want %q", pr.TargetBranch, tc.wantBase)
			}
		})
	}
}

// A base URL that ends in a slash must not produce a doubled path segment.
func TestTrailingSlashDoesNotDoubleThePath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":1}`))
	}))
	defer srv.Close()

	c, err := NewGithubClient("test-token", srv.URL+"/")
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	if _, err := c.GetPullRequest(context.Background(), "octo", "repo", 1); err != nil {
		t.Fatalf("GetPullRequest() error = %v", err)
	}
	if want := "/repos/octo/repo/pulls/1"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}
