package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRoutingPolicyRepositoryBindGroupAcceptsPreviouslyPublishedRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	publishedAt := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status FROM routing_policies WHERE id = $1")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT state, published_at FROM routing_policy_revisions WHERE id = $1 AND policy_id = $2")).
		WithArgs(int64(7), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"state", "published_at"}).AddRow(service.RoutingPolicyRevisionArchived, publishedAt))
	mock.ExpectExec("INSERT INTO group_routing_policy_bindings").
		WithArgs(int64(3), int64(9), sqlmock.AnyArg(), service.RoutingPolicyModeEnforce, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	revisionID := int64(7)
	repo := &routingPolicyRepository{db: db}
	err = repo.BindGroup(context.Background(), &service.RoutingPolicyBinding{
		GroupID:    3,
		PolicyID:   9,
		RevisionID: &revisionID,
		Mode:       service.RoutingPolicyModeEnforce,
	})
	if err != nil {
		t.Fatalf("BindGroup() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestRoutingPolicyRepositoryResolvesPreviouslyPublishedPinnedRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectQuery("FROM group_routing_policy_bindings WHERE group_id = \\$1").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "policy_id", "revision_id", "mode", "model_overrides", "created_by", "created_at", "updated_at"}).
			AddRow(int64(3), int64(9), int64(7), service.RoutingPolicyModeEnforce, []byte(`{}`), nil, now, now))
	mock.ExpectQuery("FROM routing_policies WHERE id = \\$1").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "description", "status", "draft_revision_id", "published_revision_id", "created_by", "created_at", "updated_at"}).
			AddRow(int64(9), "policy", "", "active", nil, int64(8), nil, now, now))
	mock.ExpectQuery("FROM routing_policy_revisions WHERE id = \\$1 AND policy_id = \\$2 AND published_at IS NOT NULL").
		WithArgs(int64(7), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "policy_id", "version", "state", "schema_version", "config", "checksum", "comment", "created_by", "created_at", "published_at"}).
			AddRow(int64(7), int64(9), 1, service.RoutingPolicyRevisionArchived, 1, []byte(`{"schema_version":1,"scoring":{"price":1}}`), "sum", "", sql.NullInt64{}, now, now))

	effective, err := (&routingPolicyRepository{db: db}).GetEffectiveForGroup(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetEffectiveForGroup() error = %v", err)
	}
	if effective.Revision.ID != 7 || effective.Revision.State != service.RoutingPolicyRevisionArchived {
		t.Fatalf("resolved revision = %#v, want archived revision 7", effective.Revision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestPriceBookRepositoryGetsLatestEffectiveRevisionFromActiveBook(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM upstream_price_book_revisions r\s+JOIN upstream_price_books b ON b.id = r.price_book_id\s+WHERE r.price_book_id = \$1\s+AND b.status = 'active'\s+AND r.state IN \('published', 'archived'\)\s+AND r.published_at IS NOT NULL\s+AND \(r.effective_at IS NULL OR r.effective_at <= NOW\(\)\)`).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "price_book_id", "version", "state", "effective_at", "source_snapshot", "comment", "created_by", "created_at", "published_at"}).
			AddRow(int64(7), int64(3), 1, service.RoutingPolicyRevisionArchived, nil, []byte(`{}`), "", nil, now, now))

	revision, err := (&priceBookRepository{db: db}).GetPublishedRevision(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetPublishedRevision() error = %v", err)
	}
	if revision.ID != 7 || revision.State != service.RoutingPolicyRevisionArchived {
		t.Fatalf("revision = %#v, want archived effective revision 7", revision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
