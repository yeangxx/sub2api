package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestFetchChatGPTAccountInfo_UsesSelectedAccountKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"accounts": {
				"org-selected": {
					"account": {"plan_type": "plus", "is_default": true},
					"entitlement": {"expires_at": "2099-01-01T00:00:00Z"}
				}
			}
		}`))
	}))
	defer server.Close()

	originalURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL
	defer func() { chatGPTAccountsCheckURL = originalURL }()

	info := fetchChatGPTAccountInfo(context.Background(), func(string) (*req.Client, error) {
		return req.C(), nil
	}, "test-access-token", "", "org-selected")

	require.NotNil(t, info)
	require.Equal(t, "org-selected", info.AccountID)
	require.Equal(t, "plus", info.PlanType)
}
