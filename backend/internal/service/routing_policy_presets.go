package service

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	RoutingPresetEconomy      = "economy"
	RoutingPresetStandard     = "standard"
	RoutingPresetProfessional = "professional"
)

// DefaultRoutingPolicyConfig returns an editable baseline. Presets are data,
// not special runtime branches, so administrators can clone and tune them.
func DefaultRoutingPolicyConfig(name string) (RoutingPolicyConfig, error) {
	base := RoutingPolicyConfig{
		SchemaVersion: 1,
		Retry: RoutingRetryPolicy{
			MaxAttempts:          3,
			MaxSwitches:          2,
			RetryTransportErrors: true,
			RetryableStatusCodes: []int{408, 409, 429, 500, 502, 503, 504, 529},
		},
		Hedge: RoutingHedgePolicy{
			Enabled:                 true,
			MaxConcurrent:           2,
			RequireDifferentDomain:  true,
			RequireNoSemanticOutput: true,
		},
		CircuitBreaker: RoutingCircuitBreakerPolicy{
			ConsecutiveFailures: 3,
			MinSamples:          10,
			ErrorRatePercent:    50,
			CooldownMillis:      30_000,
			MaxCooldownMillis:   300_000,
			HalfOpenMaxRequests: 1,
		},
		Fallback: RoutingFallbackPolicy{
			AllowCrossTier:          true,
			MaxCostMultiplier:       1.5,
			RequireExplicitModelMap: true,
		},
		CostBudget: RoutingCostBudget{
			MaxAttemptCostUSD:  decimal.Zero,
			ReserveForHedgeUSD: decimal.Zero,
		},
	}

	switch strings.ToLower(strings.TrimSpace(name)) {
	case RoutingPresetEconomy:
		base.CandidateFilters.RequireKnownPrice = true
		base.Scoring = RoutingScoringWeights{Price: 65, ErrorRate: 15, TTFT: 5, Load: 10, Queue: 5}
		base.Timeouts = RoutingTimeoutPolicy{RequestTimeoutMillis: 120_000, AdaptiveSoftTTFT: true, SoftTTFTFactor: 1.25, SoftTTFTMinMillis: 2_500, SoftTTFTMaxMillis: 8_000, SoftTTFTMillis: 8_000, StreamIdleMillis: 30_000}
		base.Hedge.DelayMillis = 2_500
		base.Fallback.MaxCostMultiplier = 1.5
	case RoutingPresetStandard:
		base.Scoring = RoutingScoringWeights{Price: 35, ErrorRate: 30, TTFT: 20, Load: 10, Queue: 5}
		base.Timeouts = RoutingTimeoutPolicy{RequestTimeoutMillis: 120_000, AdaptiveSoftTTFT: true, SoftTTFTFactor: 1.10, SoftTTFTMinMillis: 1_500, SoftTTFTMaxMillis: 5_000, SoftTTFTMillis: 5_000, StreamIdleMillis: 30_000}
		base.Hedge.DelayMillis = 1_500
		base.Fallback.MaxCostMultiplier = 2
	case RoutingPresetProfessional:
		base.CandidateFilters.RequireTrustedUpstream = true
		base.Scoring = RoutingScoringWeights{Price: 10, ErrorRate: 45, TTFT: 30, Load: 10, Queue: 5, Reliability: 10}
		base.Timeouts = RoutingTimeoutPolicy{RequestTimeoutMillis: 120_000, AdaptiveSoftTTFT: true, SoftTTFTFactor: 1, SoftTTFTMinMillis: 800, SoftTTFTMaxMillis: 3_000, SoftTTFTMillis: 3_000, StreamIdleMillis: 30_000}
		base.Hedge.DelayMillis = 800
		base.Fallback.MaxCostMultiplier = 3
	default:
		return RoutingPolicyConfig{}, fmt.Errorf("unknown routing policy preset %q", name)
	}
	return base, nil
}
