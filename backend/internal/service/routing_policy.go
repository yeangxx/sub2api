package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	RoutingPolicyModeShadow  = "shadow"
	RoutingPolicyModeEnforce = "enforce"

	RoutingPolicyRevisionDraft     = "draft"
	RoutingPolicyRevisionPublished = "published"
	RoutingPolicyRevisionArchived  = "archived"

	PriceBookSourceManual   = "manual"
	PriceBookSourceHTTPJSON = "http_json"
)

var (
	ErrRoutingPolicyNotFound             = errors.New("routing policy not found")
	ErrRoutingPolicyDisabled             = errors.New("routing policy is disabled")
	ErrRoutingPolicyRevisionNotFound     = errors.New("routing policy revision not found")
	ErrRoutingPolicyRevisionNotPublished = errors.New("routing policy revision is not published")
	ErrRoutingPolicyBindingNotFound      = errors.New("routing policy binding not found")
	ErrUpstreamPriceBookNotFound         = errors.New("upstream price book not found")
	ErrUpstreamPriceBookRevisionNotFound = errors.New("upstream price book revision not found")
)

// RoutingPolicy is the stable identity of a policy. Configuration lives in an
// immutable revision so an in-flight request can safely retain its snapshot.
type RoutingPolicy struct {
	ID                  int64
	Name                string
	Description         string
	Status              string
	DraftRevisionID     *int64
	PublishedRevisionID *int64
	CreatedBy           *int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type RoutingPolicyRevision struct {
	ID            int64
	PolicyID      int64
	Version       int
	State         string
	SchemaVersion int
	Config        RoutingPolicyConfig
	Checksum      string
	Comment       string
	CreatedBy     *int64
	CreatedAt     time.Time
	PublishedAt   *time.Time
}

// IsPinnable reports whether a revision was published at least once. Publishing
// a newer revision archives the old one, but existing and new pinned bindings
// must continue to resolve that immutable snapshot.
func (r RoutingPolicyRevision) IsPinnable() bool {
	return r.PublishedAt != nil && (r.State == RoutingPolicyRevisionPublished || r.State == RoutingPolicyRevisionArchived)
}

// RoutingPolicyConfig is intentionally typed rather than an expression DSL.
// It is persisted as JSONB and validated before a revision can be published.
type RoutingPolicyConfig struct {
	SchemaVersion    int                         `json:"schema_version"`
	CandidateFilters RoutingCandidateFilters     `json:"candidate_filters"`
	Scoring          RoutingScoringWeights       `json:"scoring"`
	Timeouts         RoutingTimeoutPolicy        `json:"timeouts"`
	Retry            RoutingRetryPolicy          `json:"retry"`
	Hedge            RoutingHedgePolicy          `json:"hedge"`
	CircuitBreaker   RoutingCircuitBreakerPolicy `json:"circuit_breaker"`
	Fallback         RoutingFallbackPolicy       `json:"fallback"`
	ModelMappings    map[string]string           `json:"model_mappings,omitempty"`
	CostBudget       RoutingCostBudget           `json:"cost_budget"`
}

type RoutingCandidateFilters struct {
	Platforms              []string          `json:"platforms,omitempty"`
	RequiredLabels         map[string]string `json:"required_labels,omitempty"`
	ExcludedLabels         map[string]string `json:"excluded_labels,omitempty"`
	ReliabilityClasses     []string          `json:"reliability_classes,omitempty"`
	RequireKnownPrice      bool              `json:"require_known_price"`
	RequireTrustedUpstream bool              `json:"require_trusted_upstream"`
}

type RoutingScoringWeights struct {
	Price       float64 `json:"price"`
	ErrorRate   float64 `json:"error_rate"`
	TTFT        float64 `json:"ttft"`
	Load        float64 `json:"load"`
	Queue       float64 `json:"queue"`
	Reliability float64 `json:"reliability"`
}

type RoutingTimeoutPolicy struct {
	RequestTimeoutMillis int     `json:"request_timeout_ms"`
	SoftTTFTMillis       int     `json:"soft_ttft_ms"`
	AdaptiveSoftTTFT     bool    `json:"adaptive_soft_ttft"`
	SoftTTFTFactor       float64 `json:"soft_ttft_factor"`
	SoftTTFTMinMillis    int     `json:"soft_ttft_min_ms"`
	SoftTTFTMaxMillis    int     `json:"soft_ttft_max_ms"`
	StreamIdleMillis     int     `json:"stream_idle_ms"`
}

type RoutingRetryPolicy struct {
	MaxAttempts          int   `json:"max_attempts"`
	RetryableStatusCodes []int `json:"retryable_status_codes,omitempty"`
	RetryTransportErrors bool  `json:"retry_transport_errors"`
	MaxSwitches          int   `json:"max_switches"`
}

type RoutingHedgePolicy struct {
	Enabled                 bool `json:"enabled"`
	DelayMillis             int  `json:"delay_ms"`
	MaxConcurrent           int  `json:"max_concurrent"`
	RequireDifferentDomain  bool `json:"require_different_failure_domain"`
	RequireNoSemanticOutput bool `json:"require_no_semantic_output"`
}

type RoutingCircuitBreakerPolicy struct {
	ConsecutiveFailures int `json:"consecutive_failures"`
	MinSamples          int `json:"min_samples"`
	ErrorRatePercent    int `json:"error_rate_percent"`
	CooldownMillis      int `json:"cooldown_ms"`
	MaxCooldownMillis   int `json:"max_cooldown_ms"`
	HalfOpenMaxRequests int `json:"half_open_max_requests"`
}

type RoutingFallbackPolicy struct {
	GroupIDs                []int64 `json:"group_ids,omitempty"`
	AllowCrossTier          bool    `json:"allow_cross_tier"`
	MaxCostMultiplier       float64 `json:"max_cost_multiplier"`
	RequireExplicitModelMap bool    `json:"require_explicit_model_map"`
}

type RoutingCostBudget struct {
	MaxUpstreamUSD     decimal.Decimal `json:"max_upstream_usd"`
	MaxAttemptCostUSD  decimal.Decimal `json:"max_attempt_cost_usd"`
	ReserveForHedgeUSD decimal.Decimal `json:"reserve_for_hedge_usd"`
}

func (c RoutingPolicyConfig) MarshalJSON() ([]byte, error) {
	type alias RoutingPolicyConfig
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}
	return json.Marshal(alias(c))
}

