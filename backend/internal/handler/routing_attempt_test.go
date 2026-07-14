package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestBeginRoutingAttemptAppliesRequestTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	selection := &service.AccountSelectionResult{RoutingPolicy: routingAttemptPolicy(service.RoutingTimeoutPolicy{RequestTimeoutMillis: 20})}

	ctx, finish := beginRoutingAttempt(c, selection, false)
	defer finish()
	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("request timeout did not cancel routing attempt")
	}
}

func TestBeginRoutingAttemptResetsStreamIdleTimeoutOnWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	selection := &service.AccountSelectionResult{RoutingPolicy: routingAttemptPolicy(service.RoutingTimeoutPolicy{StreamIdleMillis: 40})}

	ctx, finish := beginRoutingAttempt(c, selection, true)
	defer finish()
	time.Sleep(25 * time.Millisecond)
	_, _ = c.Writer.Write([]byte("chunk"))
	time.Sleep(25 * time.Millisecond)
	select {
	case <-ctx.Done():
		t.Fatal("stream idle timeout was not reset by response write")
	default:
	}
	select {
	case <-ctx.Done():
	case <-time.After(150 * time.Millisecond):
		t.Fatal("stream idle timeout did not cancel inactive routing attempt")
	}
}

func routingAttemptPolicy(timeouts service.RoutingTimeoutPolicy) *service.EffectiveRoutingPolicy {
	return &service.EffectiveRoutingPolicy{Revision: service.RoutingPolicyRevision{Config: service.RoutingPolicyConfig{Timeouts: timeouts}}}
}
