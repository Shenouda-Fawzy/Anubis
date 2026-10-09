package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
)

// --- parser ---

func TestParseAgentCardStripsFrontMatter(t *testing.T) {
	raw := "---\nname: docs\n" +
		"description: Review documentation and comments.\n" +
		"model: cheap-model\n" +
		"---\n\nYou are a documentation reviewer.\nBe concise.\n"

	card, err := parseAgentCard("docs.md", []byte(raw))
	if err != nil {
		t.Fatalf("parseAgentCard() error = %v", err)
	}
	if card.Name != "docs" {
		t.Errorf("Name = %q, want %q", card.Name, "docs")
	}
	if card.Description != "Review documentation and comments." {
		t.Errorf("Description = %q", card.Description)
	}
	if card.Model != "cheap-model" {
		t.Errorf("Model = %q, want %q", card.Model, "cheap-model")
	}
	if card.SystemPrompt != "You are a documentation reviewer.\nBe concise." {
		t.Errorf("SystemPrompt = %q, want the body with front-matter removed", card.SystemPrompt)
	}
	if strings.Contains(card.SystemPrompt, "name: docs") {
		t.Error("SystemPrompt still contains front-matter")
	}
}

func TestParseAgentCardDerivesNameAndFocus(t *testing.T) {
	card, err := parseAgentCard("accessibility.md", []byte("You review accessibility.\n"))
	if err != nil {
		t.Fatalf("parseAgentCard() error = %v", err)
	}
	if card.Name != "accessibility" {
		t.Errorf("Name = %q, want the filename without .md", card.Name)
	}
	if card.Description != defaultAgentFocus {
		t.Errorf("Description = %q, want the default focus", card.Description)
	}
}

func TestParseAgentCardRejectsEmptyBody(t *testing.T) {
	cases := map[string]string{
		"empty file":        "",
		"whitespace only":   "   \n\n",
		"front-matter only": "---\nname: x\ndescription: y\n---\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAgentCard("x.md", []byte(raw)); err == nil {
				t.Error("parseAgentCard() error = nil, want an empty-body error")
			}
		})
	}
}

func TestParseAgentCardHandlesCRLFAndQuotes(t *testing.T) {
	raw := "---\r\nname: \"security\"\r\ndescription: 'quoted focus'\r\n---\r\nBody here.\r\n"
	card, err := parseAgentCard("security.md", []byte(raw))
	if err != nil {
		t.Fatalf("parseAgentCard() error = %v", err)
	}
	if card.Name != "security" {
		t.Errorf("Name = %q, want the unquoted value", card.Name)
	}
	if card.Description != "quoted focus" {
		t.Errorf("Description = %q, want the unquoted value", card.Description)
	}
	if !strings.Contains(card.SystemPrompt, "Body here.") {
		t.Errorf("SystemPrompt = %q, want the CRLF body", card.SystemPrompt)
	}
}

// A horizontal rule at the top of a body must not be mistaken for front-matter.
func TestParseAgentCardBodyWithoutClosingFence(t *testing.T) {
	raw := "---\nThis is a body that starts with a rule.\nmore text\n"
	card, err := parseAgentCard("odd.md", []byte(raw))
	if err != nil {
		t.Fatalf("parseAgentCard() error = %v", err)
	}
	if !strings.Contains(card.SystemPrompt, "This is a body") {
		t.Errorf("SystemPrompt = %q, want the body preserved", card.SystemPrompt)
	}
}

// The built-in prompts are the reference format; the parser must accept them.
func TestParseAgentCardAcceptsBuiltinPrompts(t *testing.T) {
	builtins := map[string]string{
		"security":        securityAgentPrompt,
		"correctness":     correctnessAgentPrompt,
		"performance":     performanceAgentPrompt,
		"maintainability": maintainabilityAgentPrompt,
		"master":          masterPrompt,
	}
	for wantName, raw := range builtins {
		t.Run(wantName, func(t *testing.T) {
			card, err := parseAgentCard(wantName+".md", []byte(raw))
			if err != nil {
				t.Fatalf("parseAgentCard() error = %v", err)
			}
			if card.Name != wantName+"-agent" && card.Name != wantName {
				t.Errorf("Name = %q, want %q or %q", card.Name, wantName+"-agent", wantName)
			}
			if card.Description == "" || card.SystemPrompt == "" {
				t.Errorf("parsed builtin %q is missing description or prompt", wantName)
			}
			if strings.HasPrefix(card.SystemPrompt, "---") {
				t.Errorf("parsed builtin %q kept its front-matter", wantName)
			}
		})
	}
}

