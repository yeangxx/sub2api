package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// RoutingPolicyHandler exposes the routing control plane. Runtime request
// forwarding does not depend on this handler; all mutations go through the
// versioned service facade and therefore become visible atomically.
type RoutingPolicyHandler struct {
	control     *service.RoutingPolicyControlService
	accountRepo service.AccountRepository
	runtime     *service.RoutingPolicyRuntime
	encryptor   service.SecretEncryptor
	cfg         *config.Config
}

func NewRoutingPolicyHandler(
	control *service.RoutingPolicyControlService,
	accountRepo service.AccountRepository,
	runtime *service.RoutingPolicyRuntime,
	encryptor service.SecretEncryptor,
	cfg *config.Config,
) *RoutingPolicyHandler {
	return &RoutingPolicyHandler{control: control, accountRepo: accountRepo, runtime: runtime, encryptor: encryptor, cfg: cfg}
}

type routingPolicyDTO struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	DraftRevisionID     *int64 `json:"draft_revision_id,omitempty"`
	PublishedRevisionID *int64 `json:"published_revision_id,omitempty"`
	CreatedBy           *int64 `json:"created_by,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type routingPolicyRevisionDTO struct {
	ID            int64                       `json:"id"`
	PolicyID      int64                       `json:"policy_id"`
	Version       int                         `json:"version"`
	State         string                      `json:"state"`
	SchemaVersion int                         `json:"schema_version"`
	Config        service.RoutingPolicyConfig `json:"config"`
	Checksum      string                      `json:"checksum"`
	Comment       string                      `json:"comment"`
	CreatedBy     *int64                      `json:"created_by,omitempty"`
	CreatedAt     string                      `json:"created_at"`
	PublishedAt   *string                     `json:"published_at,omitempty"`
}

type upstreamPriceBookDTO struct {
	ID               int64          `json:"id"`
	Name             string         `json:"name"`
	Source           string         `json:"source"`
	Status           string         `json:"status"`
	Currency         string         `json:"currency"`
	LatestRevisionID *int64         `json:"latest_revision_id,omitempty"`
	SourceConfig     map[string]any `json:"source_config,omitempty"`
	CreatedBy        *int64         `json:"created_by,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

const (
	upstreamHeaderMask           = "********"
	upstreamEncryptedValuePrefix = "enc:v1:"
)

type upstreamPriceBookRevisionDTO struct {
	ID             int64                   `json:"id"`
	PriceBookID    int64                   `json:"price_book_id"`
	Version        int                     `json:"version"`
	State          string                  `json:"state"`
	EffectiveAt    *string                 `json:"effective_at,omitempty"`
	SourceSnapshot map[string]any          `json:"source_snapshot"`
	Comment        string                  `json:"comment"`
	CreatedBy      *int64                  `json:"created_by,omitempty"`
	CreatedAt      string                  `json:"created_at"`
	PublishedAt    *string                 `json:"published_at,omitempty"`
	Prices         []upstreamModelPriceDTO `json:"prices,omitempty"`
}

type upstreamModelPriceDTO struct {
	ID                        int64          `json:"id,omitempty"`
	RevisionID                int64          `json:"revision_id,omitempty"`
	ModelPattern              string         `json:"model_pattern"`
	InputPricePerMillion      string         `json:"input_price_per_million,omitempty"`
	OutputPricePerMillion     string         `json:"output_price_per_million,omitempty"`
	CacheReadPricePerMillion  string         `json:"cache_read_price_per_million,omitempty"`
	CacheWritePricePerMillion string         `json:"cache_write_price_per_million,omitempty"`
	RequestPrice              string         `json:"request_price,omitempty"`
	Metadata                  map[string]any `json:"metadata,omitempty"`
	CreatedAt                 string         `json:"created_at,omitempty"`
}

type routingPolicyCreateRequest struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Status      string                      `json:"status"`
	Config      service.RoutingPolicyConfig `json:"config"`
	Comment     string                      `json:"comment"`
}

type routingPolicyUpdateRequest struct {
	Name        *string                      `json:"name,omitempty"`
	Description *string                      `json:"description,omitempty"`
	Status      *string                      `json:"status,omitempty"`
	Config      *service.RoutingPolicyConfig `json:"config,omitempty"`
	Comment     string                       `json:"comment"`
}

type routingPolicyValidateRequest struct {
	Config service.RoutingPolicyConfig `json:"config"`
}
type routingPolicySimulateRequest struct {
	GroupID               int64  `json:"group_id"`
	Model                 string `json:"model"`
	RevisionID            *int64 `json:"revision_id,omitempty"`
	EstimatedInputTokens  *int   `json:"estimated_input_tokens,omitempty"`
	EstimatedOutputTokens *int   `json:"estimated_output_tokens,omitempty"`
}

type routingSimulationAccountDTO struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	FailureDomain    string `json:"failure_domain,omitempty"`
	ReliabilityClass string `json:"reliability_class,omitempty"`
}

