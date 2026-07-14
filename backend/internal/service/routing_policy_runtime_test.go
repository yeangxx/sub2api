package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type testRoutingPriceResolver map[int64]RoutingPriceQuote

func (r testRoutingPriceResolver) Quote(_ context.Context, account *Account, _ string) (RoutingPriceQuote, bool, error) {
	quote, ok := r[account.ID]
	return quote, ok, nil
}

type recordingRoutingPriceResolver struct {
	model string
	quote RoutingPriceQuote
}

func (r *recordingRoutingPriceResolver) Quote(_ context.Context, _ *Account, model string) (RoutingPriceQuote, bool, error) {
	r.model = model
	return r.quote, true, nil
}

type effectiveRoutingPolicyRepoStub struct {
	RoutingPolicyRepository
	effective *EffectiveRoutingPolicy
}

type routingFallbackAccountRepoStub struct {
	AccountRepository
	groups map[int64][]Account
}

func (r *routingFallbackAccountRepoStub) ListByGroup(_ context.Context, groupID int64) ([]Account, error) {
	return r.groups[groupID], nil
}

func (r *effectiveRoutingPolicyRepoStub) GetEffectiveForGroup(context.Context, int64) (*EffectiveRoutingPolicy, error) {
	return r.effective, nil
}

func newTestEffectivePolicy(config RoutingPolicyConfig) *EffectiveRoutingPolicy {
	return &EffectiveRoutingPolicy{
		Policy:   RoutingPolicy{ID: 1},
		Revision: RoutingPolicyRevision{ID: 1, PolicyID: 1, SchemaVersion: 1, Config: config},
		Binding:  RoutingPolicyBinding{GroupID: 10, PolicyID: 1, RevisionID: ptrInt64(1), Mode: RoutingPolicyModeEnforce},
	}
}

func ptrInt64(v int64) *int64 { return &v }

func testPolicyConfig() RoutingPolicyConfig {
	return RoutingPolicyConfig{
		SchemaVersion:  1,
		Scoring:        RoutingScoringWeights{Price: 1},
		Retry:          RoutingRetryPolicy{MaxAttempts: 2, MaxSwitches: 1},
		Hedge:          RoutingHedgePolicy{MaxConcurrent: 2},
		CircuitBreaker: RoutingCircuitBreakerPolicy{MinSamples: 2, ConsecutiveFailures: 3},
	}
}

func testAccount(id int64, class string) Account {
	return Account{ID: id, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 2, ReliabilityClass: class}
}

func TestRoutingPolicyRuntimeSelectsLowestKnownCost(t *testing.T) {
	runtime := NewRoutingPolicyRuntime(nil, testRoutingPriceResolver{
		1: {InputPerMillion: 2},
		2: {InputPerMillion: 1},
	}, NewMemoryRoutingHealthStore())
	selection, err := runtime.Select(context.Background(), newTestEffectivePolicy(testPolicyConfig()), []Account{testAccount(1, "standard"), testAccount(2, "standard")}, RoutingRequestDescriptor{Model: "gpt-test", EstimatedInputToken: 1_000_000}, nil)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil || selection.Account.ID != 2 {
		t.Fatalf("selected account = %#v, want account 2", selection.Account)
	}
}

func TestRoutingPolicyRuntimeDoesNotExcludeWhenLabelsUnset(t *testing.T) {
	config := testPolicyConfig()
	selection, err := NewRoutingPolicyRuntime(nil, nil, NewMemoryRoutingHealthStore()).Select(context.Background(), newTestEffectivePolicy(config), []Account{testAccount(1, "standard")}, RoutingRequestDescriptor{Model: "gpt-test"}, nil)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil {
		t.Fatalf("account was excluded without label constraints: %#v", selection.Candidates)
	}
}

