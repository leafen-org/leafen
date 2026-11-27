package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A PDF a page links to shows its first page in the block, as Notion's does,
// but the browser's PDF viewer draws nothing inside the isolated frame an
// outside page gets. The server reads the linked PDF for a signed-in reader
// the way it reads a link's metadata — public addresses only, redirects
// checked hop by hop, within a size and time limit — and hands back only a
// file that begins as a PDF; the page shows it as it shows an uploaded one.

var errURLMetadataNotPdf = errors.New(errorCodeURLMetadataNotPdf)

const linkedPdfHeaderScanBytes = 1024

func readLinkedPdf(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := urlMetadataRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeURLMetadataInvalidURL,
			refusalMessage("Check the public HTTP(S) URL.", bodyRefusalReason("PDF to read", err)))
	}
	if strings.TrimSpace(input.URL) == "" {
		return refuseMissingInput(event, errorCodeURLMetadataInvalidURL, "url", "the public http or https address of the PDF.")
	}
	release, refused, admitted := admitOutboundFetch(event, user.Id)
	if !admitted {
		return refused
	}
	defer release()
	pdf, err := fetchLinkedPdf(event.Request.Context(), newLinkedPdfHTTPClient(), input.URL)
	if err != nil {
		if errors.Is(err, errURLMetadataNotPdf) {
			return apiErrorWithCause(event, http.StatusUnprocessableEntity, errorCodeURLMetadataNotPdf,
				"The URL is not a PDF.", "url-metadata", "pdf", errorCodeURLMetadataNotPdf, err)
		}
		return urlMetadataRouteError(event, err)
	}
	header := event.Response.Header()
	header.Set("Content-Type", "application/pdf")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox")
	header.Set("Cache-Control", "private, max-age=300")
	defer pdf.close()
	// The PDF goes on as it arrives instead of whole in memory (2026-10-18).
	// One longer than the limit without saying its length is cut off as a
	// failed transfer rather than handed over shortened.
	if pdf.length >= 0 {
		header.Set("Content-Length", strconv.FormatInt(pdf.length, 10))
	}
	event.Response.WriteHeader(http.StatusOK)
	if _, err := event.Response.Write(pdf.head); err != nil {
		return nil
	}
	copied, err := io.Copy(event.Response, io.LimitReader(pdf.body, urlMetadataMaxPdfBytes-int64(len(pdf.head))+1))
	if err == nil && int64(len(pdf.head))+copied > urlMetadataMaxPdfBytes {
		panic(http.ErrAbortHandler)
	}
	return nil
}

// linkedPdf is a linked PDF whose first bytes were read and checked; the
// rest is read as it is sent on.
type linkedPdf struct {
	head   []byte
	body   io.Reader
	length int64
	close  func()
}

func newLinkedPdfHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	transport.MaxResponseHeaderBytes = urlMetadataMaxResponseHeaderBytes
	phaseTimeout := time.Duration(urlMetadataPhaseTimeoutMilliseconds) * time.Millisecond
	transport.ResponseHeaderTimeout = phaseTimeout
	transport.TLSHandshakeTimeout = phaseTimeout
	dialer := &net.Dialer{Timeout: phaseTimeout, KeepAlive: transport.IdleConnTimeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(
			ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext,
		)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(urlMetadataPdfTimeoutMilliseconds) * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func fetchLinkedPdf(ctx context.Context, client *http.Client, rawURL string) (pdf *linkedPdf, err error) {
	current, err := normalizeURLMetadataURL(rawURL)
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Duration(urlMetadataPdfTimeoutMilliseconds)*time.Millisecond)
	defer func() {
		if pdf == nil {
			cancel()
			client.CloseIdleConnections()
		}
	}()
	for redirects := 0; ; redirects++ {
		request, requestErr := http.NewRequestWithContext(requestContext, http.MethodGet, current, nil)
		if requestErr != nil {
			return nil, fmt.Errorf("%w: %v", errURLMetadataInvalidURL, requestErr)
		}
		request.Header.Set("Accept", "application/pdf,*/*;q=0.5")
		request.Header.Set("User-Agent", "LeafenBot/1.0")
		response, responseErr := client.Do(request)
		if responseErr != nil {
			return nil, classifyURLMetadataFetchError(requestContext, responseErr)
		}
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			location := strings.TrimSpace(response.Header.Get("Location"))
			_ = response.Body.Close()
			if redirects >= urlMetadataMaxRedirects {
				return nil, errURLMetadataRedirectLimit
			}
			if location == "" {
				return nil, errURLMetadataFetchFailed
			}
			if current, err = resolveURLMetadataRedirect(current, location); err != nil {
				return nil, err
			}
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			_ = response.Body.Close()
			return nil, errURLMetadataFetchFailed
		}
		if response.ContentLength > urlMetadataMaxPdfBytes {
			_ = response.Body.Close()
			return nil, errURLMetadataResponseTooLarge
		}
		head := make([]byte, linkedPdfHeaderScanBytes)
		read, readErr := io.ReadFull(response.Body, head)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			_ = response.Body.Close()
			return nil, classifyURLMetadataFetchError(requestContext, readErr)
		}
		head = head[:read]
		if !bytes.Contains(head, []byte("%PDF-")) {
			_ = response.Body.Close()
			return nil, errURLMetadataNotPdf
		}
		body := response.Body
		return &linkedPdf{head: head, body: body, length: response.ContentLength, close: func() {
			_ = body.Close()
			cancel()
			client.CloseIdleConnections()
		}}, nil
	}
}
