// Package github provides the minimal GitHub pull-request API used by Anubis.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
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
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: http.DefaultClient}
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

func (c *Client) do(ctx context.Context, method, path string, accept string, requestBody any, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		data, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		log.Printf("ReqBody: %s\n", string(data))

		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	log.Printf("URL: %s %s\n", method, path)
	log.Println("token=", c.Token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("API error response= %v", err)
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		log.Printf("error response= %v", err)
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github: HTTP %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if responseBody != nil && len(data) > 0 {
		return json.Unmarshal(data, responseBody)
	}
	log.Println("Resp body=", string(data))
	return nil
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var pr PullRequest
	log.Println("GetPullRequest started")
	defer log.Println("GetPullRequest done")
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), "application/vnd.github+json", nil, &pr)
	return pr, err
}

func (c *Client) GetDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	log.Println("GetDiff started")
	defer log.Println("GetDiff done")
	var data []byte
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github: HTTP %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func (c *Client) ListFiles(ctx context.Context, owner, repo string, number int) ([]File, error) {
	log.Println("ListFiles started")
	defer log.Println("ListFiles done")
	var all []File
	for page := 1; ; page++ {
		var files []File
		err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%s", owner, repo, number, strconv.Itoa(page)), "application/vnd.github+json", nil, &files)
		if err != nil {
			return nil, err
		}
		all = append(all, files...)
		if len(files) < 100 {
			return all, nil
		}
	}
}

func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	log.Println("CreateComment started")
	defer log.Println("CreateComment done")
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number), "application/vnd.github+json", map[string]string{"body": body}, nil)
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
