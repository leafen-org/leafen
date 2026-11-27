package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	importedPeopleSourcePrefix      = "notion-user:"
	importedPeopleRequestIDMaxRunes = 255
	importedPeopleCursorMaxBytes    = 2048
	importedPeopleScanCursorVersion = 1
	importedPeopleApplyPageLimit    = 8
	importedPeopleRetentionKeepMax  = 25
	importedPeopleRetentionBatch    = 20
	// A mapping left pending that hasn't moved for this long, its asker gone,
	// gives way to a new one.
	importedPeopleStalledAfterMillis = int64(2 * 60 * 1000)
)

// Imported people are mapped by anyone who may change the workspace's pages,
// within the pages they may change: the owner across the workspace, a member
// in their own pages and the shared ones they edit. A page private to someone
// else stays theirs to map.
const importedPeopleMappingPermission = permissionWorkspaceWrite

var (
	errImportedPeopleCursorInvalid      = errors.New(errorCodeImportedPeopleCursorInvalid)
	errImportedPeopleMappingFailed      = errors.New(errorCodeImportedPeopleMappingFailed)
	errImportedPeopleItemAlreadyApplied = errors.New("imported people item already applied")
	errImportedPeopleMappingBusy        = errors.New("imported people mapping busy")
)

type storedImportedPeopleMappingJob struct {
	ID               string `db:"id"`
	ActorID          string `db:"actor_id"`
	OwnerID          string `db:"owner_id"`
	WorkspaceID      string `db:"workspace_id"`
	OrganizationID   string `db:"organization_id"`
	RequestID        string `db:"request_id"`
	RequestHash      string `db:"request_hash"`
	MappingsJSON     string `db:"mappings_json"`
	Status           string `db:"status"`
	Phase            string `db:"phase"`
	DiscoveryCreated string `db:"discovery_created"`
	DiscoveryPageID  string `db:"discovery_page_id"`
	SnapshotCreated  string `db:"snapshot_created"`
	SnapshotPageID   string `db:"snapshot_page_id"`
	MappedPeople     int    `db:"mapped_people"`
	Generation       int    `db:"generation"`
	ChangedPages     int    `db:"changed_pages"`
	ChangedBlocks    int    `db:"changed_blocks"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
	CompletedAt      int64  `db:"completed_at"`
}

type storedImportedPeopleMappingItem struct {
	JobID         string `db:"job_id"`
	PageID        string `db:"page_id"`
	PageCreated   string `db:"page_created"`
	Applied       int    `db:"applied"`
	ChangedPage   int    `db:"changed_page"`
	ChangedBlocks int    `db:"changed_blocks"`
}

type importedPeopleSource struct {
	SourceID    string
	DisplayName *string
	Email       *string
	Count       int
}

func normalizedImportedPeopleMappings(input notionImportedPeopleApplyInput) (map[string]string, []byte, string, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > importedPeopleRequestIDMaxRunes ||
		len(input.Mappings) == 0 || len(input.Mappings) > notionImportedPeopleItems {
		return nil, nil, "", errImportedPeopleMappingFailed
	}
	normalized := make(map[string]string, len(input.Mappings))
	for sourceID, targetID := range input.Mappings {
		sourceID = strings.TrimSpace(sourceID)
		targetID = strings.TrimSpace(targetID)
		if !validImportedPeopleSourceID(sourceID) || targetID == "" ||
			len(sourceID) > maxWorkspacePermissionIdentifierLength || len(targetID) > maxWorkspacePermissionIdentifierLength {
			return nil, nil, "", errImportedPeopleMappingFailed
		}
		normalized[sourceID] = targetID
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, nil, "", err
	}
	return normalized, encoded, input.RequestID, nil
}

func importedPeopleMappingRequestHash(workspaceID, requestID string, mappings []byte) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + requestID + "\x00" + string(mappings)))
	return hex.EncodeToString(digest[:])
}

func importedPeopleMappingJobID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte("imported-people\x00" + actorID + "\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}

// Imported people are Notion's, and a workspace moved from another Leafen
// server brings its unpaired people as stand-ins (workspace_import_archive_people.go).
var importedPeopleSourcePrefixes = []string{importedPeopleSourcePrefix, workspaceImportPersonPrefix}

func validImportedPeopleSourceID(value string) bool {
	for _, prefix := range importedPeopleSourcePrefixes {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
			return true
		}
	}
	return false
}

// importedPeopleMarked says whether stored text names an imported person.
func importedPeopleMarked(text string) bool {
	for _, prefix := range importedPeopleSourcePrefixes {
		if strings.Contains(text, prefix) {
			return true
		}
	}
	return false
}

// importedPeopleMarkedSQL is importedPeopleMarked for a column, in a query.
func importedPeopleMarkedSQL(column string) string {
	return "(instr(" + column + ", '" + importedPeopleSourcePrefix + "')>0 OR instr(" + column + ", '" + workspaceImportPersonPrefix + "')>0)"
}

// importedPeopleMarkedBlobSQL is importedPeopleMarkedSQL for a stored Ygo
// document or update. Its encoding keeps each string it holds — a mention's
// userId among them — as its UTF-8 bytes, so a document naming an imported
// person holds the ID's prefix; the bytes are compared as they are.
func importedPeopleMarkedBlobSQL(column string) string {
	return "(instr(" + column + ", CAST('" + importedPeopleSourcePrefix + "' AS BLOB))>0 OR instr(" + column + ", CAST('" + workspaceImportPersonPrefix + "' AS BLOB))>0)"
}

// importedPeoplePageMarkedSQL is, in a query, whether the page the column
// names can name an imported person anywhere a mapping reads it: its text's
// stored document and updates, its blocks' content, and the database values
// and views it holds. A page it rules out names no one, so finding and
// mapping people neither read nor change it (2026-11-13: reading every
// page's document took 25–38 s on a NAS-class server for 11,300 blocks).
func importedPeoplePageMarkedSQL(page string) string {
	return `(EXISTS (SELECT 1 FROM {{leafen_ygo_document_states}} [[stored]]
			WHERE [[stored]].[[room]]=` + page + ` AND ` + importedPeopleMarkedBlobSQL("[[stored]].[[state]]") + `)
		OR EXISTS (SELECT 1 FROM {{leafen_ygo_updates}} [[pending]]
			WHERE [[pending]].[[room]]=` + page + ` AND ` + importedPeopleMarkedBlobSQL("[[pending]].[[update_blob]]") + `)
		OR EXISTS (SELECT 1 FROM {{leafen_blocks}} [[block]]
			WHERE [[block]].[[page_id]]=` + page + ` AND ` + importedPeopleMarkedSQL("[[block]].[[content]]") + `)
		OR EXISTS (SELECT 1 FROM {{leafen_database_row_values}} [[cell]]
			WHERE [[cell]].[[page_id]]=` + page + ` AND (` + importedPeopleMarkedSQL("[[cell]].[[value]]") + ` OR ` + importedPeopleMarkedSQL("[[cell]].[[spans]]") + `))
		OR EXISTS (SELECT 1 FROM {{leafen_database_views}} [[view]]
			WHERE [[view]].[[page_id]]=` + page + ` AND ` + importedPeopleMarkedSQL("[[view]].[[config]]") + `))`
}

func importedPeopleSourceFromMap(value map[string]any) (importedPeopleSource, bool) {
	sourceID, _ := value["userId"].(string)
	if !validImportedPeopleSourceID(sourceID) {
		sourceID, _ = value["id"].(string)
	}
	if !validImportedPeopleSourceID(sourceID) {
		return importedPeopleSource{}, false
	}
	return importedPeopleSource{
		SourceID: sourceID, DisplayName: importedPeopleOptionalString(value["displayName"]),
		Email: importedPeopleOptionalString(value["email"]), Count: 1,
	}, true
}

func importedPeopleOptionalString(value any) *string {
	text, ok := value.(string)
	text = strings.TrimSpace(text)
	if !ok || text == "" {
		return nil
	}
	return &text
}

func collectImportedPeopleSources(value any, found map[string]importedPeopleSource) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectImportedPeopleSources(item, found)
		}
	case map[string]any:
		if source, ok := importedPeopleSourceFromMap(typed); ok {
			mergeImportedPeopleSource(found, source)
			return
		}
		for _, item := range typed {
			collectImportedPeopleSources(item, found)
		}
	}
}

func mergeImportedPeopleSource(found map[string]importedPeopleSource, source importedPeopleSource) {
	current := found[source.SourceID]
	current.SourceID = source.SourceID
	current.Count += source.Count
	if current.DisplayName == nil {
		current.DisplayName = source.DisplayName
	}
	if current.Email == nil {
		current.Email = source.Email
	}
	found[source.SourceID] = current
}

// importedPeopleCleared is the target of an imported person a Notion
// migration's plan leaves out of person values (notion_migration_people.go):
// their values lose them; their mentions and views' filters keep them.
const importedPeopleCleared = "leafen:cleared"

func replaceImportedPeopleSources(value any, mappings map[string]string) (any, bool) {
	switch typed := value.(type) {
	case string:
		// A view's filter names an imported person by source ID alone.
		if targetID, mapped := mappings[typed]; mapped && validImportedPeopleSourceID(typed) && targetID != importedPeopleCleared {
			return targetID, true
		}
		return value, false
	case []any:
		// One a migration leaves out goes from the list (importedPeopleCleared).
		changed := false
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			replaced, changedItem := replaceImportedPeopleSources(item, mappings)
			if replaced != importedPeopleCleared {
				result = append(result, replaced)
			}
			changed = changed || changedItem
		}
		return result, changed
	case map[string]any:
		if source, ok := importedPeopleSourceFromMap(typed); ok {
			if targetID, mapped := mappings[source.SourceID]; mapped {
				return targetID, true
			}
			return typed, false
		}
		changed := false
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			replaced, changedItem := replaceImportedPeopleSources(item, mappings)
			result[key] = replaced
			changed = changed || changedItem
		}
		return result, changed
	default:
		return value, false
	}
}

// visitImportedPeopleContentMentions calls visit for each imported person
// mention in the rich text a block keeps in its content: a caption or table
// cells.
func visitImportedPeopleContentMentions(value any, richText bool, visit func(span map[string]any, sourceID string)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			visitImportedPeopleContentMentions(item, richText, visit)
		}
	case map[string]any:
		if sourceID, _ := typed["userId"].(string); richText && typed["mention"] == "person" &&
			validImportedPeopleSourceID(sourceID) {
			visit(typed, sourceID)
			return
		}
		for key, item := range typed {
			visitImportedPeopleContentMentions(item, richText || graphContentRichTextField(key), visit)
		}
	}
}

// collectImportedPeopleContentSources adds the imported people a block's
// content, or a database cell's spans, mentions. A span carries only the
// imported ID, so its visible text stands in for the name.
func collectImportedPeopleContentSources(content string, richText bool, found map[string]importedPeopleSource) error {
	if !importedPeopleMarked(content) {
		return nil
	}
	var value any
	if json.Unmarshal([]byte(content), &value) != nil {
		return errImportedPeopleMappingFailed
	}
	visitImportedPeopleContentMentions(value, richText, func(span map[string]any, sourceID string) {
		text, _ := span["text"].(string)
		mergeImportedPeopleSource(found, importedPeopleSource{
			SourceID: sourceID, DisplayName: importedPeopleOptionalString(strings.TrimPrefix(text, "@")), Count: 1,
		})
	})
	return nil
}
