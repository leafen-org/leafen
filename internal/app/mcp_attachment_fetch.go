package app

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errMCPAttachmentInvalid = errors.New("LEAFEN_MCP_ATTACHMENT_INVALID")

func fetchMCPAttachmentURL(
	ctx context.Context,
	rawURL, filename, expectedContentType string,
) ([]byte, string, error) {
	normalized, err := normalizeOutboundPublicURL(rawURL, 8192, true)
	if err != nil {
		return nil, "", errors.Join(errMCPAttachmentInvalid, err)
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme != "https" {
		return nil, "", errMCPAttachmentInvalid
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, "", errMCPAttachmentInvalid
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("User-Agent", "LeafenMCP/2")
	client := newMCPAttachmentHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return nil, "", errors.Join(errMCPAttachmentInvalid, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		response.ContentLength == 0 || response.ContentLength > mcpAttachmentURLMaximumBytes {
		return nil, "", errMCPAttachmentInvalid
	}
	contentType, err := validateMCPAttachmentResponseType(
		filename, expectedContentType, response.Header.Get("Content-Type"),
	)
	if err != nil {
		return nil, "", err
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, mcpAttachmentURLMaximumBytes+1))
	if err != nil || len(content) == 0 || len(content) > mcpAttachmentURLMaximumBytes {
		return nil, "", errors.Join(errMCPAttachmentInvalid, err)
	}
	return content, contentType, nil
}

func newMCPAttachmentHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	transport.MaxResponseHeaderBytes = 64 << 10
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: transport.IdleConnTimeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(
			ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext,
		)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func validateMCPAttachmentResponseType(
	filename, expectedContentType, responseContentType string,
) (string, error) {
	expected, err := normalizeStoredFileContentType(expectedContentType)
	if err != nil {
		return "", errors.Join(errMCPAttachmentInvalid, err)
	}
	responseContentType = strings.TrimSpace(responseContentType)
	if responseContentType == "" {
		return expected, nil
	}
	actual, _, err := mime.ParseMediaType(responseContentType)
	if err != nil {
		return "", errMCPAttachmentInvalid
	}
	actual, err = normalizeStoredFileContentType(actual)
	if err != nil {
		return "", errors.Join(errMCPAttachmentInvalid, err)
	}
	if expected != defaultFileContentType && actual != defaultFileContentType && actual != expected {
		return "", errMCPAttachmentInvalid
	}
	if expected == defaultFileContentType && actual != defaultFileContentType {
		expected = actual
	}
	admitted, err := admitStoredFileType(filename, expected)
	if err != nil {
		return "", errors.Join(errMCPAttachmentInvalid, err)
	}
	return admitted, nil
}
