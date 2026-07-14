package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIHedgeAttemptResult struct {
	account *service.Account
	writer  *hedgeResponseWriter
	result  *service.OpenAIForwardResult
	err     error
}

func awaitOpenAIHedge(
	ctx context.Context,
	attempts <-chan openAIHedgeAttemptResult,
	delay time.Duration,
	secondaryCount int,
	startSecondary func(),
	releaseSecondary func(),
) openAIHedgeAttemptResult {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	secondaryStarted := false
	started, completed := 1, 0
	var firstFailure openAIHedgeAttemptResult
	hasFailure := false

	for {
		select {
		case attempt := <-attempts:
			completed++
			if attempt.err == nil {
				if !secondaryStarted {
					releaseSecondary()
				}
				return attempt
			}
			if !hasFailure {
				firstFailure = attempt
				hasFailure = true
			}
			if !secondaryStarted {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				secondaryStarted = true
				started += secondaryCount
				startSecondary()
			}
			if completed >= started {
				return firstFailure
			}
		case <-timer.C:
			if !secondaryStarted {
				secondaryStarted = true
				started += secondaryCount
				startSecondary()
			}
		case <-ctx.Done():
			if !secondaryStarted {
				releaseSecondary()
			}
			return openAIHedgeAttemptResult{err: ctx.Err()}
		}
	}
}

// forwardWithPolicyHedge implements the safe first slice of Hedge: complete
// non-streaming HTTP responses are buffered independently and only the first
// valid response is committed to the real client writer.
func (h *OpenAIGatewayHandler) forwardWithPolicyHedge(
	c *gin.Context,
	apiKeyGroupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	requestPlatform string,
	requireCompact bool,
	body []byte,
	primary *service.Account,
	primaryRelease func(),
	failedAccountIDs map[int64]struct{},
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (*service.OpenAIForwardResult, *service.Account, error, bool) {
	if h == nil || h.gatewayService == nil || c == nil || primary == nil || primaryRelease == nil || reqStream || streamStarted == nil || *streamStarted {
		return nil, nil, nil, false
	}
	if previousResponseID != "" {
		return nil, nil, nil, false
	}
	hedgePolicy, enabled := h.gatewayService.RoutingHedgePolicy(c.Request.Context(), apiKeyGroupID)
	if !enabled || !hedgePolicy.Enabled || hedgePolicy.MaxConcurrent < 2 {
		return nil, nil, nil, false
	}
	delay, delayOK := h.gatewayService.RoutingHedgeDelay(c.Request.Context(), apiKeyGroupID, primary.ID, requestedModel)
	if !delayOK {
		return nil, nil, nil, false
	}

	excluded := make(map[int64]struct{}, len(failedAccountIDs)+1)
	for id := range failedAccountIDs {
		excluded[id] = struct{}{}
	}
	excluded[primary.ID] = struct{}{}
	secondarySelections := make([]*service.AccountSelectionResult, 0, hedgePolicy.MaxConcurrent-1)
	failureDomains := make(map[string]struct{})
	if domain := primary.FailureDomain; domain != "" {
		failureDomains[domain] = struct{}{}
	}
	for len(secondarySelections) < hedgePolicy.MaxConcurrent-1 {
		selection, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(), apiKeyGroupID, previousResponseID, sessionHash, requestedModel, excluded,
			service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, requireCompact, false, requestPlatform,
		)
		if selectErr != nil || selection == nil || selection.Account == nil || !selection.Acquired || selection.ReleaseFunc == nil {
			break
		}
		excluded[selection.Account.ID] = struct{}{}
		if hedgePolicy.RequireDifferentDomain && selection.Account.FailureDomain != "" {
			if _, duplicate := failureDomains[selection.Account.FailureDomain]; duplicate {
				selection.ReleaseFunc()
				continue
			}
			failureDomains[selection.Account.FailureDomain] = struct{}{}
		}
		secondarySelections = append(secondarySelections, selection)
	}
	if len(secondarySelections) == 0 {
		return nil, nil, nil, false
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	attempts := make(chan openAIHedgeAttemptResult, 1+len(secondarySelections))
	start := func(account *service.Account, release func()) {
		go func() {
			attemptCtx := c.Copy()
			attemptCtx.Request = c.Request.WithContext(ctx)
			writer := newHedgeResponseWriter(ctx)
			attemptCtx.Writer = writer
			result, err := h.gatewayService.Forward(ctx, attemptCtx, account, body)
			release()
			attempts <- openAIHedgeAttemptResult{account: account, writer: writer, result: result, err: err}
		}()
	}

	start(primary, primaryRelease)
	winner := awaitOpenAIHedge(
		ctx,
		attempts,
		delay,
		len(secondarySelections),
		func() {
			for _, selection := range secondarySelections {
				start(selection.Account, selection.ReleaseFunc)
			}
		},
		func() {
			for _, selection := range secondarySelections {
				selection.ReleaseFunc()
			}
		},
	)
	if winner.err != nil {
		if reqLog != nil && winner.account != nil {
			reqLog.Debug("openai.hedge_attempts_failed", zap.Int64("account_id", winner.account.ID), zap.Error(winner.err))
		}
		return winner.result, winner.account, winner.err, true
	}
	cancel()
	if err := winner.writer.commit(c.Writer); err != nil {
		return winner.result, winner.account, err, true
	}
	return winner.result, winner.account, nil, true
}
