package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type routingActivityWriter struct {
	gin.ResponseWriter
	touch func()
}

func (w *routingActivityWriter) Write(data []byte) (int, error) {
	w.touch()
	return w.ResponseWriter.Write(data)
}

func (w *routingActivityWriter) WriteString(value string) (int, error) {
	w.touch()
	return w.ResponseWriter.WriteString(value)
}

func (w *routingActivityWriter) Flush() {
	w.touch()
	w.ResponseWriter.Flush()
}

func (w *routingActivityWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func beginRoutingAttempt(c *gin.Context, selection *service.AccountSelectionResult, stream bool) (context.Context, func()) {
	if c == nil || c.Request == nil {
		return context.Background(), func() {}
	}
	originalRequest := c.Request
	originalWriter := c.Writer
	ctx := originalRequest.Context()
	cancel := func() {}
	if selection != nil && selection.RoutingPolicy != nil {
		timeouts := selection.RoutingPolicy.Revision.Config.Timeouts
		if timeouts.RequestTimeoutMillis > 0 {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeouts.RequestTimeoutMillis)*time.Millisecond)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}
		var idleTimer *time.Timer
		var idleMu sync.Mutex
		if stream && timeouts.StreamIdleMillis > 0 {
			idleDuration := time.Duration(timeouts.StreamIdleMillis) * time.Millisecond
			idleTimer = time.AfterFunc(idleDuration, cancel)
			touch := func() {
				idleMu.Lock()
				if idleTimer != nil {
					idleTimer.Reset(idleDuration)
				}
				idleMu.Unlock()
			}
			c.Writer = &routingActivityWriter{ResponseWriter: originalWriter, touch: touch}
		}
		c.Request = originalRequest.WithContext(ctx)
		var once sync.Once
		return ctx, func() {
			once.Do(func() {
				idleMu.Lock()
				if idleTimer != nil {
					idleTimer.Stop()
					idleTimer = nil
				}
				idleMu.Unlock()
				c.Request = originalRequest
				c.Writer = originalWriter
				cancel()
			})
		}
	}
	return ctx, func() {}
}