type routingSimulationHealthDTO struct {
	ErrorRate        float64 `json:"error_rate"`
	TTFTMillis       float64 `json:"ttft_ms"`
	Load             float64 `json:"load"`
	Queue            float64 `json:"queue"`
	ConsecutiveFails int     `json:"consecutive_failures"`
	Samples          int     `json:"samples"`
	CircuitState     string  `json:"circuit_state,omitempty"`
}

type routingSimulationCandidateDTO struct {
	AccountID       int64                      `json:"account_id"`
	AccountName     string                     `json:"account_name"`
	Platform        string                     `json:"platform"`
	Score           float64                    `json:"score"`
	EstimatedCost   float64                    `json:"estimated_cost_usd"`
	PriceKnown      bool                       `json:"price_known"`
	Excluded        bool                       `json:"excluded"`
	ExclusionReason string                     `json:"exclusion_reason,omitempty"`
	Health          routingSimulationHealthDTO `json:"health"`
}

type routingSimulationSelectionDTO struct {
	SelectedAccount *routingSimulationAccountDTO    `json:"selected_account,omitempty"`
	Candidates      []routingSimulationCandidateDTO `json:"candidates"`
}

func newRoutingSimulationSelectionDTO(selection *service.RoutingSelection, accounts []service.Account) routingSimulationSelectionDTO {
	accountByID := make(map[int64]service.Account, len(accounts))
	for i := range accounts {
		accountByID[accounts[i].ID] = accounts[i]
	}
	out := routingSimulationSelectionDTO{Candidates: make([]routingSimulationCandidateDTO, 0)}
	if selection == nil {
		return out
	}
	if selection.Account != nil {
		out.SelectedAccount = &routingSimulationAccountDTO{
			ID:               selection.Account.ID,
			Name:             selection.Account.Name,
			Platform:         string(selection.Account.Platform),
			FailureDomain:    selection.Account.FailureDomain,
			ReliabilityClass: selection.Account.ReliabilityClass,
		}
	}
	for _, candidate := range selection.Candidates {
		account := accountByID[candidate.AccountID]
		out.Candidates = append(out.Candidates, routingSimulationCandidateDTO{
			AccountID:       candidate.AccountID,
			AccountName:     account.Name,
			Platform:        string(account.Platform),
			Score:           candidate.Score,
			EstimatedCost:   candidate.EstimatedCost,
			PriceKnown:      candidate.PriceKnown,
			Excluded:        candidate.Excluded,
			ExclusionReason: candidate.ExclusionReason,
			Health: routingSimulationHealthDTO{
				ErrorRate:        candidate.Health.ErrorRate,
				TTFTMillis:       candidate.Health.TTFTMillis,
				Load:             candidate.Health.Load,
				Queue:            candidate.Health.Queue,
				ConsecutiveFails: candidate.Health.ConsecutiveFails,
				Samples:          candidate.Health.Samples,
				CircuitState:     candidate.Health.CircuitState,
			},
		})
	}
	return out
}

type routingPolicyPublishRequest struct {
	RevisionID *int64 `json:"revision_id,omitempty"`
	Version    *int   `json:"version,omitempty"`
}
type routingPolicyBindingRequest struct {
	PolicyID       *int64         `json:"policy_id,omitempty"`
	RevisionID     *int64         `json:"revision_id,omitempty"`
	Mode           string         `json:"mode"`
	ModelOverrides map[string]any `json:"model_overrides,omitempty"`
}

type priceBookCreateRequest struct {
	Name         string         `json:"name"`
	Source       string         `json:"source"`
	Status       string         `json:"status"`
	Currency     string         `json:"currency"`
	SourceConfig map[string]any `json:"source_config,omitempty"`
}
type priceBookUpdateRequest struct {
	Name         *string         `json:"name,omitempty"`
	Source       *string         `json:"source,omitempty"`
	Status       *string         `json:"status,omitempty"`
	Currency     *string         `json:"currency,omitempty"`
	SourceConfig *map[string]any `json:"source_config,omitempty"`
}
type priceBookRevisionRequest struct {
	Version        int                     `json:"version,omitempty"`
	State          string                  `json:"state,omitempty"`
	EffectiveAt    *string                 `json:"effective_at,omitempty"`
	SourceSnapshot map[string]any          `json:"source_snapshot,omitempty"`
	Comment        string                  `json:"comment,omitempty"`
	Prices         []upstreamModelPriceDTO `json:"prices,omitempty"`
}

func decodeStrict(c *gin.Context, dst any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}

func actorID(c *gin.Context) *int64 {
	if sub, ok := middleware.GetAuthSubjectFromContext(c); ok && sub.UserID > 0 {
		id := sub.UserID
		return &id
	}
	return nil
}

func parsePositiveID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

