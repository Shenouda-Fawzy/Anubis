package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	ghb "github.com/google/go-github/v92/github"
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

// ListFiles gets list of changed/added files on the PR (not the text diff itself - just the changed files)
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

// Default repo branch i.e. main/master/staging ... etc whatever the owner specified at as
// default branch on Github settings. On Github free plan, it is the main branch
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.GhbClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	branch := r.GetDefaultBranch()
	if branch == "" {
		return "", fmt.Errorf("repository %s/%s has no default branch", owner, repo)
	}
	return branch, nil
}

// directoryEntry is the subset of a repository directory listing that Anubis
// needs to decide which files to fetch.
type directoryEntry struct {
	Name string
	Path string
	Type string
	Size int
}

// List directory files (no file content yet)
func (c *Client) ListDirectory(ctx context.Context, owner, repo, path, branchName string) ([]directoryEntry, error) {
	_, dir, _, err := c.GhbClient.Repositories.GetContents(ctx, owner, repo, path, &ghb.RepositoryContentGetOptions{Ref: branchName})
	if err != nil {
		return nil, err
	}
	entries := make([]directoryEntry, 0, len(dir))
	for _, e := range dir {
		entries = append(entries, directoryEntry{
			Name: e.GetName(),
			Path: e.GetPath(),
			Type: e.GetType(),
			Size: e.GetSize(),
		})
	}
	return entries, nil
}

func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, branchName string) (string, error) {
	file, _, _, err := c.GhbClient.Repositories.GetContents(ctx, owner, repo, path, &ghb.RepositoryContentGetOptions{Ref: branchName})
	if err != nil {
		return "", err
	}
	if file == nil {
		return "", fmt.Errorf("github: %s is not a file", path)
	}
	return file.GetContent()
}

func isGitHubNotFound(err error) bool {
	var er *ghb.ErrorResponse
	return errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusNotFound
}

// CreateComment posts body as a comment on the pull request and returns the URL
// of the created comment.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) (string, error) {
	slog.Info("publishing review comment", "owner", owner, "repo", repo, "number", number, "bytes", len(body))
	comment := ghb.IssueCommentRequest{Body: body}
	created, _, err := c.GhbClient.Issues.CreateComment(ctx, owner, repo, number, comment)
	if err != nil {
		return "", err
	}
	url := created.GetHTMLURL()
	slog.Info("review comment published", "url", url)
	return url, nil
}
