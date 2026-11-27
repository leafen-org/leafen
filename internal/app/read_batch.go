package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A page's start asks for some thirty reads at once, and over a plain http
// address (HTTP/1.1) a browser sends at most six at a time to one server,
// each a whole round trip: on a NAS's address a database page's reads
// waited up to 370 ms for a connection, and the start's last answer came
// 1.3 s in (2026-11-13). The app sends the reads it asks for together as one
// request (web/src/lib/readBatch.ts): GET /read-batch?read=/bootstrap&read=…
// The server runs each as the request it would have been alone — the same
// route, sign-in, key rules, limits and turns, from the same address — and
// answers each as soon as it is done, in the order they finish:
//
//	{"read":2,"status":200,"headers":{…},"length":1234}\n<1234 bytes of its body>
//
// A read it doesn't hold — a stream, a file's bytes — is answered at once
// with status 0, and the app sends it alone.
//
// A line of its own (a newline) now and then while a read takes long keeps
// the answer from looking stalled to the app (lib/requestDeadline.ts).

const (
	readBatchPath        = "/read-batch"
	readBatchContentType = "application/x-leafen-read-batch"
	errorCodeReadBatch   = "LEAFEN_READ_BATCH_INVALID"
	// At most this many reads in one request, and this many of them worked
	// on at once: a browser's six connections were six at once too.
	readBatchMost   = 40
	readBatchAtOnce = 6
	readBatchBeat   = 10 * time.Second
)

// A read's answer keeps the headers the app reads; how it went over the
// wire is the batch's own.
var readBatchDroppedHeaders = map[string]bool{"Content-Length": true, "Content-Encoding": true, "Transfer-Encoding": true,
	"Connection": true, "Keep-Alive": true, "Vary": true, "Date": true}

type readBatchAnswer struct {
	Read    int               `json:"read"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Length  int               `json:"length"`
}

func registerReadBatchRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+readBatchPath, serveReadBatch)
}

// readBatchReads are the reads a batch names, each a path under apiRoot
// with its query: none outside it, nor the batch itself.
func readBatchReads(values []string) ([]*url.URL, bool) {
	if len(values) == 0 || len(values) > readBatchMost {
		return nil, false
	}
	reads := make([]*url.URL, 0, len(values))
	for _, value := range values {
		read, err := url.Parse(value)
		if err != nil || read.Scheme != "" || read.Host != "" || read.Fragment != "" || read.User != nil ||
			!strings.HasPrefix(read.Path, "/") || strings.HasPrefix(read.Path, "//") ||
			path.Clean(read.Path) != read.Path || read.Path == readBatchPath {
			return nil, false
		}
		reads = append(reads, read)
	}
	return reads, true
}

func serveReadBatch(event *core.RequestEvent) error {
	reads, ok := readBatchReads(event.Request.URL.Query()["read"])
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeReadBatch,
			"A read batch names one to forty reads, each a path of this API.")
	}
	mux, _, err := nativeOperationRuntime()
	if err != nil {
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeServerBusy,
			"The server is starting. Try again in a moment.", "read_batch", "router", errorCodeServerBusy, err)
	}
	// Each read is its own request: the batch's headers and address, none of
	// its context's values, ended when the batch's sender goes.
	outer := event.Request
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer context.AfterFunc(outer.Context(), cancel)()
	header := event.Response.Header()
	header.Set("Content-Type", readBatchContentType)
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	event.Response.WriteHeader(http.StatusOK)
	control := http.NewResponseController(event.Response)
	_ = control.Flush()

	var writing sync.Mutex
	write := func(chunks ...[]byte) {
		writing.Lock()
		defer writing.Unlock()
		for _, chunk := range chunks {
			if _, err := event.Response.Write(chunk); err != nil {
				cancel()
				return
			}
		}
		_ = control.Flush()
	}
	beat := time.NewTicker(readBatchBeat)
	defer beat.Stop()
	done, beaten := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(beaten)
		for {
			select {
			case <-beat.C:
				write([]byte("\n"))
			case <-done:
				return
			}
		}
	}()
	turns := make(chan struct{}, readBatchAtOnce)
	var all sync.WaitGroup
	for index, read := range reads {
		select {
		case turns <- struct{}{}:
		case <-base.Done():
		}
		if base.Err() != nil {
			break
		}
		all.Add(1)
		go func() {
			defer all.Done()
			defer func() { <-turns }()
			// A stream or a file's bytes goes as it is, never held whole
			// (requestAdmitted): the app is told to send it alone.
			if !requestAdmitted(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: apiRoot + read.Path}, Header: http.Header{}}) {
				line, _ := json.Marshal(readBatchAnswer{Read: index})
				write(append(line, '\n'))
				return
			}
			status, headers, body := readBatchOne(base, mux, outer, read)
			line, _ := json.Marshal(readBatchAnswer{Read: index, Status: status, Headers: headers, Length: len(body)})
			write(append(line, '\n'), body)
		}()
	}
	all.Wait()
	close(done)
	<-beaten
	return nil
}

// readBatchOne runs one read through the server's router as if it had come
// alone.
func readBatchOne(ctx context.Context, mux http.Handler, outer *http.Request, read *url.URL) (int, map[string]string, []byte) {
	target := *read
	target.Path = apiRoot + read.Path
	if read.RawPath != "" {
		target.RawPath = apiRoot + read.RawPath
	}
	inner, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return http.StatusBadRequest, nil, nil
	}
	inner.Header = outer.Header.Clone()
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Encoding", "Accept-Encoding", "Range",
		"If-None-Match", "If-Modified-Since", "Connection", "Upgrade", "Te", "Trailer", "Transfer-Encoding"} {
		inner.Header.Del(name)
	}
	inner.RemoteAddr, inner.Host, inner.TLS = outer.RemoteAddr, outer.Host, outer.TLS
	inner.Proto, inner.ProtoMajor, inner.ProtoMinor = outer.Proto, outer.ProtoMajor, outer.ProtoMinor
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, inner)
	headers := map[string]string{}
	for name, values := range recorder.Header() {
		if !readBatchDroppedHeaders[name] && !strings.HasPrefix(name, "Access-Control-") {
			headers[name] = strings.Join(values, ", ")
		}
	}
	return recorder.Code, headers, recorder.Body.Bytes()
}
