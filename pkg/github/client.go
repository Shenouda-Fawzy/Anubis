// Package github provides the minimal GitHub pull-request API used by Anubis.
package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	ghb "github.com/google/go-github/v90/github"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	GhbClient  *ghb.Client
}

// API is the subset needed by command-line and integrations.
type API interface {
	GetPullRequest(context.Context, string, string, int) (PullRequest, error)
	GetDiff(context.Context, string, string, int) (string, error)
	ListFiles(context.Context, string, string, int) ([]File, error)
	CreateComment(context.Context, string, string, int, string) error
	PublishReview(context.Context, string, string, int, domain.Review) error
}

func NewClient(token, baseURL string) *Client {
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
		log.Println(err)
		return nil
	}
	return &Client{BaseURL: trimmedBaseURL, Token: token, HTTPClient: httpClient, GhbClient: c}
}

type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

type File struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	log.Println("GetPullRequest started")
	defer log.Println("GetPullRequest done")

	pr, _, err := c.GhbClient.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return PullRequest{}, err
	}
	return PullRequest{
		Number:  pr.GetNumber(),
		Title:   pr.GetTitle(),
		Body:    pr.GetBody(),
		HTMLURL: pr.GetHTMLURL(),
	}, nil
}

func (c *Client) GetDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	log.Println("GetDiff started")
	defer log.Println("GetDiff done")

	diff, _, err := c.GhbClient.PullRequests.GetRaw(ctx, owner, repo, number, ghb.RawOptions{Type: ghb.Diff})
	return diff, err
}

// ListFiles gets list of changed/added files on the PR
// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#list-pull-requests-files
func (c *Client) ListFiles(ctx context.Context, owner, repo string, number int) ([]File, error) {
	log.Println("ListFiles started")
	defer log.Println("ListFiles done")
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
	log.Println("CreateComment started")
	defer log.Println("CreateComment done")
	_, _, err := c.GhbClient.Issues.CreateComment(ctx, owner, repo, number, &ghb.IssueComment{Body: ghb.Ptr(body)})
	return err
}

func (c *Client) PublishReview(ctx context.Context, owner, repo string, number int, review domain.Review) error {
	log.Println("PublishReview started")
	defer log.Println("PublishReview done")
	var b strings.Builder
	b.WriteString("## Anubis review\n\n")
	b.WriteString(review.Summary)
	b.WriteString("\n\n")
	if len(review.Findings) > 0 {
		for _, f := range review.Findings {
			location := f.File
			if f.Line > 0 {
				location += ":" + strconv.Itoa(f.Line)
			}
			fmt.Fprintf(&b, "- **%s** %s — %s", f.Severity, f.Title, f.Description)
			if location != "" {
				fmt.Fprintf(&b, " (`%s`)", location)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, " Suggestion: %s", f.Suggestion)
			}
			b.WriteByte('\n')
		}
	}
	return c.CreateComment(ctx, owner, repo, number, b.String())
}
