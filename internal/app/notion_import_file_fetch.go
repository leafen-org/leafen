package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type notionFetchedFile struct {
	Path        string
	Name        string
	ContentType string
	Size        int64
	SHA256      string
}

const notionFileSourcePrefix = "leafen-notion-file-v1:"

type notionFileSourceMetadata struct {
	Caption     string `json:"caption"`
	ExternalURL string `json:"externalUrl,omitempty"`
	// Block is the block the file becomes when it isn't the source's kind:
	// an embed of a file Notion keeps stays an embed of the copied file.
	Block string `json:"block,omitempty"`
}

type notionFileFetchFailure struct {
	code string
	kind string
}

var (
	errNotionFileUnsafeDestination = errors.New(errorCodeNotionImportFilePermanent + ": unsafe destination")
)

func (failure *notionFileFetchFailure) Error() string { return failure.code }

func prepareNotionFileSource(
	app core.App,
	parentID string,
	parentKind string,
	blueprint bool,
	position int,
	block notionProviderObject,
) (stagedNotionSource, error) {
	providerFile, ok := notionProviderBlockFile(block)
	if !ok {
		return stagedNotionSource{}, errors.New(errorCodeNotionImportFilePermanent)
	}
	caption, err := notionProviderRichTextSourceText(providerFile.Caption, false)
	if err != nil {
		return stagedNotionSource{}, err
	}
	metadata := notionFileSourceMetadata{Caption: caption}
	source := stagedNotionSource{
		SourceID: block.ID, Kind: notionImportFileKind(block), ParentSourceID: parentID,
		ParentKind: parentKind, Position: position, Blueprint: blueprint,
	}
	if block.Type == graphBlockTypeEmbed {
		metadata.Block = graphBlockTypeEmbed
	}
	rawURL := ""
	switch providerFile.Type {
	case "file":
		rawURL = strings.TrimSpace(providerFile.File.URL)
		if rawURL == "" {
			return stagedNotionSource{}, errors.New(errorCodeNotionImportFilePermanent)
		}
		key, err := notionImportEncryptionKey(app)
		if err != nil {
			return stagedNotionSource{}, err
		}
		source.FileURLCipher, err = security.Encrypt([]byte(rawURL), key)
		if err != nil {
			return stagedNotionSource{}, err
		}
	case graphExternalFileSource:
		rawURL = strings.TrimSpace(providerFile.External.URL)
		if !validGraphExternalFileURL(rawURL) {
			return stagedNotionSource{}, errors.New(errorCodeNotionImportFilePermanent)
		}
		metadata.ExternalURL = rawURL
	default:
		return stagedNotionSource{}, errors.New(errorCodeNotionImportFilePermanent)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return stagedNotionSource{}, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return stagedNotionSource{}, errors.New(errorCodeNotionImportFilePermanent)
	}
	name := strings.TrimSpace(providerFile.Name)
	if name == "" {
		name = path.Base(parsed.Path)
	}
	if name == "" || name == "." || name == "/" {
		name = "notion-" + block.Type
	}
	source.Title = name
	source.Text = notionFileSourcePrefix + string(encoded)
	return source, nil
}

func notionProviderBlockFile(block notionProviderObject) (notionProviderFileObject, bool) {
	switch block.Type {
	case graphBlockTypeFile:
		return block.File, true
	case graphBlockTypePDF:
		return block.PDF, true
	case graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio:
		file := notionProviderFileObject{}
		if len(block.Payload) == 0 || json.Unmarshal(block.Payload, &file) != nil {
			return notionProviderFileObject{}, false
		}
		return file, true
	case graphBlockTypeEmbed:
		// An embed of a file Notion keeps (its file preview's "페이지에 추가",
		// 2026-11-06) names the file by Notion's signed, soon expired address.
		embed := struct {
			URL     string                   `json:"url"`
			Caption []notionProviderRichText `json:"caption"`
		}{}
		if len(block.Payload) == 0 || json.Unmarshal(block.Payload, &embed) != nil || !notionFileHostedURL(embed.URL) {
			return notionProviderFileObject{}, false
		}
		file := notionProviderFileObject{Type: "file", Caption: embed.Caption}
		file.File.URL = strings.TrimSpace(embed.URL)
		return file, true
	default:
		return notionProviderFileObject{}, false
	}
}

