package app

import (
	"bufio"
	"io"
	"mime"
	"net"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Leafen's JSON answers say they are UTF-8: Content-Type: application/json;
// charset=utf-8. JSON is UTF-8 whatever its header says (RFC 8259), and
// browsers, MCP clients (the MCP SDK reads the media type alone) and the
// Notion SDK read it so (its own API said the charset already); but Windows PowerShell
// 5.1's Invoke-RestMethod reads an answer whose type names no charset as
// ISO-8859-1, so a workspace named “June’s 스튜디오” came out as "âJuneâs
// ì¤íëì¤" in the connection text's own lines and in every call an agent
// makes from Windows PowerShell (2026-11-25, on Windows PowerShell 5.1). The
// charset is said here, at one place outside every other writer, for each
// JSON answer that names none — PocketBase's e.JSON, Leafen's own writers,
// and the router's answer to an error that no writer answered. Another
// type stays as it was set: a file's own (model/gltf+json), the web app's
// manifest (application/manifest+json).

const responseJSONCharsetID = "leafenResponseJSONCharset"

// responseJSONCharsetPriority runs it outside every other writer, the API
// error form's included (api_error_form.go).
const responseJSONCharsetPriority = apiErrorFormPriority - 1

func registerResponseJSONCharset(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: responseJSONCharsetID, Priority: responseJSONCharsetPriority, Func: sayJSONCharset,
	})
}

func sayJSONCharset(event *core.RequestEvent) error {
	// A socket's upgrade answers nothing in JSON once it is taken over.
	if event.Request.Header.Get("Upgrade") != "" {
		return event.Next()
	}
	writer := &jsonCharsetResponse{ResponseWriter: event.Response}
	event.Response = writer
	err := event.Next()
	event.Response = writer.ResponseWriter
	// The router answers an error no writer answered in JSON, keeping a type
	// already set: that answer says its charset too.
	if err != nil && !writer.wroteHeader && writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	return err
}

// withJSONCharset adds charset=utf-8 to application/json naming no charset.
func withJSONCharset(header http.Header) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || params["charset"] != "" {
		return
	}
	params["charset"] = "utf-8"
	header.Set("Content-Type", mime.FormatMediaType(mediaType, params))
}

// jsonCharsetResponse says the charset as the status line goes out, after
// every writer inside it has set its type; flushing, taking the connection
// over and sending a file go on through it as through the router's own.
type jsonCharsetResponse struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *jsonCharsetResponse) WriteHeader(status int) {
	if !w.wroteHeader && status >= http.StatusOK {
		w.wroteHeader = true
		withJSONCharset(w.Header())
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *jsonCharsetResponse) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *jsonCharsetResponse) ReadFrom(reader io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return io.Copy(w.ResponseWriter, reader)
}

func (w *jsonCharsetResponse) Flush() {
	_ = w.FlushError()
}

func (w *jsonCharsetResponse) FlushError() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *jsonCharsetResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *jsonCharsetResponse) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
