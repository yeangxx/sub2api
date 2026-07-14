package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type routingPolicyRepository struct{ db *sql.DB }

// NewRoutingPolicyRepository creates the SQL-backed routing policy control
// plane repository. Runtime callers should keep the returned snapshots in a
// cache instead of querying this repository for every request.
func NewRoutingPolicyRepository(db *sql.DB) service.RoutingPolicyRepository {
	return &routingPolicyRepository{db: db}
}

func (r *routingPolicyRepository) GetPolicy(ctx context.Context, id int64) (*service.RoutingPolicy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("routing policy repository database is nil")
	}
	var p service.RoutingPolicy
	var draftID, publishedID, createdBy sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, status, draft_revision_id, published_revision_id,
		       created_by, created_at, updated_at
		FROM routing_policies WHERE id = $1`, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.Status, &draftID, &publishedID,
		&createdBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRoutingPolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get routing policy: %w", err)
	}
	p.DraftRevisionID = nullInt64Ptr(draftID)
	p.PublishedRevisionID = nullInt64Ptr(publishedID)
	p.CreatedBy = nullInt64Ptr(createdBy)
	return &p, nil
}

func (r *routingPolicyRepository) GetPublishedRevision(ctx context.Context, policyID int64) (*service.RoutingPolicyRevision, error) {
	return r.getRevision(ctx, `
		SELECT id, policy_id, version, state, schema_version, config, checksum,
		       comment, created_by, created_at, published_at
		FROM routing_policy_revisions
		WHERE policy_id = $1 AND state = 'published'
		ORDER BY version DESC LIMIT 1`, policyID)
}

func (r *routingPolicyRepository) getRevision(ctx context.Context, query string, arg any) (*service.RoutingPolicyRevision, error) {
	var rev service.RoutingPolicyRevision
	var raw []byte
	var createdBy sql.NullInt64
	var publishedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, arg).Scan(
		&rev.ID, &rev.PolicyID, &rev.Version, &rev.State, &rev.SchemaVersion,
		&raw, &rev.Checksum, &rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRoutingPolicyRevisionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get routing policy revision: %w", err)
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &rev.Config); err != nil {
			return nil, fmt.Errorf("decode routing policy revision config: %w", err)
		}
	}
	if rev.Config.SchemaVersion == 0 {
		rev.Config.SchemaVersion = rev.SchemaVersion
	}
	rev.CreatedBy = nullInt64Ptr(createdBy)
	if publishedAt.Valid {
		rev.PublishedAt = &publishedAt.Time
	}
	return &rev, nil
}

func (r *routingPolicyRepository) GetEffectiveForGroup(ctx context.Context, groupID int64) (*service.EffectiveRoutingPolicy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("routing policy repository database is nil")
	}
	var binding service.RoutingPolicyBinding
	var revisionID sql.NullInt64
	var modelOverrides []byte
	var createdBy sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT group_id, policy_id, revision_id, mode, model_overrides,
		       created_by, created_at, updated_at
		FROM group_routing_policy_bindings WHERE group_id = $1`, groupID).Scan(
		&binding.GroupID, &binding.PolicyID, &revisionID, &binding.Mode,
		&modelOverrides, &createdBy, &binding.CreatedAt, &binding.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRoutingPolicyBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get routing policy binding: %w", err)
	}
	binding.RevisionID = nullInt64Ptr(revisionID)
	binding.CreatedBy = nullInt64Ptr(createdBy)
	if len(modelOverrides) != 0 {
		if err := json.Unmarshal(modelOverrides, &binding.ModelOverrides); err != nil {
			return nil, fmt.Errorf("decode routing policy model overrides: %w", err)
		}
	}
	policy, err := r.GetPolicy(ctx, binding.PolicyID)
	if err != nil {
		return nil, err
	}
	if policy.Status != "active" {
		return nil, service.ErrRoutingPolicyDisabled
	}
	var revision *service.RoutingPolicyRevision
	if binding.RevisionID != nil {
		var rev service.RoutingPolicyRevision
		var raw []byte
		var revCreatedBy sql.NullInt64
		var publishedAt sql.NullTime
		err = r.db.QueryRowContext(ctx, `
			SELECT id, policy_id, version, state, schema_version, config, checksum,
			       comment, created_by, created_at, published_at
			FROM routing_policy_revisions
			WHERE id = $1 AND policy_id = $2
			  AND published_at IS NOT NULL
			  AND state IN ('published', 'archived')`,
			*binding.RevisionID, binding.PolicyID).Scan(
			&rev.ID, &rev.PolicyID, &rev.Version, &rev.State, &rev.SchemaVersion,
			&raw, &rev.Checksum, &rev.Comment, &revCreatedBy, &rev.CreatedAt, &publishedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrRoutingPolicyRevisionNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get pinned routing policy revision: %w", err)
		}
		if err := json.Unmarshal(raw, &rev.Config); err != nil {
			return nil, fmt.Errorf("decode pinned routing policy config: %w", err)
		}
		rev.CreatedBy = nullInt64Ptr(revCreatedBy)
		if publishedAt.Valid {
			rev.PublishedAt = &publishedAt.Time
		}
		revision = &rev
	} else {
		revision, err = r.GetPublishedRevision(ctx, binding.PolicyID)
	}
	if err != nil {
		return nil, err
	}
	return &service.EffectiveRoutingPolicy{Policy: *policy, Revision: *revision, Binding: binding}, nil
}

