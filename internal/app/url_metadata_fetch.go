package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

var (
	errURLMetadataInvalidURL        = errors.New(errorCodeURLMetadataInvalidURL)
	errURLMetadataUnsafeDestination = errors.New(errorCodeURLMetadataUnsafeDestination)
	errURLMetadataRedirectLimit     = errors.New(errorCodeURLMetadataRedirectLimit)
	errURLMetadataResponseTooLarge  = errors.New(errorCodeURLMetadataResponseTooLarge)
	errURLMetadataTimeout           = errors.New(errorCodeURLMetadataTimeout)
	errURLMetadataFetchFailed       = errors.New(errorCodeURLMetadataFetchFailed)
	errURLMetadataParserLimit       = errors.New(errorCodeURLMetadataParserLimit)
	// The address answered that nothing is there (404, 410) or its host has
	// no address at all: Notion's picture says "couldn't find" for both, with
	// error 404 (seen 2026-11-12).
	errURLMetadataNotFound = errors.New(errorCodeURLMetadataNotFound)
)

type urlMetadataFetcher struct {
	client *http.Client
}

func normalizeURLMetadataURL(raw string) (string, error) {
	normalized, err := normalizeOutboundPublicURL(raw, urlMetadataMaxURLChars, true)
	if err == nil {
		return normalized, nil
	}
	if errors.Is(err, errOutboundUnsafeDestination) {
		return "", refuse(errURLMetadataUnsafeDestination, "%s", refusalReason(err))
	}
	return "", refuse(errURLMetadataInvalidURL, "%s", refusalReason(err))
}

func newURLMetadataFetcher() *urlMetadataFetcher {
	return newURLMetadataFetcherWithClient(newURLMetadataHTTPClient())
}

func newURLMetadataFetcherWithClient(client *http.Client) *urlMetadataFetcher {
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &urlMetadataFetcher{client: client}
}

