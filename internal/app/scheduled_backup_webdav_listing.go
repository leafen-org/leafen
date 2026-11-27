package app

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// A WebDAV listing (PROPFIND) answers each path it names by an href that a
// server may write as a path from its root (Apache's mod_dav and pCloud,
// with its spaces as %20), as a whole URL, relative to the address asked,
// or with spaces and line breaks around it (2026-11-22). Every href is read
// here, once, as the decoded path it names, and told apart as the path
// asked about itself or one directly in it.

type webdavMultistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Status   string `xml:"status"`
		Propstat []struct {
			Status string `xml:"status"`
			Prop   struct {
				ResourceType struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"resourcetype"`
				ContentLength string `xml:"getcontentlength"`
				LastModified  string `xml:"getlastmodified"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

const webdavPropfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`

// webdavListed is one path a listing names: whether it is the path asked
// about itself or one directly in it.
type webdavListed struct {
	entry       scheduledBackupStoreEntry
	self, child bool
}

// propfind lists a path (depth 0) or a folder and its entries (depth 1).
func (store *webdavBackupStore) propfind(relative, depth string, collection bool) ([]webdavListed, error) {
	asked, err := url.Parse(store.address(relative, collection))
	if err != nil {
		return nil, err
	}
	response, err := store.request("PROPFIND", relative, collection, strings.NewReader(webdavPropfindBody), map[string]string{
		"Depth": depth, "Content-Type": "application/xml; charset=utf-8",
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMultiStatus {
		return nil, webdavBackupStatusError("PROPFIND", response)
	}
	// Relative hrefs are read against the address that answered, a folder
	// asked again with its trailing slash included.
	if response.Request != nil && response.Request.URL != nil {
		asked = response.Request.URL
	}
	status := webdavMultistatus{}
	if err := xml.NewDecoder(io.LimitReader(response.Body, webdavBackupListLimit)).Decode(&status); err != nil {
		return nil, err
	}
	self := webdavCleanPath(asked.Path)
	listed := []webdavListed{}
	for _, item := range status.Responses {
		at, ok := webdavHrefPath(asked, item.Href)
		// A path answered as a whole with a status of its own other than
		// success (404 and the like) isn't there to list.
		if !ok || (strings.TrimSpace(item.Status) != "" && !webdavStatusOK(item.Status)) {
			continue
		}
		entry := scheduledBackupStoreEntry{Name: path.Base(at)}
		for _, propstat := range item.Propstat {
			if !webdavStatusOK(propstat.Status) {
				continue
			}
			if propstat.Prop.ResourceType.Collection != nil {
				entry.Dir = true
			}
			if size := strings.TrimSpace(propstat.Prop.ContentLength); size != "" {
				entry.Size, _ = strconv.ParseInt(size, 10, 64)
			}
			if modified := strings.TrimSpace(propstat.Prop.LastModified); modified != "" {
				entry.Modified, _ = http.ParseTime(modified)
			}
		}
		listed = append(listed, webdavListed{entry: entry, self: at == self, child: at != self && path.Dir(at) == self})
	}
	return listed, nil
}

// webdavHrefPath is the decoded path an href names, read against the
// address asked: a path from the server's root, a whole URL (its server
// left aside) or a path relative to the address, its escapes undone.
func webdavHrefPath(asked *url.URL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}
	parsed, err := url.Parse(href)
	if err != nil {
		// A character a URL can't hold, written as is.
		decoded, err := url.PathUnescape(href)
		if err != nil {
			return "", false
		}
		return webdavCleanPath(decoded), true
	}
	return webdavCleanPath(asked.ResolveReference(parsed).Path), true
}

// webdavCleanPath is a path from the root with no trailing slash, the way
// a folder and a file are compared.
func webdavCleanPath(value string) string {
	return path.Clean("/" + value)
}

// webdavStatusOK reads a status line ("HTTP/1.1 200 OK", or with no reason
// after the code) as success.
func webdavStatusOK(line string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[1] == "200"
}