func (r *routingPolicyRepository) CreatePolicy(ctx context.Context, policy *service.RoutingPolicy) error {
	if policy == nil {
		return errors.New("routing policy cannot be nil")
	}
	if strings.TrimSpace(policy.Name) == "" {
		return errors.New("routing policy name cannot be empty")
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO routing_policies (name, description, status, created_by)
		VALUES ($1, $2, COALESCE(NULLIF($3, ''), 'active'), $4)
		RETURNING id, created_at, updated_at`, policy.Name, policy.Description, policy.Status, policy.CreatedBy).
		Scan(&policy.ID, &policy.CreatedAt, &policy.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create routing policy: %w", err)
	}
	return nil
}

// CreatePolicyWithRevision commits the policy identity and its initial draft
// in one transaction. This avoids leaving an active policy without a draft
// when a revision insert or pointer update fails.
func (r *routingPolicyRepository) CreatePolicyWithRevision(ctx context.Context, policy *service.RoutingPolicy, revision *service.RoutingPolicyRevision) error {
	if policy == nil || revision == nil {
		return errors.New("routing policy and revision cannot be nil")
	}
	if strings.TrimSpace(policy.Name) == "" {
		return errors.New("routing policy name cannot be empty")
	}
	if err := revision.Config.Validate(); err != nil {
		return err
	}
	if policy.Status == "" {
		policy.Status = "active"
	}
	if policy.Status != "active" && policy.Status != "disabled" && policy.Status != "archived" {
		return errors.New("routing policy status must be active, disabled, or archived")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create routing policy: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `INSERT INTO routing_policies (name, description, status, created_by) VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`, policy.Name, policy.Description, policy.Status, policy.CreatedBy).Scan(&policy.ID, &policy.CreatedAt, &policy.UpdatedAt); err != nil {
		return fmt.Errorf("create routing policy: %w", err)
	}
	revision.PolicyID = policy.ID
	if revision.State == "" {
		revision.State = service.RoutingPolicyRevisionDraft
	}
	if revision.Version == 0 {
		revision.Version = 1
	}
	raw, err := json.Marshal(revision.Config)
	if err != nil {
		return fmt.Errorf("encode routing policy config: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO routing_policy_revisions (policy_id, version, state, schema_version, config, checksum, comment, created_by) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8) RETURNING id, created_at`, revision.PolicyID, revision.Version, revision.State, revision.SchemaVersion, raw, revision.Checksum, revision.Comment, revision.CreatedBy).Scan(&revision.ID, &revision.CreatedAt); err != nil {
		return fmt.Errorf("create routing policy revision: %w", err)
	}
	if revision.State == service.RoutingPolicyRevisionDraft {
		if _, err := tx.ExecContext(ctx, `UPDATE routing_policies SET draft_revision_id = $1, updated_at = NOW() WHERE id = $2`, revision.ID, policy.ID); err != nil {
			return fmt.Errorf("update routing policy draft pointer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create routing policy: %w", err)
	}
	return nil
}

func (r *routingPolicyRepository) CreateRevision(ctx context.Context, revision *service.RoutingPolicyRevision) error {
	if revision == nil {
		return errors.New("routing policy revision cannot be nil")
	}
	if err := revision.Config.Validate(); err != nil {
		return err
	}
	if revision.SchemaVersion == 0 {
		revision.SchemaVersion = revision.Config.SchemaVersion
	}
	if revision.State == "" {
		revision.State = service.RoutingPolicyRevisionDraft
	}
	raw, err := json.Marshal(revision.Config)
	if err != nil {
		return fmt.Errorf("encode routing policy config: %w", err)
	}
	if revision.Version == 0 {
		if err := r.db.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM routing_policy_revisions WHERE policy_id = $1`,
			revision.PolicyID).Scan(&revision.Version); err != nil {
			return fmt.Errorf("allocate routing policy revision version: %w", err)
		}
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO routing_policy_revisions
			(policy_id, version, state, schema_version, config, checksum, comment, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING id, created_at`, revision.PolicyID, revision.Version, revision.State,
		revision.SchemaVersion, raw, revision.Checksum, revision.Comment, revision.CreatedBy).
		Scan(&revision.ID, &revision.CreatedAt)
	if err != nil {
		return fmt.Errorf("create routing policy revision: %w", err)
	}
	if revision.State == service.RoutingPolicyRevisionDraft {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE routing_policies SET draft_revision_id = $1, updated_at = NOW() WHERE id = $2`, revision.ID, revision.PolicyID); err != nil {
			return fmt.Errorf("update routing policy draft pointer: %w", err)
		}
	}
	return nil
}

func (r *routingPolicyRepository) ListPolicies(ctx context.Context) ([]service.RoutingPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, status, draft_revision_id, published_revision_id,
		       created_by, created_at, updated_at
		FROM routing_policies ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list routing policies: %w", err)
	}
	defer rows.Close()
	items := make([]service.RoutingPolicy, 0)
	for rows.Next() {
		var p service.RoutingPolicy
		var draftID, publishedID, createdBy sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &draftID, &publishedID, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan routing policy: %w", err)
		}
		p.DraftRevisionID, p.PublishedRevisionID, p.CreatedBy = nullInt64Ptr(draftID), nullInt64Ptr(publishedID), nullInt64Ptr(createdBy)
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing policies: %w", err)
	}
	return items, nil
}

func (r *routingPolicyRepository) UpdatePolicy(ctx context.Context, policy *service.RoutingPolicy) error {
	if policy == nil {
		return errors.New("routing policy cannot be nil")
	}
	if strings.TrimSpace(policy.Name) == "" {
		return errors.New("routing policy name cannot be empty")
	}
	if policy.Status == "" {
		policy.Status = "active"
	}
	if policy.Status != "active" && policy.Status != "disabled" && policy.Status != "archived" {
		return errors.New("routing policy status must be active, disabled, or archived")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE routing_policies SET name = $1, description = $2, status = $3, updated_at = NOW() WHERE id = $4`,
		policy.Name, policy.Description, policy.Status, policy.ID)
	if err != nil {
		return fmt.Errorf("update routing policy: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrRoutingPolicyNotFound
	}
	return nil
}

// UpdatePolicyWithRevision updates policy metadata and inserts a new draft in
// one transaction, so a failed draft write cannot leave a partial update.
func (r *routingPolicyRepository) UpdatePolicyWithRevision(ctx context.Context, policy *service.RoutingPolicy, revision *service.RoutingPolicyRevision) error {
	if policy == nil || revision == nil {
		return errors.New("routing policy and revision cannot be nil")
	}
	if strings.TrimSpace(policy.Name) == "" {
		return errors.New("routing policy name cannot be empty")
	}
	if policy.Status == "" {
		policy.Status = "active"
	}
	if policy.Status != "active" && policy.Status != "disabled" && policy.Status != "archived" {
		return errors.New("routing policy status must be active, disabled, or archived")
	}
	if err := revision.Config.Validate(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update routing policy: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE routing_policies SET name = $1, description = $2, status = $3, updated_at = NOW() WHERE id = $4`, policy.Name, policy.Description, policy.Status, policy.ID)
	if err != nil {
		return fmt.Errorf("update routing policy: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrRoutingPolicyNotFound
	}
	revision.PolicyID = policy.ID
	if revision.State == "" {
		revision.State = service.RoutingPolicyRevisionDraft
	}
	if revision.Version == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM routing_policy_revisions WHERE policy_id = $1`, policy.ID).Scan(&revision.Version); err != nil {
			return fmt.Errorf("allocate routing policy revision version: %w", err)
		}
	}
	raw, err := json.Marshal(revision.Config)
	if err != nil {
		return fmt.Errorf("encode routing policy config: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO routing_policy_revisions (policy_id, version, state, schema_version, config, checksum, comment, created_by) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8) RETURNING id, created_at`, revision.PolicyID, revision.Version, revision.State, revision.SchemaVersion, raw, revision.Checksum, revision.Comment, revision.CreatedBy).Scan(&revision.ID, &revision.CreatedAt); err != nil {
		return fmt.Errorf("create routing policy revision: %w", err)
	}
	if revision.State == service.RoutingPolicyRevisionDraft {
		if _, err := tx.ExecContext(ctx, `UPDATE routing_policies SET draft_revision_id = $1, updated_at = NOW() WHERE id = $2`, revision.ID, policy.ID); err != nil {
			return fmt.Errorf("update routing policy draft pointer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update routing policy: %w", err)
	}
	return nil
}

func (r *routingPolicyRepository) DeletePolicy(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM routing_policies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete routing policy: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrRoutingPolicyNotFound
	}
	return nil
}

func (r *routingPolicyRepository) ListRevisions(ctx context.Context, policyID int64) ([]service.RoutingPolicyRevision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, policy_id, version, state, schema_version, config, checksum,
		       comment, created_by, created_at, published_at
		FROM routing_policy_revisions WHERE policy_id = $1 ORDER BY version DESC`, policyID)
	if err != nil {
		return nil, fmt.Errorf("list routing policy revisions: %w", err)
	}
	defer rows.Close()
	items := make([]service.RoutingPolicyRevision, 0)
	for rows.Next() {
		var rev service.RoutingPolicyRevision
		var raw []byte
		var createdBy sql.NullInt64
		var publishedAt sql.NullTime
		if err := rows.Scan(&rev.ID, &rev.PolicyID, &rev.Version, &rev.State, &rev.SchemaVersion, &raw, &rev.Checksum, &rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt); err != nil {
			return nil, fmt.Errorf("scan routing policy revision: %w", err)
		}
		if err := json.Unmarshal(raw, &rev.Config); err != nil {
			return nil, fmt.Errorf("decode routing policy revision config: %w", err)
		}
		rev.CreatedBy = nullInt64Ptr(createdBy)
		if publishedAt.Valid {
			rev.PublishedAt = &publishedAt.Time
		}
		items = append(items, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing policy revisions: %w", err)
	}
	return items, nil
}

func (r *routingPolicyRepository) GetRevision(ctx context.Context, policyID, revisionID int64) (*service.RoutingPolicyRevision, error) {
	var rev service.RoutingPolicyRevision
	var raw []byte
	var createdBy sql.NullInt64
	var publishedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id, policy_id, version, state, schema_version, config, checksum,
		       comment, created_by, created_at, published_at
		FROM routing_policy_revisions WHERE id = $1 AND policy_id = $2`, revisionID, policyID).Scan(
		&rev.ID, &rev.PolicyID, &rev.Version, &rev.State, &rev.SchemaVersion, &raw, &rev.Checksum,
		&rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRoutingPolicyRevisionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get routing policy revision: %w", err)
	}
	if err := json.Unmarshal(raw, &rev.Config); err != nil {
		return nil, fmt.Errorf("decode routing policy revision config: %w", err)
	}
	rev.CreatedBy = nullInt64Ptr(createdBy)
	if publishedAt.Valid {
		rev.PublishedAt = &publishedAt.Time
	}
	return &rev, nil
}

func (r *routingPolicyRepository) GetRevisionByVersion(ctx context.Context, policyID int64, version int) (*service.RoutingPolicyRevision, error) {
	var rev service.RoutingPolicyRevision
	var raw []byte
	var createdBy sql.NullInt64
	var publishedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id, policy_id, version, state, schema_version, config, checksum,
		       comment, created_by, created_at, published_at
		FROM routing_policy_revisions WHERE policy_id = $1 AND version = $2`, policyID, version).Scan(
		&rev.ID, &rev.PolicyID, &rev.Version, &rev.State, &rev.SchemaVersion, &raw, &rev.Checksum,
		&rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRoutingPolicyRevisionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get routing policy revision by version: %w", err)
	}
	if err := json.Unmarshal(raw, &rev.Config); err != nil {
		return nil, fmt.Errorf("decode routing policy revision config: %w", err)
	}
	rev.CreatedBy = nullInt64Ptr(createdBy)
	if publishedAt.Valid {
		rev.PublishedAt = &publishedAt.Time
	}
	return &rev, nil
}

func (r *routingPolicyRepository) RestoreRevision(ctx context.Context, policyID, revisionID int64, createdBy *int64) (*service.RoutingPolicyRevision, error) {
	original, err := r.GetRevision(ctx, policyID, revisionID)
	if err != nil {
		return nil, err
	}
	restored := &service.RoutingPolicyRevision{PolicyID: policyID, State: service.RoutingPolicyRevisionDraft, SchemaVersion: original.SchemaVersion, Config: original.Config, Checksum: original.Checksum, Comment: "restored from version " + fmt.Sprint(original.Version), CreatedBy: createdBy}
	if err := r.CreateRevision(ctx, restored); err != nil {
		return nil, err
	}
	return restored, nil
}

func (r *routingPolicyRepository) PublishRevision(ctx context.Context, policyID, revisionID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin publish routing policy revision: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE routing_policy_revisions
		SET state = 'archived'
		WHERE policy_id = $1 AND state = 'published' AND id <> $2`, policyID, revisionID); err != nil {
		return fmt.Errorf("archive previous routing policy revision: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE routing_policy_revisions
		SET state = 'published', published_at = NOW()
		WHERE id = $1 AND policy_id = $2 AND state IN ('draft', 'published')`, revisionID, policyID)
	if err != nil {
		return fmt.Errorf("publish routing policy revision: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return service.ErrRoutingPolicyRevisionNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE routing_policies
		SET published_revision_id = $1,
		    draft_revision_id = CASE WHEN draft_revision_id = $1 THEN NULL ELSE draft_revision_id END,
		    updated_at = NOW()
		WHERE id = $2`, revisionID, policyID); err != nil {
		return fmt.Errorf("update routing policy published pointer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit routing policy revision publish: %w", err)
	}
	return nil
}

func (r *routingPolicyRepository) BindGroup(ctx context.Context, binding *service.RoutingPolicyBinding) error {
	if binding == nil {
		return errors.New("routing policy binding cannot be nil")
	}
	mode, err := service.NormalizeRoutingPolicyMode(binding.Mode)
	if err != nil {
		return err
	}
	binding.Mode = mode
	var policyStatus string
	if err := r.db.QueryRowContext(ctx, `SELECT status FROM routing_policies WHERE id = $1`, binding.PolicyID).Scan(&policyStatus); errors.Is(err, sql.ErrNoRows) {
		return service.ErrRoutingPolicyNotFound
	} else if err != nil {
		return fmt.Errorf("check routing policy status: %w", err)
	}
	if policyStatus != "active" {
		return service.ErrRoutingPolicyDisabled
	}
	if binding.RevisionID != nil {
		var state string
		var publishedAt sql.NullTime
		if err := r.db.QueryRowContext(ctx, `SELECT state, published_at FROM routing_policy_revisions WHERE id = $1 AND policy_id = $2`, *binding.RevisionID, binding.PolicyID).Scan(&state, &publishedAt); errors.Is(err, sql.ErrNoRows) {
			return service.ErrRoutingPolicyRevisionNotFound
		} else if err != nil {
			return fmt.Errorf("check routing policy revision: %w", err)
		}
		revision := service.RoutingPolicyRevision{State: state}
		if publishedAt.Valid {
			revision.PublishedAt = &publishedAt.Time
		}
		if !revision.IsPinnable() {
			return service.ErrRoutingPolicyRevisionNotPublished
		}
	}
	raw, err := json.Marshal(binding.ModelOverrides)
	if err != nil {
		return fmt.Errorf("encode routing policy model overrides: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO group_routing_policy_bindings
			(group_id, policy_id, revision_id, mode, model_overrides, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		ON CONFLICT (group_id) DO UPDATE SET
			policy_id = EXCLUDED.policy_id,
			revision_id = EXCLUDED.revision_id,
			mode = EXCLUDED.mode,
			model_overrides = EXCLUDED.model_overrides,
			updated_at = NOW()`, binding.GroupID, binding.PolicyID, binding.RevisionID, binding.Mode, raw, binding.CreatedBy)
	if err != nil {
		return fmt.Errorf("bind routing policy to group: %w", err)
	}
	return nil
}

func (r *routingPolicyRepository) UnbindGroup(ctx context.Context, groupID int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM group_routing_policy_bindings WHERE group_id = $1`, groupID)
	if err != nil {
		return fmt.Errorf("unbind routing policy group: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrRoutingPolicyBindingNotFound
	}
	return nil
}

func (r *routingPolicyRepository) RecordAudit(ctx context.Context, log *service.RoutingPolicyAuditLog) error {
	if log == nil {
		return errors.New("routing policy audit log cannot be nil")
	}
	raw, err := json.Marshal(log.Details)
	if err != nil {
		return fmt.Errorf("encode routing policy audit details: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO routing_policy_audit_logs
			(policy_id, revision_id, group_id, actor_user_id, action, details)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		RETURNING id, created_at`, log.PolicyID, log.RevisionID, log.GroupID, log.ActorUserID, log.Action, raw).
		Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("record routing policy audit: %w", err)
	}
	return nil
}

// priceBookRepository is kept separate from routingPolicyRepository so a
// price synchronizer can be granted only price-book access in the future.
type priceBookRepository struct{ db *sql.DB }

func NewUpstreamPriceBookRepository(db *sql.DB) service.UpstreamPriceBookRepository {
	return &priceBookRepository{db: db}
}

func (r *priceBookRepository) GetBook(ctx context.Context, id int64) (*service.UpstreamPriceBook, error) {
	var b service.UpstreamPriceBook
	var latestID, createdBy sql.NullInt64
	var sourceConfig []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, source, status, currency, source_config, latest_revision_id, created_by, created_at, updated_at
		FROM upstream_price_books WHERE id = $1`, id).Scan(
		&b.ID, &b.Name, &b.Source, &b.Status, &b.Currency, &sourceConfig, &latestID, &createdBy, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUpstreamPriceBookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upstream price book: %w", err)
	}
	b.LatestRevisionID = nullInt64Ptr(latestID)
	b.CreatedBy = nullInt64Ptr(createdBy)
	if len(sourceConfig) > 0 {
		_ = json.Unmarshal(sourceConfig, &b.SourceConfig)
	}
	return &b, nil
}

func (r *priceBookRepository) ListBooks(ctx context.Context) ([]service.UpstreamPriceBook, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, source, status, currency, source_config, latest_revision_id, created_by, created_at, updated_at
		FROM upstream_price_books ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list upstream price books: %w", err)
	}
	defer rows.Close()
	items := make([]service.UpstreamPriceBook, 0)
	for rows.Next() {
		var b service.UpstreamPriceBook
		var latestID, createdBy sql.NullInt64
		var sourceConfig []byte
		if err := rows.Scan(&b.ID, &b.Name, &b.Source, &b.Status, &b.Currency, &sourceConfig, &latestID, &createdBy, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream price book: %w", err)
		}
		if len(sourceConfig) > 0 {
			_ = json.Unmarshal(sourceConfig, &b.SourceConfig)
		}
		b.LatestRevisionID, b.CreatedBy = nullInt64Ptr(latestID), nullInt64Ptr(createdBy)
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream price books: %w", err)
	}
	return items, nil
}

func (r *priceBookRepository) UpdateBook(ctx context.Context, book *service.UpstreamPriceBook) error {
	if book == nil {
		return errors.New("upstream price book cannot be nil")
	}
	config, err := json.Marshal(book.SourceConfig)
	if err != nil {
		return fmt.Errorf("encode upstream price source config: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_price_books
		SET name = $1, source = $2, status = $3, currency = $4, source_config = $5::jsonb, updated_at = NOW()
		WHERE id = $6`, book.Name, book.Source, book.Status, book.Currency, config, book.ID)
	if err != nil {
		return fmt.Errorf("update upstream price book: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrUpstreamPriceBookNotFound
	}
	return nil
}

func (r *priceBookRepository) DeleteBook(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM upstream_price_books WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete upstream price book: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrUpstreamPriceBookNotFound
	}
	return nil
}

func (r *priceBookRepository) ListRevisions(ctx context.Context, bookID int64) ([]service.UpstreamPriceBookRevision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, price_book_id, version, state, effective_at, source_snapshot,
		       comment, created_by, created_at, published_at
		FROM upstream_price_book_revisions WHERE price_book_id = $1 ORDER BY version DESC`, bookID)
	if err != nil {
		return nil, fmt.Errorf("list upstream price book revisions: %w", err)
	}
	defer rows.Close()
	items := make([]service.UpstreamPriceBookRevision, 0)
	for rows.Next() {
		var rev service.UpstreamPriceBookRevision
		var raw []byte
		var effectiveAt, publishedAt sql.NullTime
		var createdBy sql.NullInt64
		if err := rows.Scan(&rev.ID, &rev.PriceBookID, &rev.Version, &rev.State, &effectiveAt, &raw, &rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt); err != nil {
			return nil, fmt.Errorf("scan upstream price book revision: %w", err)
		}
		if effectiveAt.Valid {
			rev.EffectiveAt = &effectiveAt.Time
		}
		if publishedAt.Valid {
			rev.PublishedAt = &publishedAt.Time
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &rev.SourceSnapshot); err != nil {
				return nil, fmt.Errorf("decode upstream price book snapshot: %w", err)
			}
		}
		rev.CreatedBy = nullInt64Ptr(createdBy)
		items = append(items, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream price book revisions: %w", err)
	}
	return items, nil
}

func (r *priceBookRepository) GetPublishedRevision(ctx context.Context, bookID int64) (*service.UpstreamPriceBookRevision, error) {
	var rev service.UpstreamPriceBookRevision
	var raw []byte
	var effectiveAt, publishedAt sql.NullTime
	var createdBy sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT r.id, r.price_book_id, r.version, r.state, r.effective_at, r.source_snapshot,
		       r.comment, r.created_by, r.created_at, r.published_at
		FROM upstream_price_book_revisions r
		JOIN upstream_price_books b ON b.id = r.price_book_id
		WHERE r.price_book_id = $1
		  AND b.status = 'active'
		  AND r.state IN ('published', 'archived')
		  AND r.published_at IS NOT NULL
		  AND (r.effective_at IS NULL OR r.effective_at <= NOW())
		ORDER BY r.published_at DESC, r.id DESC LIMIT 1`, bookID).Scan(
		&rev.ID, &rev.PriceBookID, &rev.Version, &rev.State, &effectiveAt, &raw,
		&rev.Comment, &createdBy, &rev.CreatedAt, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUpstreamPriceBookRevisionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upstream price book revision: %w", err)
	}
	if effectiveAt.Valid {
		rev.EffectiveAt = &effectiveAt.Time
	}
	if publishedAt.Valid {
		rev.PublishedAt = &publishedAt.Time
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &rev.SourceSnapshot); err != nil {
			return nil, fmt.Errorf("decode upstream price book snapshot: %w", err)
		}
	}
	rev.CreatedBy = nullInt64Ptr(createdBy)
	return &rev, nil
}

func (r *priceBookRepository) ListModelPrices(ctx context.Context, revisionID int64) ([]service.UpstreamModelPrice, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, revision_id, model_pattern, input_price_per_million,
		       output_price_per_million, cache_read_price_per_million,
		       cache_write_price_per_million, request_price, metadata, created_at
		FROM upstream_price_model_prices WHERE revision_id = $1 ORDER BY id`, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list upstream model prices: %w", err)
	}
	defer rows.Close()
	prices := make([]service.UpstreamModelPrice, 0)
	for rows.Next() {
		var p service.UpstreamModelPrice
		var input, output, cacheRead, cacheWrite, request sql.NullString
		var metadata []byte
		if err := rows.Scan(&p.ID, &p.RevisionID, &p.ModelPattern, &input, &output, &cacheRead, &cacheWrite, &request, &metadata, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream model price: %w", err)
		}
		p.InputPricePerMillion = parseDecimal(input)
		p.OutputPricePerMillion = parseDecimal(output)
		p.CacheReadPricePerMillion = parseDecimal(cacheRead)
		p.CacheWritePricePerMillion = parseDecimal(cacheWrite)
		p.RequestPrice = parseDecimal(request)
		if len(metadata) != 0 {
			if err := json.Unmarshal(metadata, &p.Metadata); err != nil {
				return nil, fmt.Errorf("decode upstream model price metadata: %w", err)
			}
		}
		prices = append(prices, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream model prices: %w", err)
	}
	return prices, nil
}

func (r *priceBookRepository) CreateBook(ctx context.Context, book *service.UpstreamPriceBook) error {
	if book == nil {
		return errors.New("upstream price book cannot be nil")
	}
	config, err := json.Marshal(book.SourceConfig)
	if err != nil {
		return fmt.Errorf("encode upstream price source config: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO upstream_price_books (name, source, status, currency, source_config, created_by)
		VALUES ($1, COALESCE(NULLIF($2, ''), 'manual'), COALESCE(NULLIF($3, ''), 'active'), $4, $5::jsonb, $6)
		RETURNING id, created_at, updated_at`, book.Name, book.Source, book.Status, book.Currency, config, book.CreatedBy).
		Scan(&book.ID, &book.CreatedAt, &book.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create upstream price book: %w", err)
	}
	return nil
}

func (r *priceBookRepository) CreateRevision(ctx context.Context, revision *service.UpstreamPriceBookRevision, prices []service.UpstreamModelPrice) error {
	if revision == nil {
		return errors.New("upstream price book revision cannot be nil")
	}
	raw, err := json.Marshal(revision.SourceSnapshot)
	if err != nil {
		return fmt.Errorf("encode upstream price book snapshot: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if revision.Version == 0 {
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM upstream_price_book_revisions WHERE price_book_id = $1`,
			revision.PriceBookID).Scan(&revision.Version); err != nil {
			return fmt.Errorf("allocate upstream price book revision version: %w", err)
		}
	}
	if revision.State == "" {
		revision.State = service.RoutingPolicyRevisionDraft
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO upstream_price_book_revisions
			(price_book_id, version, state, effective_at, source_snapshot, comment, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
		RETURNING id, created_at`, revision.PriceBookID, revision.Version, revision.State,
		revision.EffectiveAt, raw, revision.Comment, revision.CreatedBy).
		Scan(&revision.ID, &revision.CreatedAt)
	if err != nil {
		return fmt.Errorf("create upstream price book revision: %w", err)
	}
	for i := range prices {
		p := &prices[i]
		metadata, err := json.Marshal(p.Metadata)
		if err != nil {
			return fmt.Errorf("encode upstream model price metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO upstream_price_model_prices
				(revision_id, model_pattern, input_price_per_million, output_price_per_million,
				 cache_read_price_per_million, cache_write_price_per_million, request_price, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, revision.ID, p.ModelPattern,
			p.InputPricePerMillion, p.OutputPricePerMillion, p.CacheReadPricePerMillion,
			p.CacheWritePricePerMillion, p.RequestPrice, metadata); err != nil {
			return fmt.Errorf("create upstream model price: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upstream price book revision: %w", err)
	}
	return nil
}

func (r *priceBookRepository) PublishRevision(ctx context.Context, bookID, revisionID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE upstream_price_book_revisions SET state = 'archived' WHERE price_book_id = $1 AND state = 'published' AND id <> $2`, bookID, revisionID); err != nil {
		return fmt.Errorf("archive previous upstream price revision: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE upstream_price_book_revisions SET state = 'published', published_at = NOW()
		WHERE id = $1 AND price_book_id = $2 AND state IN ('draft', 'published')`, revisionID, bookID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return service.ErrUpstreamPriceBookRevisionNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE upstream_price_books SET latest_revision_id = $1, updated_at = NOW() WHERE id = $2`, revisionID, bookID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func parseDecimal(v sql.NullString) decimal.Decimal {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(v.String)
	if err != nil {
		return decimal.Zero
	}
	return d
}
