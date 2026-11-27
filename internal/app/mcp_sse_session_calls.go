package app

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// A session over server-sent events takes its calls on posts apart from its
// stream and answers each on the stream: the post is answered 202 once the
// session has the call, its answer comes later. A closing server ended the
// stream as it stopped its parts, at once, and a call the session had —
// under way as the server began to stop, or posted as it did — was never
// answered: its client saw the stream end (2026-11-18: none of 3 calls
// answered 202 was answered on the stream; 3 posted a little later were told
// the session was closed).
//
// So once the server is stopping, a post is told busy before the session
// takes it, as any request that comes then is (server_stopping.go), and its
// client sends it again once the server is back; its body is read whole
// first, since a post let in before the stop may finish arriving after it.
// And a stream the closing server ends stays open until the calls its
// session took are answered on it — each as its tools end it, a write of it
// stopped with the requests' (request_write_stop.go) — or the server's time
// runs out, and closes then.

// mcpSSECalls are the calls a session took and has not answered on its
// stream yet, by their IDs.
type mcpSSECalls struct {
	mu      sync.Mutex
	pending map[string]int
	idle    chan struct{} // closed while none is pending
	ended   chan struct{} // closed once the stream ended
	end     func()
}

func newMCPSSECalls() *mcpSSECalls {
	idle := make(chan struct{})
	close(idle)
	ended := make(chan struct{})
	return &mcpSSECalls{pending: map[string]int{}, idle: idle, ended: ended, end: sync.OnceFunc(func() { close(ended) })}
}

func (calls *mcpSSECalls) take(id string) {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if len(calls.pending) == 0 {
		calls.idle = make(chan struct{})
	}
	calls.pending[id]++
}

func (calls *mcpSSECalls) answered(id string) {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	count, ok := calls.pending[id]
	if !ok {
		return
	}
	if count > 1 {
		calls.pending[id] = count - 1
		return
	}
	delete(calls.pending, id)
	if len(calls.pending) == 0 {
		close(calls.idle)
	}
}

// settled is closed once the calls taken so far are answered or the stream
// ended; once no call can be taken any more, it stays so.
func (calls *mcpSSECalls) settled() <-chan struct{} {
	calls.mu.Lock()
	idle := calls.idle
	calls.mu.Unlock()
	settled := make(chan struct{})
	go func() {
		defer close(settled)
		select {
		case <-idle:
		case <-calls.ended:
		}
	}()
	return settled
}

// mcpSSECallID is the ID of the call a post carries, as its answer names it.
func mcpSSECallID(body []byte) (string, bool) {
	message, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		return "", false
	}
	request, ok := message.(*jsonrpc.Request)
	if !ok || !request.ID.IsValid() {
		return "", false
	}
	return mcpSSEIDKey(request.ID), true
}

func mcpSSEIDKey(id jsonrpc.ID) string {
	return fmt.Sprintf("%T:%v", id.Raw(), id.Raw())
}

// mcpSSEStream is a session's stream: each event the library sends is one
// write, and an answer to a call its session took is noted as it goes.
type mcpSSEStream struct {
	http.ResponseWriter
	calls *mcpSSECalls
}

func (stream *mcpSSEStream) Write(event []byte) (int, error) {
	written, err := stream.ResponseWriter.Write(event)
	if id, ok := mcpSSEAnswerID(event); ok {
		stream.calls.answered(id)
	}
	return written, err
}

func (stream *mcpSSEStream) Flush() {
	if flusher, ok := stream.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (stream *mcpSSEStream) Unwrap() http.ResponseWriter { return stream.ResponseWriter }

// mcpSSEAnswerID is the call an event answers, if it is an answer.
func mcpSSEAnswerID(event []byte) (string, bool) {
	start := bytes.Index(event, []byte("data: "))
	if start < 0 || !bytes.HasPrefix(event, []byte("event: message\n")) {
		return "", false
	}
	data := bytes.TrimRight(event[start+len("data: "):], "\n")
	message, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		return "", false
	}
	response, ok := message.(*jsonrpc.Response)
	if !ok || !response.ID.IsValid() {
		return "", false
	}
	return mcpSSEIDKey(response.ID), true
}

// mcpSSEPost is a post's answer, its status kept.
type mcpSSEPost struct {
	http.ResponseWriter
	status int
}

func (post *mcpSSEPost) WriteHeader(status int) {
	if post.status == 0 {
		post.status = status
	}
	post.ResponseWriter.WriteHeader(status)
}

func (post *mcpSSEPost) Write(body []byte) (int, error) {
	if post.status == 0 {
		post.status = http.StatusOK
	}
	return post.ResponseWriter.Write(body)
}

func (post *mcpSSEPost) Unwrap() http.ResponseWriter { return post.ResponseWriter }

// servePOST hands a message posted to a session to it, unless the server is
// stopping: then the post is told busy and none of it runs.
func (runtime *mcpSSESessionRuntime) servePOST(response http.ResponseWriter, request *http.Request, authority mcpAuthority) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "failed to read body", http.StatusBadRequest)
		return
	}
	id, call := mcpSSECallID(body)
	handler, calls, found, stopping := runtime.take(authority, id, call)
	switch {
	case stopping:
		if !turnMCPRequestAwayBusy(request) {
			response.Header().Set("Retry-After", requestAdmissionRetryAfter)
			writeMCPHTTPError(response, http.StatusServiceUnavailable, errorCodeServerBusy)
		}
		return
	case !found:
		http.Error(response, "session not found", http.StatusNotFound)
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	post := &mcpSSEPost{ResponseWriter: response}
	handler.ServeHTTP(post, request)
	if call && post.status != http.StatusAccepted {
		// Not taken: no answer comes on the stream.
		calls.answered(id)
	}
}

// take is the session a post goes to, the call it carries noted as taken,
// and whether the server is stopping, when none is.
func (runtime *mcpSSESessionRuntime) take(authority mcpAuthority, id string, call bool) (http.Handler, *mcpSSECalls, bool, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed || serverStopping(runtime.app) {
		return nil, nil, false, true
	}
	session, ok := runtime.sessions[authority.ConnectionID]
	if !ok || session.credentialHash != authority.CredentialHash {
		return nil, nil, false, false
	}
	if call {
		session.calls.take(id)
	}
	return session.handler, session.calls, true, false
}