func newURLMetadataHTTPClient() *http.Client {
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
		Timeout:   time.Duration(urlMetadataRequestTimeoutMilliseconds) * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (fetcher *urlMetadataFetcher) CloseIdleConnections() {
	fetcher.client.CloseIdleConnections()
}

func (fetcher *urlMetadataFetcher) Fetch(ctx context.Context, rawURL string) (urlMetadata, error) {
	current, err := normalizeURLMetadataURL(rawURL)
	if err != nil {
		return urlMetadata{}, err
	}
	requestContext, cancel := context.WithTimeout(
		ctx, time.Duration(urlMetadataRequestTimeoutMilliseconds)*time.Millisecond,
	)
	defer cancel()
	for redirects := 0; ; redirects++ {
		request, requestErr := http.NewRequestWithContext(requestContext, http.MethodGet, current, nil)
		if requestErr != nil {
			return urlMetadata{}, fmt.Errorf("%w: %v", errURLMetadataInvalidURL, requestErr)
		}
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/*;q=0.8")
		request.Header.Set("User-Agent", "LeafenBot/1.0")
		response, responseErr := fetcher.client.Do(request)
		if responseErr != nil {
			return urlMetadata{}, classifyURLMetadataFetchError(requestContext, responseErr)
		}
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			location := strings.TrimSpace(response.Header.Get("Location"))
			_ = response.Body.Close()
			if redirects >= urlMetadataMaxRedirects {
				return urlMetadata{}, errURLMetadataRedirectLimit
			}
			if location == "" {
				return urlMetadata{}, errURLMetadataFetchFailed
			}
			next, resolveErr := resolveURLMetadataRedirect(current, location)
			if resolveErr != nil {
				return urlMetadata{}, resolveErr
			}
			current = next
			continue
		}
		metadata, oEmbed, readErr := readURLMetadataResponse(response, current)
		closeErr := response.Body.Close()
		if readErr != nil {
			return urlMetadata{}, readErr
		}
		if closeErr != nil {
			return urlMetadata{}, fmt.Errorf("%w: %v", errURLMetadataFetchFailed, closeErr)
		}
		if oEmbed != "" && urlMetadataWantsVideoSize(ctx) {
			metadata.VideoWidth, metadata.VideoHeight = fetcher.readOEmbedVideoSize(requestContext, oEmbed)
		}
		// A Wikipedia article is named and described by Wikipedia's own
		// summary of it, as Notion's card names one (readWikipediaSummary).
		summary := fetcher.readWikipediaSummary(requestContext, current)
		if summary.Title != "" {
			metadata.Title = truncateURLMetadataText(summary.Title, urlMetadataMaxTitleRunes)
		}
		if summary.Extract != "" {
			metadata.Description = truncateURLMetadataText(summary.Extract, urlMetadataMaxDescriptionRunes)
		}
		// The site's /favicon.ico, which a page naming no icon is given, is
		// kept only where it answers: a site with none has no icon, as
		// Notion's mention and card of example.com have none (seen
		// 2026-11-11), rather than a broken picture.
		if urlMetadataSiteFavicon(metadata.IconURL) && !fetcher.answers(requestContext, metadata.IconURL) {
			metadata.IconURL = ""
		}
		return metadata, nil
	}
}

// urlMetadataSiteFavicon reports whether an icon is a site's own
// /favicon.ico (buildURLMetadata's last choice).
func urlMetadataSiteFavicon(icon string) bool {
	parsed, err := url.Parse(icon)
	return err == nil && icon != "" && parsed.Path == "/favicon.ico" && parsed.RawQuery == ""
}

// answers reports whether a public address gives a page, through the same
// guarded redirects a read takes.
func (fetcher *urlMetadataFetcher) answers(ctx context.Context, address string) bool {
	current, err := normalizeURLMetadataURL(address)
	if err != nil {
		return false
	}
	for redirects := 0; redirects <= urlMetadataMaxRedirects; redirects++ {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if requestErr != nil {
			return false
		}
		request.Header.Set("Accept", "image/*")
		request.Header.Set("User-Agent", "LeafenBot/1.0")
		response, responseErr := fetcher.client.Do(request)
		if responseErr != nil {
			return false
		}
		_ = response.Body.Close()
		if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
			return response.StatusCode == http.StatusOK
		}
		next, resolveErr := resolveURLMetadataRedirect(current, strings.TrimSpace(response.Header.Get("Location")))
		if resolveErr != nil {
			return false
		}
		current = next
	}
	return false
}

func resolveURLMetadataRedirect(current, location string) (string, error) {
	base, err := url.Parse(current)
	if err != nil {
		return "", errURLMetadataInvalidURL
	}
	reference, err := url.Parse(location)
	if err != nil {
		return "", refuse(errURLMetadataInvalidURL, "it redirects to %q, which isn't an address", location)
	}
	target := base.ResolveReference(reference).String()
	next, err := normalizeURLMetadataURL(target)
	if err != nil {
		// The reason is the redirect's, not the URL given.
		return "", refusalAt(err, "it redirects to "+target+" — ")
	}
	return next, nil
}

func readURLMetadataResponse(response *http.Response, finalURL string) (urlMetadata, string, error) {
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return urlMetadata{}, "", errURLMetadataNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return urlMetadata{}, "", errURLMetadataFetchFailed
	}
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if !urlMetadataContentTypeAdmitted(contentType) {
		metadata, oEmbed, err := parseURLMetadataPage(strings.NewReader(""), finalURL)
		metadata.ContentType = urlMetadataMediaType(contentType)
		return metadata, oEmbed, err
	}
	// A page's card comes from its head, so a long page is read as far as the
	// limit and described from that, as Notion's card describes it (seen
	// 2026-11-07: github.com's 525 KB home page had its title, words and
	// picture there, where Leafen refused the page and showed its address).
	raw, err := readURLMetadataPrefix(response.Body, urlMetadataMaxDecodedBodyBytes)
	if err != nil {
		return urlMetadata{}, "", err
	}
	decoded := io.Reader(bytes.NewReader(raw))
	if contentType != "" {
		decoded, err = charset.NewReader(bytes.NewReader(raw), contentType)
		if err != nil {
			return urlMetadata{}, "", fmt.Errorf("%w: %v", errURLMetadataFetchFailed, err)
		}
	}
	converted, err := readURLMetadataPrefix(decoded, urlMetadataMaxDecodedBodyBytes)
	if err != nil {
		return urlMetadata{}, "", err
	}
	metadata, oEmbed, err := parseURLMetadataPage(bytes.NewReader(converted), finalURL)
	metadata.ContentType = urlMetadataMediaType(contentType)
	return metadata, oEmbed, err
}

// readURLMetadataPrefix reads a page as far as the limit; what lies past it
// is not read.
func readURLMetadataPrefix(reader io.Reader, limit int) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errURLMetadataFetchFailed, err)
	}
	return value, nil
}

// urlMetadataMediaType is what a link answers it is, without its parameters
// ("image/svg+xml" of "image/svg+xml; charset=utf-8"): the editor makes a
// link given in a picture's window a picture by it (mediaLinkBlock.ts).
func urlMetadataMediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return mediaType
}

func urlMetadataContentTypeAdmitted(value string) bool {
	if value == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mediaType, "text/") || strings.Contains(mediaType, "html") || strings.Contains(mediaType, "xml")
}

func classifyURLMetadataFetchError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", errURLMetadataTimeout, err)
	}
	if errors.Is(err, errOutboundUnsafeDestination) {
		return &requestRefusal{kind: fmt.Errorf("%w: %v", errURLMetadataUnsafeDestination, err),
			reason: "its host's name doesn't lead to a public internet address"}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return fmt.Errorf("%w: %v", errURLMetadataTimeout, err)
	}
	var lookupError *net.DNSError
	if errors.As(err, &lookupError) && lookupError.IsNotFound {
		return fmt.Errorf("%w: %v", errURLMetadataNotFound, err)
	}
	return fmt.Errorf("%w: %v", errURLMetadataFetchFailed, err)
}
