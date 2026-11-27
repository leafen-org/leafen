package app

import (
	"mime"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// A file's name in a response. The filesystem's Serve writes it quoted as it
// is, and a Korean name is then raw bytes in a header: WebKit — Safari, and
// the desktop and phone apps — refuses the whole response, so every file with
// such a name failed to open there (seen 2026-11-05: "Type error" in the
// document apps, while Word files named in English opened). Any name goes in
// the standard form (RFC 6266).

// contentDisposition says how a file is given and under what name.
func contentDisposition(kind, name string) string {
	if value := mime.FormatMediaType(kind, map[string]string{"filename": name}); value != "" {
		return value
	}
	return kind
}

// serveNamedFile is the filesystem's Serve, with the name written as above.
func serveNamedFile(fsys *filesystem.System, res http.ResponseWriter, req *http.Request, fileKey, name string) error {
	return fsys.Serve(namedFileWriter{ResponseWriter: res, name: name}, req, fileKey, name)
}

type namedFileWriter struct {
	http.ResponseWriter
	name string
}

func (w namedFileWriter) WriteHeader(status int) {
	header := w.Header()
	if value := header.Get("Content-Disposition"); value != "" {
		kind, _, _ := strings.Cut(value, ";")
		header.Set("Content-Disposition", contentDisposition(strings.TrimSpace(kind), w.name))
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w namedFileWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