// --- loader ---

const (
	agentTestOwner = "acme"
	agentTestRepo  = "widgets"
)

type agentMockGitHub struct {
	mu            sync.Mutex
	defaultBranch string
	listing       []map[string]any
	files         map[string]string
	dirStatus     int
	refs          []string
}

func (m *agentMockGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.refs = append(m.refs, r.URL.Query().Get("ref"))
	m.mu.Unlock()

	dirPath := "/repos/" + agentTestOwner + "/" + agentTestRepo + "/contents/" + agentsDir
	switch {
	case r.URL.Path == "/repos/"+agentTestOwner+"/"+agentTestRepo:
		writeTestJSON(w, http.StatusOK, map[string]any{"default_branch": m.defaultBranch})
	case r.URL.Path == dirPath:
		if m.dirStatus != 0 && m.dirStatus != http.StatusOK {
			writeTestJSON(w, m.dirStatus, map[string]any{"message": "error"})
			return
		}
		writeTestJSON(w, http.StatusOK, m.listing)
	case strings.HasPrefix(r.URL.Path, dirPath+"/"):
		name := strings.TrimPrefix(r.URL.Path, dirPath+"/")
		content, ok := m.files[name]
		if !ok {
			writeTestJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		writeTestJSON(w, http.StatusOK, map[string]any{
			"name":     path.Base(name),
			"path":     agentsDir + "/" + name,
			"type":     "file",
			"size":     len(content),
			"content":  base64.StdEncoding.EncodeToString([]byte(content)),
			"encoding": "base64",
		})
	default:
		http.NotFound(w, r)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newAgentTestClient(t *testing.T, mock *agentMockGitHub) *Client {
	t.Helper()
	srv := httptest.NewServer(mock)
	t.Cleanup(srv.Close)
	client, err := NewGithubClient("test-token", srv.URL)
	if err != nil {
		t.Fatalf("NewGithubClient() error = %v", err)
	}
	return client
}

func listingEntry(name, content string) map[string]any {
	return map[string]any{
		"name": name,
		"path": agentsDir + "/" + name,
		"type": "file",
		"size": len(content),
	}
}

func TestLoadAgentSetReadsSpecialistsAndMaster(t *testing.T) {
	sec := "---\nname: security\ndescription: Find security bugs.\n---\nYou are a security reviewer.\n"
	perf := "You are a performance reviewer.\n"
	master := "---\nname: master-agent\n---\nYou are the custom coordinator.\n"
	mock := &agentMockGitHub{
		defaultBranch: "main",
		listing: []map[string]any{
			listingEntry("security.md", sec),
			listingEntry(masterAgentFile, master),
			listingEntry("performance.md", perf),
			listingEntry("README.txt", "not an agent"),
		},
		files: map[string]string{
			"security.md":    sec,
			masterAgentFile:  master,
			"performance.md": perf,
		},
	}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "default-model")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v", err)
	}
	if set.Ref != "main" {
		t.Errorf("Ref = %q, want %q", set.Ref, "main")
	}
	if len(set.Specialists) != 2 {
		t.Fatalf("specialists = %d, want 2 (master and non-md excluded)", len(set.Specialists))
	}
	// Sorted by filename: performance before security.
	if set.Specialists[0].Name != "performance" || set.Specialists[1].Name != "security" {
		t.Errorf("specialist order = %q, %q; want performance, security",
			set.Specialists[0].Name, set.Specialists[1].Name)
	}
	if set.Specialists[0].Model != "default-model" {
		t.Errorf("default model not applied: %q", set.Specialists[0].Model)
	}
	if set.Specialists[1].Description != "Find security bugs." {
		t.Errorf("front-matter description lost: %q", set.Specialists[1].Description)
	}
	if set.MasterPrompt != "You are the custom coordinator." {
		t.Errorf("MasterPrompt = %q", set.MasterPrompt)
	}
}

func TestLoadAgentSetMissingDirectoryIsNotAnError(t *testing.T) {
	mock := &agentMockGitHub{defaultBranch: "main", dirStatus: http.StatusNotFound}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "m")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v, want nil for a missing directory", err)
	}
	if len(set.Specialists) != 0 || set.MasterPrompt != "" {
		t.Errorf("missing directory produced %+v, want an empty set", set)
	}
}

