package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

	// #nosec G204 -- test-only: constant command; e2eBinaryPath is a file this
	// test created under its own os.MkdirTemp directory.
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
	e2eAgentCount = 4
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
	e2eCorrectnessFinding = "Correctness finding: the new handler never writes a response on the empty-id path."
	e2ePerformanceFinding = "Performance finding: the inner loop runs in O(n^2) over the user list."
	e2eStandardsFinding   = "Coding standards finding: the error return from db.Close is ignored."
	e2eCustomFinding      = "Custom agent finding: the commit message does not reference the issue."
)

// e2e custom agent fixtures: a repository-supplied specialist and coordinator.
const (
	e2eCustomAgentName = "custom.md"
	e2eCustomAgentBody = "---\nname: custom\ndescription: custom review focus\n---\n" +
		"You are a custom reviewer.\n"
	e2eCustomMasterName = "master-agent.md"
	e2eCustomMasterBody = "---\nname: master-agent\n---\nYou are the custom coordinator.\n"
)

const e2eFinalReview = "### [warning] Parameterize the SQL query in handleUsers\n\n" +
	"The id query parameter is concatenated directly into the SQL string, which allows SQL injection. " +
	"Use parameterized statements instead.\n"

// mockGitHub reproduces the handful of REST endpoints the CLI touches:
// GET repos/{owner}/{repo}/pulls/{n} (JSON, or raw diff by Accept header),
// POST repos/{owner}/{repo}/issues/{n}/comments, and — when the custom-agent
// test is running — the default branch and .anubis-agents contents.
type mockGitHub struct {
	mu           sync.Mutex
	prRequests   int
	diffRequests int
	comments     []string
	// agentFiles backs the .anubis-agents contents endpoints. Empty means the
	// directory is not served; add a helper or set it directly in a test.
	agentFiles map[string]string
}

// setAgentFiles installs the agents directory served to a run. It takes the
// lock because the mock server may already be handling requests.
func (m *mockGitHub) setAgentFiles(files map[string]string) {
	m.mu.Lock()
	m.agentFiles = files
	m.mu.Unlock()
}

func (m *mockGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	m.mu.Lock()
	agentFiles := m.agentFiles
	m.mu.Unlock()

	switch {
	case len(parts) == 3 && parts[0] == "repos" && r.Method == http.MethodGet:
		// GET repos/{owner}/{repo}: the default branch agents are read from.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"default_branch":"main"}`)
		return

	case len(parts) == 5 && parts[0] == "repos" && parts[3] == "contents" && r.Method == http.MethodGet:
		// Directory listing for .anubis-agents.
		if agentFiles == nil {
			http.NotFound(w, r)
			return
		}
		entries := make([]map[string]any, 0, len(agentFiles))
		for name, body := range agentFiles {
			entries = append(entries, map[string]any{
				"name": name,
				"path": ".anubis-agents/" + name,
				"type": "file",
				"size": len(body),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
		return

	case len(parts) == 6 && parts[0] == "repos" && parts[3] == "contents" && r.Method == http.MethodGet:
		name := parts[5]
		body, ok := agentFiles[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":     name,
			"path":     ".anubis-agents/" + name,
			"type":     "file",
			"size":     len(body),
			"content":  base64.StdEncoding.EncodeToString([]byte(body)),
			"encoding": "base64",
		})
		return

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
	case strings.Contains(user, "<reviewer_findings>"):
		m.mu.Lock()
		m.synthesisCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eFinalReview))
	case strings.Contains(user, "authentication, authorization, injection"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eSecurityFinding))
	case strings.Contains(user, "concrete bugs, incorrect edge cases"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eCorrectnessFinding))
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
	case strings.Contains(user, "custom review focus"):
		m.mu.Lock()
		m.agentCalls++
		m.mu.Unlock()
		_, _ = w.Write(llmResponseJSON(e2eCustomFinding))
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

	// #nosec G204 -- test-only: e2eBinaryPath is a file this test created under
	// its own os.MkdirTemp directory, and every arg is a test constant.
	cmd := exec.CommandContext(ctx, e2eBinaryPath, args...) // #nosec G204
	// Clear every provider variable so the test exercises the documented
	// ANUBIS_LLM_API_KEY path rather than inheriting the developer's shell. The
	// provider-named ones are emptied as well: Anubis must not read them, and an
	// end-to-end run that passed because one leaked in would hide that.
	cmd.Env = append(os.Environ(),
		"GITHUB_TOKEN=e2e-token",
		"ANUBIS_LLM_API_KEY=e2e-key",
		"OPENCODE_API_KEY=",
		"OPENAI_API_KEY=",
		"GEMINI_API_KEY=",
		"AI_API_KEY=",
	)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
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

// TestE2ECustomAgents drives the -agents path end to end: the agent set is
// loaded from .anubis-agents on the default branch, replaces the built-ins, and
// master-agent.md overrides the coordinator. The published comment discloses
// both so a reader knows the built-in pipeline was replaced.
func TestE2ECustomAgents(t *testing.T) {
	h, ghURL, llmURL := newE2EHarness(t)
	h.github.setAgentFiles(map[string]string{
		e2eCustomAgentName:  e2eCustomAgentBody,
		e2eCustomMasterName: e2eCustomMasterBody,
	})

	out, code := runAnubis(
		t,
		"-repo", e2eOwner+"/"+e2eRepo,
		"-pr", "1",
		"-github-base-url", ghURL,
		"-llm-base-url", llmURL,
		"-model", "e2e-model",
		"-publish",
		"-agents",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}

	// Only the single custom specialist runs; the four built-ins are replaced.
	if got := h.llm.agentCallsCount(); got != 1 {
		t.Errorf("LLM agent calls = %d, want 1 (custom specialist only)", got)
	}
	if got := h.llm.synthesisCallsCount(); got != 1 {
		t.Errorf("LLM synthesis calls = %d, want 1", got)
	}
	if got := h.llm.unexpectedCount(); got != 0 {
		t.Errorf("unexpected LLM requests = %d, want 0", got)
	}

	comments := h.github.commentBodies()
	if len(comments) != 1 {
		t.Fatalf("comments posted = %d, want 1", len(comments))
	}
	for _, want := range []string{e2eFinalReview, agentsDir + "/", masterAgentFile} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("comment does not mention %q:\n%s", want, comments[0])
		}
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