func TestRoutingPolicyRuntimeRequiresEveryConfiguredLabel(t *testing.T) {
	config := testPolicyConfig()
	config.CandidateFilters.RequiredLabels = map[string]string{"region": "us", "tier": "primary"}
	matching := testAccount(1, "standard")
	matching.RoutingLabels = map[string]string{"region": "US", "tier": "primary", "owner": "ops"}
	mismatching := testAccount(2, "standard")
	mismatching.RoutingLabels = map[string]string{"region": "eu", "tier": "primary"}

	selection, err := NewRoutingPolicyRuntime(nil, nil, NewMemoryRoutingHealthStore()).Select(
		context.Background(),
		newTestEffectivePolicy(config),
		[]Account{matching, mismatching},
		RoutingRequestDescriptor{Model: "gpt-test"},
		nil,
	)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil || selection.Account.ID != matching.ID {
		t.Fatalf("selected account = %#v, want matching account %d; candidates = %#v", selection.Account, matching.ID, selection.Candidates)
	}
	if len(selection.Candidates) != 2 || !selection.Candidates[1].Excluded {
		t.Fatalf("mismatching account was not excluded: %#v", selection.Candidates)
	}
}

func TestRoutingPolicyRuntimeExcludesAnyConfiguredLabelMatch(t *testing.T) {
	config := testPolicyConfig()
	config.CandidateFilters.ExcludedLabels = map[string]string{"blocked": "true", "region": "eu"}
	allowed := testAccount(1, "standard")
	allowed.RoutingLabels = map[string]string{"blocked": "false", "region": "us"}
	blocked := testAccount(2, "standard")
	blocked.RoutingLabels = map[string]string{"blocked": "TRUE", "region": "us"}

	selection, err := NewRoutingPolicyRuntime(nil, nil, NewMemoryRoutingHealthStore()).Select(
		context.Background(),
		newTestEffectivePolicy(config),
		[]Account{allowed, blocked},
		RoutingRequestDescriptor{Model: "gpt-test"},
		nil,
	)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil || selection.Account.ID != allowed.ID {
		t.Fatalf("selected account = %#v, want allowed account %d; candidates = %#v", selection.Account, allowed.ID, selection.Candidates)
	}
}

func TestRoutingPolicyRuntimeFiltersUntrustedAndOpenCircuit(t *testing.T) {
	config := testPolicyConfig()
	config.CandidateFilters.RequireTrustedUpstream = true
	health := NewMemoryRoutingHealthStore()
	for i := 0; i < 3; i++ {
		health.Record(context.Background(), 2, "gpt-test", "openai", false, 0)
	}
	entry := health.entries[routingHealthKey{accountID: 2, model: "gpt-test", endpoint: "openai"}]
	entry.CircuitState = "open"
	entry.OpenUntil = time.Now().Add(time.Minute)
	health.entries[routingHealthKey{accountID: 2, model: "gpt-test", endpoint: "openai"}] = entry
	selection, err := NewRoutingPolicyRuntime(nil, nil, health).Select(context.Background(), newTestEffectivePolicy(config), []Account{testAccount(1, "trusted"), testAccount(2, "trusted")}, RoutingRequestDescriptor{Model: "gpt-test", Endpoint: "openai"}, nil)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil || selection.Account.ID != 1 {
		t.Fatalf("selected account = %#v, want healthy account 1", selection.Account)
	}
	for _, decision := range selection.Candidates {
		if decision.AccountID == 2 && decision.ExclusionReason != "circuit_open" {
			t.Fatalf("account 2 exclusion = %q, want circuit_open", decision.ExclusionReason)
		}
	}
}

func TestDefaultRoutingPolicyConfigPresets(t *testing.T) {
	for _, name := range []string{RoutingPresetEconomy, RoutingPresetStandard, RoutingPresetProfessional} {
		config, err := DefaultRoutingPolicyConfig(name)
		if err != nil {
			t.Fatalf("preset %q: %v", name, err)
		}
		if err := config.Validate(); err != nil {
			t.Fatalf("preset %q validation: %v", name, err)
		}
		if !config.Hedge.Enabled || !config.Timeouts.AdaptiveSoftTTFT {
			t.Fatalf("preset %q missing adaptive hedge baseline: %#v", name, config)
		}
	}
}