func TestLoadAgentSetPropagatesAPIError(t *testing.T) {
	mock := &agentMockGitHub{defaultBranch: "main", dirStatus: http.StatusInternalServerError}
	c := newAgentTestClient(t, mock)

	if _, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "m"); err == nil {
		t.Error("loadCustomAgentSet() error = nil, want the API error propagated")
	}
}

func TestLoadAgentSetSkipsInvalidAndOversize(t *testing.T) {
	good := "You are a good reviewer.\n"
	empty := "---\nname: empty\n---\n"
	big := strings.Repeat("x", maxAgentFileBytes+1)
	mock := &agentMockGitHub{
		defaultBranch: "main",
		listing: []map[string]any{
			listingEntry("good.md", good),
			listingEntry("empty.md", empty),
			listingEntry("big.md", big),
		},
		files: map[string]string{
			"good.md":  good,
			"empty.md": empty,
			"big.md":   big,
		},
	}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "m")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v", err)
	}
	if len(set.Specialists) != 1 || set.Specialists[0].Name != "good" {
		t.Fatalf("specialists = %+v, want only %q", set.Specialists, "good")
	}
}

func TestLoadAgentSetCapsFileCount(t *testing.T) {
	files := map[string]string{}
	listing := make([]map[string]any, 0, maxAgentFiles+3)
	for i := 0; i < maxAgentFiles+3; i++ {
		name := string(rune('a'+i)) + ".md"
		body := "reviewer " + name + "\n"
		files[name] = body
		listing = append(listing, listingEntry(name, body))
	}
	mock := &agentMockGitHub{defaultBranch: "main", listing: listing, files: files}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "m")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v", err)
	}
	if len(set.Specialists) != maxAgentFiles {
		t.Errorf("specialists = %d, want the cap %d", len(set.Specialists), maxAgentFiles)
	}
}

// Reading from the default branch, not the pull-request head, is the guard that
// stops a contributor from rewriting the instructions that review their change.
func TestLoadAgentSetUsesDefaultBranchRef(t *testing.T) {
	mock := &agentMockGitHub{defaultBranch: "release", listing: []map[string]any{}}
	c := newAgentTestClient(t, mock)

	if _, err := loadCustomAgentGroup(t.Context(), c, agentTestOwner, agentTestRepo, "m"); err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for _, ref := range mock.refs {
		if ref == "release" {
			return
		}
	}
	t.Errorf("no request used the default branch ref; refs = %v", mock.refs)
}

// --- selection ---

func TestSelectAgentsReplacesBuiltins(t *testing.T) {
	llm := &OpenAIClient{Model: "m"}
	custom := []*AgentCard{{Name: "docs", Description: "d", SystemPrompt: "p"}}

	agents, replaced := selectAgents(custom, llm)
	if !replaced {
		t.Error("replaced = false, want true when custom agents exist")
	}
	if len(agents) != 1 || agents[0].Name != "docs" {
		t.Fatalf("agents = %+v, want only the custom agent", agents)
	}
	if agents[0].LlmClient != llm {
		t.Error("custom agent is not wired to the shared client")
	}
	if agents[0].Model != "m" {
		t.Errorf("Model = %q, want the client model", agents[0].Model)
	}
}

func TestSelectAgentsKeepsBuiltinsWhenNoCustom(t *testing.T) {
	llm := &OpenAIClient{Model: "m"}
	agents, replaced := selectAgents(nil, llm)
	if replaced {
		t.Error("replaced = true, want false when there are no custom agents")
	}
	if len(agents) != len(defaultAgents(llm)) {
		t.Errorf("agents = %d, want the built-in set", len(agents))
	}
}
