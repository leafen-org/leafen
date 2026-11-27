package app

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What Notion's view-version, get-version-changes and restore-version take
// (seen 2026-11-13; restore-version's tool restores "a page or database"): a
// page, or a database — by its address, its page's for a full-page one, or a
// data source's collection:// — which reads as its data sources, views and
// rows, and whose changes are its data sources', views' and rows'. Leafen
// reads a database from the history of the page that holds it: a full-page
// database's own, an inline one's page's, and restores an inline one alone.

// mcpHistoryTarget is a version tool's page or database.
type mcpHistoryTarget struct {
	// PageID is the page whose history holds it, and Page that page.
	PageID string
	Page   *core.Record
	// Database is set for a database.
	Database *mcpHistoryDatabase
}

// mcpHistoryDatabase is a database a version tool reads.
type mcpHistoryDatabase struct {
	ContainerID string
	// BlockID is its block in the page's body.
	BlockID string
	Inline  bool
	URL     string
	// SourceIDs are the data sources it holds itself, in its order; a
	// linked database holds none.
	SourceIDs []string
}

func mcpHistoryTargetOf(app core.App, authority mcpAuthority, raw string) (mcpHistoryTarget, error) {
	containerID := ""
	if sourceID, ok := parseMCPDataSourceURL(mcpLocalID(app, raw)); ok {
		containerID = mcpOwningDatabaseContainer(app, sourceID)
	} else if found, ok := resolveMCPDatabaseID(app, raw); ok {
		containerID = found
	}
	if containerID == "" {
		pageID, page, err := mcpHistoryPage(app, authority, raw)
		return mcpHistoryTarget{PageID: pageID, Page: page}, err
	}
	location, err := loadMCPDatabaseContainerLocation(app, containerID)
	if err != nil {
		return mcpHistoryTarget{}, mcpNotFound("database", raw)
	}
	_, page, readable := mcpListedPage(app, authority, location.PageID)
	if !readable {
		return mcpHistoryTarget{}, mcpNotFound("database", raw)
	}
	sources, err := loadMCPDatabaseContainerOwnMembers(app, containerID)
	if err != nil {
		return mcpHistoryTarget{}, failedBecause(errMCPToolFailed, err)
	}
	if location.Kind != databaseContainerKindSource {
		sources = nil
	}
	database := &mcpHistoryDatabase{
		ContainerID: containerID, BlockID: location.BlockID, SourceIDs: sources,
		Inline: mcpFullPageDatabaseContainer(app, location.PageID) != containerID,
		URL:    mcpDatabaseAddress(authority, containerID),
	}
	if database.BlockID == "" {
		database.BlockID = containerID
	}
	return mcpHistoryTarget{PageID: location.PageID, Page: page, Database: database}, nil
}

// mcpOwningDatabaseContainer is the database that holds a data source
// itself, "" for none.
func mcpOwningDatabaseContainer(app core.App, sourceID string) string {
	containerID := ""
	_ = app.DB().NewQuery(`
		SELECT [[membership]].[[container_id]] FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[data_source_id]] = {:sourceId} AND [[membership]].[[source_container_id]] = ''
			AND [[container]].[[kind]] = 'source'
		ORDER BY [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"sourceId": sourceID}).Row(&containerID)
	return containerID
}

// mcpVersionTime writes a time as Notion's version tools do: UTC to the
// millisecond, always three digits (seen 2026-11-13: "2026-11-13T09:17:00.000Z"),
// which orders the changes; their answer puts it on the clock after
// (person_clock.go).
func mcpVersionTime(milliseconds int64) string {
	return time.UnixMilli(milliseconds).UTC().Format("2006-01-02T15:04:05.000Z")
}

// mcpOrderedObject is a JSON object whose keys keep their order, as Notion's
// version answers list a database's views in its tabs' order.
type mcpOrderedObject []mcpOrderedEntry

type mcpOrderedEntry struct {
	Key   string
	Value any
}

func (object mcpOrderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, entry := range object {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := mcpFetchJSON(entry.Key)
		if err != nil {
			return nil, err
		}
		value, err := mcpFetchJSON(entry.Value)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(key)
		buffer.WriteByte(':')
		buffer.WriteString(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

var _ json.Marshaler = mcpOrderedObject{}
