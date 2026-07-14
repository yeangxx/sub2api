package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routingPolicyAdminRepoStub struct {
	service.RoutingPolicyAdminRepository
	policy   *service.RoutingPolicy
	revision *service.RoutingPolicyRevision
	bound    *service.RoutingPolicyBinding
}

func testPriceSourceConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		AllowInsecureHTTP: true,
		AllowPrivateHosts: true,
	}}}
}

type routingAccountRepoStub struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *routingAccountRepoStub) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return r.accounts, nil
}

type routingPriceResolverStub map[int64]service.RoutingPriceQuote

func (r routingPriceResolverStub) Quote(_ context.Context, account *service.Account, _ string) (service.RoutingPriceQuote, bool, error) {
	quote, ok := r[account.ID]
	return quote, ok, nil
}

type routingPriceBookRepoStub struct {
	service.UpstreamPriceBookAdminRepository
	book     *service.UpstreamPriceBook
	created  *service.UpstreamPriceBook
	updated  *service.UpstreamPriceBook
	revision *service.UpstreamPriceBookRevision
}

func (r *routingPriceBookRepoStub) GetBook(context.Context, int64) (*service.UpstreamPriceBook, error) {
	return r.book, nil
}

func (r *routingPriceBookRepoStub) CreateBook(_ context.Context, book *service.UpstreamPriceBook) error {
	r.created = book
	book.ID = 1
	return nil
}

func (r *routingPriceBookRepoStub) UpdateBook(_ context.Context, book *service.UpstreamPriceBook) error {
	r.updated = book
	return nil
}

func (r *routingPriceBookRepoStub) CreateRevision(_ context.Context, revision *service.UpstreamPriceBookRevision, _ []service.UpstreamModelPrice) error {
	r.revision = revision
	revision.ID = 1
	return nil
}

type routingSecretEncryptorStub struct{}

func (routingSecretEncryptorStub) Encrypt(value string) (string, error) {
	return "sealed:" + value, nil
}
func (routingSecretEncryptorStub) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "sealed:"), nil
}

func (r *routingPolicyAdminRepoStub) GetPolicy(context.Context, int64) (*service.RoutingPolicy, error) {
	return r.policy, nil
}

func (r *routingPolicyAdminRepoStub) GetRevision(context.Context, int64, int64) (*service.RoutingPolicyRevision, error) {
	return r.revision, nil
}

func (r *routingPolicyAdminRepoStub) BindGroup(_ context.Context, binding *service.RoutingPolicyBinding) error {
	r.bound = binding
	return nil
}

func (r *routingPolicyAdminRepoStub) RecordAudit(context.Context, *service.RoutingPolicyAuditLog) error {
	return nil
}

func TestRoutingSimulationDTOOmitsAccountSecrets(t *testing.T) {
	accounts := []service.Account{{
		ID:               7,
		Name:             "upstream-a",
		Platform:         service.PlatformOpenAI,
		FailureDomain:    "provider-a",
		ReliabilityClass: "trusted",
		Credentials:      map[string]any{"api_key": "secret-value"},
	}}
	selection := &service.RoutingSelection{
		Account: &accounts[0],
		Candidates: []service.RoutingCandidateDecision{{
			AccountID:     7,
			Score:         1.25,
			EstimatedCost: 0.01,
			PriceKnown:    true,
		}},
	}

	dto := newRoutingSimulationSelectionDTO(selection, accounts)
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-value")
	require.NotContains(t, string(raw), "Credentials")
	require.Equal(t, int64(7), dto.SelectedAccount.ID)
	require.Equal(t, "upstream-a", dto.Candidates[0].AccountName)
}

func TestRoutingPolicyValidateRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRoutingPolicyHandler(nil, nil, nil, nil, nil)
	r := gin.New()
	r.POST("/validate", h.Validate)
	req := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(`{"config":{"schema_version":1,"scoring":{"price":1},"unexpected":true}}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRoutingPolicyValidateAcceptsValidConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRoutingPolicyHandler(nil, nil, nil, nil, nil)
	r := gin.New()
	r.POST("/validate", h.Validate)
	body := `{"config":{"schema_version":1,"scoring":{"price":1},"timeouts":{"request_timeout_ms":1000}}}`
	req := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRoutingPolicyBindingAcceptsArchivedRevisionThatWasPublished(t *testing.T) {
	gin.SetMode(gin.TestMode)
	publishedAt := time.Now().UTC()
	repo := &routingPolicyAdminRepoStub{
		policy:   &service.RoutingPolicy{ID: 9, Status: "active"},
		revision: &service.RoutingPolicyRevision{ID: 7, PolicyID: 9, State: service.RoutingPolicyRevisionArchived, PublishedAt: &publishedAt},
	}
	h := NewRoutingPolicyHandler(service.NewRoutingPolicyControlService(repo, nil), nil, nil, nil, nil)
	r := gin.New()
	r.POST("/routing-policies/:id/bindings/groups/:group_id", h.BindGroup)
	req := httptest.NewRequest(http.MethodPost, "/routing-policies/9/bindings/groups/3", strings.NewReader(`{"revision_id":7,"mode":"enforce"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, repo.bound)
	require.Equal(t, int64(7), *repo.bound.RevisionID)
}

