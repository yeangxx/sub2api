package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// RoutingRequestDescriptor is the protocol-neutral subset needed by policy
// evaluation. Protocol handlers keep the original request and use this only
// for candidate selection and cost/health accounting.
type RoutingRequestDescriptor struct {
	GroupID              int64
	Platform             string
	Model                string
	Endpoint             string
	Stream               bool
	EstimatedInputToken  int
	EstimatedOutputToken int
}

type routingTokenEstimate struct {
	input  int
	output int
}

type routingTokenEstimateContextKey struct{}

// WithRoutingTokenEstimatesFromJSON attaches a conservative pre-forward token
// estimate to the request context so price-based routing can score token-priced
// upstreams before actual usage is known.
func WithRoutingTokenEstimatesFromJSON(ctx context.Context, body []byte) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	input := (len(body) + 3) / 4
	if input <= 0 {
		input = 1
	}
	output := 1024
	for _, path := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens"} {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Int() > 0 {
			output = int(value.Int())
			break
		}
	}
	return context.WithValue(ctx, routingTokenEstimateContextKey{}, routingTokenEstimate{input: input, output: output})
}

func routingRequestDescriptor(ctx context.Context, groupID int64, platform, model, endpoint string, stream bool) RoutingRequestDescriptor {
	descriptor := RoutingRequestDescriptor{GroupID: groupID, Platform: platform, Model: model, Endpoint: endpoint, Stream: stream}
	if estimate, ok := ctx.Value(routingTokenEstimateContextKey{}).(routingTokenEstimate); ok {
		descriptor.EstimatedInputToken = estimate.input
		descriptor.EstimatedOutputToken = estimate.output
	}
	return descriptor
}

type RoutingPriceQuote struct {
	InputPerMillion  float64
	OutputPerMillion float64
	RequestPrice     float64
}

type RoutingPriceResolver interface {
	Quote(ctx context.Context, account *Account, model string) (RoutingPriceQuote, bool, error)
}

type RoutingHealthSnapshot struct {
	ErrorRate        float64
	TTFTMillis       float64
	Load             float64
	Queue            float64
	ConsecutiveFails int
	Samples          int
	CircuitState     string
	OpenUntil        time.Time
}

type RoutingHealthStore interface {
	Snapshot(ctx context.Context, accountID int64, model, endpoint string) RoutingHealthSnapshot
	Record(ctx context.Context, accountID int64, model, endpoint string, success bool, ttft time.Duration)
}

type routingCircuitController interface {
	OpenCircuit(ctx context.Context, accountID int64, model, endpoint string, cooldown time.Duration)
	CloseCircuit(ctx context.Context, accountID int64, model, endpoint string)
}

type routingCircuitBackoffController interface {
	OpenCircuitWithMax(ctx context.Context, accountID int64, model, endpoint string, cooldown, maxCooldown time.Duration)
}

type routingHalfOpenController interface {
	AllowHalfOpen(ctx context.Context, accountID int64, model, endpoint string, maxRequests int) bool
}

type routingHalfOpenCapacityController interface {
	CanHalfOpen(ctx context.Context, accountID int64, model, endpoint string, maxRequests int) bool
}

type routingHalfOpenReleaseController interface {
	ReleaseHalfOpen(ctx context.Context, accountID int64, model, endpoint string)
}

type routingHealthKey struct {
	accountID int64
	model     string
	endpoint  string
}

type routingHealthEntry struct {
	RoutingHealthSnapshot
	lastSample       time.Time
	halfOpenRequests int
	reopenCount      int
}

// MemoryRoutingHealthStore is the local fallback store. It deliberately keeps
// only short-lived counters; durable account status continues to be managed by
// AccountRepository and the existing rate-limit service.
type MemoryRoutingHealthStore struct {
	mu      sync.RWMutex
	entries map[routingHealthKey]routingHealthEntry
}

func NewMemoryRoutingHealthStore() *MemoryRoutingHealthStore {
	return &MemoryRoutingHealthStore{entries: make(map[routingHealthKey]routingHealthEntry)}
}

