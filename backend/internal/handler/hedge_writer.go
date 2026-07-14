package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

// hedgeResponseWriter isolates one upstream attempt from the real client
// writer. It is intentionally used only for non-streaming Hedge requests;
// streaming responses must commit their first semantic event immediately.
type hedgeResponseWriter struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
	flushCount  int
	requestCtx  context.Context
}

func newHedgeResponseWriter(ctx context.Context) *hedgeResponseWriter {
	return &hedgeResponseWriter{header: make(http.Header), requestCtx: ctx}
}

func (w *hedgeResponseWriter) Header() http.Header { return w.header }

func (w *hedgeResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
}

func (w *hedgeResponseWriter) WriteHeaderNow() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *hedgeResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(p)
}

func (w *hedgeResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *hedgeResponseWriter) Flush() {
	w.WriteHeaderNow()
	w.flushCount++
}

func (w *hedgeResponseWriter) CloseNotify() <-chan bool {
	if w.requestCtx == nil {
		return make(chan bool)
	}
	closed := make(chan bool, 1)
	go func() {
		<-w.requestCtx.Done()
		closed <- true
	}()
	return closed
}

func (w *hedgeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijacking is not supported for hedged attempts")
}

func (w *hedgeResponseWriter) Pusher() http.Pusher { return nil }

func (w *hedgeResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	w.WriteHeaderNow()
	return io.Copy(&w.body, reader)
}

func (w *hedgeResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *hedgeResponseWriter) Size() int { return w.body.Len() }

func (w *hedgeResponseWriter) Written() bool { return w.wroteHeader || w.body.Len() > 0 }

func (w *hedgeResponseWriter) Unwrap() http.ResponseWriter { return w }

func (w *hedgeResponseWriter) commit(dst gin.ResponseWriter) error {
	if dst == nil {
		return errors.New("destination response writer is nil")
	}
	for key, values := range w.header {
		dst.Header()[key] = append([]string(nil), values...)
	}
	dst.WriteHeader(w.Status())
	_, err := dst.Write(w.body.Bytes())
	return err
}
