package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// webdavBackupStore keeps scheduled backups on a WebDAV server (user
// decision 2026-10-03: Nextcloud, Synology and the like), in the same
// layout as a folder: collections for folders, PUT for whole files, MOVE
// for the renames that publish a snapshot or a content. A file is spooled
// beside the data and sent with its length, as some servers keep nothing of
// an upload sent in pieces.
type webdavBackupStore struct {
	root     *url.URL
	username string
	password string
	spool    string
	client   *http.Client
	ctx      context.Context
	folders  knownFolders
}

const (
	webdavBackupTimeout = 10 * time.Minute
	// What a listing may hold, so a hostile server can't make one huge.
	webdavBackupListLimit = 16 << 20
)

var (
	errWebDAVBackupUnauthorized = errors.New(errorCodeScheduledBackupWebDAVUnauthorized)
	errWebDAVBackupNoSpace      = errors.New(errorCodeScheduledBackupNoSpace)
)

func newWebDAVBackupStore(ctx context.Context, address, username, password, spool string) (*webdavBackupStore, error) {
	root, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (root.Scheme != "https" && root.Scheme != "http") || root.Host == "" || root.User != nil ||
		root.RawQuery != "" || root.Fragment != "" {
		return nil, errors.New(errorCodeScheduledBackupWebDAVInvalid)
	}
	if !strings.HasSuffix(root.Path, "/") {
		root.Path += "/"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &webdavBackupStore{
		root: root, username: username, password: password, spool: spool, ctx: ctx,
		client: &http.Client{Timeout: webdavBackupTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			// Credentials go only where they were set.
			return http.ErrUseLastResponse
		}},
	}, nil
}

// address is a store path's URL under the root, each part escaped; a
// folder's ends with a slash.
func (store *webdavBackupStore) address(relative string, collection bool) string {
	parts := []string{}
	for _, part := range strings.Split(strings.Trim(relative, "/"), "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	target := store.root.JoinPath(parts...).String()
	if collection && !strings.HasSuffix(target, "/") {
		target += "/"
	}
	return target
}

func (store *webdavBackupStore) request(method, relative string, collection bool, body io.Reader, headers map[string]string) (*http.Response, error) {
	return store.send(method, relative, collection, body, -1, headers)
}

// send is a request with a body of a known length, or of the length the
// body tells (length < 0).
func (store *webdavBackupStore) send(method, relative string, collection bool, body io.Reader, length int64, headers map[string]string) (*http.Response, error) {
	if length == 0 {
		body = http.NoBody
	}
	request, err := http.NewRequestWithContext(store.ctx, method, store.address(relative, collection), body)
	if err != nil {
		return nil, err
	}
	if length > 0 {
		request.ContentLength = length
	}
	if store.username != "" || store.password != "" {
		request.SetBasicAuth(store.username, store.password)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := store.client.Do(request)
	if err == nil && !collection {
		response, err = store.askFolderAgain(request, response)
	}
	if err != nil {
		return nil, err
	}
	// Refused, full or missing, with what the server answered beside it.
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, webdavBackupAnswered(method, response, errWebDAVBackupUnauthorized)
	case http.StatusInsufficientStorage, http.StatusRequestEntityTooLarge:
		return nil, webdavBackupAnswered(method, response, errWebDAVBackupNoSpace)
	case http.StatusNotFound:
		return nil, webdavBackupAnswered(method, response, fs.ErrNotExist)
	}
	return response, nil
}

// askFolderAgain asks a folder named without its trailing slash again, the
// same way, at its address with the slash, when the server answered by
// sending to that path: Apache's mod_dav (pCloud's WebDAV, 2026-11-22)
// answers a folder's PROPFIND so, and the client follows no redirect by
// itself. The slash goes on the address that was asked, whatever server or
// scheme the answer names, so the credentials go only where they were set;
// any other answer is the request's own.
func (store *webdavBackupStore) askFolderAgain(request *http.Request, response *http.Response) (*http.Response, error) {
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return response, nil
	}
	location, err := response.Location()
	if err != nil || location.Path != request.URL.Path+"/" {
		return response, nil
	}
	again := request.Clone(store.ctx)
	folder := *request.URL
	folder.Path += "/"
	if folder.RawPath != "" {
		folder.RawPath += "/"
	}
	again.URL = &folder
	if request.Body != nil && request.Body != http.NoBody {
		if request.GetBody == nil {
			return response, nil
		}
		if again.Body, err = request.GetBody(); err != nil {
			return response, nil
		}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	response.Body.Close()
	return store.client.Do(again)
}

// webdavBackupStatusError is an answer the store can't go on from: the
// status with the server's own words for it ("401 Unauthorized"), which
// backup_failure_reason.go reads the cause from.
func webdavBackupStatusError(method string, response *http.Response) error {
	return webdavBackupAnswered(method, response, nil)
}

func webdavBackupAnswered(method string, response *http.Response, named error) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	response.Body.Close()
	answered := strings.TrimSpace(response.Status)
	if answered == "" {
		answered = strconv.Itoa(response.StatusCode)
	}
	if named == nil {
		return fmt.Errorf("WebDAV %s answered %s", method, answered)
	}
	return fmt.Errorf("WebDAV %s answered %s: %w", method, answered, named)
}