// Validate rejects configurations that could make routing unsafe or
// impossible. Unknown JSON fields are rejected by API decoding; this method
// protects callers that construct the typed value directly.
func (c RoutingPolicyConfig) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("routing policy schema_version 1 is required")
	}
	weights := []float64{c.Scoring.Price, c.Scoring.ErrorRate, c.Scoring.TTFT, c.Scoring.Load, c.Scoring.Queue, c.Scoring.Reliability}
	weightTotal := 0.0
	for _, weight := range weights {
		if weight < 0 {
			return errors.New("routing policy scoring weights must be non-negative")
		}
		weightTotal += weight
	}
	if weightTotal == 0 {
		return errors.New("routing policy must define at least one scoring weight")
	}
	if c.Timeouts.RequestTimeoutMillis < 0 || c.Timeouts.SoftTTFTMillis < 0 || c.Timeouts.SoftTTFTMinMillis < 0 || c.Timeouts.SoftTTFTMaxMillis < 0 || c.Timeouts.StreamIdleMillis < 0 {
		return errors.New("routing policy timeouts must be non-negative")
	}
	if c.Timeouts.SoftTTFTMinMillis > 0 && c.Timeouts.SoftTTFTMaxMillis > 0 && c.Timeouts.SoftTTFTMinMillis > c.Timeouts.SoftTTFTMaxMillis {
		return errors.New("routing policy soft TTFT minimum cannot exceed maximum")
	}
	if c.Timeouts.AdaptiveSoftTTFT && c.Timeouts.SoftTTFTFactor <= 0 {
		return errors.New("adaptive soft TTFT requires a positive factor")
	}
	if c.Retry.MaxAttempts < 0 || c.Retry.MaxSwitches < 0 {
		return errors.New("routing policy retry limits must be non-negative")
	}
	if c.Hedge.Enabled {
		if c.Hedge.DelayMillis <= 0 {
			return errors.New("enabled hedge requires delay_ms")
		}
		if c.Hedge.MaxConcurrent < 2 {
			return errors.New("enabled hedge requires max_concurrent >= 2")
		}
	}
	if c.CircuitBreaker.ErrorRatePercent < 0 || c.CircuitBreaker.ErrorRatePercent > 100 {
		return errors.New("circuit breaker error_rate_percent must be between 0 and 100")
	}
	if c.CircuitBreaker.MinSamples < 0 || c.CircuitBreaker.ConsecutiveFailures < 0 || c.CircuitBreaker.HalfOpenMaxRequests < 0 {
		return errors.New("circuit breaker limits must be non-negative")
	}
	if c.Fallback.MaxCostMultiplier < 0 {
		return errors.New("fallback max_cost_multiplier must be non-negative")
	}
	if c.CostBudget.MaxUpstreamUSD.IsNegative() || c.CostBudget.MaxAttemptCostUSD.IsNegative() || c.CostBudget.ReserveForHedgeUSD.IsNegative() {
		return errors.New("routing policy cost budget must be non-negative")
	}
	return nil
}