func (s *MemoryRoutingHealthStore) Snapshot(_ context.Context, accountID int64, model, endpoint string) RoutingHealthSnapshot {
	if s == nil {
		return RoutingHealthSnapshot{}
	}
	s.mu.RLock()
	entry, ok := s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}]
	// Monitor probes are protocol-neutral. Use them as a warm baseline until
	// the live gateway has collected endpoint-specific samples.
	if (!ok || entry.Samples == 0) && endpoint != "monitor" {
		if monitorEntry, monitorOK := s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: "monitor"}]; monitorOK {
			entry = monitorEntry
		}
	}
	s.mu.RUnlock()
	return entry.RoutingHealthSnapshot
}

func (s *MemoryRoutingHealthStore) Record(_ context.Context, accountID int64, model, endpoint string, success bool, ttft time.Duration) {
	if s == nil || accountID <= 0 {
		return
	}
	now := time.Now()
	key := routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}
	s.mu.Lock()
	entry := s.entries[key]
	entry.Samples++
	if success {
		entry.ConsecutiveFails = 0
	} else {
		entry.ConsecutiveFails++
	}
	// EWMA keeps the score responsive without allowing one noisy request to
	// dominate an otherwise healthy upstream.
	failure := 0.0
	if !success {
		failure = 1
	}
	entry.ErrorRate = ewma(entry.ErrorRate, failure, 0.2)
	if ttft > 0 {
		entry.TTFTMillis = ewma(entry.TTFTMillis, float64(ttft.Milliseconds()), 0.2)
	}
	entry.lastSample = now
	s.entries[key] = entry
	s.mu.Unlock()
}

func (s *MemoryRoutingHealthStore) OpenCircuit(_ context.Context, accountID int64, model, endpoint string, cooldown time.Duration) {
	s.OpenCircuitWithMax(context.Background(), accountID, model, endpoint, cooldown, 0)
}

func (s *MemoryRoutingHealthStore) OpenCircuitWithMax(_ context.Context, accountID int64, model, endpoint string, cooldown, maxCooldown time.Duration) {
	if s == nil || accountID <= 0 {
		return
	}
	s.mu.Lock()
	entry := s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}]
	entry.reopenCount++
	if entry.reopenCount > 1 {
		shift := entry.reopenCount - 1
		if shift > 20 {
			shift = 20
		}
		cooldown *= time.Duration(1 << shift)
	}
	if maxCooldown > 0 && cooldown > maxCooldown {
		cooldown = maxCooldown
	}
	entry.CircuitState = "open"
	entry.OpenUntil = time.Now().Add(cooldown)
	entry.halfOpenRequests = 0
	s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}] = entry
	s.mu.Unlock()
}

func (s *MemoryRoutingHealthStore) CloseCircuit(_ context.Context, accountID int64, model, endpoint string) {
	if s == nil || accountID <= 0 {
		return
	}
	s.mu.Lock()
	entry := s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}]
	entry.CircuitState = "closed"
	entry.OpenUntil = time.Time{}
	entry.ConsecutiveFails = 0
	entry.halfOpenRequests = 0
	entry.reopenCount = 0
	s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}] = entry
	s.mu.Unlock()
}

func (s *MemoryRoutingHealthStore) AllowHalfOpen(_ context.Context, accountID int64, model, endpoint string, maxRequests int) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	if maxRequests <= 0 {
		maxRequests = 1
	}
	key := routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[key]
	if entry.CircuitState == "open" && entry.OpenUntil.After(time.Now()) {
		return false
	}
	if entry.CircuitState != "open" && entry.CircuitState != "half_open" {
		return true
	}
	if entry.halfOpenRequests >= maxRequests {
		return false
	}
	entry.CircuitState = "half_open"
	entry.halfOpenRequests++
	s.entries[key] = entry
	return true
}

