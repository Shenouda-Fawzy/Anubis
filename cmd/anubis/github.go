package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	ghb "github.com/google/go-github/v90/github"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	GhbClient  *ghb.Client
}

func NewGithubClient(token, baseURL string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultGitHubBaseURL
	}
	httpClient := &http.Client{Timeout: httpTimeout}

	opts := []ghb.ClientOptionsFunc{ghb.WithHTTPClient(httpClient)}
	trimmedBaseURL := strings.TrimRight(baseURL, "/")
	if trimmedBaseURL != defaultGitHubBaseURL {
		sdkBaseURL := trimmedBaseURL
		opts = append(opts, ghb.WithURLs(&sdkBaseURL, nil))
	}
	if token != "" {
		opts = append(opts, ghb.WithAuthToken(token))
	}

	c, err := ghb.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	slog.Debug("github client configured", "base_url", trimmedBaseURL, "authenticated", token != "")
	return &Client{BaseURL: trimmedBaseURL, Token: token, HTTPClient: httpClient, GhbClient: c}, nil
}

type PullRequest struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Description string `json:"body"`
	HTMLURL     string `json:"html_url"`
}

type File struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	slog.Debug("GetPullRequest started")
	defer slog.Debug("GetPullRequest done")

	pr, _, err := c.GhbClient.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return PullRequest{}, err
	}
	return PullRequest{
		Number:      pr.GetNumber(),
		Title:       pr.GetTitle(),
		Description: pr.GetBody(),
		HTMLURL:     pr.GetHTMLURL(),
	}, nil
}

func (c *Client) GetDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	slog.Debug("GetDiff started")
	defer slog.Debug("GetDiff done")

	diff, _, err := c.GhbClient.PullRequests.GetRaw(ctx, owner, repo, number, ghb.RawOptions{Type: ghb.Diff})
	return diff, err
}

// ListFiles gets list of changed/added files on the PR
// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#list-pull-requests-files
func (c *Client) ListFiles(ctx context.Context, owner, repo string, number int) ([]File, error) {
	slog.Debug("ListFiles started")
	defer slog.Debug("ListFiles done")
	var all []File
	opts := &ghb.ListOptions{PerPage: 100}
	for {
		files, resp, err := c.GhbClient.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			all = append(all, File{
				Filename:  f.GetFilename(),
				Status:    f.GetStatus(),
				Patch:     f.GetPatch(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
			})
		}
		if resp == nil || resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// CreateComment posts body as a comment on the pull request and returns the URL
// of the created comment.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) (string, error) {
	slog.Info("publishing review comment", "owner", owner, "repo", repo, "number", number, "bytes", len(body))
	created, _, err := c.GhbClient.Issues.CreateComment(ctx, owner, repo, number, &ghb.IssueComment{Body: ghb.Ptr(body)})
	if err != nil {
		return "", err
	}
	url := created.GetHTMLURL()
	slog.Info("review comment published", "url", url)
	return url, nil
}