func routingError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, service.ErrRoutingPolicyNotFound), errors.Is(err, service.ErrRoutingPolicyRevisionNotFound), errors.Is(err, service.ErrRoutingPolicyBindingNotFound), errors.Is(err, service.ErrUpstreamPriceBookNotFound), errors.Is(err, service.ErrUpstreamPriceBookRevisionNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrRoutingPolicyRevisionNotPublished), errors.Is(err, service.ErrRoutingPolicyDisabled):
		response.BadRequest(c, err.Error())
	default:
		if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "must ") || strings.Contains(err.Error(), "cannot ") || strings.Contains(err.Error(), "requires ") || strings.Contains(err.Error(), "unsupported") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := formatTime(*t)
	return &v
}

func (h *RoutingPolicyHandler) audit(c *gin.Context, policyID, revisionID, groupID *int64, action string, details map[string]any) {
	if h == nil || h.control == nil {
		return
	}
	if err := h.control.RecordAudit(c.Request.Context(), &service.RoutingPolicyAuditLog{PolicyID: policyID, RevisionID: revisionID, GroupID: groupID, ActorUserID: actorID(c), Action: action, Details: details}); err != nil {
		slog.Warn("routing_policy_audit_failed", "action", action, "error", err)
	}
}

func policyDTO(p service.RoutingPolicy) routingPolicyDTO {
	return routingPolicyDTO{ID: p.ID, Name: p.Name, Description: p.Description, Status: p.Status, DraftRevisionID: p.DraftRevisionID, PublishedRevisionID: p.PublishedRevisionID, CreatedBy: p.CreatedBy, CreatedAt: formatTime(p.CreatedAt), UpdatedAt: formatTime(p.UpdatedAt)}
}
func revisionDTO(r service.RoutingPolicyRevision) routingPolicyRevisionDTO {
	return routingPolicyRevisionDTO{ID: r.ID, PolicyID: r.PolicyID, Version: r.Version, State: r.State, SchemaVersion: r.SchemaVersion, Config: r.Config, Checksum: r.Checksum, Comment: r.Comment, CreatedBy: r.CreatedBy, CreatedAt: formatTime(r.CreatedAt), PublishedAt: formatTimePtr(r.PublishedAt)}
}
func priceBookDTO(b service.UpstreamPriceBook) upstreamPriceBookDTO {
	return upstreamPriceBookDTO{ID: b.ID, Name: b.Name, Source: b.Source, Status: b.Status, Currency: b.Currency, SourceConfig: publicPriceSourceConfig(b.SourceConfig), LatestRevisionID: b.LatestRevisionID, CreatedBy: b.CreatedBy, CreatedAt: formatTime(b.CreatedAt), UpdatedAt: formatTime(b.UpdatedAt)}
}

func publicPriceSourceConfig(config map[string]any) map[string]any {
	if len(config) == 0 {
		return config
	}
	public := make(map[string]any, len(config))
	for key, value := range config {
		public[key] = value
	}
	switch headers := config["headers"].(type) {
	case map[string]any:
		masked := make(map[string]any, len(headers))
		for key := range headers {
			masked[key] = upstreamHeaderMask
		}
		public["headers"] = masked
	case map[string]string:
		masked := make(map[string]string, len(headers))
		for key := range headers {
			masked[key] = upstreamHeaderMask
		}
		public["headers"] = masked
	}
	return public
}

func protectedPriceSourceConfig(config, previous map[string]any, encryptor service.SecretEncryptor) (map[string]any, error) {
	protected := make(map[string]any, len(config))
	for key, value := range config {
		protected[key] = value
	}
	headers, err := priceSourceHeaders(config["headers"])
	if err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		protected["headers"] = map[string]any{}
		return protected, nil
	}
	if encryptor == nil {
		return nil, errors.New("price source header encryption is not configured")
	}
	previousHeaders, err := priceSourceHeaders(previous["headers"])
	if err != nil {
		return nil, err
	}
	stored := make(map[string]any, len(headers))
	for key, value := range headers {
		if value == upstreamHeaderMask {
			value, ok := previousHeaders[key]
			if !ok {
				return nil, fmt.Errorf("masked price source header %q requires an existing value", key)
			}
			if strings.HasPrefix(value, upstreamEncryptedValuePrefix) {
				stored[key] = value
				continue
			}
		}
		ciphertext, encryptErr := encryptor.Encrypt(value)
		if encryptErr != nil {
			return nil, fmt.Errorf("encrypt price source header %q: %w", key, encryptErr)
		}
		stored[key] = upstreamEncryptedValuePrefix + ciphertext
	}
	protected["headers"] = stored
	return protected, nil
}

func priceSourceHeaders(value any) (map[string]string, error) {
	headers := make(map[string]string)
	switch typed := value.(type) {
	case nil:
		return headers, nil
	case map[string]any:
		for key, value := range typed {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("price source header %q must be a string", key)
			}
			headers[key] = text
		}
	case map[string]string:
		for key, value := range typed {
			headers[key] = value
		}
	default:
		return nil, errors.New("price source headers must be an object")
	}
	return headers, nil
}