func (s *MemoryRoutingHealthStore) CanHalfOpen(_ context.Context, accountID int64, model, endpoint string, maxRequests int) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	if maxRequests <= 0 {
		maxRequests = 1
	}
	s.mu.RLock()
	entry := s.entries[routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}]
	s.mu.RUnlock()
	if entry.CircuitState == "open" && entry.OpenUntil.After(time.Now()) {
		return false
	}
	if entry.CircuitState != "open" && entry.CircuitState != "half_open" {
		return true
	}
	return entry.halfOpenRequests < maxRequests
}

func (s *MemoryRoutingHealthStore) ReleaseHalfOpen(_ context.Context, accountID int64, model, endpoint string) {
	if s == nil || accountID <= 0 {
		return
	}
	key := routingHealthKey{accountID: accountID, model: model, endpoint: endpoint}
	s.mu.Lock()
	entry := s.entries[key]
	if entry.CircuitState == "half_open" && entry.halfOpenRequests > 0 {
		entry.halfOpenRequests--
		s.entries[key] = entry
	}
	s.mu.Unlock()
}

func ewma(previous, sample, alpha float64) float64 {
	if previous == 0 {
		return sample
	}
	return previous*(1-alpha) + sample*alpha
}

type RoutingCandidateDecision struct {
	AccountID       int64
	Score           float64
	EstimatedCost   float64
	PriceKnown      bool
	Excluded        bool
	ExclusionReason string
	Health          RoutingHealthSnapshot
}

type RoutingSelection struct {
	Account     *Account
	MappedModel string
	Candidates  []RoutingCandidateDecision
}

// RoutingPolicyRuntime is shared by the generic and OpenAI schedulers. It is
// intentionally independent of request forwarding so all protocols can use
// the same candidate decisions without sharing response writers.
type RoutingPolicyRuntime struct {
	control *RoutingPolicyControlService
	prices  RoutingPriceResolver
	health  RoutingHealthStore
}

func NewRoutingPolicyRuntime(control *RoutingPolicyControlService, prices RoutingPriceResolver, health RoutingHealthStore) *RoutingPolicyRuntime {
	if health == nil {
		health = NewMemoryRoutingHealthStore()
	}
	return &RoutingPolicyRuntime{control: control, prices: prices, health: health}
}

func (r *RoutingPolicyRuntime) HealthStore() RoutingHealthStore {
	if r == nil {
		return nil
	}
	return r.health
}

func (r *RoutingPolicyRuntime) EffectiveForGroup(ctx context.Context, groupID int64) (*EffectiveRoutingPolicy, error) {
	if r == nil || r.control == nil {
		return nil, errors.New("routing policy runtime is not configured")
	}
	return r.control.EffectiveForGroup(ctx, groupID)
}

func (r *RoutingPolicyRuntime) SoftTTFTDelay(ctx context.Context, effective *EffectiveRoutingPolicy, accountID int64, model, endpoint string) time.Duration {
	if effective == nil {
		return 0
	}
	timeouts := effective.Revision.Config.Timeouts
	delay := time.Duration(timeouts.SoftTTFTMillis) * time.Millisecond
	if timeouts.AdaptiveSoftTTFT && r != nil && r.health != nil {
		snapshot := r.health.Snapshot(ctx, accountID, model, endpoint)
		if snapshot.TTFTMillis > 0 {
			factor := timeouts.SoftTTFTFactor
			if factor <= 0 {
				factor = 1
			}
			delay = time.Duration(snapshot.TTFTMillis*factor) * time.Millisecond
		}
	}
	if min := time.Duration(timeouts.SoftTTFTMinMillis) * time.Millisecond; min > 0 && delay < min {
		delay = min
	}
	if max := time.Duration(timeouts.SoftTTFTMaxMillis) * time.Millisecond; max > 0 && delay > max {
		delay = max
	}
	return delay
}

