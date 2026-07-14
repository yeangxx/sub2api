package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAwaitOpenAIHedgeReturnsSecondaryWithoutWaitingForCanceledPrimary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := make(chan openAIHedgeAttemptResult, 2)
	secondary := &service.Account{ID: 2}
	startedSecondary := make(chan struct{}, 1)

	startSecondary := func() {
		startedSecondary <- struct{}{}
		attempts <- openAIHedgeAttemptResult{account: secondary, result: &service.OpenAIForwardResult{RequestID: "secondary"}}
	}
	start := time.Now()
	winner := awaitOpenAIHedge(ctx, attempts, 5*time.Millisecond, 1, startSecondary, func() {})
	elapsed := time.Since(start)

	if winner.err != nil {
		t.Fatalf("awaitOpenAIHedge() error = %v", winner.err)
	}
	if winner.account == nil || winner.account.ID != secondary.ID {
		t.Fatalf("winner account = %#v, want secondary account %d", winner.account, secondary.ID)
	}
	if winner.result == nil || winner.result.RequestID != "secondary" {
		t.Fatalf("winner result = %#v", winner.result)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("secondary winner waited for nonresponsive primary: %v", elapsed)
	}
	select {
	case <-startedSecondary:
	default:
		t.Fatal("secondary attempt was not started")
	}
}

func TestAwaitOpenAIHedgeReleasesUnusedSecondaryWhenPrimaryWinsBeforeDelay(t *testing.T) {
	attempts := make(chan openAIHedgeAttemptResult, 1)
	primary := &service.Account{ID: 1}
	attempts <- openAIHedgeAttemptResult{account: primary, result: &service.OpenAIForwardResult{RequestID: "primary"}}
	released := false
	started := false

	winner := awaitOpenAIHedge(context.Background(), attempts, time.Second, 1, func() { started = true }, func() { released = true })

	if winner.account == nil || winner.account.ID != primary.ID {
		t.Fatalf("winner account = %#v, want primary account %d", winner.account, primary.ID)
	}
	if started {
		t.Fatal("secondary attempt started after primary already won")
	}
	if !released {
		t.Fatal("unused secondary reservation was not released")
	}
}

func TestAwaitOpenAIHedgeSupportsConfiguredSecondaryConcurrency(t *testing.T) {
	attempts := make(chan openAIHedgeAttemptResult, 3)
	third := &service.Account{ID: 3}
	started := 0
	winner := awaitOpenAIHedge(context.Background(), attempts, time.Millisecond, 2, func() {
		started = 2
		attempts <- openAIHedgeAttemptResult{account: &service.Account{ID: 2}, err: context.DeadlineExceeded}
		attempts <- openAIHedgeAttemptResult{account: third, result: &service.OpenAIForwardResult{RequestID: "third"}}
	}, func() {})
	if started != 2 {
		t.Fatalf("started secondaries = %d, want 2", started)
	}
	if winner.account == nil || winner.account.ID != third.ID {
		t.Fatalf("winner account = %#v, want account 3", winner.account)
	}
}