func plaintextPriceSourceHeaders(config map[string]any, encryptor service.SecretEncryptor) (map[string]string, error) {
	headers, err := priceSourceHeaders(config["headers"])
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		if !strings.HasPrefix(value, upstreamEncryptedValuePrefix) {
			continue
		}
		if encryptor == nil {
			return nil, errors.New("price source header encryption is not configured")
		}
		plaintext, decryptErr := encryptor.Decrypt(strings.TrimPrefix(value, upstreamEncryptedValuePrefix))
		if decryptErr != nil {
			return nil, fmt.Errorf("decrypt price source header %q: %w", key, decryptErr)
		}
		headers[key] = plaintext
	}
	return headers, nil
}
func priceRevisionDTO(r service.UpstreamPriceBookRevision, prices []service.UpstreamModelPrice) upstreamPriceBookRevisionDTO {
	out := upstreamPriceBookRevisionDTO{ID: r.ID, PriceBookID: r.PriceBookID, Version: r.Version, State: r.State, EffectiveAt: formatTimePtr(r.EffectiveAt), SourceSnapshot: r.SourceSnapshot, Comment: r.Comment, CreatedBy: r.CreatedBy, CreatedAt: formatTime(r.CreatedAt), PublishedAt: formatTimePtr(r.PublishedAt)}
	for _, p := range prices {
		out.Prices = append(out.Prices, upstreamModelPriceDTO{ID: p.ID, RevisionID: p.RevisionID, ModelPattern: p.ModelPattern, InputPricePerMillion: p.InputPricePerMillion.String(), OutputPricePerMillion: p.OutputPricePerMillion.String(), CacheReadPricePerMillion: p.CacheReadPricePerMillion.String(), CacheWritePricePerMillion: p.CacheWritePricePerMillion.String(), RequestPrice: p.RequestPrice.String(), Metadata: p.Metadata, CreatedAt: formatTime(p.CreatedAt)})
	}
	return out
}

func (h *RoutingPolicyHandler) List(c *gin.Context) {
	if h == nil || h.control == nil {
		response.Error(c, http.StatusServiceUnavailable, "routing policy service not available")
		return
	}
	items, err := h.control.ListPolicies(c.Request.Context())
	if err != nil {
		routingError(c, err)
		return
	}
	out := make([]routingPolicyDTO, 0, len(items))
	for _, p := range items {
		out = append(out, policyDTO(p))
	}
	response.Success(c, out)
}

func (h *RoutingPolicyHandler) Get(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	p, err := h.control.GetPolicy(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	response.Success(c, policyDTO(*p))
}

func (h *RoutingPolicyHandler) Create(c *gin.Context) {
	var req routingPolicyCreateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if err := req.Config.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	p := &service.RoutingPolicy{Name: req.Name, Description: req.Description, Status: req.Status, CreatedBy: actorID(c)}
	rev := &service.RoutingPolicyRevision{PolicyID: p.ID, State: service.RoutingPolicyRevisionDraft, SchemaVersion: req.Config.SchemaVersion, Config: req.Config, Comment: req.Comment, CreatedBy: actorID(c)}
	if err := h.control.CreatePolicyWithRevision(c.Request.Context(), p, rev); err != nil {
		routingError(c, err)
		return
	}
	if current, getErr := h.control.GetPolicy(c.Request.Context(), p.ID); getErr == nil {
		p = current
	}
	h.audit(c, &p.ID, &rev.ID, nil, "create", map[string]any{"name": p.Name})
	response.Created(c, gin.H{"policy": policyDTO(*p), "revision": revisionDTO(*rev)})
}

func (h *RoutingPolicyHandler) Update(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req routingPolicyUpdateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	p, err := h.control.GetPolicy(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	if req.Config != nil {
		if err := req.Config.Validate(); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	result := gin.H{}
	if req.Config != nil {
		rev := &service.RoutingPolicyRevision{PolicyID: id, State: service.RoutingPolicyRevisionDraft, SchemaVersion: req.Config.SchemaVersion, Config: *req.Config, Comment: req.Comment, CreatedBy: actorID(c)}
		if err := h.control.UpdatePolicyWithRevision(c.Request.Context(), p, rev); err != nil {
			routingError(c, err)
			return
		}
		if current, getErr := h.control.GetPolicy(c.Request.Context(), id); getErr == nil {
			p = current
		}
		result["revision"] = revisionDTO(*rev)
		h.audit(c, &p.ID, &rev.ID, nil, "draft_update", nil)
	} else if err := h.control.UpdatePolicy(c.Request.Context(), p); err != nil {
		routingError(c, err)
		return
	}
	result["policy"] = policyDTO(*p)
	h.audit(c, &p.ID, nil, nil, "update", nil)
	response.Success(c, result)
}

func (h *RoutingPolicyHandler) Delete(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	if err := h.control.DeletePolicy(c.Request.Context(), id); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, &id, nil, nil, "delete", nil)
	response.Success(c, gin.H{"deleted": true, "id": id})
}

func (h *RoutingPolicyHandler) Validate(c *gin.Context) {
	var req routingPolicyValidateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if err := req.Config.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"valid": true, "schema_version": req.Config.SchemaVersion})
}

