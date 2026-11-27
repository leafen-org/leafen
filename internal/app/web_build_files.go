package app

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/web"
)

// The app's build files go as small and as seldom as they can (2026-10-17:
// a first load is several megabytes of script, which took over two minutes
// through a speed-limited relay). Scripts, styles
// and other text go gzipped to a browser that takes it, compressed once and
// kept; files whose names carry their content's hash (assets/, fonts/) are
// kept by the browser for a year without asking again. A file is compressed
// as it is first asked for, so after an update the first page waits on it:
// at gzip's default level, about half the time of its best for files 0.2%
// larger (2026-11-18, on a NAS-class server the first page after an update
// showed in 2.9–3.3 s instead of 3.4–4.9 s).

var (
	webBuildGzipped  sync.Map // path → webBuildGzippedFile
	webBuildTextKind = map[string]bool{".js": true, ".mjs": true, ".css": true, ".json": true, ".svg": true,
		".html": true, ".txt": true, ".map": true, ".webmanifest": true, ".wasm": true}
)

// webBuildGzippedFile is a build file gzipped, kept with the size and time
// of the file it was made from. An embedded build never changes, so each of
// its files is compressed once; a development build that reads the web build
// from disk (web/dist_devweb.go) compresses a file anew once a fresh build
// has changed it.
type webBuildGzippedFile struct {
	size       int64
	modified   time.Time
	compressed []byte // nil when gzip wouldn't make it smaller
}

// The app's manifest goes as a manifest: Go knows no type for its name's
// ending, so it went as plain text.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// serveWebBuildFile serves a build file, or leaves it to serve when it isn't
// one this build has or there's nothing to add.
func serveWebBuildFile(request *core.RequestEvent, serve func(*core.RequestEvent) error) error {
	name := strings.TrimPrefix(path.Clean("/"+request.Request.URL.Path), "/")
	info, err := fs.Stat(web.Dist(), name)
	if err != nil || info.IsDir() {
		return serve(request)
	}
	header := request.Response.Header()
	if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "fonts/") {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	extension := strings.ToLower(path.Ext(name))
	if !webBuildTextKind[extension] || !acceptsGzip(request.Request) || request.Request.Header.Get("Range") != "" {
		return serve(request)
	}
	compressed := webBuildGzip(name, info)
	if compressed == nil {
		return serve(request)
	}
	header.Add("Vary", "Accept-Encoding")
	if kind := mime.TypeByExtension(extension); kind != "" {
		header.Set("Content-Type", kind)
	}
	header.Set("Content-Encoding", "gzip")
	header.Set("Content-Length", strconv.Itoa(len(compressed)))
	request.Response.WriteHeader(http.StatusOK)
	if request.Request.Method != http.MethodHead {
		_, _ = request.Response.Write(compressed)
	}
	return nil
}

// webBuildGzip is a build file gzipped, made the first time it's asked (or
// the first time since the file changed), or nil when gzip wouldn't make it
// smaller.
func webBuildGzip(name string, info fs.FileInfo) []byte {
	if kept, ok := webBuildGzipped.Load(name); ok {
		if file, _ := kept.(webBuildGzippedFile); file.size == info.Size() && file.modified.Equal(info.ModTime()) {
			return file.compressed
		}
	}
	content, err := fs.ReadFile(web.Dist(), name)
	if err != nil {
		return nil
	}
	var buffer bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&buffer, gzip.DefaultCompression)
	_, _ = writer.Write(content)
	_ = writer.Close()
	var compressed []byte
	if buffer.Len() < len(content)*9/10 {
		compressed = buffer.Bytes()
	}
	webBuildGzipped.Store(name, webBuildGzippedFile{size: info.Size(), modified: info.ModTime(), compressed: compressed})
	return compressed
}

func acceptsGzip(request *http.Request) bool {
	for _, part := range strings.Split(request.Header.Get("Accept-Encoding"), ",") {
		coding, quality, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.ReplaceAll(strings.TrimSpace(quality), " ", "") != "q=0" {
			return true
		}
	}
	return false
}
