//go:build !leafen_noapps

package app

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An endpoint's pages: a public endpoint that declares "pages" may answer
// with one of its package's site/ files — a web app of its own, such as the
// Vault app's web vault, beside the API the endpoint serves. Leafen sends the
// file as it is in the package (as <name>.gz when the package keeps it
// compressed), never a body the worker made, under a policy of its own: no
// origin (sandbox), so a page can't read Leafen's storage or cookies; scripts
// only from the endpoint's address; and a page's storage kept in memory,
// since a page with no origin has none. Requests such a page makes carry the
// origin "null", which the endpoint answers for, with credentials — its
// sign-in is the app's own bearer token, never Leafen's.

const (
	// A browser opens several requests at once; one waits this long for
	// another to finish before the endpoint says it is busy.
	appEndpointGateWait = 10 * time.Second
)

// appEndpointStorageShim stands in for localStorage and sessionStorage, which
// a page with no origin can't open, before the page's own scripts run.
const appEndpointStorageShim = `(()=>{const m=()=>{const i=new Map();return{get length(){return i.size},key:(n)=>[...i.keys()][n]??null,` +
	`getItem:(k)=>i.has(String(k))?i.get(String(k)):null,setItem:(k,v)=>{i.set(String(k),String(v))},removeItem:(k)=>{i.delete(String(k))},` +
	`clear:()=>i.clear()}};for(const n of["localStorage","sessionStorage"]){let ok=false;try{ok=Boolean(window[n])}catch{ok=false}` +
	`if(!ok)Object.defineProperty(window,n,{value:m(),configurable:true})}})()`

var appEndpointStorageShimHash = func() string {
	digest := sha256.Sum256([]byte(appEndpointStorageShim))
	return "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
}()

// appEndpointPagePolicy is a page's content security policy: no origin of
// its own, everything from the endpoint's address (the page's 'self'), the
// storage shim by its hash, WebAssembly, and nothing framing it.
func appEndpointPagePolicy() string {
	return strings.Join([]string{
		"sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads",
		"default-src 'self'",
		"script-src 'self' 'wasm-unsafe-eval' " + appEndpointStorageShimHash,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"media-src 'self' blob:",
		"connect-src 'self'",
		"worker-src 'self' blob:",
		"frame-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// appEndpointPageCORS answers a page's requests, which carry the origin
// "null": a preflight answered here, and the others marked for it. It says
// whether it answered the request whole.
func appEndpointPageCORS(event *core.RequestEvent, endpoint appManifestEndpoint) (bool, error) {
	if !endpoint.Pages || !endpoint.Public || event.Request.Header.Get("Origin") != "null" {
		return false, nil
	}
	header := event.Response.Header()
	header.Set("Access-Control-Allow-Origin", "null")
	header.Set("Access-Control-Allow-Credentials", "true")
	header.Add("Vary", "Origin")
	if event.Request.Method != http.MethodOptions || event.Request.Header.Get("Access-Control-Request-Method") == "" {
		return false, nil
	}
	methods := endpoint.Methods
	if len(methods) == 0 {
		methods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	}
	header.Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	if asked := event.Request.Header.Get("Access-Control-Request-Headers"); asked != "" && !strings.ContainsAny(asked, "\r\n") {
		header.Set("Access-Control-Allow-Headers", asked)
	}
	header.Set("Access-Control-Max-Age", "600")
	appEndpointHeaders(header)
	return true, event.NoContent(http.StatusNoContent)
}

// appEndpointServePage sends one of the package's site/ files as the
// worker named it.
func appEndpointServePage(event *core.RequestEvent, install storedAppInstall, endpoint appManifestEndpoint, name string, status int) error {
	clean := path.Clean("/" + name)[1:]
	if !endpoint.Pages || clean == "" || clean != name {
		return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen sends.")
	}
	full := appEndpointPagesFolder + clean
	compressed := true
	file, err := findAppPackageFile(event.App, install.PackageHash, full+".gz")
	if errors.Is(err, sql.ErrNoRows) {
		compressed = false
		file, err = findAppPackageFile(event.App, install.PackageHash, full)
	}
	if errors.Is(err, sql.ErrNoRows) {
		// Pages kept apart from the package come from their pack
		// (app_platform_page_packs.go).
		if pack, found := appPagePackFor(install.AppID, endpoint.Key); found {
			return appEndpointServePackPage(event, pack, clean, status)
		}
		return appEndpointPlain(event, http.StatusNotFound, "Not found.")
	}
	if err != nil {
		return appEndpointFailure(event, "Leafen couldn't read the app.", err)
	}
	html, accepts := appEndpointPageHeaders(event, clean)
	if compressed && !html && accepts {
		// Sent compressed as kept; the browser opens it.
		event.Response.Header().Set("Content-Encoding", "gzip")
		event.Response.Header().Add("Vary", "Accept-Encoding")
		return appEndpointSendPackageFile(event, file, status)
	}
	data := file.Data
	if !file.inRow() {
		if data, err = loadAppPackageFile(event.App, install.PackageHash, full+map[bool]string{true: ".gz"}[compressed]); err != nil {
			return appEndpointFailure(event, "Leafen couldn't read the app.", err)
		}
	}
	if compressed {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(reader, appPackageMaxFileBytes))
		}
		if err != nil {
			return appEndpointFailure(event, "Leafen couldn't read the app.", err)
		}
	}
	if html {
		data = appEndpointPageWithShim(data)
	}
	event.Response.WriteHeader(status)
	if event.Request.Method == http.MethodHead {
		return nil
	}
	_, err = event.Response.Write(data)
	return err
}

// appEndpointPageHeaders sets a page's headers, and says whether it is a
// document and whether the browser takes it compressed.
func appEndpointPageHeaders(event *core.RequestEvent, name string) (bool, bool) {
	contentType := appEndpointPageType(name)
	header := event.Response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Security-Policy", appEndpointPagePolicy())
	// To a page with no origin its own files are another origin's.
	header.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if header.Get("Cache-Control") == "private, no-store" {
		header.Set("Cache-Control", "no-cache")
	}
	accepts := slices.ContainsFunc(strings.Split(event.Request.Header.Get("Accept-Encoding"), ","), func(part string) bool {
		return strings.TrimSpace(strings.SplitN(part, ";", 2)[0]) == "gzip"
	})
	return strings.HasPrefix(contentType, "text/html"), accepts
}

// appEndpointWritePage sends a page held in memory, compressed or not.
func appEndpointWritePage(event *core.RequestEvent, name string, data []byte, compressed bool, status int) error {
	html, accepts := appEndpointPageHeaders(event, name)
	if compressed && (html || !accepts) {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(reader, appPackageMaxFileBytes))
		}
		if err != nil {
			return appEndpointFailure(event, "Leafen couldn't read the app.", err)
		}
		compressed = false
	}
	if compressed {
		event.Response.Header().Set("Content-Encoding", "gzip")
		event.Response.Header().Add("Vary", "Accept-Encoding")
	}
	if html {
		data = appEndpointPageWithShim(data)
	}
	event.Response.WriteHeader(status)
	if event.Request.Method == http.MethodHead {
		return nil
	}
	_, err := event.Response.Write(data)
	return err
}

