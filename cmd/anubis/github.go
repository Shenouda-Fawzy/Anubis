package main

import (
	"context"
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

func NewGithubClient(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	httpClient := http.DefaultClient
	opts := []ghb.ClientOptionsFunc{ghb.WithHTTPClient(httpClient)}
	trimmedBaseURL := strings.TrimRight(baseURL, "/")
	if trimmedBaseURL != "https://api.github.com" {
		sdkBaseURL := trimmedBaseURL
		opts = append(opts, ghb.WithURLs(&sdkBaseURL, nil))
	}
	if token != "" {
		opts = append(opts, ghb.WithAuthToken(token))
	}

	c, err := ghb.NewClient(opts...)
	if err != nil {
		slog.Error("failed to create github client", "error", err)
		return nil
	}
	return &Client{BaseURL: trimmedBaseURL, Token: token, HTTPClient: httpClient, GhbClient: c}
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
		Description: pr.GetDescription(),
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

func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	slog.Info("CreateComment started", "owner", owner, "repo", repo, "number", number)
	defer slog.Debug("CreateComment done")
	_, _, err := c.GhbClient.Issues.CreateComment(ctx, owner, repo, number, &ghb.IssueComment{Body: ghb.Ptr(body)})
	return err
}
