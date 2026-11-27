package app

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The product's reads answer in JSON, and the page list of a large workspace
// is a lot of it: 33 MB for 40,000 pages, read in parts of 1,000, which a
// NAS's 20 Mbps took 23.5 s to send (2026-11-14). A browser that takes gzip
// (every one) gets a read's answer of a few kilobytes or more compressed —
// JSON shrinks about tenfold — and the read batch's answer, which goes out
// read by read as each is done, compressed as it goes, each read flushed.
// (The app's own files go gzipped too, web_build_files.go.) What is
// already encoded, a file's bytes, a part of one (Range), a socket
// and an answer too small to gain anything go as they are.

const (
	responseCompressionMinBytes = 4 << 10
	responseCompressionID       = "leafenResponseCompression"
)

// Fastest gzip: it keeps most of the gain on JSON's repeated names for a
// fraction of the default level's time, which a NAS's slow cores feel.
var responseGzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return writer
}}

func registerResponseCompression(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:   responseCompressionID,
		Func: compressReadAnswers,
	})
}

func compressReadAnswers(event *core.RequestEvent) error {
	request := event.Request
	if request.Method != http.MethodGet || !strings.HasPrefix(request.URL.Path, apiRoot+"/") ||
		request.Header.Get("Range") != "" || request.Header.Get("Upgrade") != "" ||
		!acceptsGzip(request) {
		return event.Next()
	}
	writer := &compressingResponse{ResponseWriter: event.Response}
	event.Response = writer
	defer func() {
		writer.finish()
		event.Response = writer.ResponseWriter
	}()
	return event.Next()
}

// compressionKind says how an answer of this type goes: "" as it is, "json"
// compressed once it is large enough, "stream" compressed as it goes.
func compressionKind(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	switch {
	case mediaType == readBatchContentType:
		return "stream"
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return "json"
	}
	return ""
}

// compressingResponse holds a JSON answer back until it is large enough to
// compress or ends, then sends it compressed or as it is.
type compressingResponse struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
	kind        string
	decided     bool
	buffer      []byte
	gzip        *gzip.Writer
	failed      error
}

func (w *compressingResponse) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if code < http.StatusOK {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wroteHeader, w.code = true, code
	header := w.Header()
	if code == http.StatusNoContent || code == http.StatusNotModified || code == http.StatusPartialContent ||
		header.Get("Content-Encoding") != "" {
		w.decide(false)
		return
	}
	w.kind = compressionKind(header.Get("Content-Type"))
	switch w.kind {
	case "":
		w.decide(false)
	case "stream":
		w.decide(true)
	}
}

// decide sends the status line, compressed or not, then what was held back.
func (w *compressingResponse) decide(compress bool) {
	if w.decided {
		return
	}
	w.decided = true
	header := w.Header()
	if w.kind != "" {
		header.Add("Vary", "Accept-Encoding")
	}
	if compress {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		w.gzip = responseGzipWriters.Get().(*gzip.Writer)
		w.gzip.Reset(w.ResponseWriter)
	}
	w.ResponseWriter.WriteHeader(w.code)
	if len(w.buffer) > 0 {
		held := w.buffer
		w.buffer = nil
		_, w.failed = w.out().Write(held)
	}
}

func (w *compressingResponse) out() io.Writer {
	if w.gzip != nil {
		return w.gzip
	}
	return w.ResponseWriter
}

func (w *compressingResponse) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.decided {
		w.buffer = append(w.buffer, data...)
		if len(w.buffer) >= responseCompressionMinBytes {
			w.decide(true)
		}
		if w.failed != nil {
			return 0, w.failed
		}
		return len(data), nil
	}
	if w.failed != nil {
		return 0, w.failed
	}
	return w.out().Write(data)
}

func (w *compressingResponse) Flush() {
	_ = w.FlushError()
}

// FlushError sends what is held back now, compressed when the answer is
// large enough so far.
func (w *compressingResponse) FlushError() error {
	if !w.decided {
		if !w.wroteHeader {
			w.WriteHeader(http.StatusOK)
		}
		w.decide(len(w.buffer) >= responseCompressionMinBytes)
	}
	if w.gzip != nil {
		if err := w.gzip.Flush(); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// finish sends what is still held back and ends the compressed stream.
func (w *compressingResponse) finish() {
	if !w.decided && (w.wroteHeader || len(w.buffer) > 0) {
		w.decide(false)
	}
	if w.gzip != nil {
		_ = w.gzip.Close()
		w.gzip.Reset(io.Discard)
		responseGzipWriters.Put(w.gzip)
		w.gzip = nil
	}
}

// Written and Status answer as the router's own writer does, for what is
// held back too.
func (w *compressingResponse) Written() bool {
	return w.wroteHeader || len(w.buffer) > 0
}

func (w *compressingResponse) Status() int {
	return w.code
}

func (w *compressingResponse) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