func TestRoutingPolicyRuntimeSoftTTFTDelayUsesHealthAndBounds(t *testing.T) {
	health := NewMemoryRoutingHealthStore()
	health.Record(context.Background(), 1, "gpt-test", "openai", true, 2*time.Second)
	config := testPolicyConfig()
	config.Timeouts = RoutingTimeoutPolicy{AdaptiveSoftTTFT: true, SoftTTFTFactor: 2, SoftTTFTMinMillis: 1000, SoftTTFTMaxMillis: 3000}
	effective := newTestEffectivePolicy(config)
	delay := NewRoutingPolicyRuntime(nil, nil, health).SoftTTFTDelay(context.Background(), effective, 1, "gpt-test", "openai")
	if delay != 3*time.Second {
		t.Fatalf("delay = %s, want 3s max bound", delay)
	}
}

func TestOpenAIRoutingHedgeDelayUsesHedgeDelaySetting(t *testing.T) {
	config := testPolicyConfig()
	config.Hedge = RoutingHedgePolicy{Enabled: true, DelayMillis: 1234, MaxConcurrent: 2}
	config.Timeouts.SoftTTFTMillis = 9000
	effective := newTestEffectivePolicy(config)
	control := NewRoutingPolicyControlService(&effectiveRoutingPolicyRepoStub{effective: effective}, nil)
	svc := &OpenAIGatewayService{routingPolicyRuntime: NewRoutingPolicyRuntime(control, nil, NewMemoryRoutingHealthStore())}
	groupID := int64(10)

	delay, ok := svc.RoutingHedgeDelay(context.Background(), &groupID, 1, "gpt-test")
	if !ok {
		t.Fatal("RoutingHedgeDelay() was disabled")
	}
	if delay != 1234*time.Millisecond {
		t.Fatalf("RoutingHedgeDelay() = %v, want 1234ms", delay)
	}
}

func TestOpenAIRoutingHedgeDelayUsesEarlierSoftTTFTThreshold(t *testing.T) {
	config := testPolicyConfig()
	config.Hedge = RoutingHedgePolicy{Enabled: true, DelayMillis: 1500, MaxConcurrent: 2}
	config.Timeouts.SoftTTFTMillis = 700
	effective := newTestEffectivePolicy(config)
	control := NewRoutingPolicyControlService(&effectiveRoutingPolicyRepoStub{effective: effective}, nil)
	svc := &OpenAIGatewayService{routingPolicyRuntime: NewRoutingPolicyRuntime(control, nil, NewMemoryRoutingHealthStore())}
	groupID := int64(10)

	delay, ok := svc.RoutingHedgeDelay(context.Background(), &groupID, 1, "gpt-test")
	if !ok || delay != 700*time.Millisecond {
		t.Fatalf("RoutingHedgeDelay() = %v, %v; want 700ms, true", delay, ok)
	}
}

func TestRoutingPolicyRuntimeUsesMappedModelForPriceQuote(t *testing.T) {
	config := testPolicyConfig()
	config.ModelMappings = map[string]string{"client-model": "upstream-model"}
	resolver := &recordingRoutingPriceResolver{quote: RoutingPriceQuote{RequestPrice: 1}}
	selection, err := NewRoutingPolicyRuntime(nil, resolver, NewMemoryRoutingHealthStore()).Select(
		context.Background(),
		newTestEffectivePolicy(config),
		[]Account{testAccount(1, "standard")},
		RoutingRequestDescriptor{Model: "client-model"},
		nil,
	)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if resolver.model != "upstream-model" {
		t.Fatalf("quoted model = %q, want upstream-model", resolver.model)
	}
	if selection.MappedModel != "upstream-model" {
		t.Fatalf("selection mapped model = %q, want upstream-model", selection.MappedModel)
	}
}

func TestRoutingPolicyRuntimeReservesHedgeCostInsideTotalBudget(t *testing.T) {
	config := testPolicyConfig()
	config.Hedge = RoutingHedgePolicy{Enabled: true, DelayMillis: 100, MaxConcurrent: 2}
	config.CostBudget.MaxUpstreamUSD = decimal.RequireFromString("0.03")
	config.CostBudget.ReserveForHedgeUSD = decimal.RequireFromString("0.02")
	runtime := NewRoutingPolicyRuntime(nil, testRoutingPriceResolver{1: {RequestPrice: 0.02}}, NewMemoryRoutingHealthStore())
	selection, err := runtime.Select(context.Background(), newTestEffectivePolicy(config), []Account{testAccount(1, "standard")}, RoutingRequestDescriptor{Model: "gpt-test"}, nil)
	if err != ErrNoAvailableAccounts {
		t.Fatalf("Select() error = %v, want ErrNoAvailableAccounts", err)
	}
	if len(selection.Candidates) != 1 || selection.Candidates[0].ExclusionReason != "upstream_cost_budget" {
		t.Fatalf("candidates = %#v, want total budget exclusion", selection.Candidates)
	}
}