func (h *RoutingPolicyHandler) Simulate(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req routingPolicySimulateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	policy, err := h.control.GetPolicy(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	var revision *service.RoutingPolicyRevision
	if req.RevisionID != nil {
		revision, err = h.control.GetRevision(c.Request.Context(), id, *req.RevisionID)
	} else if policy.PublishedRevisionID != nil {
		revision, err = h.control.GetRevision(c.Request.Context(), id, *policy.PublishedRevisionID)
	} else {
		response.BadRequest(c, "policy has no published revision")
		return
	}
	if err != nil {
		routingError(c, err)
		return
	}
	if err := revision.Config.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	estimatedInput, estimatedOutput := 1024, 1024
	if req.EstimatedInputTokens != nil {
		estimatedInput = *req.EstimatedInputTokens
	}
	if req.EstimatedOutputTokens != nil {
		estimatedOutput = *req.EstimatedOutputTokens
	}
	if estimatedInput < 0 || estimatedOutput < 0 {
		response.BadRequest(c, "estimated token counts must be non-negative")
		return
	}
	result := gin.H{"policy": policyDTO(*policy), "revision": revisionDTO(*revision), "group_id": req.GroupID, "model": req.Model}
	if req.GroupID > 0 && h.accountRepo != nil {
		accounts, listErr := h.accountRepo.ListByGroup(c.Request.Context(), req.GroupID)
		if listErr != nil {
			routingError(c, listErr)
			return
		}
		effective := &service.EffectiveRoutingPolicy{Policy: *policy, Revision: *revision, Binding: service.RoutingPolicyBinding{GroupID: req.GroupID, PolicyID: id, RevisionID: &revision.ID, Mode: service.RoutingPolicyModeEnforce}}
		if h.runtime == nil {
			response.Error(c, http.StatusServiceUnavailable, "routing policy runtime not available")
			return
		}
		selection, selectErr := h.runtime.Select(c.Request.Context(), effective, accounts, service.RoutingRequestDescriptor{GroupID: req.GroupID, Model: req.Model, Endpoint: "openai", EstimatedInputToken: estimatedInput, EstimatedOutputToken: estimatedOutput}, nil)
		if selectErr != nil && !errors.Is(selectErr, service.ErrNoAvailableAccounts) {
			routingError(c, selectErr)
			return
		}
		result["selection"] = newRoutingSimulationSelectionDTO(selection, accounts)
	}
	response.Success(c, result)
}

func (h *RoutingPolicyHandler) Publish(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req routingPolicyPublishRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	var rev *service.RoutingPolicyRevision
	var err error
	if req.RevisionID != nil {
		rev, err = h.control.GetRevision(c.Request.Context(), id, *req.RevisionID)
	} else if req.Version != nil {
		rev, err = h.control.GetRevisionByVersion(c.Request.Context(), id, *req.Version)
	} else {
		response.BadRequest(c, "revision_id or version is required")
		return
	}
	if err != nil {
		routingError(c, err)
		return
	}
	if err := rev.Config.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.control.PublishRevision(c.Request.Context(), id, rev.ID); err != nil {
		routingError(c, err)
		return
	}
	if current, getErr := h.control.GetRevision(c.Request.Context(), id, rev.ID); getErr == nil {
		rev = current
	}
	h.audit(c, &id, &rev.ID, nil, "publish", map[string]any{"version": rev.Version})
	response.Success(c, gin.H{"published": true, "revision": revisionDTO(*rev)})
}

func (h *RoutingPolicyHandler) Versions(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	items, err := h.control.ListRevisions(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	out := make([]routingPolicyRevisionDTO, 0, len(items))
	for _, r := range items {
		out = append(out, revisionDTO(r))
	}
	response.Success(c, out)
}

func (h *RoutingPolicyHandler) Restore(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	version, err := strconv.Atoi(strings.TrimSpace(c.Param("version")))
	if err != nil || version <= 0 {
		response.BadRequest(c, "invalid version")
		return
	}
	rev, err := h.control.GetRevisionByVersion(c.Request.Context(), id, version)
	if err != nil {
		routingError(c, err)
		return
	}
	restored, err := h.control.RestoreRevision(c.Request.Context(), id, rev.ID, actorID(c))
	if err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, &id, &restored.ID, nil, "restore", map[string]any{"from_version": version})
	response.Created(c, revisionDTO(*restored))
}

func (h *RoutingPolicyHandler) BindGroup(c *gin.Context) {
	groupID, ok := parsePositiveID(c, "group_id")
	if !ok {
		return
	}
	var req routingPolicyBindingRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	policyID := groupID // overwritten below; keep path parsing independent from body validation
	pathPolicyID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || pathPolicyID <= 0 {
		response.BadRequest(c, "invalid id")
		return
	}
	policyID = pathPolicyID
	if req.PolicyID != nil && *req.PolicyID != policyID {
		response.BadRequest(c, "policy_id does not match path id")
		return
	}
	mode, err := service.NormalizeRoutingPolicyMode(req.Mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	policy, err := h.control.GetPolicy(c.Request.Context(), policyID)
	if err != nil {
		routingError(c, err)
		return
	}
	var revisionID *int64 = req.RevisionID
	if revisionID != nil {
		rev, e := h.control.GetRevision(c.Request.Context(), policyID, *revisionID)
		if e != nil {
			routingError(c, e)
			return
		}
		if !rev.IsPinnable() {
			response.BadRequest(c, "bindings may reference published revisions only")
			return
		}
	} else if policy.PublishedRevisionID == nil {
		response.BadRequest(c, "policy has no published revision")
		return
	}
	binding := &service.RoutingPolicyBinding{GroupID: groupID, PolicyID: policyID, RevisionID: revisionID, Mode: mode, ModelOverrides: req.ModelOverrides, CreatedBy: actorID(c)}
	if err := h.control.BindGroup(c.Request.Context(), binding); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, &binding.PolicyID, binding.RevisionID, &binding.GroupID, "bind", map[string]any{"mode": mode})
	response.Success(c, gin.H{"group_id": groupID, "policy_id": policyID, "revision_id": revisionID, "mode": mode})
}

func (h *RoutingPolicyHandler) UnbindGroup(c *gin.Context) {
	groupID, ok := parsePositiveID(c, "group_id")
	if !ok {
		return
	}
	if err := h.control.UnbindGroup(c.Request.Context(), groupID); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, nil, &groupID, "unbind", nil)
	response.Success(c, gin.H{"unbound": true, "group_id": groupID})
}

