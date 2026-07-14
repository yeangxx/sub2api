package service

import (
	"context"
	"testing"
)

type routingMonitorRepoStub struct {
	ChannelMonitorRepository
	created *ChannelMonitor
}

func (r *routingMonitorRepoStub) Create(_ context.Context, monitor *ChannelMonitor) error {
	r.created = monitor
	monitor.ID = 1
	return nil
}

type routingMonitorEncryptorStub struct{}

func (routingMonitorEncryptorStub) Encrypt(value string) (string, error) {
	return "encrypted:" + value, nil
}
func (routingMonitorEncryptorStub) Decrypt(value string) (string, error) { return value, nil }

func TestChannelMonitorCreatePreservesAccountBinding(t *testing.T) {
	accountID := int64(42)
	repo := &routingMonitorRepoStub{}
	monitor, err := NewChannelMonitorService(repo, routingMonitorEncryptorStub{}).Create(context.Background(), ChannelMonitorCreateParams{
		Name:            "upstream monitor",
		Provider:        MonitorProviderOpenAI,
		APIMode:         MonitorAPIModeChatCompletions,
		Endpoint:        "https://example.com",
		APIKey:          "secret",
		PrimaryModel:    "gpt-test",
		IntervalSeconds: 60,
		AccountID:       &accountID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if monitor.AccountID == nil || *monitor.AccountID != accountID {
		t.Fatalf("created account binding = %v, want %d", monitor.AccountID, accountID)
	}
	if repo.created == nil || repo.created.AccountID == nil || *repo.created.AccountID != accountID {
		t.Fatalf("persisted account binding = %v, want %d", repo.created, accountID)
	}
}

func TestApplyMonitorUpdateChangesAccountBinding(t *testing.T) {
	oldID := int64(1)
	newID := int64(2)
	existing := &ChannelMonitor{AccountID: &oldID}
	if err := applyMonitorUpdate(existing, ChannelMonitorUpdateParams{AccountID: &newID}); err != nil {
		t.Fatalf("applyMonitorUpdate() error = %v", err)
	}
	if existing.AccountID == nil || *existing.AccountID != newID {
		t.Fatalf("updated account binding = %v, want %d", existing.AccountID, newID)
	}
}

func TestApplyMonitorUpdateCanClearAccountBinding(t *testing.T) {
	accountID := int64(1)
	existing := &ChannelMonitor{AccountID: &accountID}
	if err := applyMonitorUpdate(existing, ChannelMonitorUpdateParams{ClearAccountID: true}); err != nil {
		t.Fatalf("applyMonitorUpdate() error = %v", err)
	}
	if existing.AccountID != nil {
		t.Fatalf("cleared account binding = %v, want nil", existing.AccountID)
	}
}
