package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
)

type reviewAgent struct {
	name  string
	delay time.Duration
	item  domain.Finding
}

func (a reviewAgent) Name() string { return a.name }
func (a reviewAgent) Review(ctx context.Context, _ domain.ReviewInput) ([]domain.Finding, error) {
	select {
	case <-time.After(a.delay):
		return []domain.Finding{a.item}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type synthesisLLM struct {
	mu    sync.Mutex
	calls int
}

func (s *synthesisLLM) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return llm.CompletionResponse{Content: "summary"}, nil
}

func TestReviewDeduplicatesAndSynthesizes(t *testing.T) {
	finding := domain.Finding{Title: "same", Description: "one", Severity: domain.SeverityHigh, File: "a.go", Line: 2}
	client := &synthesisLLM{}
	master := New([]domain.Agent{
		reviewAgent{name: "slow", delay: 20 * time.Millisecond, item: finding},
		reviewAgent{name: "fast", item: finding},
	}, client)
	review, err := master.Review(context.Background(), domain.ReviewInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Findings) != 1 || review.Summary != "summary" || review.Approved {
		t.Fatalf("unexpected review: %+v", review)
	}
	if client.calls != 1 {
		t.Fatalf("synthesis calls = %d, want 1", client.calls)
	}
}
