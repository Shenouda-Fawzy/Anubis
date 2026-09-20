package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// e2eBinaryPath is the real Anubis CLI, built once in TestMain and driven
// end to end against mock GitHub and OpenAI-compatible LLM servers.
var e2eBinaryPath string

func TestMain(m *testing.M) {
	slog.Info("Test main started")
	root := moduleRoot()
	if root == "" {
		slog.Error("e2e: unable to locate go.mod")
		os.Exit(1)
	}

	tmp, err := os.MkdirTemp("", "anubis-e2e-*")
	if err != nil {
		slog.Error("e2e: create temp dir", "error", err)
		os.Exit(1)
	}
	e2eBinaryPath = filepath.Join(tmp, "anubis")

	// #nosec G204 -- test-only: command and args are constant except for a path under our own os.MkdirTemp dir
	cmd := exec.Command("go", "build", "-o", e2eBinaryPath, ".")
	cmd.Dir = filepath.Join(root, "cmd", "anubis")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmp)
		slog.Error("e2e: go build ./cmd/anubis failed", "error", err)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

const (
	e2eOwner      = "acme"
	e2eRepo       = "widgets"
	e2eAgentCount = 3
)

const e2ePRJSON = `{
  "number": 1,
  "title": "Add /api/v1/users endpoint",
  "body": "Introduces a public users endpoint and a query merge fix.",
  "html_url": "https://github.com/acme/widgets/pull/1",
  "state": "open"
}`

const e2eDiff = `diff --git a/src/server.go b/src/server.go
index 1234567..89abcde 100644
--- a/src/server.go
+++ b/src/server.go
@@ -12,7 +12,8 @@ func NewServer() *Server {
 		s.mux.HandleFunc("/health", s.handleHealth)
+		s.mux.HandleFunc("/api/v1/users", s.handleUsers)
 		return s
 	}
`

const (
	e2eSecurityFinding    = "Security finding: user-supplied id is interpolated into the SQL query."
	e2ePerformanceFinding = "Performance finding: the inner loop runs in O(n^2) over the user list."
	e2eStandardsFinding   = "Coding standards finding: the error return from db.Close is ignored."
)

const e2eFinalReview = "### [warning] Parameterize the SQL query in handleUsers\n\n" +
	"The id query parameter is concatenated directly into the SQL string, which allows SQL injection. " +
	"Use parameterized statements instead.\n"

// mockGitHub reproduces the handful of REST endpoints the CLI touches:
// GET repos/{owner}/{repo}/pulls/{n} (JSON, or raw diff by Accept header) and
// POST repos/{owner}/{repo}/issues/{n}/comments.
type mockGitHub struct {
	mu           sync.Mutex
	prRequests   int
	diffRequests int
	comments     []string
}

func (m *mockGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	switch {
	case len(parts) == 5 && parts[0] == "repos" && parts[3] == "pulls" && r.Method == http.MethodGet:
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			m.mu.Lock()
			m.diffRequests++
			m.mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, e2eDiff)
			return
		}
		m.mu.Lock()
		m.prRequests++
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, e2ePRJSON)
		return

	case len(parts) == 6 && parts[0] == "repos" && parts[3] == "issues" &&
		parts[5] == "comments" && r.Method == http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		var comment struct {
			Body string `json:"body"`
		}
		_ = json.Unmarshal(body, &comment)
		m.mu.Lock()
		m.comments = append(m.comments, comment.Body)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id": 1, "body": ""}`)
		return
	}

	http.NotFound(w, r)
}

func (m *mockGitHub) prRequestsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prRequests
}

func (m *mockGitHub) diffRequestsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.diffRequests
}

func (m *mockGitHub) commentBodies() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.comments...)
}

// mockLLM serves /chat/completions and routes the response based on the user
// message: sub-agent descriptions produce per-agent findings, and the final
// coordinator prompt produces the synthesized review.
type mockLLM struct {
	mu             sync.Mutex
	agentCalls     int
	synthesisCalls int
	unexpected     int
}