// AcquireHalfOpenProbe reserves a half-open probe only after the scheduler has
// acquired the account's concurrency slot. The returned release function must
// be paired with that slot's release so abandoned selections cannot leak the
// circuit-breaker probe allowance.
func (r *RoutingPolicyRuntime) AcquireHalfOpenProbe(ctx context.Context, effective *EffectiveRoutingPolicy, accountID int64, request RoutingRequestDescriptor) (func(), bool) {
	noop := func() {}
	if r == nil || r.health == nil || effective == nil || accountID <= 0 {
		return noop, true
	}
	snapshot := r.health.Snapshot(ctx, accountID, request.Model, request.Endpoint)
	if snapshot.CircuitState == "open" && snapshot.OpenUntil.After(time.Now()) {
		return noop, false
	}
	if snapshot.CircuitState != "open" && snapshot.CircuitState != "half_open" {
		return noop, true
	}
	controller, ok := r.health.(routingHalfOpenController)
	if !ok {
		return noop, true
	}
	if !controller.AllowHalfOpen(ctx, accountID, request.Model, request.Endpoint, effective.Revision.Config.CircuitBreaker.HalfOpenMaxRequests) {
		return noop, false
	}
	releaseController, ok := r.health.(routingHalfOpenReleaseController)
	if !ok {
		return noop, true
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseController.ReleaseHalfOpen(ctx, accountID, request.Model, request.Endpoint)
		})
	}, true
}

func combineRoutingReleases(releases ...func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, release := range releases {
				if release != nil {
					release()
				}
			}
		})
	}
}

// RecordResult closes the loop between forwarding outcomes and policy health.
// Protocol handlers can call this without knowing whether health is local or
// backed by a distributed store.
func (r *RoutingPolicyRuntime) RecordResult(ctx context.Context, effective *EffectiveRoutingPolicy, accountID int64, request RoutingRequestDescriptor, success bool, ttft time.Duration) {
	if r == nil || r.health == nil || accountID <= 0 {
		return
	}
	r.health.Record(ctx, accountID, request.Model, request.Endpoint, success, ttft)
	controller, ok := r.health.(routingCircuitController)
	if !ok || effective == nil {
		return
	}
	if success {
		controller.CloseCircuit(ctx, accountID, request.Model, request.Endpoint)
		return
	}
	snapshot := r.health.Snapshot(ctx, accountID, request.Model, request.Endpoint)
	cb := effective.Revision.Config.CircuitBreaker
	threshold := cb.ConsecutiveFailures > 0 && snapshot.ConsecutiveFails >= cb.ConsecutiveFailures
	if cb.MinSamples > 0 && snapshot.Samples >= cb.MinSamples && cb.ErrorRatePercent > 0 {
		threshold = threshold || snapshot.ErrorRate*100 >= float64(cb.ErrorRatePercent)
	}
	if threshold {
		cooldown := time.Duration(cb.CooldownMillis) * time.Millisecond
		if cooldown <= 0 {
			cooldown = 30 * time.Second
		}
		if backoff, ok := r.health.(routingCircuitBackoffController); ok {
			backoff.OpenCircuitWithMax(ctx, accountID, request.Model, request.Endpoint, cooldown, time.Duration(cb.MaxCooldownMillis)*time.Millisecond)
		} else {
			controller.OpenCircuit(ctx, accountID, request.Model, request.Endpoint, cooldown)
		}
	}
}

func (r *RoutingPolicyRuntime) RecordMonitorResult(ctx context.Context, accountID int64, model, endpoint string, success bool, latency time.Duration) {
	if r == nil || r.health == nil || accountID <= 0 {
		return
	}
	r.health.Record(ctx, accountID, model, endpoint, success, latency)
}

func (r *RoutingPolicyRuntime) RecordUnscopedResult(ctx context.Context, accountID int64, model, endpoint string, success bool, ttft time.Duration) {
	if r == nil || r.health == nil || accountID <= 0 {
		return
	}
	r.health.Record(ctx, accountID, model, endpoint, success, ttft)
	controller, ok := r.health.(routingCircuitController)
	if !ok {
		return
	}
	snapshot := r.health.Snapshot(ctx, accountID, model, endpoint)
	if !success && (snapshot.ConsecutiveFails >= 3 || (snapshot.Samples >= 10 && snapshot.ErrorRate >= 0.5)) {
		controller.OpenCircuit(ctx, accountID, model, endpoint, 30*time.Second)
	}
}

