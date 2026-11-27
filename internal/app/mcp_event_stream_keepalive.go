package app

import (
	"bytes"
	"mime"
	"net/http"
	"sync"
	"time"
)

// An MCP event stream held open — a client listening for changes
// (subscriptions/listen) or a session over server-sent events — says
// nothing between the changes it carries, and a proxy in front of the
// server ends a stream it read nothing from for a while: the Synology https
// proxy after 60 s. Claude Code opens its listen stream again each time and
// reads the tool list again as it does, but after five in an hour it stops
// listening for about six hours (2.1.280: "the server keeps killing held
// streams; parking re-listen"); then a server upgraded meanwhile went
// unnoticed, and Claude Code checked the new answers against the old list
// (2026-11-23, a connection through the NAS's proxy cut every 60 s since
// 2026-11-19). While such a stream is quiet it carries a comment line every
// mcpEventStreamKeepAlive, which every event-stream reader skips; a stream
// that lasts is then cut only as the server stops, and the client opens it
// again on the new server and reads its lists again.

// mcpEventStreamKeepAlive is how often a held stream carries a comment —
// well within the 60 s most proxies wait (nginx's, Synology's, a cloud load
// balancer's).
const mcpEventStreamKeepAlive = 15 * time.Second

// mcpEventStreamComment is the comment: a line of its own between events,
// with no blank line after it, so a reader dispatches no event for it.
var mcpEventStreamComment = []byte(": keep-alive\n")

// mcpEventStreamsKeptAlive keeps the event streams its handler answers with
// alive.
func mcpEventStreamsKeptAlive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		kept := &mcpKeptAliveResponse{ResponseWriter: response}
		defer kept.end()
		next.ServeHTTP(kept, request)
	})
}

// mcpKeptAliveResponse writes a comment into an event stream while it is
// quiet. The library writes each event in one write; a comment goes only
// between two, never once the handler returned.
type mcpKeptAliveResponse struct {
	http.ResponseWriter
	mu      sync.Mutex
	between bool
	last    time.Time
	stop    chan struct{}
	ended   bool
}

func (response *mcpKeptAliveResponse) WriteHeader(status int) {
	response.mu.Lock()
	defer response.mu.Unlock()
	response.ResponseWriter.WriteHeader(status)
}

func (response *mcpKeptAliveResponse) Write(data []byte) (int, error) {
	response.mu.Lock()
	defer response.mu.Unlock()
	written, err := response.ResponseWriter.Write(data)
	response.between = bytes.HasSuffix(data[:written], []byte("\n\n"))
	response.last = time.Now()
	if response.stop == nil && !response.ended && mcpEventStreamAnswer(response.Header()) {
		response.stop = make(chan struct{})
		go response.keepAlive(response.stop)
	}
	return written, err
}

func (response *mcpKeptAliveResponse) Flush() {
	_ = response.FlushError()
}

func (response *mcpKeptAliveResponse) FlushError() error {
	response.mu.Lock()
	defer response.mu.Unlock()
	return http.NewResponseController(response.ResponseWriter).Flush()
}

func (response *mcpKeptAliveResponse) Unwrap() http.ResponseWriter {
	return response.ResponseWriter
}

func (response *mcpKeptAliveResponse) keepAlive(stop <-chan struct{}) {
	ticker := time.NewTicker(mcpEventStreamKeepAlive / 3)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		response.mu.Lock()
		if !response.ended && response.between && time.Since(response.last) >= mcpEventStreamKeepAlive {
			if _, err := response.ResponseWriter.Write(mcpEventStreamComment); err == nil {
				_ = http.NewResponseController(response.ResponseWriter).Flush()
			}
			response.last = time.Now()
		}
		response.mu.Unlock()
	}
}

// end stops the comments as the handler returns.
func (response *mcpKeptAliveResponse) end() {
	response.mu.Lock()
	defer response.mu.Unlock()
	response.ended = true
	if response.stop != nil {
		close(response.stop)
	}
}

// mcpEventStreamAnswer is whether an answer's header says it is an event
// stream.
func mcpEventStreamAnswer(header http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}
