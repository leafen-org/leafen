package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const workspaceImportArchiveFileMarkerPrefix = "leafen-archive://files/"

type workspaceImportArchiveQueryer interface {
	QueryRow(string, ...any) *sql.Row
}

type workspaceImportArchiveReferenceValidator interface {
	workspaceImportArchiveQueryer
	Query(string, ...any) (*sql.Rows, error)
}

func workspaceImportArchiveReference(
	db workspaceImportArchiveQueryer,
	sourceID, expectedKind string,
) (string, error) {
	if sourceID == "" || strings.TrimSpace(sourceID) != sourceID {
		return "", workspaceImportInputError()
	}
	var targetID string
	if err := db.QueryRow(
		`SELECT target_id FROM mappings WHERE source_id = ? AND kind = ?`,
		sourceID,
		expectedKind,
	).Scan(&targetID); err != nil {
		return "", workspaceImportInputError()
	}
	return targetID, nil
}

func workspaceImportArchiveOptionalLiveOrTrashBlockReference(
	db workspaceImportArchiveQueryer, sourceID string,
) (string, error) {
	if sourceID == "" {
		return "", nil
	}
	// A sub-page's block in its parent's body has the sub-page's ID, and the
	// imported sub-page's block takes the imported page's.
	if target, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemPage); err == nil {
		return target, nil
	}
	if target, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemBlock); err == nil {
		return target, nil
	}
	if target, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemBlockTrash); err == nil {
		return target, nil
	}
	// A database page's body block.
	return workspaceImportArchiveReference(db, sourceID, workspaceExportItemRowPageBlock)
}

// workspaceImportArchiveHostReference maps the page that holds a database,
// its rows or comments: a page, or a database page whose body holds them.
func workspaceImportArchiveHostReference(db workspaceImportArchiveQueryer, sourceID string) (string, error) {
	targetID, err := workspaceImportArchiveReference(db, sourceID, workspaceExportItemPage)
	if err == nil {
		return targetID, nil
	}
	return workspaceImportArchiveReference(db, sourceID, workspaceExportItemRow)
}

func rewriteWorkspaceImportArchiveJSON(db *sql.DB, raw json.RawMessage) (json.RawMessage, error) {
	value := any(nil)
	if json.Unmarshal(raw, &value) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten, err := rewriteWorkspaceImportArchiveValue(db, "", value)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	return encoded, nil
}

// rewriteWorkspaceImportArchiveSpans remaps the references in a database
// text cell's spans as block text's are.
func rewriteWorkspaceImportArchiveSpans(db *sql.DB, spans []graphTextSpan) ([]graphTextSpan, error) {
	if len(spans) == 0 {
		return spans, nil
	}
	encoded, err := json.Marshal(spans)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	value := any(nil)
	if json.Unmarshal(encoded, &value) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten, err := rewriteWorkspaceImportArchiveNode(db, "spans", value, true, false)
	if err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	result := []graphTextSpan{}
	if json.Unmarshal(encoded, &result) != nil {
		return nil, workspaceImportInputError()
	}
	return result, nil
}

func rewriteWorkspaceImportArchiveValue(db *sql.DB, field string, value any) (any, error) {
	return rewriteWorkspaceImportArchiveNode(db, field, value, false, workspaceImportArchiveActionFields[field])
}

// workspaceImportArchiveActionFields hold a button's actions: a button
// block's content and a button property's settings keep them there.
var workspaceImportArchiveActionFields = map[string]bool{"buttonActionDocument": true, "button": true}