func (store *webdavBackupStore) MakeDir(relative string) error {
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")
		if current == "" || store.folders.has(current) {
			continue
		}
		entry, err := store.Stat(current)
		if err == nil && entry.Dir {
			store.folders.add(current)
			continue
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		response, err := store.request("MKCOL", current, true, nil, nil)
		if err != nil {
			return err
		}
		// 405: it exists already, as another request made it meanwhile.
		if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusMethodNotAllowed &&
			response.StatusCode != http.StatusOK {
			return webdavBackupStatusError("MKCOL", response)
		}
		response.Body.Close()
		store.folders.add(current)
	}
	return nil
}

// Create spools the file as it is written and uploads it whole on Close.
func (store *webdavBackupStore) Create(relative string) (io.WriteCloser, error) {
	if _, err := store.Stat(relative); err == nil {
		return nil, fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(store.spool, 0o700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(store.spool, "leafen-webdav-upload-*")
	if err != nil {
		return nil, err
	}
	return &webdavBackupWriter{store: store, relative: relative, file: file}, nil
}

type webdavBackupWriter struct {
	store    *webdavBackupStore
	relative string
	file     *os.File
}

func (writer *webdavBackupWriter) Write(data []byte) (int, error) { return writer.file.Write(data) }

func (writer *webdavBackupWriter) Close() error {
	defer os.Remove(writer.file.Name())
	defer writer.file.Close()
	length, err := writer.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := writer.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return writer.store.put(writer.relative, writer.file, length, true)
}

// put uploads a whole file; a new one only where none is there yet.
func (store *webdavBackupStore) put(relative string, body io.Reader, length int64, create bool) error {
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	if create {
		headers["If-None-Match"] = "*"
	}
	response, err := store.send(http.MethodPut, relative, false, body, length, headers)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusPreconditionFailed {
		response.Body.Close()
		return fs.ErrExist
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent &&
		response.StatusCode != http.StatusOK {
		return webdavBackupStatusError("PUT", response)
	}
	response.Body.Close()
	return nil
}

func (store *webdavBackupStore) Open(relative string) (io.ReadCloser, error) {
	response, err := store.request(http.MethodGet, relative, false, nil, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, webdavBackupStatusError("GET", response)
	}
	return response.Body, nil
}

func (store *webdavBackupStore) Stat(relative string) (scheduledBackupStoreEntry, error) {
	listed, err := store.propfind(relative, "0", false)
	if err != nil {
		return scheduledBackupStoreEntry{}, err
	}
	for _, item := range listed {
		if item.self {
			return item.entry, nil
		}
	}
	if len(listed) == 0 {
		return scheduledBackupStoreEntry{}, fs.ErrNotExist
	}
	// A server naming its paths apart from the address asked (a proxy in
	// front) answers for the path itself.
	return listed[0].entry, nil
}

func (store *webdavBackupStore) Rename(from, to string) error {
	if scheduledBackupStoreExists(store, to) {
		return fs.ErrExist
	}
	fromEntry, err := store.Stat(from)
	if err != nil {
		return err
	}
	response, err := store.request("MOVE", from, fromEntry.Dir, nil, map[string]string{
		"Destination": store.address(to, fromEntry.Dir), "Overwrite": "F",
	})
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusPreconditionFailed {
		response.Body.Close()
		return fs.ErrExist
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent {
		return webdavBackupStatusError("MOVE", response)
	}
	response.Body.Close()
	// A folder moved takes those under it along.
	store.folders.forget(from)
	return nil
}

// List answers the entries directly in a folder, told from the folder
// itself by their addresses, never by their place in the answer: a server
// may name the folder anywhere in it, or not at all (2026-11-22).
func (store *webdavBackupStore) List(relative string) ([]scheduledBackupStoreEntry, error) {
	// A folder is named with its trailing slash, as the server names it.
	listed, err := store.propfind(relative, "1", true)
	if err != nil {
		return nil, err
	}
	entries, matched, folder := []scheduledBackupStoreEntry{}, false, true
	for _, item := range listed {
		switch {
		case item.self:
			matched, folder = true, item.entry.Dir
		case item.child:
			matched = true
			entries = append(entries, item.entry)
		}
	}
	if matched {
		if !folder {
			return nil, fs.ErrNotExist
		}
		return entries, nil
	}
	// A server naming its paths apart from the address asked (a proxy in
	// front) answers for the folder itself first.
	if len(listed) == 0 || !listed[0].entry.Dir {
		return nil, fs.ErrNotExist
	}
	for _, item := range listed[1:] {
		entries = append(entries, item.entry)
	}
	return entries, nil
}

func (store *webdavBackupStore) RemoveAll(relative string) error {
	entry, err := store.Stat(relative)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// Forgotten before and again once it is gone, so no folder known
	// meanwhile is taken for there after.
	store.folders.forget(relative)
	defer store.folders.forget(relative)
	response, err := store.request(http.MethodDelete, relative, entry.Dir, nil, nil)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		return webdavBackupStatusError("DELETE", response)
	}
	response.Body.Close()
	return nil
}

// Replace uploads the file whole over the one there.
func (store *webdavBackupStore) Replace(relative string, content []byte) error {
	return store.put(relative, bytes.NewReader(content), int64(len(content)), false)
}

// Sync has nothing to do: a WebDAV server answers once it holds the file.
func (store *webdavBackupStore) Sync(string) error { return nil }