// appEndpointSendPackageFile sends a kept file's bytes as they are.
func appEndpointSendPackageFile(event *core.RequestEvent, file storedAppPackageFile, status int) error {
	if file.inRow() {
		event.Response.WriteHeader(status)
		if event.Request.Method == http.MethodHead {
			return nil
		}
		_, err := event.Response.Write(file.Data)
		return err
	}
	fsys, reader, err := openAppPackageFile(event.App, file)
	if err != nil {
		return appEndpointFailure(event, "Leafen couldn't read the app.", err)
	}
	defer fsys.Close()
	defer reader.Close()
	event.Response.WriteHeader(status)
	if event.Request.Method == http.MethodHead {
		return nil
	}
	_, err = io.Copy(event.Response, reader)
	return err
}

// appEndpointPageWithShim puts the storage shim first in a page's head.
func appEndpointPageWithShim(page []byte) []byte {
	shim := []byte("<script>" + appEndpointStorageShim + "</script>")
	if at := bytes.Index(bytes.ToLower(page), []byte("<head>")); at >= 0 {
		at += len("<head>")
		return append(append(append([]byte{}, page[:at]...), shim...), page[at:]...)
	}
	return append(shim, page...)
}

// appEndpointPageType is a page file's type by its name.
func appEndpointPageType(name string) string {
	switch path.Ext(name) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".wasm":
		return "application/wasm"
	case ".svg":
		return "image/svg+xml"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	}
	if found := mime.TypeByExtension(path.Ext(name)); found != "" {
		return found
	}
	return "application/octet-stream"
}

// appEndpointFailure answers an endpoint that failed on the server's side
// as appEndpointPlain does, through the rules of every failure's answer
// (answeredFailure): logged with its cause, and told busy — 503 with
// Retry-After, the shared busy words — when the database stayed busy, so an
// app or a sync tool waits and sends it again.
func appEndpointFailure(event *core.RequestEvent, message string, cause error) error {
	status, _, said := answeredFailure(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, message, cause,
		diagnosticSourcePosition(1))
	return appEndpointPlain(event, status, said)
}