// rewriteWorkspaceImportArchiveNode remaps the references in archived JSON.
// A reference inside rich text is remapped as block text's is, and so is a
// link a block makes: one to content the archive does not carry stays as it
// was, as page duplication keeps links to what it didn't copy. A button's
// action (action) is such a link too: the database, properties, view, rows
// and page it works on may be outside a page's archive, which keeps them as
// they were, as page duplication does (page_duplication_references.go), so
// that the button works on them again where they are.
func rewriteWorkspaceImportArchiveNode(db *sql.DB, field string, value any, richText, action bool) (any, error) {
	kinds := map[string]string{
		"customEmojiId":           workspaceExportItemCustomEmoji,
		"blockId":                 workspaceExportItemBlock,
		"parentBlockId":           workspaceExportItemBlock,
		"sourceContainerId":       workspaceExportItemBlock,
		"dataSourceId":            workspaceExportItemDataSource,
		"dataSourceIds":           workspaceExportItemDataSource,
		"additionalDataSourceIds": workspaceExportItemDataSource,
		"targetDataSourceId":      workspaceExportItemDataSource,
		"relationDataSourceId":    workspaceExportItemDataSource,
		"relationDatabaseId":      workspaceExportItemDataSource,
		"viewId":                  workspaceExportItemView,
		"viewIds":                 workspaceExportItemView,
		"dashboardViewId":         workspaceExportItemView,
		"dashboardRows":           workspaceExportItemView,
		"propertyId":              workspaceExportItemProperty,
		"groupBy":                 workspaceExportItemProperty,
		"relationPropertyId":      workspaceExportItemProperty,
		"rollupPropertyId":        workspaceExportItemProperty,
		"titlePropertyId":         workspaceExportItemProperty,
		"rowId":                   workspaceExportItemRow,
	}
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, workspaceImportArchiveFileMarkerPrefix) {
			if field != "icon" && field != "cover" && field != "fileId" &&
				field != "uploadId" && field != "fileUploadId" && field != "attachmentIds" {
				return nil, workspaceImportInputError()
			}
			return workspaceImportArchiveFileReference(
				db, strings.TrimPrefix(typed, workspaceImportArchiveFileMarkerPrefix),
			)
		}
		if richText && workspaceImportArchiveRichTextKinds[field] != nil {
			return workspaceImportArchiveMentionReference(db, field, typed)
		}
		if links := workspaceImportArchiveLinkKinds[field]; links != nil && typed != "" {
			return workspaceImportArchiveHeldReference(db, links, typed)
		}
		// A two-way relation names its other side, which the archive carries
		// with it; one whose other side is gone comes back one-way. A rollup
		// names the relation it goes through and the property it reads there;
		// one naming a property gone for good reads nothing, as it did.
		if (field == "relatedPropertyId" || field == "rollupRelationPropertyId" ||
			field == "rollupTargetPropertyId") && typed != "" {
			related, err := workspaceImportArchiveReference(db, typed, workspaceExportItemProperty)
			if err != nil {
				return nil, nil
			}
			return related, nil
		}
		// A database is named by its block in most places and by its data
		// source in a button's actions (interactive_automation_plan.go); the
		// two never share an ID, so either one is looked up.
		if field == "databaseId" && typed != "" {
			if target, err := workspaceImportArchiveReference(db, typed, workspaceExportItemBlock); err == nil {
				return target, nil
			}
			if action {
				return workspaceImportArchiveHeldReference(db, []string{workspaceExportItemDataSource}, typed)
			}
			return workspaceImportArchiveReference(db, typed, workspaceExportItemDataSource)
		}
		// A linked database's source sits on a page or in a database page's
		// body, as a page's database does (2026-11-17: one in a database
		// page's body failed a whole archive import).
		if field == "sourceContainerId" && typed != "" && !action {
			if target, err := workspaceImportArchiveReference(db, typed, workspaceExportItemBlock); err == nil {
				return target, nil
			}
			return workspaceImportArchiveReference(db, typed, workspaceExportItemRowPageBlock)
		}
		// A view's own order of its rows names the restored rows, so it shows
		// them as it did; a row the archive doesn't hold matches none.
		if field == "rowOrder" {
			if target, err := workspaceImportArchiveReference(db, typed, workspaceExportItemRow); err == nil {
				return target, nil
			}
			return typed, nil
		}
		kind := kinds[field]
		if kind == "" {
			// A view's or a property's other references to properties and
			// views — a calendar's date, a timeline's start and end, a task
			// database's status — are named as page duplication names them,
			// so the two never drift apart.
			switch pageDuplicationDatabaseReferenceKind(field) {
			case "property":
				kind = workspaceExportItemProperty
			case "view":
				kind = workspaceExportItemView
			}
		}
		// A view setting naming a property names the restored one, or keeps
		// a name it has no copy of (database_view_property_settings.go).
		if kind == "" && databaseViewPropertyFields[field] && typed != "" {
			if target, err := workspaceImportArchiveReference(db, typed, workspaceExportItemProperty); err == nil {
				return target, nil
			}
			return typed, nil
		}
		if kind == "" || typed == "" {
			return typed, nil
		}
		if action {
			return workspaceImportArchiveHeldReference(db, []string{kind}, typed)
		}
		return workspaceImportArchiveReference(db, typed, kind)
	case []any:
		for index, child := range typed {
			rewritten, err := rewriteWorkspaceImportArchiveNode(db, field, child, richText, action)
			if err != nil {
				return nil, err
			}
			typed[index] = rewritten
		}
	case map[string]any:
		// A callout's workspace emoji the archive didn't bring shows its picture.
		if !richText {
			workspaceImportArchiveCalloutCustomEmoji(db, typed)
		}
		renameDatabaseViewSettingProperties(field, typed, func(source string) (string, bool) {
			target, err := workspaceImportArchiveReference(db, source, workspaceExportItemProperty)
			return target, err == nil
		})
		for key, child := range typed {
			rewritten, err := rewriteWorkspaceImportArchiveNode(
				db, key, child, richText || graphContentRichTextField(key), action || workspaceImportArchiveActionFields[key],
			)
			if err != nil {
				return nil, err
			}
			typed[key] = rewritten
		}
		// A person a span mentions is the one here (workspace_import_archive_people.go).
		if richText {
			if err := workspaceImportArchiveMention(db, typed); err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}