func TestPriceBookDTORejectsStoredHeaderSecrets(t *testing.T) {
	dto := priceBookDTO(service.UpstreamPriceBook{
		ID: 1,
		SourceConfig: map[string]any{
			"url":     "https://example.com/prices.json",
			"headers": map[string]any{"Authorization": "Bearer super-secret"},
		},
	})
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "super-secret")
}

func TestRoutingPolicySimulationUsesConfiguredPriceResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := service.RoutingPolicyConfig{
		SchemaVersion:    1,
		CandidateFilters: service.RoutingCandidateFilters{RequireKnownPrice: true},
		Scoring:          service.RoutingScoringWeights{Price: 1},
	}
	revisionID := int64(7)
	repo := &routingPolicyAdminRepoStub{
		policy:   &service.RoutingPolicy{ID: 9, Status: "active", PublishedRevisionID: &revisionID},
		revision: &service.RoutingPolicyRevision{ID: revisionID, PolicyID: 9, State: service.RoutingPolicyRevisionPublished, Config: config},
	}
	accounts := []service.Account{
		{ID: 1, Name: "expensive", Platform: service.PlatformOpenAI, Status: service.StatusActive, Schedulable: true, Concurrency: 1},
		{ID: 2, Name: "cheap", Platform: service.PlatformOpenAI, Status: service.StatusActive, Schedulable: true, Concurrency: 1},
	}
	h := &RoutingPolicyHandler{
		control:     service.NewRoutingPolicyControlService(repo, nil),
		accountRepo: &routingAccountRepoStub{accounts: accounts},
		runtime: service.NewRoutingPolicyRuntime(nil, routingPriceResolverStub{
			1: {InputPerMillion: 2},
			2: {InputPerMillion: 1},
		}, service.NewMemoryRoutingHealthStore()),
	}
	r := gin.New()
	r.POST("/routing-policies/:id/simulate", h.Simulate)
	req := httptest.NewRequest(http.MethodPost, "/routing-policies/9/simulate", strings.NewReader(`{"group_id":3,"model":"gpt-test","estimated_input_tokens":1000000,"estimated_output_tokens":0}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"selected_account":{"id":2`)
	require.Contains(t, w.Body.String(), `"price_known":true`)
	require.Contains(t, w.Body.String(), `"estimated_cost_usd":1`)
}