func (r *RoutingPolicyRuntime) Select(ctx context.Context, effective *EffectiveRoutingPolicy, accounts []Account, request RoutingRequestDescriptor, excluded map[int64]struct{}) (*RoutingSelection, error) {
	if effective == nil {
		return nil, errors.New("effective routing policy is nil")
	}
	if err := effective.Revision.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid effective routing policy: %w", err)
	}
	policy := effective.Revision.Config
	mappedModel := resolveRoutingModel(policy.ModelMappings, request.Model)
	decisions := make([]RoutingCandidateDecision, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		decision := RoutingCandidateDecision{AccountID: account.ID}
		if _, ok := excluded[account.ID]; ok {
			decision.Excluded = true
			decision.ExclusionReason = "excluded_by_attempt"
			decisions = append(decisions, decision)
			continue
		}
		if !account.IsSchedulable() {
			decision.Excluded = true
			decision.ExclusionReason = "account_not_schedulable"
			decisions = append(decisions, decision)
			continue
		}
		if len(policy.CandidateFilters.Platforms) > 0 && !containsFold(policy.CandidateFilters.Platforms, account.Platform) {
			decision.Excluded = true
			decision.ExclusionReason = "platform_not_allowed"
			decisions = append(decisions, decision)
			continue
		}
		if len(policy.CandidateFilters.ReliabilityClasses) > 0 && !containsFold(policy.CandidateFilters.ReliabilityClasses, account.ReliabilityClass) {
			decision.Excluded = true
			decision.ExclusionReason = "reliability_class_not_allowed"
			decisions = append(decisions, decision)
			continue
		}
		if policy.CandidateFilters.RequireTrustedUpstream && !isTrustedReliabilityClass(account.ReliabilityClass) {
			decision.Excluded = true
			decision.ExclusionReason = "trusted_upstream_required"
			decisions = append(decisions, decision)
			continue
		}
		if !labelsMatch(account.RoutingLabels, policy.CandidateFilters.RequiredLabels, false) || labelsMatch(account.RoutingLabels, policy.CandidateFilters.ExcludedLabels, true) {
			decision.Excluded = true
			decision.ExclusionReason = "routing_labels_mismatch"
			decisions = append(decisions, decision)
			continue
		}
		health := RoutingHealthSnapshot{}
		if r.health != nil {
			health = r.health.Snapshot(ctx, account.ID, request.Model, request.Endpoint)
		}
		decision.Health = health
		if health.CircuitState == "open" && health.OpenUntil.After(time.Now()) {
			decision.Excluded = true
			decision.ExclusionReason = "circuit_open"
			decisions = append(decisions, decision)
			continue
		}
		if health.CircuitState == "open" || health.CircuitState == "half_open" {
			capacity, ok := r.health.(routingHalfOpenCapacityController)
			if ok && !capacity.CanHalfOpen(ctx, account.ID, request.Model, request.Endpoint, policy.CircuitBreaker.HalfOpenMaxRequests) {
				decision.Excluded = true
				decision.ExclusionReason = "circuit_half_open_limit"
				decisions = append(decisions, decision)
				continue
			}
		}
		quote, known, err := r.quote(ctx, account, mappedModel)
		if err != nil {
			return nil, err
		}
		decision.PriceKnown = known
		if policy.CandidateFilters.RequireKnownPrice && !known {
			decision.Excluded = true
			decision.ExclusionReason = "price_unknown"
			decisions = append(decisions, decision)
			continue
		}
		decision.EstimatedCost = estimateCost(quote, request.EstimatedInputToken, request.EstimatedOutputToken)
		totalReservedCost := decision.EstimatedCost
		if policy.Hedge.Enabled {
			totalReservedCost += policy.CostBudget.ReserveForHedgeUSD.InexactFloat64()
		}
		if policy.CostBudget.MaxUpstreamUSD.GreaterThan(decimalFromFloat(0)) && totalReservedCost > policy.CostBudget.MaxUpstreamUSD.InexactFloat64() {
			decision.Excluded = true
			decision.ExclusionReason = "upstream_cost_budget"
			decisions = append(decisions, decision)
			continue
		}
		if policy.CostBudget.MaxAttemptCostUSD.GreaterThan(decimalFromFloat(0)) && decision.EstimatedCost > policy.CostBudget.MaxAttemptCostUSD.InexactFloat64() {
			decision.Excluded = true
			decision.ExclusionReason = "attempt_cost_budget"
			decisions = append(decisions, decision)
			continue
		}
		decision.Score = scoreCandidate(policy.Scoring, decision.EstimatedCost, known, health, account)
		decisions = append(decisions, decision)
	}
	available := make([]RoutingCandidateDecision, 0, len(decisions))
	for _, decision := range decisions {
		if !decision.Excluded {
			available = append(available, decision)
		}
	}
	if len(available) == 0 {
		return &RoutingSelection{MappedModel: mappedModel, Candidates: decisions}, ErrNoAvailableAccounts
	}
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].Score == available[j].Score {
			return available[i].AccountID < available[j].AccountID
		}
		return available[i].Score < available[j].Score
	})
	selectedID := available[0].AccountID
	for i := range accounts {
		if accounts[i].ID == selectedID {
			return &RoutingSelection{Account: &accounts[i], MappedModel: mappedModel, Candidates: decisions}, nil
		}
	}
	return &RoutingSelection{MappedModel: mappedModel, Candidates: decisions}, ErrNoAvailableAccounts
}