// workspaceImportArchiveRichTextKinds are the kinds a rich-text reference
// attribute may name, in the order they are looked up.
var workspaceImportArchiveRichTextKinds = map[string][]string{
	"pageId":        {workspaceExportItemPage, workspaceExportItemRow},
	"databaseId":    {workspaceExportItemBlock},
	"customEmojiId": {workspaceExportItemCustomEmoji},
	"commentId":     {workspaceExportItemComment},
}

// workspaceImportArchiveLinkKinds are the links a block makes, to a page,
// a database, a comment or a synced block's original, with the kinds each
// may name in the order they are looked up — a button's "열기" names a
// database a page holds by its block (interactive_automation_open_target.go).
var workspaceImportArchiveLinkKinds = map[string][]string{
	"pageId":              {workspaceExportItemPage, workspaceExportItemRow, workspaceExportItemBlock},
	"childPageId":         {workspaceExportItemPage, workspaceExportItemRow},
	"syncedPageId":        {workspaceExportItemPage, workspaceExportItemRow},
	"databaseContainerId": {workspaceExportItemBlock},
	"commentId":           {workspaceExportItemComment},
	"syncedBlockId":       {workspaceExportItemBlock, workspaceExportItemRowPageBlock},
}

// workspaceImportArchiveHeldReference remaps one reference to the imported
// object, or keeps it when the archive does not carry it.
func workspaceImportArchiveHeldReference(
	db workspaceImportArchiveQueryer, kinds []string, sourceID string,
) (string, error) {
	for _, kind := range kinds {
		var targetID string
		err := db.QueryRow(`SELECT target_id FROM mappings WHERE source_id = ? AND kind = ?`, sourceID, kind).Scan(&targetID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", workspaceImportStorageError(err)
		}
		return targetID, nil
	}
	return sourceID, nil
}

func workspaceImportArchiveFileReference(db *sql.DB, sourceID string) (string, error) {
	if !workspaceExportFileRecordIDPattern.MatchString(sourceID) {
		return "", workspaceImportInputError()
	}
	var targetID string
	if err := db.QueryRow(`SELECT target_id FROM files WHERE source_id = ?`, sourceID).Scan(&targetID); err != nil {
		return "", workspaceImportInputError()
	}
	return targetID, nil
}

