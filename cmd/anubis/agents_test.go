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
	if strings.Contains(card.SystemPrompt, "Body here.") == false {
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
	if strings.Contains(card.SystemPrompt, "This is a body") == false {
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

// agentTestPR builds the common case: a pull request from a branch of the
// repository under test, with no target-branch restriction.
func agentTestPR(branch string) PullRequest {
	return PullRequest{SourceBranch: branch, Repository: agentTestOwner + "/" + agentTestRepo}
}

type agentMockGitHub struct {
	mu        sync.Mutex
	listing   []map[string]any
	files     map[string]string
	dirStatus int
	refs      []string
	paths     []string
}

func (m *agentMockGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.refs = append(m.refs, r.URL.Query().Get("ref"))
	m.paths = append(m.paths, r.URL.Path)
	m.mu.Unlock()

	// The .anubis-agents contents endpoints are served for any owner/name, so a
	// fork repository is served too; tests assert which repository the loader
	// actually asked, via requestedPaths.
	contents := "/contents/" + agentsDir
	switch {
	case strings.HasSuffix(r.URL.Path, contents):
		if m.dirStatus != 0 && m.dirStatus != http.StatusOK {
			writeTestJSON(w, m.dirStatus, map[string]any{"message": "error"})
			return
		}
		writeTestJSON(w, http.StatusOK, m.listing)
	case strings.Contains(r.URL.Path, contents+"/"):
		name := r.URL.Path[strings.Index(r.URL.Path, contents)+len(contents)+1:]
		content, ok := m.files[name]
		if ok == false {
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

// requestedPaths returns a copy of the request paths the mock has served.
func (m *agentMockGitHub) requestedPaths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.paths...)
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

	set, err := loadCustomAgentsGroup(t.Context(), c, "default-model", agentTestPR("main"), "")
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
	mock := &agentMockGitHub{dirStatus: http.StatusNotFound}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentsGroup(t.Context(), c, "m", agentTestPR("main"), "")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v, want nil for a missing directory", err)
	}
	if len(set.Specialists) != 0 || set.MasterPrompt != "" {
		t.Errorf("missing directory produced %+v, want an empty set", set)
	}
}

func TestLoadAgentSetPropagatesAPIError(t *testing.T) {
	mock := &agentMockGitHub{dirStatus: http.StatusInternalServerError}
	c := newAgentTestClient(t, mock)

	if _, err := loadCustomAgentsGroup(t.Context(), c, "m", agentTestPR("main"), ""); err == nil {
		t.Error("loadCustomAgentSet() error = nil, want the API error propagated")
	}
}

func TestLoadAgentSetSkipsInvalidAndOversize(t *testing.T) {
	good := "You are a good reviewer.\n"
	empty := "---\nname: empty\n---\n"
	big := strings.Repeat("x", maxAgentFileBytes+1)
	mock := &agentMockGitHub{
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

	set, err := loadCustomAgentsGroup(t.Context(), c, "m", agentTestPR("main"), "")
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
	mock := &agentMockGitHub{listing: listing, files: files}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentsGroup(t.Context(), c, "m", agentTestPR("main"), "")
	if err != nil {
		t.Fatalf("loadCustomAgentSet() error = %v", err)
	}
	if len(set.Specialists) != maxAgentFiles {
		t.Errorf("specialists = %d, want the cap %d", len(set.Specialists), maxAgentFiles)
	}
}

// Custom agents are read from the branch the pull request proposes, so a
// feature branch can add and try its own reviewers without merging first.
func TestLoadAgentSetReadsPullRequestHeadBranch(t *testing.T) {
	mock := &agentMockGitHub{listing: []map[string]any{}}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentsGroup(t.Context(), c, "m", agentTestPR("feature/widgets"), "")
	if err != nil {
		t.Fatalf("loadCustomAgentGroup() error = %v", err)
	}
	if set.Ref != "feature/widgets" {
		t.Errorf("Ref = %q, want the pull request head branch", set.Ref)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.refs) == 0 {
		t.Fatal("no contents request was made")
	}
	for _, ref := range mock.refs {
		if ref != "feature/widgets" {
			t.Errorf("request used ref %q, want the head branch only; refs = %v", ref, mock.refs)
		}
	}
}

// A pull request from a fork is allowed to supply its own reviewers: the
// loader reads .anubis-agents/ from the fork repository that holds the head
// branch, not from the repository under review.
func TestLoadAgentSetReadsFromForkRepository(t *testing.T) {
	const fork = "attacker/widgets"
	body := "You are a reviewer.\n"
	mock := &agentMockGitHub{
		listing: []map[string]any{listingEntry("good.md", body)},
		files:   map[string]string{"good.md": body},
	}
	c := newAgentTestClient(t, mock)

	pr := agentTestPR("feature")
	pr.Repository = fork
	set, err := loadCustomAgentsGroup(t.Context(), c, "m", pr, "")
	if err != nil {
		t.Fatalf("loadCustomAgentsGroup() error = %v", err)
	}
	if len(set.Specialists) != 1 || set.Specialists[0].Name != "good" {
		t.Fatalf("specialists = %+v, want one loaded from the fork", set.Specialists)
	}
	// The contents request went to the fork, not the repository under review.
	var readFromFork bool
	for _, p := range mock.requestedPaths() {
		if strings.HasPrefix(p, "/repos/"+fork+"/contents/") {
			readFromFork = true
		}
		if strings.HasPrefix(p, "/repos/"+agentTestOwner+"/"+agentTestRepo+"/contents/") {
			t.Errorf("request read from the repository under review: %q", p)
		}
	}
	if readFromFork == false {
		t.Errorf("no contents request to the fork; paths = %v", mock.requestedPaths())
	}
}

// When GitHub does not report the head repository (for example a deleted
// fork), there is nowhere to read .anubis-agents/ from. That is an error, not
// a skip: a run that asked for custom agents must fail rather than quietly use
// the built-ins.
func TestLoadAgentSetUnknownHeadRepositoryErrors(t *testing.T) {
	mock := &agentMockGitHub{listing: []map[string]any{}}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentsGroup(t.Context(), c, "m",
		PullRequest{SourceBranch: "feature"}, "")
	if err == nil {
		t.Fatal("loadCustomAgentsGroup() error = nil, want an error for an unknown head repository")
	}
	if set != nil {
		t.Errorf("group = %+v, want nil on a refused load", set)
	}
	if len(mock.requestedPaths()) != 0 {
		t.Errorf("requested %v, want no contents request when the head repository is unknown", mock.requestedPaths())
	}
}

// When GitHub does not report the head branch there is no ref to read the
// agents from, so the load is refused rather than guessed.
func TestLoadAgentSetNoHeadBranchErrors(t *testing.T) {
	mock := &agentMockGitHub{listing: []map[string]any{}}
	c := newAgentTestClient(t, mock)

	set, err := loadCustomAgentsGroup(t.Context(), c, "m",
		PullRequest{Repository: agentTestOwner + "/" + agentTestRepo}, "")
	if err == nil {
		t.Fatal("loadCustomAgentsGroup() error = nil, want an error for a missing head branch")
	}
	if set != nil {
		t.Errorf("group = %+v, want nil on a refused load", set)
	}
}

// allowed_target_branch restricts custom agents to pull requests that target
// one branch. Unset means any target branch. A mismatch is an error, not a
// silent fallback: a run that asked for custom agents must fail rather than use
// the built-ins. The filter is on the target, not the head the agents are read
// from, so "matching target loads" below has a head of feat/new-api and still
// loads because it targets development.
func TestLoadAgentSetAllowedTargetBranchGates(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		allowed string
		wantErr bool
	}{
		{name: "any target when unset", target: "development"},
		{name: "matching target loads", target: "development", allowed: "development"},
		{name: "other target is refused", target: "development", allowed: "main", wantErr: true},
		{name: "empty target is refused", target: "", allowed: "main", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			good := "You are a reviewer.\n"
			mock := &agentMockGitHub{
				listing: []map[string]any{listingEntry("good.md", good)},
				files:   map[string]string{"good.md": good},
			}
			c := newAgentTestClient(t, mock)

			pr := agentTestPR("feat/new-api")
			pr.TargetBranch = tc.target
			set, err := loadCustomAgentsGroup(t.Context(), c, "m", pr, tc.allowed)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("loadCustomAgentsGroup() error = nil, want a target-branch refusal")
				}
				if set != nil {
					t.Errorf("group = %+v, want nil on a refused load", set)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadCustomAgentsGroup() error = %v", err)
			}
			if len(set.Specialists) != 1 {
				t.Errorf("specialists = %d, want the custom set loaded", len(set.Specialists))
			}
		})
	}
}

// --- selection ---

func TestSelectAgentsReplacesBuiltins(t *testing.T) {
	llm := &OpenAIClient{Model: "m"}
	custom := []*AgentCard{{Name: "docs", Description: "d", SystemPrompt: "p"}}

	agents, replaced := extractAgents(custom, llm)
	if replaced == false {
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

// When the repository provided no custom agents, extractAgents reports false
// and returns nothing, leaving the caller's built-in set in place. It never
// invents specialists of its own.
func TestExtractAgentsNoneWhenNoCustom(t *testing.T) {
	llm := &OpenAIClient{Model: "m"}
	agents, replaced := extractAgents(nil, llm)
	if replaced {
		t.Error("replaced = true, want false when there are no custom agents")
	}
	if agents != nil {
		t.Errorf("agents = %+v, want nil so the caller keeps the built-ins", agents)
	}
}