func TestRoutingPolicyRuntimeLimitsHalfOpenProbeRequests(t *testing.T) {
	config := testPolicyConfig()
	config.CircuitBreaker.HalfOpenMaxRequests = 1
	health := NewMemoryRoutingHealthStore()
	health.entries[routingHealthKey{accountID: 1, model: "gpt-test", endpoint: "openai"}] = routingHealthEntry{RoutingHealthSnapshot: RoutingHealthSnapshot{
		CircuitState: "open",
		OpenUntil:    time.Now().Add(-time.Second),
	}}
	runtime := NewRoutingPolicyRuntime(nil, nil, health)
	request := RoutingRequestDescriptor{Model: "gpt-test", Endpoint: "openai"}

	effective := newTestEffectivePolicy(config)
	if _, err := runtime.Select(context.Background(), effective, []Account{testAccount(1, "standard")}, request, nil); err != nil {
		t.Fatalf("first half-open Select() error = %v", err)
	}
	release, allowed := runtime.AcquireHalfOpenProbe(context.Background(), effective, 1, request)
	if !allowed {
		t.Fatal("first half-open probe was rejected")
	}
	if _, allowed := runtime.AcquireHalfOpenProbe(context.Background(), effective, 1, request); allowed {
		t.Fatal("second concurrent half-open probe was allowed")
	}
	selection, err := runtime.Select(context.Background(), effective, []Account{testAccount(1, "standard")}, request, nil)
	if err != ErrNoAvailableAccounts {
		t.Fatalf("Select() with reserved probe error = %v, want ErrNoAvailableAccounts", err)
	}
	if len(selection.Candidates) != 1 || selection.Candidates[0].ExclusionReason != "circuit_half_open_limit" {
		t.Fatalf("candidates = %#v, want half-open limit exclusion", selection.Candidates)
	}
	release()
	if _, err := runtime.Select(context.Background(), effective, []Account{testAccount(1, "standard")}, request, nil); err != nil {
		t.Fatalf("Select() after probe release error = %v", err)
	}
}

func TestRoutingPolicyRuntimeDoesNotReserveUnselectedHalfOpenCandidates(t *testing.T) {
	config := testPolicyConfig()
	config.CircuitBreaker.HalfOpenMaxRequests = 1
	health := NewMemoryRoutingHealthStore()
	for _, accountID := range []int64{1, 2} {
		health.entries[routingHealthKey{accountID: accountID, model: "gpt-test", endpoint: "openai"}] = routingHealthEntry{RoutingHealthSnapshot: RoutingHealthSnapshot{
			CircuitState: "open",
			OpenUntil:    time.Now().Add(-time.Second),
		}}
	}
	runtime := NewRoutingPolicyRuntime(nil, testRoutingPriceResolver{
		1: {RequestPrice: 0.01},
		2: {RequestPrice: 0.02},
	}, health)
	effective := newTestEffectivePolicy(config)
	request := RoutingRequestDescriptor{Model: "gpt-test", Endpoint: "openai"}

	selection, err := runtime.Select(context.Background(), effective, []Account{testAccount(1, "standard"), testAccount(2, "standard")}, request, nil)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Account == nil || selection.Account.ID != 1 {
		t.Fatalf("selected account = %#v, want account 1", selection.Account)
	}
	if health.entries[routingHealthKey{accountID: 1, model: "gpt-test", endpoint: "openai"}].halfOpenRequests != 0 ||
		health.entries[routingHealthKey{accountID: 2, model: "gpt-test", endpoint: "openai"}].halfOpenRequests != 0 {
		t.Fatal("candidate scoring reserved a half-open probe")
	}
	if _, allowed := runtime.AcquireHalfOpenProbe(context.Background(), effective, selection.Account.ID, request); !allowed {
		t.Fatal("selected account half-open probe was rejected")
	}
	if health.entries[routingHealthKey{accountID: 2, model: "gpt-test", endpoint: "openai"}].halfOpenRequests != 0 {
		t.Fatal("unselected account consumed a half-open probe")
	}
}