func inventoryWorkspaceImportArchiveFileReferences(tx *sql.Tx, field string, value any) error {
	switch typed := value.(type) {
	case string:
		if !strings.HasPrefix(typed, workspaceImportArchiveFileMarkerPrefix) {
			return nil
		}
		if field != "icon" && field != "cover" && field != "fileId" &&
			field != "uploadId" && field != "fileUploadId" && field != "attachmentIds" {
			return workspaceImportInputError()
		}
		sourceID := strings.TrimPrefix(typed, workspaceImportArchiveFileMarkerPrefix)
		if !workspaceExportFileRecordIDPattern.MatchString(sourceID) {
			return workspaceImportInputError()
		}
		result, err := tx.Exec(`UPDATE files SET reference_count = reference_count + 1 WHERE source_id = ?`, sourceID)
		if err != nil {
			return workspaceImportStorageErrorAt(err, "archive-file-reference-inventory")
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return workspaceImportInputError()
		}
	case []any:
		for _, child := range typed {
			if err := inventoryWorkspaceImportArchiveFileReferences(tx, field, child); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range typed {
			if err := inventoryWorkspaceImportArchiveFileReferences(tx, key, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func workspaceImportArchiveMarkedFileReference(db *sql.DB, marker string) (string, error) {
	if !strings.HasPrefix(marker, workspaceImportArchiveFileMarkerPrefix) {
		return "", workspaceImportInputError()
	}
	return workspaceImportArchiveFileReference(
		db, strings.TrimPrefix(marker, workspaceImportArchiveFileMarkerPrefix),
	)
}

func validateWorkspaceImportArchiveReferences(db workspaceImportArchiveReferenceValidator) error {
	var invalidPages int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM entities AS item
		LEFT JOIN mappings AS page ON page.source_id = item.page_id AND page.kind IN ('page', 'row')
		WHERE item.kind NOT IN (?, ?) AND page.source_id IS NULL
			-- People's automations stand on People, not a page (workspace_import_archive_automations.go).
			AND NOT (item.kind = ? AND substr(item.page_id, 1, ?) = ?)
	`, workspaceExportItemCustomEmoji, workspaceExportItemTeamspace, workspaceExportItemDatabaseAutomation,
		len(peopleArchiveSourcePrefix), peopleArchiveSourcePrefix).Scan(&invalidPages); err != nil {
		return workspaceImportStorageError(err)
	}
	var invalidYgo int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM ygo_pages AS ygo
		LEFT JOIN mappings AS page ON page.source_id = ygo.page_id AND page.kind = 'page'
		WHERE page.source_id IS NULL
	`).Scan(&invalidYgo); err != nil {
		return workspaceImportStorageError(err)
	}
	var unreferencedFiles int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files WHERE reference_count = 0`).Scan(&unreferencedFiles); err != nil {
		return workspaceImportStorageError(err)
	}
	if invalidPages != 0 || invalidYgo != 0 || unreferencedFiles != 0 {
		return workspaceImportInputError()
	}
	return validateWorkspaceImportArchiveDatabaseContainers(db)
}

func workspaceImportArchiveOperationCount(db workspaceImportArchiveQueryer) int {
	var blocks, sources, views, properties, rows, comments, templates, blockTrash, customEmojis int
	var rowPageBlocks, rowBlockTrash, automations int
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemBlock).Scan(&blocks)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemDataSource).Scan(&sources)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemView).Scan(&views)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemProperty).Scan(&properties)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemRow).Scan(&rows)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemComment).Scan(&comments)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemTemplate).Scan(&templates)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemBlockTrash).Scan(&blockTrash)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemCustomEmoji).Scan(&customEmojis)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemRowPageBlock).Scan(&rowPageBlocks)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemRowBlockTrash).Scan(&rowBlockTrash)
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemDatabaseAutomation).Scan(&automations)
	return blocks*2 + sources + views + properties + rows*2 + comments + templates + blockTrash +
		customEmojis + rowPageBlocks*2 + rowBlockTrash + automations
}

func workspaceImportArchiveOperationCountFromCounts(counts map[string]int) int {
	return counts[workspaceExportItemBlock]*2 +
		counts[workspaceExportItemDataSource] +
		counts[workspaceExportItemView] +
		counts[workspaceExportItemProperty] +
		counts[workspaceExportItemRow]*2 +
		counts[workspaceExportItemComment] +
		counts[workspaceExportItemTemplate] +
		counts[workspaceExportItemBlockTrash] +
		counts[workspaceExportItemCustomEmoji] +
		counts[workspaceExportItemRowPageBlock]*2 +
		counts[workspaceExportItemRowBlockTrash] +
		counts[workspaceExportItemDatabaseAutomation]
}

func workspaceImportArchiveKindCount(db workspaceImportArchiveQueryer, kind string) int {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind = ?`, kind).Scan(&count)
	return count
}
