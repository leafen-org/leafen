package app

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"time"
)

// A response that outlives its answer — a websocket its handler takes over,
// or an event stream — is judged by how it started (2026-10-18: a socket's
// taken-over response reads as status 0, and every page opened kept an
// entry as if it had failed, 95% of the log after four hours; streams open
// for minutes kept one as slow). Taken over, it answered 101; its slowness
// is the time to take over or to answer first, not how long it stayed open.

// requestLogStreamed is a request whose answer may stay open.
func requestLogStreamed(request *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(request.Header.Get("Upgrade")), "websocket") ||
		strings.Contains(request.Header.Get("Accept"), "text/event-stream")
}

// requestLogStream watches a streamed response start.
type requestLogStream struct {
	http.ResponseWriter
	started  time.Time
	at       time.Time
	upgraded bool
}

func (stream *requestLogStream) mark() {
	if stream.at.IsZero() {
		stream.at = time.Now()
	}
}

func (stream *requestLogStream) WriteHeader(code int) {
	stream.mark()
	stream.ResponseWriter.WriteHeader(code)
}

func (stream *requestLogStream) Write(data []byte) (int, error) {
	stream.mark()
	return stream.ResponseWriter.Write(data)
}

func (stream *requestLogStream) Flush() {
	_ = http.NewResponseController(stream.ResponseWriter).Flush()
}

func (stream *requestLogStream) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(stream.ResponseWriter).Hijack()
	if err == nil {
		stream.mark()
		stream.upgraded = true
	}
	return conn, buffer, err
}

func (stream *requestLogStream) Unwrap() http.ResponseWriter {
	return stream.ResponseWriter
}

// outcome is the stream's status and how long it took to start.
func (stream *requestLogStream) outcome(status int) (int, time.Duration) {
	if stream.upgraded {
		status = http.StatusSwitchingProtocols
	}
	if stream.at.IsZero() {
		return status, time.Since(stream.started)
	}
	return status, stream.at.Sub(stream.started)
}