func (h *RoutingPolicyHandler) Effective(c *gin.Context) {
	groupID, err := strconv.ParseInt(strings.TrimSpace(c.Query("group_id")), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "group_id must be positive")
		return
	}
	effective, err := h.control.EffectiveForGroup(c.Request.Context(), groupID)
	if err != nil {
		routingError(c, err)
		return
	}
	response.Success(c, effective)
}

func (h *RoutingPolicyHandler) ListPriceBooks(c *gin.Context) {
	items, err := h.control.ListPriceBooks(c.Request.Context())
	if err != nil {
		routingError(c, err)
		return
	}
	out := make([]upstreamPriceBookDTO, 0, len(items))
	for _, b := range items {
		out = append(out, priceBookDTO(b))
	}
	response.Success(c, out)
}
func (h *RoutingPolicyHandler) CreatePriceBook(c *gin.Context) {
	var req priceBookCreateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if req.Source == "" {
		req.Source = service.PriceBookSourceManual
	}
	if req.Source != service.PriceBookSourceManual && req.Source != service.PriceBookSourceHTTPJSON {
		response.BadRequest(c, "source must be manual or http_json")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" && req.Status != "archived" {
		response.BadRequest(c, "status must be active, disabled, or archived")
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if len(req.Currency) != 3 {
		response.BadRequest(c, "currency must be a 3-letter code")
		return
	}
	protectedConfig, err := protectedPriceSourceConfig(req.SourceConfig, nil, h.encryptor)
	if err != nil {
		routingError(c, err)
		return
	}
	b := &service.UpstreamPriceBook{Name: req.Name, Source: req.Source, Status: req.Status, Currency: strings.ToUpper(req.Currency), SourceConfig: protectedConfig, CreatedBy: actorID(c)}
	if err := h.control.CreatePriceBook(c.Request.Context(), b); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, nil, nil, "price_book_create", map[string]any{"price_book_id": b.ID, "name": b.Name})
	response.Created(c, priceBookDTO(*b))
}

func (h *RoutingPolicyHandler) UpdatePriceBook(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req priceBookUpdateRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	b, err := h.control.GetPriceBook(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	if req.Name != nil {
		b.Name = strings.TrimSpace(*req.Name)
	}
	if b.Name == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if req.Source != nil {
		b.Source = *req.Source
	}
	if b.Source != service.PriceBookSourceManual && b.Source != service.PriceBookSourceHTTPJSON {
		response.BadRequest(c, "source must be manual or http_json")
		return
	}
	if req.Status != nil {
		b.Status = *req.Status
	}
	if b.Status != "active" && b.Status != "disabled" && b.Status != "archived" {
		response.BadRequest(c, "status must be active, disabled, or archived")
		return
	}
	if req.Currency != nil {
		b.Currency = strings.ToUpper(strings.TrimSpace(*req.Currency))
	}
	if len(b.Currency) != 3 {
		response.BadRequest(c, "currency must be a 3-letter code")
		return
	}
	if req.SourceConfig != nil {
		protectedConfig, protectErr := protectedPriceSourceConfig(*req.SourceConfig, b.SourceConfig, h.encryptor)
		if protectErr != nil {
			routingError(c, protectErr)
			return
		}
		b.SourceConfig = protectedConfig
	}
	if err := h.control.UpdatePriceBook(c.Request.Context(), b); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, nil, nil, "price_book_update", map[string]any{"price_book_id": b.ID})
	response.Success(c, priceBookDTO(*b))
}