func TestRoutingPolicyRuntimeBacksOffCircuitCooldownUpToConfiguredMaximum(t *testing.T) {
	config := testPolicyConfig()
	config.CircuitBreaker.ConsecutiveFailures = 1
	config.CircuitBreaker.CooldownMillis = 100
	config.CircuitBreaker.MaxCooldownMillis = 250
	health := NewMemoryRoutingHealthStore()
	runtime := NewRoutingPolicyRuntime(nil, nil, health)
	effective := newTestEffectivePolicy(config)
	request := RoutingRequestDescriptor{Model: "gpt-test", Endpoint: "openai"}

	runtime.RecordResult(context.Background(), effective, 1, request, false, 0)
	runtime.RecordResult(context.Background(), effective, 1, request, false, 0)
	secondRemaining := time.Until(health.Snapshot(context.Background(), 1, "gpt-test", "openai").OpenUntil)
	if secondRemaining < 170*time.Millisecond || secondRemaining > 260*time.Millisecond {
		t.Fatalf("second cooldown remaining = %v, want approximately 200ms", secondRemaining)
	}
	runtime.RecordResult(context.Background(), effective, 1, request, false, 0)
	thirdRemaining := time.Until(health.Snapshot(context.Background(), 1, "gpt-test", "openai").OpenUntil)
	if thirdRemaining < 220*time.Millisecond || thirdRemaining > 270*time.Millisecond {
		t.Fatalf("third cooldown remaining = %v, want capped at 250ms", thirdRemaining)
	}
}