func (m *mockLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)

	user := ""
	if len(req.Messages) > 0 {
		user = req.Messages[len(req.Messages)-1].Content
	}

	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.Contains(user, "# Pull Request"):
		m.mu.Lock()
		m.synthesisCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eFinalReview))
	case strings.Contains(user, "authentication, authorization, injection"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eSecurityFinding))
	case strings.Contains(user, "avoidable latency"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2ePerformanceFinding))
	case strings.Contains(user, "maintainability, readability"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eStandardsFinding))
	default:
		m.mu.Lock()
		m.unexpected++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON("unexpected request: " + user))
	}
}

func (m *mockLLM) agentCallsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agentCalls
}

func (m *mockLLM) synthesisCallsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.synthesisCalls
}

func (m *mockLLM) unexpectedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unexpected
}

func llmResponseJSON(content string) []byte {
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{},
	})
	return payload
}

// e2eHarness bundles the mock servers backing an end-to-end run.
type e2eHarness struct {
	github *mockGitHub
	llm    *mockLLM
}

func newE2EHarness(t *testing.T) (*e2eHarness, string, string) {
	t.Helper()
	h := &e2eHarness{github: &mockGitHub{}, llm: &mockLLM{}}
	ghSrv := httptest.NewServer(h.github)
	llmSrv := httptest.NewServer(h.llm)
	t.Cleanup(ghSrv.Close)
	t.Cleanup(llmSrv.Close)
	return h, ghSrv.URL, llmSrv.URL
}

func runAnubis(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, e2eBinaryPath, args...)
	cmd.Env = append(
		os.Environ(),
		"GITHUB_TOKEN=e2e-token",
		"AI_API_KEY=e2e-key",
	)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(combined), ee.ExitCode()
		}
		t.Fatalf("run anubis: %v\n%s", err, combined)
	}
	return string(combined), 0
}

func TestE2EFullReviewPublishesComment(t *testing.T) {
	h, ghURL, llmURL := newE2EHarness(t)

	out, code := runAnubis(
		t,
		"-repo", e2eOwner+"/"+e2eRepo,
		"-pr", "1",
		"-github-base-url", ghURL,
		"-llm-base-url", llmURL,
		"-model", "e2e-model",
		"-publish",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}

	if got := h.github.prRequestsCount(); got != 1 {
		t.Errorf("PR fetches = %d, want 1", got)
	}
	if got := h.github.diffRequestsCount(); got != 1 {
		t.Errorf("diff fetches = %d, want 1", got)
	}
	comments := h.github.commentBodies()
	if len(comments) != 1 {
		t.Fatalf("comments posted = %d, want 1", len(comments))
	}
	if comments[0] != e2eFinalReview {
		t.Errorf("comment body = %q, want %q", comments[0], e2eFinalReview)
	}

	if got := h.llm.agentCallsCount(); got != e2eAgentCount {
		t.Errorf("LLM agent calls = %d, want %d", got, e2eAgentCount)
	}
	if got := h.llm.synthesisCallsCount(); got != 1 {
		t.Errorf("LLM synthesis calls = %d, want 1", got)
	}
	if got := h.llm.unexpectedCount(); got != 0 {
		t.Errorf("unexpected LLM requests = %d, want 0", got)
	}
}

func TestE2EReviewWithoutPublishDoesNotComment(t *testing.T) {
	h, ghURL, llmURL := newE2EHarness(t)

	out, code := runAnubis(
		t,
		"-repo", e2eOwner+"/"+e2eRepo,
		"-pr", "1",
		"-github-base-url", ghURL,
		"-llm-base-url", llmURL,
		"-model", "e2e-model",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if got := h.github.commentBodies(); len(got) != 0 {
		t.Errorf("comments posted = %d, want 0", len(got))
	}
	if got := h.llm.agentCallsCount(); got != e2eAgentCount {
		t.Errorf("LLM agent calls = %d, want %d", got, e2eAgentCount)
	}
	if got := h.llm.synthesisCallsCount(); got != 1 {
		t.Errorf("LLM synthesis calls = %d, want 1", got)
	}
}

func TestE2EMissingRequiredArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"missing pr", []string{"-repo", e2eOwner + "/" + e2eRepo}},
		{"malformed repo", []string{"-repo", "not-qualified", "-pr", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runAnubis(t, tc.args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2\noutput:\n%s", code, out)
			}
		})
	}
}