// notionImportFileKind is the kind a Notion block whose file the import
// copies or links is staged as — the block's own type, or "file" for an embed
// of a file Notion keeps, which the file's metadata makes an embed again — or
// "" for a block that isn't one.
func notionImportFileKind(block notionProviderObject) string {
	if _, ok := notionProviderBlockFile(block); !ok {
		return ""
	}
	if block.Type == graphBlockTypeEmbed {
		return graphBlockTypeFile
	}
	return block.Type
}

// notionFileHostedURL reports whether an address is a file Notion keeps: its
// file storage's signed addresses, which expire within hours, or the stand-in
// Notion's own address in a build pointed at one.
func notionFileHostedURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Scheme == "http" && parsed.Host == notionFileAllowedLoopbackHost() {
		return true
	}
	if parsed.Scheme != "https" {
		return false
	}
	switch strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".") {
	case "prod-files-secure.s3.us-west-2.amazonaws.com", "file.notion.so":
		return true
	case "s3.us-west-2.amazonaws.com":
		return strings.HasPrefix(parsed.Path, "/secure.notion-static.com/")
	}
	return false
}

// notionImportFileSourceKind reports whether a staged source is a block whose
// file the import copies from Notion or keeps as an external link.
func notionImportFileSourceKind(kind string) bool {
	switch kind {
	case graphBlockTypeFile, graphBlockTypePDF, graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio:
		return true
	default:
		return false
	}
}

func decodeNotionFileSourceMetadata(value string) (notionFileSourceMetadata, error) {
	if !strings.HasPrefix(value, notionFileSourcePrefix) {
		return notionFileSourceMetadata{Caption: value}, nil
	}
	metadata := notionFileSourceMetadata{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(value, notionFileSourcePrefix)), &metadata); err != nil {
		return notionFileSourceMetadata{}, errors.New(errorCodeNotionImportProviderFailed)
	}
	if metadata.ExternalURL != "" && !validGraphExternalFileURL(metadata.ExternalURL) {
		return notionFileSourceMetadata{}, errors.New(errorCodeNotionImportProviderFailed)
	}
	return metadata, nil
}

