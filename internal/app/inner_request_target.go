package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
)

// A request the server makes to its own router — an app worker's or an
// agent's native call, a Notion bundle's — takes its path from whoever
// calls: an app that put a person's words in it ("/pages/North yard") or
// left a bare "%". Built as it came, such a request line didn't parse and
// building it stopped the call midway (2026-11-16: an app's run stayed
// running and kept its worker's place until the server restarted). Every
// character a request line can't carry is written as its %XX, which the
// router reads back as the same path; the call answers as for any other path.

// errInnerRequestMethod refuses a method that isn't one word of letters.
var errInnerRequestMethod = errors.New("the method is an HTTP method, such as GET or POST")

// innerRequestTarget is target with every byte a request line can't carry
// written as %XX: spaces and controls, letters past ASCII, '#', a '%' that
// doesn't begin an escape, and the few marks a URL never holds bare. The
// path's '/' and the query's '?', '&' and '=' stay as they are.
func innerRequestTarget(target string) string {
	var escaped strings.Builder
	for index := 0; index < len(target); index++ {
		char := target[index]
		if char == '%' && index+2 < len(target) && innerRequestHexDigit(target[index+1]) && innerRequestHexDigit(target[index+2]) {
			escaped.WriteByte(char)
			continue
		}
		if innerRequestTargetKeeps(char) {
			escaped.WriteByte(char)
			continue
		}
		const hex = "0123456789ABCDEF"
		escaped.WriteByte('%')
		escaped.WriteByte(hex[char>>4])
		escaped.WriteByte(hex[char&15])
	}
	return escaped.String()
}

// innerRequestTargetKeeps is whether a byte stands bare in a request target:
// letters, digits and the marks RFC 3986 allows in a path and query.
func innerRequestTargetKeeps(char byte) bool {
	switch {
	case 'a' <= char && char <= 'z', 'A' <= char && char <= 'Z', '0' <= char && char <= '9':
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@/?", char) >= 0
}

func innerRequestHexDigit(char byte) bool {
	return '0' <= char && char <= '9' || 'a' <= char && char <= 'f' || 'A' <= char && char <= 'F'
}

// newInnerRequest is a request to the server's own router for method and
// target (escaped by innerRequestTarget), or an error for a method that
// isn't an HTTP method's one word.
func newInnerRequest(ctx context.Context, method, target string, body []byte) (*http.Request, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || strings.IndexFunc(method, func(char rune) bool { return char < 'A' || char > 'Z' }) >= 0 {
		return nil, errInnerRequestMethod
	}
	request := httptest.NewRequestWithContext(ctx, method, innerRequestTarget(target), bytes.NewReader(body))
	// It arrives where its outer request did (inner_request_origin.go).
	arriveInnerRequest(request, innerRequestArrivalOf(ctx))
	return request, nil
}