func TestRoutingRequestDescriptorUsesJSONTokenEstimatesFromContext(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"abcdefgh","max_output_tokens":2048}`)
	ctx := WithRoutingTokenEstimatesFromJSON(context.Background(), body)
	descriptor := routingRequestDescriptor(ctx, 10, "openai", "gpt-test", "openai", true)

	if descriptor.EstimatedInputToken <= 0 {
		t.Fatalf("estimated input tokens = %d, want positive", descriptor.EstimatedInputToken)
	}
	if descriptor.EstimatedOutputToken != 2048 {
		t.Fatalf("estimated output tokens = %d, want 2048", descriptor.EstimatedOutputToken)
	}
}

func TestRoutingTokenEstimateSupportsAnthropicMaxTokens(t *testing.T) {
	ctx := WithRoutingTokenEstimatesFromJSON(context.Background(), []byte(`{"max_tokens":512,"messages":[{"role":"user","content":"hello"}]}`))
	descriptor := routingRequestDescriptor(ctx, 1, "anthropic", "claude-test", "gateway", true)
	if descriptor.EstimatedOutputToken != 512 {
		t.Fatalf("estimated output tokens = %d, want 512", descriptor.EstimatedOutputToken)
	}
}

func TestRoutingFallbackAccountsLoadsConfiguredGroupsWithExplicitModelMap(t *testing.T) {
	config := testPolicyConfig()
	config.Fallback = RoutingFallbackPolicy{GroupIDs: []int64{20, 30}, AllowCrossTier: true, RequireExplicitModelMap: true}
	config.ModelMappings = map[string]string{"client-model": "fallback-model"}
	effective := newTestEffectivePolicy(config)
	repo := &routingFallbackAccountRepoStub{groups: map[int64][]Account{
		20: {testAccount(2, "standard")},
		30: {testAccount(2, "standard"), testAccount(3, "standard")},
	}}

	accounts, mappedModel, err := routingFallbackAccounts(context.Background(), repo, effective, 10, "client-model", map[int64]struct{}{1: {}})
	if err != nil {
		t.Fatalf("routingFallbackAccounts() error = %v", err)
	}
	if mappedModel != "fallback-model" {
		t.Fatalf("mapped model = %q, want fallback-model", mappedModel)
	}
	if len(accounts) != 2 || accounts[0].ID != 2 || accounts[1].ID != 3 {
		t.Fatalf("fallback accounts = %#v, want accounts 2 and 3", accounts)
	}
}

func TestRoutingFallbackAccountsRequiresExplicitMapWhenConfigured(t *testing.T) {
	config := testPolicyConfig()
	config.Fallback = RoutingFallbackPolicy{GroupIDs: []int64{20}, AllowCrossTier: true, RequireExplicitModelMap: true}
	effective := newTestEffectivePolicy(config)
	repo := &routingFallbackAccountRepoStub{groups: map[int64][]Account{20: {testAccount(2, "standard")}}}

	accounts, _, err := routingFallbackAccounts(context.Background(), repo, effective, 10, "client-model", nil)
	if err != nil {
		t.Fatalf("routingFallbackAccounts() error = %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("fallback accounts = %#v, want none without explicit model map", accounts)
	}
}

func TestRoutingRetryDecisionHonorsStatusAndTransportConfiguration(t *testing.T) {
	policy := RoutingRetryPolicy{RetryableStatusCodes: []int{429, 503}, RetryTransportErrors: true}
	if !routingRetryableError(policy, &UpstreamFailoverError{StatusCode: 503}) {
		t.Fatal("503 should be retryable")
	}
	if routingRetryableError(policy, &UpstreamFailoverError{StatusCode: 500}) {
		t.Fatal("500 should not be retryable when absent from configured statuses")
	}
	if !routingRetryableError(policy, errors.New("connection reset")) {
		t.Fatal("transport error should be retryable")
	}
	if routingRetryableError(RoutingRetryPolicy{}, errors.New("connection reset")) {
		t.Fatal("transport error should not be retryable when disabled")
	}
}

func TestRoutingRetrySwitchLimitIsBoundedByMaxAttempts(t *testing.T) {
	if got := routingRetrySwitchLimit(RoutingRetryPolicy{MaxAttempts: 2, MaxSwitches: 5}, 9); got != 1 {
		t.Fatalf("routingRetrySwitchLimit() = %d, want 1", got)
	}
	if got := routingRetrySwitchLimit(RoutingRetryPolicy{MaxAttempts: 4, MaxSwitches: 2}, 9); got != 2 {
		t.Fatalf("routingRetrySwitchLimit() = %d, want 2", got)
	}
	if got := routingRetrySwitchLimit(RoutingRetryPolicy{}, 9); got != 0 {
		t.Fatalf("routingRetrySwitchLimit() = %d, want explicit zero", got)
	}
}

func TestMemoryRoutingHealthStoreUsesMonitorBaseline(t *testing.T) {
	health := NewMemoryRoutingHealthStore()
	health.Record(context.Background(), 7, "gpt-test", "monitor", false, 1500*time.Millisecond)
	snapshot := health.Snapshot(context.Background(), 7, "gpt-test", "openai")
	if snapshot.Samples != 1 || snapshot.ErrorRate != 1 || snapshot.TTFTMillis != 1500 {
		t.Fatalf("monitor baseline snapshot = %#v", snapshot)
	}
}

func TestOpenAIRoutingHealthReportUsesRequestModelWithoutSharedAccountState(t *testing.T) {
	health := NewMemoryRoutingHealthStore()
	svc := &OpenAIGatewayService{routingPolicyRuntime: NewRoutingPolicyRuntime(nil, nil, health)}

	svc.ReportOpenAIAccountScheduleResult(10, "model-a", true, nil)
	svc.ReportOpenAIAccountScheduleResult(10, "model-b", false, nil)

	modelA := health.Snapshot(context.Background(), 10, "model-a", "openai")
	modelB := health.Snapshot(context.Background(), 10, "model-b", "openai")
	if modelA.Samples != 1 || modelA.ErrorRate != 0 {
		t.Fatalf("model-a health = %#v, want one successful sample", modelA)
	}
	if modelB.Samples != 1 || modelB.ErrorRate == 0 {
		t.Fatalf("model-b health = %#v, want one failed sample", modelB)
	}
}
