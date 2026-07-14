package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration178AddsCompositeKeysBeforeRevisionOwnershipForeignKeys(t *testing.T) {
	content, err := FS.ReadFile("178_routing_policy_integrity.sql")
	require.NoError(t, err)

	sql := string(content)
	constraints := []struct {
		uniqueKey  string
		foreignKey string
	}{
		{
			uniqueKey:  "UNIQUE (policy_id, id)",
			foreignKey: "REFERENCES routing_policy_revisions(policy_id, id)",
		},
		{
			uniqueKey:  "UNIQUE (price_book_id, id)",
			foreignKey: "REFERENCES upstream_price_book_revisions(price_book_id, id)",
		},
	}

	for _, constraint := range constraints {
		uniqueKeyIndex := strings.Index(sql, constraint.uniqueKey)
		foreignKeyIndex := strings.Index(sql, constraint.foreignKey)
		require.NotEqual(t, -1, uniqueKeyIndex, "missing composite unique key %q", constraint.uniqueKey)
		require.NotEqual(t, -1, foreignKeyIndex, "missing composite foreign key target %q", constraint.foreignKey)
		require.Less(t, uniqueKeyIndex, foreignKeyIndex, "composite unique key must be created before its foreign key")
	}
}

func TestMigration180DoesNotBlockCascadeDeletingModelPrices(t *testing.T) {
	content, err := FS.ReadFile("180_allow_model_price_cascade_deletes.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "DROP TRIGGER IF EXISTS trg_upstream_model_price_immutable ON upstream_price_model_prices;")
	require.NotContains(t, sql, "BEFORE UPDATE OR DELETE ON upstream_price_model_prices")
	require.Contains(t, sql, "BEFORE UPDATE ON upstream_price_model_prices")
}
