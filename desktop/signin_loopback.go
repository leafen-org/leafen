//go:build !ios && !android

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// On a desktop the sign-in comes back to a loopback address the app listens
// on for that sign-in only, at a path no other sign-in shares (RFC 8252 7.3).
// The browser then shows a page that says to go back to the app; the app
// stops listening.

const signInReturnedPage = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Leafen</title>
<style>body{font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:0;display:grid;place-items:center;min-height:100vh;color:#37352f;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e6e5;background:#191919}}main{text-align:center;padding:24px}p{margin:4px 0}</style>
</head><body><main><p>You can close this tab and go back to Leafen.</p><p lang="ko">이 탭을 닫고 리펜으로 돌아가세요.</p></main></body></html>`

func listenForSignIn(id string, returned func(string)) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	path := "/leafen-sign-in/" + id
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	server.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != path {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = io.WriteString(response, signInReturnedPage)
		go returned(request.URL.RawQuery)
	})
	go func() { _ = server.Serve(listener) }()
	stop := func() {
		// The page the browser was given finishes first.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String() + path, stop, nil
}

func openSignIn(_ string, address string, _ func(string)) error {
	return application.Get().Browser.OpenURL(address)
}
