package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A file keeps when the person's own copy was last changed, when the client
// that sent it said so: sync tools (rclone, ownCloud's and Nextcloud's
// clients) send it with a WebDAV upload as X-OC-Mtime, and read it back to
// see the file unchanged; without it every upload looked newer than the copy
// it came from (2026-10-14). The time belongs to the file's bytes, so a file
// changed any other way, which is a new file, has none.

// fileClientModifiedHeader reads X-OC-Mtime, Unix seconds, as milliseconds:
// a time after 1970 and not more than a day ahead of the server's clock.
func fileClientModifiedHeader(value string) (int64, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds <= 0 || seconds > time.Now().Add(24*time.Hour).Unix() {
		return 0, false
	}
	return seconds * 1000, true
}

// setFileClientModified keeps the time on a file.
func setFileClientModified(app core.App, fileID string, milliseconds int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_files}} SET [[client_modified]] = {:modified} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"modified": milliseconds, "id": fileID}).Execute()
	return err
}

// fileClientModified is a file's kept time, or 0.
func fileClientModified(app core.App, fileID string) int64 {
	var modified int64
	if err := app.DB().NewQuery(`
		SELECT COALESCE([[client_modified]], 0) FROM {{leafen_files}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": fileID}).Row(&modified); err != nil {
		return 0
	}
	return modified
}