func TestCreatePriceBookEncryptsHeadersAndMasksResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &routingPriceBookRepoStub{}
	h := &RoutingPolicyHandler{
		control:   service.NewRoutingPolicyControlService(nil, repo),
		encryptor: routingSecretEncryptorStub{},
	}
	r := gin.New()
	r.POST("/upstream-price-books", h.CreatePriceBook)
	req := httptest.NewRequest(http.MethodPost, "/upstream-price-books", strings.NewReader(`{
		"name":"provider prices",
		"source":"http_json",
		"status":"active",
		"currency":"USD",
		"source_config":{"url":"https://example.com/prices.json","headers":{"Authorization":"Bearer super-secret"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotNil(t, repo.created)
	headers, ok := repo.created.SourceConfig["headers"].(map[string]any)
	require.True(t, ok, "%#v", repo.created.SourceConfig)
	require.Equal(t, upstreamEncryptedValuePrefix+"sealed:Bearer super-secret", headers["Authorization"])
	require.NotContains(t, w.Body.String(), "super-secret")
	require.Contains(t, w.Body.String(), upstreamHeaderMask)
}

func TestUpdatePriceBookKeepsMaskedHeaderCiphertext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	storedValue := upstreamEncryptedValuePrefix + "sealed:Bearer super-secret"
	repo := &routingPriceBookRepoStub{book: &service.UpstreamPriceBook{
		ID:       1,
		Name:     "provider prices",
		Source:   service.PriceBookSourceHTTPJSON,
		Status:   "active",
		Currency: "USD",
		SourceConfig: map[string]any{
			"url":     "https://example.com/prices.json",
			"headers": map[string]any{"Authorization": storedValue},
		},
	}}
	h := &RoutingPolicyHandler{control: service.NewRoutingPolicyControlService(nil, repo), encryptor: routingSecretEncryptorStub{}, cfg: testPriceSourceConfig()}
	r := gin.New()
	r.PUT("/upstream-price-books/:id", h.UpdatePriceBook)
	req := httptest.NewRequest(http.MethodPut, "/upstream-price-books/1", strings.NewReader(`{
		"source_config":{"url":"https://example.com/prices.json","headers":{"Authorization":"********"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, repo.updated)
	headers, ok := repo.updated.SourceConfig["headers"].(map[string]any)
	require.True(t, ok, "%#v", repo.updated.SourceConfig)
	require.Equal(t, storedValue, headers["Authorization"])
	require.NotContains(t, w.Body.String(), "super-secret")
}

func TestSyncPriceBookDecryptsStoredHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receivedAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"prices":[{"model_pattern":"gpt-test","input_price_per_million":"1","output_price_per_million":"2","request_price":"0"}]}`))
	}))
	defer server.Close()

	repo := &routingPriceBookRepoStub{book: &service.UpstreamPriceBook{
		ID:       1,
		Name:     "provider prices",
		Source:   service.PriceBookSourceHTTPJSON,
		Status:   "active",
		Currency: "USD",
		SourceConfig: map[string]any{
			"url": server.URL,
			"headers": map[string]any{
				"Authorization": upstreamEncryptedValuePrefix + "sealed:Bearer super-secret",
			},
		},
	}}
	h := &RoutingPolicyHandler{control: service.NewRoutingPolicyControlService(nil, repo), encryptor: routingSecretEncryptorStub{}, cfg: testPriceSourceConfig()}
	r := gin.New()
	r.POST("/upstream-price-books/:id/sync", h.SyncPriceBook)
	req := httptest.NewRequest(http.MethodPost, "/upstream-price-books/1/sync", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "Bearer super-secret", receivedAuthorization)
	require.NotNil(t, repo.revision)
}

func TestSyncPriceBookDoesNotFollowRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"prices":[{"model_pattern":"gpt-test","input_price_per_million":"1","output_price_per_million":"2","request_price":"0"}]}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	repo := &routingPriceBookRepoStub{book: &service.UpstreamPriceBook{
		ID:       1,
		Name:     "provider prices",
		Source:   service.PriceBookSourceHTTPJSON,
		Status:   "active",
		Currency: "USD",
		SourceConfig: map[string]any{
			"url": source.URL,
			"headers": map[string]any{
				"Authorization": upstreamEncryptedValuePrefix + "sealed:Bearer super-secret",
			},
		},
	}}
	h := &RoutingPolicyHandler{control: service.NewRoutingPolicyControlService(nil, repo), encryptor: routingSecretEncryptorStub{}, cfg: testPriceSourceConfig()}
	r := gin.New()
	r.POST("/upstream-price-books/:id/sync", h.SyncPriceBook)
	req := httptest.NewRequest(http.MethodPost, "/upstream-price-books/1/sync", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Zero(t, targetRequests)
	require.Nil(t, repo.revision)
}

func TestSyncPriceBookRejectsPrivateURLWhenAllowlistEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &routingPriceBookRepoStub{book: &service.UpstreamPriceBook{
		ID:       1,
		Source:   service.PriceBookSourceHTTPJSON,
		Status:   "active",
		Currency: "USD",
		SourceConfig: map[string]any{
			"url": "https://127.0.0.1/prices.json",
		},
	}}
	h := &RoutingPolicyHandler{
		control: service.NewRoutingPolicyControlService(nil, repo),
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           true,
			PricingHosts:      []string{"127.0.0.1"},
			AllowPrivateHosts: false,
		}}},
	}
	r := gin.New()
	r.POST("/upstream-price-books/:id/sync", h.SyncPriceBook)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/upstream-price-books/1/sync", nil))

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Nil(t, repo.revision)
}

func TestValidatePriceSourceURLAcceptsAllowlistedHTTPSHost(t *testing.T) {
	h := &RoutingPolicyHandler{cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		Enabled:      true,
		PricingHosts: []string{"prices.example.com"},
	}}}}

	normalized, err := h.validatePriceSourceURL("https://prices.example.com/v1/")
	require.NoError(t, err)
	require.Equal(t, "https://prices.example.com/v1", normalized)
}