func fetchNotionFile(ctx context.Context, app core.App, rawURL string) (notionFetchedFile, error) {
	// A download is given up once nothing has come for a while, not after a
	// set time, so a large file that keeps coming comes however long it takes
	// (2026-11-06: a 42 MB PDF never fit the half minute a whole download had
	// once the machine was busy, and the migration stopped on it each time).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stall := time.AfterFunc(notionFileStallTimeout, cancel)
	defer stall.Stop()
	var response *http.Response
	name := ""
	if notionBundleOwnURL(rawURL) {
		// A bundle's file, from Leafen itself.
		fetched, uploaded, err := notionBundleFetch(ctx, rawURL)
		if err != nil {
			return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileTransient, kind: "transient"}
		}
		response, name = fetched, uploaded
	} else {
		parsed, err := validateNotionFileURL(rawURL)
		if err != nil {
			return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return notionFetchedFile{}, err
		}
		client := newNotionFileHTTPClient()
		defer client.CloseIdleConnections()
		response, err = client.Do(request)
		if err != nil {
			if errors.Is(err, errNotionFileUnsafeDestination) {
				return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
			}
			return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileTransient, kind: "transient"}
		}
		name = path.Base(parsed.Path)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileRefreshFailed, kind: "refresh"}
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
	case response.StatusCode >= 500:
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileTransient, kind: "transient"}
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
	}
	if name == "" || name == "." || name == "/" {
		name = "notion-file"
	}
	contentType, err := admitStoredFileType(name, response.Header.Get("Content-Type"))
	// An SVG drawing comes cleaned of what could run or reach out (notion_import_svg.go).
	svg := err != nil && storedFileTypeAdmissionCode(err) == errorCodeFileUnsafeType && notionImportSVGFile(name, response.Header.Get("Content-Type"))
	if svg {
		contentType, err = notionImportSVGContentType, nil
	}
	if err != nil {
		return notionFetchedFile{}, &notionFileFetchFailure{
			code: storedFileTypeAdmissionCode(err), kind: "permanent",
		}
	}

	directory := filepath.Join(app.DataDir(), "tmp", "notion-import")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return notionFetchedFile{}, err
	}
	file, err := os.CreateTemp(directory, "download-*")
	if err != nil {
		return notionFetchedFile{}, err
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	hasher := sha256.New()
	var body io.Reader = notionFileProgressReader{reader: response.Body, stall: stall}
	if svg {
		if body, err = readNotionImportSVG(body); err != nil {
			cleanup()
			return notionFetchedFile{}, err
		}
	}
	written, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(body, maxNativeFileSize+1))
	if err != nil {
		cleanup()
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileTransient, kind: "transient"}
	}
	if written == 0 || written > maxNativeFileSize {
		cleanup()
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		cleanup()
		return notionFetchedFile{}, err
	}
	return notionFetchedFile{
		Path: file.Name(), Name: name, ContentType: contentType,
		Size: written, SHA256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// notionFileStallTimeout is how long a Notion file's answer may take to start
// or stop coming before it is tried again later.
const notionFileStallTimeout = 30 * time.Second

// notionFileProgressReader keeps a download going while it comes.
type notionFileProgressReader struct {
	reader io.Reader
	stall  *time.Timer
}

func (r notionFileProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.stall.Reset(notionFileStallTimeout)
	}
	return n, err
}

func newNotionFileHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	allowedLoopbackHost := notionFileAllowedLoopbackHost()
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return notionFileDialContext(
			ctx,
			network,
			address,
			allowedLoopbackHost,
			net.DefaultResolver.LookupNetIP,
			dialer.DialContext,
		)
	}
	// Bounded by how long the answer takes to start; a body is bounded by
	// how long it stops coming (fetchNotionFile).
	transport.ResponseHeaderTimeout = notionFileStallTimeout
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, prior []*http.Request) error {
			if len(prior) >= 3 {
				return errors.New(errorCodeNotionImportFileTransient)
			}
			_, err := validateNotionFileURL(request.URL.String())
			return err
		},
	}
}

func validateNotionFileURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New(errorCodeNotionImportFilePermanent)
	}
	if parsed.Scheme == "http" && parsed.Host == notionFileAllowedLoopbackHost() {
		return parsed, nil
	}
	if parsed.Scheme != "https" {
		return nil, errNotionFileUnsafeDestination
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		return nil, errNotionFileUnsafeDestination
	}
	if address, parseErr := netip.ParseAddr(hostname); parseErr == nil && !notionFileAddressIsPublic(address) {
		return nil, errNotionFileUnsafeDestination
	}
	return parsed, nil
}

func notionFileAllowedLoopbackHost() string {
	base, err := url.Parse(strings.TrimSpace(os.Getenv(notionAPIBaseURLEnvironment)))
	if err != nil || base.Scheme != "http" || base.Host == "" || base.User != nil {
		return ""
	}
	address, err := netip.ParseAddr(base.Hostname())
	if err != nil || !address.Unmap().IsLoopback() {
		return ""
	}
	return base.Host
}

func notionFileAddressIsPublic(address netip.Addr) bool {
	return outboundAddressIsPublic(address)
}

func notionFileDialContext(
	ctx context.Context,
	network, address string,
	allowedLoopbackHost string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	connection, err := outboundPublicDialContext(
		ctx, network, address, allowedLoopbackHost, lookup, dial,
	)
	if errors.Is(err, errOutboundUnsafeDestination) {
		return nil, fmt.Errorf("%w: %v", errNotionFileUnsafeDestination, err)
	}
	return connection, err
}