type RoutingPolicyBinding struct {
	GroupID        int64
	PolicyID       int64
	RevisionID     *int64
	Mode           string
	ModelOverrides map[string]any
	CreatedBy      *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// EffectiveRoutingPolicy is returned by the control plane for a request. The
// revision is immutable and should be retained for the entire request.
type EffectiveRoutingPolicy struct {
	Policy   RoutingPolicy
	Revision RoutingPolicyRevision
	Binding  RoutingPolicyBinding
}

type RoutingPolicyAuditLog struct {
	ID          int64
	PolicyID    *int64
	RevisionID  *int64
	GroupID     *int64
	ActorUserID *int64
	Action      string
	Details     map[string]any
	CreatedAt   time.Time
}

// RoutingPolicyRepository is deliberately small: runtime only needs an
// effective snapshot, while admin control-plane code can use the mutation
// methods to build revisions and bindings.
type RoutingPolicyRepository interface {
	GetPolicy(ctx context.Context, id int64) (*RoutingPolicy, error)
	GetPublishedRevision(ctx context.Context, policyID int64) (*RoutingPolicyRevision, error)
	GetEffectiveForGroup(ctx context.Context, groupID int64) (*EffectiveRoutingPolicy, error)
	CreatePolicy(ctx context.Context, policy *RoutingPolicy) error
	CreateRevision(ctx context.Context, revision *RoutingPolicyRevision) error
	PublishRevision(ctx context.Context, policyID, revisionID int64) error
	BindGroup(ctx context.Context, binding *RoutingPolicyBinding) error
	UnbindGroup(ctx context.Context, groupID int64) error
	RecordAudit(ctx context.Context, log *RoutingPolicyAuditLog) error
}

// RoutingPolicyAdminRepository is the optional control-plane extension used
// by the admin API. Keeping it separate from RoutingPolicyRepository avoids
// forcing runtime-only implementations and test fakes to implement mutation
// and listing methods.
type RoutingPolicyAdminRepository interface {
	RoutingPolicyRepository
	ListPolicies(ctx context.Context) ([]RoutingPolicy, error)
	UpdatePolicy(ctx context.Context, policy *RoutingPolicy) error
	DeletePolicy(ctx context.Context, id int64) error
	ListRevisions(ctx context.Context, policyID int64) ([]RoutingPolicyRevision, error)
	GetRevision(ctx context.Context, policyID, revisionID int64) (*RoutingPolicyRevision, error)
	GetRevisionByVersion(ctx context.Context, policyID int64, version int) (*RoutingPolicyRevision, error)
	RestoreRevision(ctx context.Context, policyID, revisionID int64, createdBy *int64) (*RoutingPolicyRevision, error)
}

// RoutingPolicyAtomicAdminRepository is implemented by the SQL repository so
// policy identity and its first/draft revision are committed together. The
// optional interface keeps lightweight test and runtime repositories compatible.
type RoutingPolicyAtomicAdminRepository interface {
	RoutingPolicyAdminRepository
	CreatePolicyWithRevision(ctx context.Context, policy *RoutingPolicy, revision *RoutingPolicyRevision) error
	UpdatePolicyWithRevision(ctx context.Context, policy *RoutingPolicy, revision *RoutingPolicyRevision) error
}

type UpstreamPriceBook struct {
	ID               int64
	Name             string
	Source           string
	Status           string
	Currency         string
	SourceConfig     map[string]any
	LatestRevisionID *int64
	CreatedBy        *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UpstreamPriceBookRevision struct {
	ID             int64
	PriceBookID    int64
	Version        int
	State          string
	EffectiveAt    *time.Time
	SourceSnapshot map[string]any
	Comment        string
	CreatedBy      *int64
	CreatedAt      time.Time
	PublishedAt    *time.Time
}

type UpstreamModelPrice struct {
	ID                        int64
	RevisionID                int64
	ModelPattern              string
	InputPricePerMillion      decimal.Decimal
	OutputPricePerMillion     decimal.Decimal
	CacheReadPricePerMillion  decimal.Decimal
	CacheWritePricePerMillion decimal.Decimal
	RequestPrice              decimal.Decimal
	Metadata                  map[string]any
	CreatedAt                 time.Time
}

type UpstreamPriceBookRepository interface {
	GetBook(ctx context.Context, id int64) (*UpstreamPriceBook, error)
	GetPublishedRevision(ctx context.Context, bookID int64) (*UpstreamPriceBookRevision, error)
	ListModelPrices(ctx context.Context, revisionID int64) ([]UpstreamModelPrice, error)
	CreateBook(ctx context.Context, book *UpstreamPriceBook) error
	CreateRevision(ctx context.Context, revision *UpstreamPriceBookRevision, prices []UpstreamModelPrice) error
	PublishRevision(ctx context.Context, bookID, revisionID int64) error
}

// UpstreamPriceBookAdminRepository is the optional control-plane extension
// used by the price-book management endpoints.
type UpstreamPriceBookAdminRepository interface {
	UpstreamPriceBookRepository
	ListBooks(ctx context.Context) ([]UpstreamPriceBook, error)
	ListRevisions(ctx context.Context, bookID int64) ([]UpstreamPriceBookRevision, error)
	UpdateBook(ctx context.Context, book *UpstreamPriceBook) error
	DeleteBook(ctx context.Context, id int64) error
}

// RoutingPolicyControlService is the small control-plane facade injected into
// the application. Runtime scheduling can depend on the repository directly,
// while admin handlers can share this facade for validation and snapshot
// resolution.
type RoutingPolicyControlService struct {
	policies   RoutingPolicyRepository
	priceBooks UpstreamPriceBookRepository
}

func NewRoutingPolicyControlService(policies RoutingPolicyRepository, priceBooks UpstreamPriceBookRepository) *RoutingPolicyControlService {
	return &RoutingPolicyControlService{policies: policies, priceBooks: priceBooks}
}

func (s *RoutingPolicyControlService) EffectiveForGroup(ctx context.Context, groupID int64) (*EffectiveRoutingPolicy, error) {
	if s == nil || s.policies == nil {
		return nil, errors.New("routing policy repository is not configured")
	}
	return s.policies.GetEffectiveForGroup(ctx, groupID)
}

func (s *RoutingPolicyControlService) policyAdmin() (RoutingPolicyAdminRepository, error) {
	if s == nil || s.policies == nil {
		return nil, errors.New("routing policy repository is not configured")
	}
	repo, ok := s.policies.(RoutingPolicyAdminRepository)
	if !ok {
		return nil, errors.New("routing policy admin repository is not configured")
	}
	return repo, nil
}

func (s *RoutingPolicyControlService) priceBookAdmin() (UpstreamPriceBookAdminRepository, error) {
	if s == nil || s.priceBooks == nil {
		return nil, errors.New("upstream price book repository is not configured")
	}
	repo, ok := s.priceBooks.(UpstreamPriceBookAdminRepository)
	if !ok {
		return nil, errors.New("upstream price book admin repository is not configured")
	}
	return repo, nil
}

func (s *RoutingPolicyControlService) ListPolicies(ctx context.Context) ([]RoutingPolicy, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.ListPolicies(ctx)
}
func (s *RoutingPolicyControlService) GetPolicy(ctx context.Context, id int64) (*RoutingPolicy, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.GetPolicy(ctx, id)
}
func (s *RoutingPolicyControlService) CreatePolicy(ctx context.Context, policy *RoutingPolicy) error {
	repo, err := s.policyAdmin()
	if err != nil {
		return err
	}
	return repo.CreatePolicy(ctx, policy)
}
func (s *RoutingPolicyControlService) CreatePolicyWithRevision(ctx context.Context, policy *RoutingPolicy, revision *RoutingPolicyRevision) error {
	if policy == nil || revision == nil {
		return errors.New("routing policy and revision cannot be nil")
	}
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	if repo, ok := s.policies.(RoutingPolicyAtomicAdminRepository); ok {
		return repo.CreatePolicyWithRevision(ctx, policy, revision)
	}
	if err := s.CreatePolicy(ctx, policy); err != nil {
		return err
	}
	revision.PolicyID = policy.ID
	if err := s.CreateRevision(ctx, revision); err != nil {
		_ = s.DeletePolicy(ctx, policy.ID)
		return err
	}
	return nil
}
func (s *RoutingPolicyControlService) UpdatePolicy(ctx context.Context, policy *RoutingPolicy) error {
	repo, err := s.policyAdmin()
	if err != nil {
		return err
	}
	return repo.UpdatePolicy(ctx, policy)
}
func (s *RoutingPolicyControlService) UpdatePolicyWithRevision(ctx context.Context, policy *RoutingPolicy, revision *RoutingPolicyRevision) error {
	if policy == nil || revision == nil {
		return errors.New("routing policy and revision cannot be nil")
	}
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	if repo, ok := s.policies.(RoutingPolicyAtomicAdminRepository); ok {
		return repo.UpdatePolicyWithRevision(ctx, policy, revision)
	}
	if err := s.UpdatePolicy(ctx, policy); err != nil {
		return err
	}
	revision.PolicyID = policy.ID
	return s.CreateRevision(ctx, revision)
}
func (s *RoutingPolicyControlService) DeletePolicy(ctx context.Context, id int64) error {
	repo, err := s.policyAdmin()
	if err != nil {
		return err
	}
	return repo.DeletePolicy(ctx, id)
}
func (s *RoutingPolicyControlService) CreateRevision(ctx context.Context, revision *RoutingPolicyRevision) error {
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	return s.policies.CreateRevision(ctx, revision)
}
func (s *RoutingPolicyControlService) GetRevision(ctx context.Context, policyID, revisionID int64) (*RoutingPolicyRevision, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.GetRevision(ctx, policyID, revisionID)
}
func (s *RoutingPolicyControlService) GetRevisionByVersion(ctx context.Context, policyID int64, version int) (*RoutingPolicyRevision, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.GetRevisionByVersion(ctx, policyID, version)
}
func (s *RoutingPolicyControlService) ListRevisions(ctx context.Context, policyID int64) ([]RoutingPolicyRevision, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.ListRevisions(ctx, policyID)
}
func (s *RoutingPolicyControlService) PublishRevision(ctx context.Context, policyID, revisionID int64) error {
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	return s.policies.PublishRevision(ctx, policyID, revisionID)
}
func (s *RoutingPolicyControlService) RestoreRevision(ctx context.Context, policyID, revisionID int64, createdBy *int64) (*RoutingPolicyRevision, error) {
	repo, err := s.policyAdmin()
	if err != nil {
		return nil, err
	}
	return repo.RestoreRevision(ctx, policyID, revisionID, createdBy)
}
func (s *RoutingPolicyControlService) BindGroup(ctx context.Context, binding *RoutingPolicyBinding) error {
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	return s.policies.BindGroup(ctx, binding)
}

func (s *RoutingPolicyControlService) RecordAudit(ctx context.Context, entry *RoutingPolicyAuditLog) error {
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	return s.policies.RecordAudit(ctx, entry)
}
func (s *RoutingPolicyControlService) UnbindGroup(ctx context.Context, groupID int64) error {
	if s == nil || s.policies == nil {
		return errors.New("routing policy repository is not configured")
	}
	return s.policies.UnbindGroup(ctx, groupID)
}
func (s *RoutingPolicyControlService) ListPriceBooks(ctx context.Context) ([]UpstreamPriceBook, error) {
	repo, err := s.priceBookAdmin()
	if err != nil {
		return nil, err
	}
	return repo.ListBooks(ctx)
}
func (s *RoutingPolicyControlService) GetPriceBook(ctx context.Context, id int64) (*UpstreamPriceBook, error) {
	if s == nil || s.priceBooks == nil {
		return nil, errors.New("upstream price book repository is not configured")
	}
	return s.priceBooks.GetBook(ctx, id)
}
func (s *RoutingPolicyControlService) CreatePriceBook(ctx context.Context, book *UpstreamPriceBook) error {
	if s == nil || s.priceBooks == nil {
		return errors.New("upstream price book repository is not configured")
	}
	return s.priceBooks.CreateBook(ctx, book)
}
func (s *RoutingPolicyControlService) UpdatePriceBook(ctx context.Context, book *UpstreamPriceBook) error {
	repo, err := s.priceBookAdmin()
	if err != nil {
		return err
	}
	return repo.UpdateBook(ctx, book)
}
func (s *RoutingPolicyControlService) DeletePriceBook(ctx context.Context, id int64) error {
	repo, err := s.priceBookAdmin()
	if err != nil {
		return err
	}
	return repo.DeleteBook(ctx, id)
}
func (s *RoutingPolicyControlService) CreatePriceBookRevision(ctx context.Context, revision *UpstreamPriceBookRevision, prices []UpstreamModelPrice) error {
	if s == nil || s.priceBooks == nil {
		return errors.New("upstream price book repository is not configured")
	}
	return s.priceBooks.CreateRevision(ctx, revision, prices)
}
func (s *RoutingPolicyControlService) ListPriceBookRevisions(ctx context.Context, bookID int64) ([]UpstreamPriceBookRevision, error) {
	repo, err := s.priceBookAdmin()
	if err != nil {
		return nil, err
	}
	return repo.ListRevisions(ctx, bookID)
}
func (s *RoutingPolicyControlService) ListPriceBookPrices(ctx context.Context, revisionID int64) ([]UpstreamModelPrice, error) {
	if s == nil || s.priceBooks == nil {
		return nil, errors.New("upstream price book repository is not configured")
	}
	return s.priceBooks.ListModelPrices(ctx, revisionID)
}
func (s *RoutingPolicyControlService) PublishPriceBookRevision(ctx context.Context, bookID, revisionID int64) error {
	if s == nil || s.priceBooks == nil {
		return errors.New("upstream price book repository is not configured")
	}
	return s.priceBooks.PublishRevision(ctx, bookID, revisionID)
}

func ValidateRoutingPolicyConfig(config RoutingPolicyConfig) error {
	return config.Validate()
}

func normalizeRoutingPolicyMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RoutingPolicyModeShadow:
		return RoutingPolicyModeShadow, nil
	case RoutingPolicyModeEnforce, "":
		return RoutingPolicyModeEnforce, nil
	default:
		return "", fmt.Errorf("unsupported routing policy binding mode %q", mode)
	}
}

// NormalizeRoutingPolicyMode validates and normalizes a binding mode for
// repositories and admin handlers.
func NormalizeRoutingPolicyMode(mode string) (string, error) {
	return normalizeRoutingPolicyMode(mode)
}
