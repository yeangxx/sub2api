package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func (s *GatewayService) SetRoutingPolicyRuntime(runtime *RoutingPolicyRuntime) {
	if s != nil {
		s.routingPolicyRuntime = runtime
	}
}

func (s *GatewayService) RoutingRetryPolicy(ctx context.Context, groupID *int64) (RoutingRetryPolicy, bool) {
	if s == nil || s.routingPolicyRuntime == nil || groupID == nil || *groupID <= 0 {
		return RoutingRetryPolicy{}, false
	}
	effective, err := s.routingPolicyRuntime.EffectiveForGroup(ctx, *groupID)
	if err != nil || effective == nil || effective.Binding.Mode != RoutingPolicyModeEnforce {
		return RoutingRetryPolicy{}, false
	}
	return effective.Revision.Config.Retry, true
}

func (s *GatewayService) RoutingRetrySwitchLimit(ctx context.Context, groupID *int64, fallback int) (int, bool) {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok {
		return fallback, false
	}
	return routingRetrySwitchLimit(policy, fallback), true
}

func (s *GatewayService) RoutingErrorRetryable(ctx context.Context, groupID *int64, err error) bool {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok {
		return true
	}
	return routingRetryableError(policy, err)
}

func (s *GatewayService) RoutingTransportErrorRetryable(ctx context.Context, groupID *int64, err error) bool {
	policy, ok := s.RoutingRetryPolicy(ctx, groupID)
	if !ok || !policy.RetryTransportErrors || err == nil {
		return false
	}
	var failover *UpstreamFailoverError
	return !errors.As(err, &failover)
}

func (s *GatewayService) ReportRoutingResult(ctx context.Context, accountID int64, model, endpoint string, success bool, ttft time.Duration) {
	if s == nil || s.routingPolicyRuntime == nil {
		return
	}
	if model == "" {
		model = "*"
	}
	s.routingPolicyRuntime.RecordUnscopedResult(ctx, accountID, model, endpoint, success, ttft)
}

func (s *GatewayService) ReportRoutingSelectionResult(ctx context.Context, selection *AccountSelectionResult, accountID int64, model, endpoint string, success bool, ttft time.Duration) {
	if s == nil || s.routingPolicyRuntime == nil {
		return
	}
	if selection != nil && selection.RoutingPolicy != nil && selection.RoutingRequest != nil {
		s.routingPolicyRuntime.RecordResult(ctx, selection.RoutingPolicy, accountID, *selection.RoutingRequest, success, ttft)
		return
	}
	s.ReportRoutingResult(ctx, accountID, model, endpoint, success, ttft)
}

// selectWithRoutingPolicy runs before the legacy selector. A missing binding
// is deliberately reported as handled=false so existing groups preserve their
// exact scheduling behavior.
func (s *GatewayService) selectWithRoutingPolicy(
	ctx context.Context,
	groupID *int64,
	group *Group,
	platform string,
	useMixed bool,
	requestedModel string,
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

	modelRoute := map[int64]struct{}{}
	if group != nil && requestedModel != "" && group.ModelRoutingEnabled {
		for _, id := range group.GetRoutingAccountIDs(requestedModel) {
			modelRoute[id] = struct{}{}
		}
	}
	filterCandidates := func(source []Account, model string, enforceModelRoute bool) []Account {
		candidates := make([]Account, 0, len(source))
		for i := range source {
			account := source[i]
			if enforceModelRoute && len(modelRoute) > 0 {
				if _, ok := modelRoute[account.ID]; !ok {
					continue
				}
			}
			if !s.isAccountAllowedForPlatform(&account, platform, useMixed) ||
				(model != "" && !s.isModelSupportedByAccountWithContext(ctx, &account, model)) ||
				!s.isAccountSchedulableForModelSelection(ctx, &account, model) ||
				!s.isAccountSchedulableForQuota(&account) ||
				!s.isAccountSchedulableForWindowCost(ctx, &account, false) ||
				!s.isAccountSchedulableForRPM(ctx, &account, false) {
				continue
			}
			candidates = append(candidates, account)
		}
		return candidates
	}
	primaryModel := resolveRoutingModel(effective.Revision.Config.ModelMappings, requestedModel)
	primaryCandidates := filterCandidates(accounts, primaryModel, true)
	fallbackAccounts, fallbackModel, fallbackErr := routingFallbackAccounts(ctx, s.accountRepo, effective, *groupID, requestedModel, excluded)
	if fallbackErr != nil {
		if effective.Binding.Mode == RoutingPolicyModeShadow {
			slog.Warn("routing_policy_shadow_fallback_load_failed", "group_id", *groupID, "error", fallbackErr)
			return nil, false, nil
		}
		return nil, true, fallbackErr
	}
	candidatePhases := [][]Account{primaryCandidates}
	if fallbackCandidates := filterCandidates(fallbackAccounts, fallbackModel, false); len(fallbackCandidates) > 0 {
		candidatePhases = append(candidatePhases, fallbackCandidates)
	}

	workingExcluded := make(map[int64]struct{}, len(excluded))
	for id := range excluded {
		workingExcluded[id] = struct{}{}
	}
	primaryBaseline, primaryBaselineKnown := 0.0, false
	for phaseIndex, candidates := range candidatePhases {
		for attempts := 0; attempts < len(candidates); attempts++ {
			routingRequest := routingRequestDescriptor(ctx, *groupID, platform, requestedModel, "gateway", true)
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
					slog.Warn("routing_policy_shadow_selection_failed", "group_id", *groupID, "model", requestedModel, "error", selectErr)
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
				slog.Info("routing_policy_shadow_decision", "group_id", *groupID, "model", requestedModel, "policy_id", effective.Policy.ID, "account_id", account.ID)
				return nil, false, nil
			}
			result, acquireErr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if acquireErr == nil && result.Acquired {
				probeRelease, allowed := s.routingPolicyRuntime.AcquireHalfOpenProbe(ctx, effective, account.ID, routingRequest)
				if !allowed {
					result.ReleaseFunc()
					workingExcluded[account.ID] = struct{}{}
					continue
				}
				release := combineRoutingReleases(result.ReleaseFunc, probeRelease)
				if !s.checkAndRegisterSession(ctx, account, sessionHash) {
					release()
					workingExcluded[account.ID] = struct{}{}
					continue
				}
				if sessionHash != "" && s.cache != nil {
					_ = s.cache.SetSessionAccountID(ctx, *groupID, sessionHash, account.ID, stickySessionTTL)
				}
				selectionResult, selectionErr := s.newSelectionResult(ctx, account, true, release, nil)
				if selectionErr != nil {
					release()
					return nil, true, selectionErr
				}
				selectionResult.RoutingMappedModel = selection.MappedModel
				selectionResult.RoutingPolicy = effective
				selectionResult.RoutingRequest = &routingRequest
				return selectionResult, true, nil
			}
			workingExcluded[account.ID] = struct{}{}
		}
	}

	if effective.Binding.Mode == RoutingPolicyModeShadow {
		return nil, false, nil
	}
	slog.Warn("routing policy has no schedulable candidate", "group_id", *groupID, "model", requestedModel, "policy_id", effective.Policy.ID)
	return nil, true, ErrNoAvailableAccounts
}