func resolveRoutingModel(mappings map[string]string, requested string) string {
	requested = strings.TrimSpace(requested)
	if mapped := strings.TrimSpace(mappings[requested]); mapped != "" {
		return mapped
	}
	for pattern, mapped := range mappings {
		prefix := strings.TrimSuffix(strings.TrimSpace(pattern), "*")
		if strings.HasSuffix(strings.TrimSpace(pattern), "*") && strings.HasPrefix(requested, prefix) && strings.TrimSpace(mapped) != "" {
			return strings.TrimSpace(mapped)
		}
	}
	return requested
}

func routingFallbackAccounts(
	ctx context.Context,
	repo AccountRepository,
	effective *EffectiveRoutingPolicy,
	primaryGroupID int64,
	requestedModel string,
	excluded map[int64]struct{},
) ([]Account, string, error) {
	if effective == nil {
		return nil, strings.TrimSpace(requestedModel), nil
	}
	config := effective.Revision.Config
	mappedModel := resolveRoutingModel(config.ModelMappings, requestedModel)
	if repo == nil || !config.Fallback.AllowCrossTier || len(config.Fallback.GroupIDs) == 0 {
		return nil, mappedModel, nil
	}
	if config.Fallback.RequireExplicitModelMap && !routingModelMappingExists(config.ModelMappings, requestedModel) {
		return nil, mappedModel, nil
	}
	seen := make(map[int64]struct{}, len(excluded))
	for id := range excluded {
		seen[id] = struct{}{}
	}
	accounts := make([]Account, 0)
	for _, groupID := range config.Fallback.GroupIDs {
		if groupID <= 0 || groupID == primaryGroupID {
			continue
		}
		groupAccounts, err := repo.ListByGroup(ctx, groupID)
		if err != nil {
			return nil, mappedModel, fmt.Errorf("list routing fallback group %d: %w", groupID, err)
		}
		for _, account := range groupAccounts {
			if _, ok := seen[account.ID]; ok {
				continue
			}
			seen[account.ID] = struct{}{}
			accounts = append(accounts, account)
		}
	}
	return accounts, mappedModel, nil
}

