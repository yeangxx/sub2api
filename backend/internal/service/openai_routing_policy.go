package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func (s *OpenAIGatewayService) SetRoutingPolicyRuntime(runtime *RoutingPolicyRuntime) {
	if s != nil {
		s.routingPolicyRuntime = runtime
	}
}

func (s *OpenAIGatewayService) RoutingHedgePolicy(ctx context.Context, groupID *int64) (RoutingHedgePolicy, bool) {
	if s == nil || s.routingPolicyRuntime == nil || groupID == nil || *groupID <= 0 {
		return RoutingHedgePolicy{}, false
	}
	effective, err := s.routingPolicyRuntime.EffectiveForGroup(ctx, *groupID)
	if err != nil || effective == nil || effective.Binding.Mode != RoutingPolicyModeEnforce {
		return RoutingHedgePolicy{}, false
	}
	return effective.Revision.Config.Hedge, true
}

func (s *OpenAIGatewayService) RoutingHedgeDelay(ctx context.Context, groupID *int64, accountID int64, model string) (time.Duration, bool) {
	if s == nil || s.routingPolicyRuntime == nil || groupID == nil || *groupID <= 0 {
		return 0, false
	}
	effective, err := s.routingPolicyRuntime.EffectiveForGroup(ctx, *groupID)
	if err != nil || effective == nil || effective.Binding.Mode != RoutingPolicyModeEnforce || !effective.Revision.Config.Hedge.Enabled {
		return 0, false
	}
	delay := time.Duration(effective.Revision.Config.Hedge.DelayMillis) * time.Millisecond
	if softDelay := s.routingPolicyRuntime.SoftTTFTDelay(ctx, effective, accountID, model, "openai"); softDelay > 0 && softDelay < delay {
		delay = softDelay
	}
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

func (s *OpenAIGatewayService) RoutingRetryPolicy(ctx context.Context, groupID *int64) (RoutingRetryPolicy, bool) {
	if s == nil || s.routingPolicyRuntime == nil || groupID == nil || *groupID <= 0 {
		return RoutingRetryPolicy{}, false
	}
	effective, err := s.routingPolicyRuntime.EffectiveForGroup(ctx, *groupID)
	if err != nil || effective == nil || effective.Binding.Mode != RoutingPolicyModeEnforce {
		return RoutingRetryPolicy{}, false
	}
	return effective.Revision.Config.Retry, true
}

func (s *OpenAIGatewayService) RoutingRetrySwitchLimit(ctx context.Context, groupID *int64, fallback int) (int, bool) {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok {
		return fallback, false
	}
	return routingRetrySwitchLimit(policy, fallback), true
}

func (s *OpenAIGatewayService) RoutingErrorRetryable(ctx context.Context, groupID *int64, err error) bool {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok {
		return true
	}
	return routingRetryableError(policy, err)
}

func (s *OpenAIGatewayService) RoutingTransportErrorRetryable(ctx context.Context, groupID *int64, err error) bool {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok || !policy.RetryTransportErrors || err == nil {
		return false
	}
	var failover *UpstreamFailoverError
	return !errors.As(err, &failover)
}

func (s *OpenAIGatewayService) selectWithRoutingPolicy(
	ctx context.Context,
	groupID *int64,
	platform string,
	requestedModel string,
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
	needsUpstreamCheck bool,
	sessionHash string,
	excluded map[int64]struct{},
	accounts []Account,
) (*AccountSelectionResult, bool, error) {
	if s == nil || s.routingPolicyRuntime == nil || groupID == nil || *groupID <= 0 {
		return nil, false, nil
	}
	effective, err := s.routingPolicyRuntime.EffectiveForGroup(ctx, *groupID)
	if errors.Is(err, ErrRoutingPolicyBindingNotFound) {
		return nil, false, nil
	}
	if errors.Is(err, ErrRoutingPolicyDisabled) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	parentCache := make(map[int64]*Account)
	parentLookup := func(id int64) *Account {
		if parent, ok := parentCache[id]; ok {
			return parent
		}
		if s.accountRepo == nil {
			return nil
		}
		parent, _ := s.accountRepo.GetByID(ctx, id)
		parentCache[id] = parent
		return parent
	}
	filterCandidates := func(source []Account, model string) []Account {
		candidates := make([]Account, 0, len(source))
		for i := range source {
			account := source[i]
			if _, ok := excluded[account.ID]; ok {
				continue
			}
			if !isOpenAICompatibleAccountEligibleForRequest(ctx, &account, platform, model, false, requiredCapability) ||
				!parentHealthyForShadow(&account, parentLookup) ||
				s.isOpenAIAccountRuntimeBlocked(&account) {
				continue
			}
			if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, &account, model, requireCompact) {
				continue
			}
			candidates = append(candidates, account)
		}
		return candidates
	}
	primaryCandidates := filterCandidates(accounts, requestedModel)
	fallbackAccounts, fallbackModel, fallbackErr := routingFallbackAccounts(ctx, s.accountRepo, effective, *groupID, requestedModel, excluded)
	if fallbackErr != nil {
		if effective.Binding.Mode == RoutingPolicyModeShadow {
			slog.Warn("routing_policy_shadow_fallback_load_failed", "group_id", *groupID, "error", fallbackErr)
			return nil, false, nil
		}
		return nil, true, fallbackErr
	}
	candidatePhases := [][]Account{primaryCandidates}
	if fallbackCandidates := filterCandidates(fallbackAccounts, fallbackModel); len(fallbackCandidates) > 0 {
		candidatePhases = append(candidatePhases, fallbackCandidates)
	}
	workingExcluded := make(map[int64]struct{}, len(excluded))
	for id := range excluded {
		workingExcluded[id] = struct{}{}
	}
	primaryBaseline, primaryBaselineKnown := 0.0, false
	for phaseIndex, candidates := range candidatePhases {
		for attempts := 0; attempts < len(candidates); attempts++ {
			routingRequest := routingRequestDescriptor(ctx, *groupID, platform, requestedModel, "openai", true)
			selection, selectErr := s.routingPolicyRuntime.Select(ctx, effective, candidates, routingRequest, workingExcluded)
			if phaseIndex == 0 {
				if cost, known := routingSelectionMinKnownCost(selection); known && (!primaryBaselineKnown || cost < primaryBaseline) {
					primaryBaseline, primaryBaselineKnown = cost, true
				}
			}
			if errors.Is(selectErr, ErrNoAvailableAccounts) {
				break
			}
			if selectErr != nil {
				if effective.Binding.Mode == RoutingPolicyModeShadow {
					slog.Warn("routing_policy_shadow_selection_failed", "group_id", *groupID, "model", requestedModel, "error", selectErr, "platform", platform)
					return nil, false, nil
				}
				return nil, true, selectErr
			}
			if selection == nil || selection.Account == nil {
				if effective.Binding.Mode == RoutingPolicyModeShadow {
					return nil, false, nil
				}
				return nil, true, ErrNoAvailableAccounts
			}
			account := selection.Account
			if phaseIndex > 0 && primaryBaselineKnown && effective.Revision.Config.Fallback.MaxCostMultiplier > 0 {
				if fallbackCost, known := routingSelectionKnownCost(selection, account.ID); known && fallbackCost > primaryBaseline*effective.Revision.Config.Fallback.MaxCostMultiplier {
					workingExcluded[account.ID] = struct{}{}
					continue
				}
			}
			if effective.Binding.Mode == RoutingPolicyModeShadow {
				slog.Info("routing_policy_shadow_decision", "group_id", *groupID, "model", requestedModel, "policy_id", effective.Policy.ID, "account_id", account.ID, "platform", platform)
				return nil, false, nil
			}
			result, acquireErr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if acquireErr == nil && result != nil && result.Acquired {
				probeRelease, allowed := s.routingPolicyRuntime.AcquireHalfOpenProbe(ctx, effective, account.ID, routingRequest)
				if !allowed {
					result.ReleaseFunc()
					workingExcluded[account.ID] = struct{}{}
					continue
				}
				release := combineRoutingReleases(result.ReleaseFunc, probeRelease)
				selectionResult, selectionErr := s.newAcquiredSelectionResult(ctx, account, release)
				if selectionErr != nil {
					release()
					return nil, true, selectionErr
				}
				if sessionHash != "" {
					_ = s.setStickySessionAccountID(ctx, groupID, sessionHash, account.ID, openaiStickySessionTTL)
				}
				selectionResult.RoutingMappedModel = selection.MappedModel
				selectionResult.RoutingPolicy = effective
				return selectionResult, true, nil
			}
			workingExcluded[account.ID] = struct{}{}
		}
	}
	return nil, true, ErrNoAvailableAccounts
}