func (h *RoutingPolicyHandler) DeletePriceBook(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	if err := h.control.DeletePriceBook(c.Request.Context(), id); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, nil, nil, "price_book_delete", map[string]any{"price_book_id": id})
	response.Success(c, gin.H{"deleted": true, "id": id})
}

func (h *RoutingPolicyHandler) SyncPriceBook(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	book, err := h.control.GetPriceBook(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	if book.Source != service.PriceBookSourceHTTPJSON {
		response.BadRequest(c, "price book source must be http_json")
		return
	}
	urlValue, _ := book.SourceConfig["url"].(string)
	normalizedURL, err := h.validatePriceSourceURL(urlValue)
	if err != nil {
		response.BadRequest(c, "invalid source_config.url: "+err.Error())
		return
	}
	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		response.BadRequest(c, "invalid source_config.url")
		return
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		response.BadRequest(c, "invalid source URL")
		return
	}
	headers, err := plaintextPriceSourceHeaders(book.SourceConfig, h.encryptor)
	if err != nil {
		routingError(c, err)
		return
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" {
			request.Header.Set(key, value)
		}
	}
	client, err := h.priceSourceHTTPClient()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "create price source client failed")
		return
	}
	resp, err := client.Do(request)
	if err != nil {
		response.Error(c, http.StatusBadGateway, "price source request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		response.Error(c, http.StatusBadGateway, "price source returned status "+resp.Status)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		response.Error(c, http.StatusBadGateway, "read price source response failed: "+err.Error())
		return
	}
	var envelope struct {
		Prices []upstreamModelPriceDTO `json:"prices"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		response.BadRequest(c, "price source response must be JSON object with prices")
		return
	}
	prices := make([]service.UpstreamModelPrice, 0, len(envelope.Prices))
	for _, p := range envelope.Prices {
		input, e1 := decimalFromString(p.InputPricePerMillion)
		output, e2 := decimalFromString(p.OutputPricePerMillion)
		requestPrice, e3 := decimalFromString(p.RequestPrice)
		if strings.TrimSpace(p.ModelPattern) == "" || e1 != nil || e2 != nil || e3 != nil || input.IsNegative() || output.IsNegative() || requestPrice.IsNegative() {
			response.BadRequest(c, "price source contains invalid price row")
			return
		}
		prices = append(prices, service.UpstreamModelPrice{ModelPattern: p.ModelPattern, InputPricePerMillion: input, OutputPricePerMillion: output, RequestPrice: requestPrice, Metadata: p.Metadata})
	}
	revision := &service.UpstreamPriceBookRevision{PriceBookID: id, State: service.RoutingPolicyRevisionDraft, SourceSnapshot: map[string]any{"url": parsedURL.String(), "fetched_at": time.Now().UTC().Format(time.RFC3339)}}
	if err := h.control.CreatePriceBookRevision(c.Request.Context(), revision, prices); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, &revision.ID, nil, "price_book_sync", map[string]any{"price_book_id": id, "source": parsedURL.String()})
	response.Created(c, priceRevisionDTO(*revision, prices))
}

func (h *RoutingPolicyHandler) validatePriceSourceURL(raw string) (string, error) {
	if h.cfg == nil {
		return "", errors.New("price source security config is unavailable")
	}
	allowlist := h.cfg.Security.URLAllowlist
	if !allowlist.Enabled {
		return urlvalidator.ValidateURLFormat(raw, allowlist.AllowInsecureHTTP)
	}
	return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     allowlist.PricingHosts,
		RequireAllowlist: true,
		AllowPrivate:     allowlist.AllowPrivateHosts,
	})
}

func (h *RoutingPolicyHandler) priceSourceHTTPClient() (*http.Client, error) {
	if h.cfg == nil {
		return nil, errors.New("price source security config is unavailable")
	}
	allowlist := h.cfg.Security.URLAllowlist
	baseClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:               15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ValidateResolvedIP:    allowlist.Enabled,
		AllowPrivateHosts:     allowlist.AllowPrivateHosts,
	})
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: baseClient.Transport,
		Timeout:   baseClient.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func (h *RoutingPolicyHandler) PriceBook(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	b, err := h.control.GetPriceBook(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	response.Success(c, priceBookDTO(*b))
}
func (h *RoutingPolicyHandler) PriceBookRevisions(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	revs, err := h.control.ListPriceBookRevisions(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	out := make([]upstreamPriceBookRevisionDTO, 0, len(revs))
	for _, rev := range revs {
		prices, e := h.control.ListPriceBookPrices(c.Request.Context(), rev.ID)
		if e != nil {
			routingError(c, e)
			return
		}
		out = append(out, priceRevisionDTO(rev, prices))
	}
	response.Success(c, out)
}
func (h *RoutingPolicyHandler) CreatePriceBookRevision(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req priceBookRevisionRequest
	if err := decodeStrict(c, &req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if req.Version < 0 {
		response.BadRequest(c, "version must be non-negative")
		return
	}
	if req.State == "" {
		req.State = service.RoutingPolicyRevisionDraft
	}
	if req.State != service.RoutingPolicyRevisionDraft {
		response.BadRequest(c, "new price revisions must start as draft")
		return
	}
	rev := &service.UpstreamPriceBookRevision{PriceBookID: id, Version: req.Version, State: req.State, SourceSnapshot: req.SourceSnapshot, Comment: req.Comment, CreatedBy: actorID(c)}
	if req.EffectiveAt != nil && strings.TrimSpace(*req.EffectiveAt) != "" {
		t, err := time.Parse(time.RFC3339, *req.EffectiveAt)
		if err != nil {
			response.BadRequest(c, "effective_at must be RFC3339")
			return
		}
		rev.EffectiveAt = &t
	}
	prices := make([]service.UpstreamModelPrice, 0, len(req.Prices))
	for _, p := range req.Prices {
		if strings.TrimSpace(p.ModelPattern) == "" {
			response.BadRequest(c, "model_pattern is required")
			return
		}
		input, err := decimalFromString(p.InputPricePerMillion)
		if err != nil {
			response.BadRequest(c, "invalid input_price_per_million")
			return
		}
		output, err := decimalFromString(p.OutputPricePerMillion)
		if err != nil {
			response.BadRequest(c, "invalid output_price_per_million")
			return
		}
		cacheRead, err := decimalFromString(p.CacheReadPricePerMillion)
		if err != nil {
			response.BadRequest(c, "invalid cache_read_price_per_million")
			return
		}
		cacheWrite, err := decimalFromString(p.CacheWritePricePerMillion)
		if err != nil {
			response.BadRequest(c, "invalid cache_write_price_per_million")
			return
		}
		requestPrice, err := decimalFromString(p.RequestPrice)
		if err != nil {
			response.BadRequest(c, "invalid request_price")
			return
		}
		if input.IsNegative() || output.IsNegative() || cacheRead.IsNegative() || cacheWrite.IsNegative() || requestPrice.IsNegative() {
			response.BadRequest(c, "price values must be non-negative")
			return
		}
		prices = append(prices, service.UpstreamModelPrice{ModelPattern: p.ModelPattern, InputPricePerMillion: input, OutputPricePerMillion: output, CacheReadPricePerMillion: cacheRead, CacheWritePricePerMillion: cacheWrite, RequestPrice: requestPrice, Metadata: p.Metadata})
	}
	if err := h.control.CreatePriceBookRevision(c.Request.Context(), rev, prices); err != nil {
		routingError(c, err)
		return
	}
	h.audit(c, nil, &rev.ID, nil, "price_book_revision_create", map[string]any{"price_book_id": id, "version": rev.Version})
	response.Created(c, priceRevisionDTO(*rev, prices))
}
func (h *RoutingPolicyHandler) PublishPriceBookRevision(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	version, err := strconv.Atoi(strings.TrimSpace(c.Param("version")))
	if err != nil || version <= 0 {
		response.BadRequest(c, "invalid version")
		return
	}
	revs, err := h.control.ListPriceBookRevisions(c.Request.Context(), id)
	if err != nil {
		routingError(c, err)
		return
	}
	var target *service.UpstreamPriceBookRevision
	for i := range revs {
		if revs[i].Version == version {
			target = &revs[i]
			break
		}
	}
	if target == nil {
		response.NotFound(c, "price book revision not found")
		return
	}
	if err := h.control.PublishPriceBookRevision(c.Request.Context(), id, target.ID); err != nil {
		routingError(c, err)
		return
	}
	if refreshed, listErr := h.control.ListPriceBookRevisions(c.Request.Context(), id); listErr == nil {
		for i := range refreshed {
			if refreshed[i].ID == target.ID {
				target = &refreshed[i]
				break
			}
		}
	}
	prices, _ := h.control.ListPriceBookPrices(c.Request.Context(), target.ID)
	h.audit(c, nil, &target.ID, nil, "price_book_revision_publish", map[string]any{"price_book_id": id, "version": version})
	response.Success(c, gin.H{"published": true, "revision": priceRevisionDTO(*target, prices)})
}

func decimalFromString(v string) (decimal.Decimal, error) {
	if strings.TrimSpace(v) == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(strings.TrimSpace(v))
}