func routingModelMappingExists(mappings map[string]string, requested string) bool {
	requested = strings.TrimSpace(requested)
	if strings.TrimSpace(mappings[requested]) != "" {
		return true
	}
	for pattern, mapped := range mappings {
		pattern = strings.TrimSpace(pattern)
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(requested, strings.TrimSuffix(pattern, "*")) && strings.TrimSpace(mapped) != "" {
			return true
		}
	}
	return false
}

func routingSelectionKnownCost(selection *RoutingSelection, accountID int64) (float64, bool) {
	if selection == nil {
		return 0, false
	}
	for _, candidate := range selection.Candidates {
		if candidate.AccountID == accountID && candidate.PriceKnown {
			return candidate.EstimatedCost, true
		}
	}
	return 0, false
}

func routingSelectionMinKnownCost(selection *RoutingSelection) (float64, bool) {
	if selection == nil {
		return 0, false
	}
	minimum := 0.0
	known := false
	for _, candidate := range selection.Candidates {
		if !candidate.PriceKnown {
			continue
		}
		if !known || candidate.EstimatedCost < minimum {
			minimum = candidate.EstimatedCost
			known = true
		}
	}
	return minimum, known
}

func routingRetryableError(policy RoutingRetryPolicy, err error) bool {
	if err == nil {
		return false
	}
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) {
		for _, status := range policy.RetryableStatusCodes {
			if status == failover.StatusCode {
				return true
			}
		}
		return false
	}
	return policy.RetryTransportErrors
}

func routingRetrySwitchLimit(policy RoutingRetryPolicy, fallback int) int {
	if policy.MaxAttempts <= 0 {
		return 0
	}
	limit := policy.MaxSwitches
	if limit < 0 {
		limit = 0
	}
	if attemptsLimit := policy.MaxAttempts - 1; limit > attemptsLimit {
		limit = attemptsLimit
	}
	if fallback >= 0 && limit > fallback {
		return fallback
	}
	return limit
}

func decimalFromFloat(value float64) decimal.Decimal {
	return decimal.NewFromFloat(value)
}

func (r *RoutingPolicyRuntime) quote(ctx context.Context, account *Account, model string) (RoutingPriceQuote, bool, error) {
	if r == nil || r.prices == nil {
		return RoutingPriceQuote{}, false, nil
	}
	return r.prices.Quote(ctx, account, model)
}

func estimateCost(quote RoutingPriceQuote, input, output int) float64 {
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}
	return quote.RequestPrice + float64(input)/1_000_000*quote.InputPerMillion + float64(output)/1_000_000*quote.OutputPerMillion
}

func scoreCandidate(weights RoutingScoringWeights, cost float64, known bool, health RoutingHealthSnapshot, account *Account) float64 {
	price := cost
	if !known {
		price = 1_000_000
	}
	load := health.Load
	if load == 0 {
		load = float64(account.EffectiveLoadFactor())
	}
	queue := health.Queue
	// Lower is better for every component. Reliability is expressed as a
	// penalty so higher reliability classes naturally rank ahead.
	reliabilityPenalty := 1.0 - reliabilityScore(account.ReliabilityClass)
	return price*weights.Price + health.ErrorRate*weights.ErrorRate + health.TTFTMillis*weights.TTFT + load*weights.Load + queue*weights.Queue + reliabilityPenalty*weights.Reliability
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func labelsMatch(labels, expected map[string]string, invert bool) bool {
	if len(expected) == 0 {
		return !invert
	}
	for key, value := range expected {
		if current, ok := labels[key]; ok && strings.EqualFold(current, value) {
			if invert {
				return true
			}
			continue
		}
		if !invert {
			return false
		}
	}
	return !invert
}

func isTrustedReliabilityClass(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "trusted", "official", "partner":
		return true
	default:
		return false
	}
}

func reliabilityScore(class string) float64 {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "official":
		return 1
	case "trusted", "partner":
		return 0.8
	case "standard":
		return 0.5
	default:
		return 0.2
	}
}
