package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/tools/types"
)

// The archive item encoder turns staged pages and graph operations into
// import items in archive order and hands each one to the chunk writer.

func appendWorkspaceImportArchivePages(w *workspaceImportArchiveChunkWriter, db *sql.DB) (string, int, error) {
	// Parents first (workspace_import_archive_page_order.go).
	order, err := workspaceImportArchivePageOrder(db)
	if err != nil {
		return "", 0, err
	}
	title := "Leafen archive"
	count := 0
	for _, sequence := range order {
		var encoded string
		if err := db.QueryRow(`SELECT value_json FROM entities WHERE kind = ? AND sequence = ?`,
			workspaceExportItemPage, sequence).Scan(&encoded); err != nil {
			return "", 0, workspaceImportStorageError(err)
		}
		item, candidateTitle, err := workspaceImportArchivePageItem(db, encoded, float64(sequence))
		if err != nil {
			return "", 0, err
		}
		if count == 0 && candidateTitle != "" {
			title = candidateTitle
		}
		if err := w.append(item); err != nil {
			return "", 0, err
		}
		count++
	}
	if count == 0 {
		return "", 0, workspaceImportInputError()
	}
	return title, count, nil
}

func workspaceImportArchivePageItem(
	db *sql.DB,
	encoded string,
	fallbackPosition float64,
) (workspaceImportStagedItem, string, error) {
	value := archivePageValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil {
		return workspaceImportStagedItem{}, "", workspaceImportInputError()
	}
	parentID := ""
	var err error
	if value.ParentID != "" {
		parentID, err = workspaceImportArchiveReference(db, value.ParentID, workspaceExportItemPage)
		if err != nil {
			// A database page's sub-page is made under the page holding its
			// database, before the database page is, and moves under it once
			// it is.
			parentID, err = workspaceImportArchiveRowHostPage(db, value.ParentID)
		}
		if err != nil {
			return workspaceImportStagedItem{}, "", err
		}
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemPage)
	if err != nil {
		return workspaceImportStagedItem{}, "", err
	}
	if value.IconType == "image" && strings.HasPrefix(value.Icon, workspaceImportArchiveFileMarkerPrefix) {
		value.Icon, err = workspaceImportArchiveMarkedFileReference(db, value.Icon)
		if err != nil {
			return workspaceImportStagedItem{}, "", err
		}
	}
	if value.IconType == "custom_emoji" {
		value.Icon, value.IconType, err = workspaceImportArchiveCustomEmojiIcon(db, value.Icon, value.CustomEmoji)
		if err != nil {
			return workspaceImportStagedItem{}, "", err
		}
	}
	if strings.HasPrefix(value.Cover, workspaceImportArchiveFileMarkerPrefix) {
		value.Cover, err = workspaceImportArchiveMarkedFileReference(db, value.Cover)
		if err != nil {
			return workspaceImportStagedItem{}, "", err
		}
	}
	if !validWorkspaceImportArchivePage(value) {
		return workspaceImportStagedItem{}, "", workspaceImportInputError()
	}
	position := fallbackPosition
	if value.Position != nil {
		position = *value.Position
	}
	var hasYgo bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM ygo_pages WHERE page_id = ?)`, value.ID).Scan(&hasYgo); err != nil {
		return workspaceImportStagedItem{}, "", workspaceImportStorageError(err)
	}
	var teamspace *workspaceImportStagedTeamspace
	if value.ParentID == "" {
		if teamspace, err = workspaceImportArchivePageTeamspace(db, value.TeamspaceID); err != nil {
			return workspaceImportStagedItem{}, "", err
		}
	}
	// A page a full-page database left as a signpost leads to the database's
	// imported block, the database's own ID, when the archive holds it; else
	// it comes in as a page in the trash.
	// A moved workspace's page keeps who made it and its reactions where the
	// people were paired (workspace_import_archive_people.go).
	createdBy, err := workspaceImportArchiveAccount(db, value.CreatedBy)
	if err != nil {
		return workspaceImportStagedItem{}, "", err
	}
	if err := workspaceImportArchiveReactions(db, value.Reactions); err != nil {
		return workspaceImportStagedItem{}, "", err
	}
	titleSpans, err := archivedPageTitleSpans(db, value.Title, value.TitleSpans)
	if err != nil {
		return workspaceImportStagedItem{}, "", err
	}
	movedDatabase := ""
	if value.MovedDatabase != nil && *value.MovedDatabase != "" && value.InTrash {
		if block, err := workspaceImportArchiveReference(db, *value.MovedDatabase, workspaceExportItemBlock); err == nil {
			movedDatabase = block
		}
	}
	return workspaceImportStagedItem{PageID: id, Page: &workspaceImportStagedPage{
		ID: id, TeamspaceID: value.TeamspaceID, ParentID: parentID,
		Position: position, BodyPosition: value.BodyPosition, Title: value.Title, TitleSpans: titleSpans, Content: value.Content,
		Icon: value.Icon, IconType: value.IconType, Cover: value.Cover,
		CoverPosition: value.CoverPosition, Font: normalizedPageFont(value.Font),
		SmallText:              value.SmallText != nil && *value.SmallText,
		FullWidth:              value.FullWidth != nil && *value.FullWidth,
		BacklinksDisplay:       normalizedPageCustomizationDisplay(value.BacklinksDisplay),
		PageCommentsDisplay:    normalizedPageCustomizationDisplay(value.PageCommentsDisplay),
		TableOfContentsDisplay: normalizedPageTableOfContentsDisplay(value.TableOfContentsDisplay), InlineCommentsDisplay: normalizedPageInlineCommentsDisplay(value.InlineCommentsDisplay),
		Kind: archivePageKind(value.Kind), MovedDatabase: movedDatabase, workspaceImportPageAnchor: workspaceImportArchivePageAnchor(db, value),
		InTrash: value.InTrash, TrashedAt: value.TrashedAt, ArchivedAt: value.ArchivedAt,
		IsLocked: value.IsLocked != nil && *value.IsLocked,
		Root:     value.ParentID == "", HasYgo: hasYgo, Teamspace: teamspace,
		Reactions: value.Reactions, CreatedBy: createdBy, EditedAt: workspaceImportArchiveEditedAt(value.UpdatedAt),
	}}, strings.TrimSpace(value.Title), nil
}

// workspaceImportArchiveEditedAt is an archived page's last edit time as a
// page keeps it, or none for a time that can't be read.
func workspaceImportArchiveEditedAt(raw string) string {
	edited, err := types.ParseDateTime(strings.TrimSpace(raw))
	if err != nil || edited.IsZero() || edited.Time().After(time.Now().Add(time.Minute)) {
		return ""
	}
	return edited.String()
}

func validWorkspaceImportArchivePage(value archivePageValue) bool {
	if strings.TrimSpace(value.ID) != value.ID || value.ID == "" || len(value.ID) > maxGraphIdentifierLength ||
		strings.TrimSpace(value.TeamspaceID) != value.TeamspaceID || len(value.TeamspaceID) > maxWorkspacePermissionIdentifierLength ||
		strings.TrimSpace(value.ParentID) != value.ParentID || len(value.ParentID) > maxGraphIdentifierLength ||
		value.ParentID == value.ID || utf8.RuneCountInString(value.Title) > maxGraphTitleRunes || len(value.Content) > 2_000_000 ||
		!validWorkspaceImportArchivePageIcon(value.Icon, value.IconType) ||
		utf8.RuneCountInString(value.Cover) > maxPageCoverRunes || value.CoverPosition < 0 || value.CoverPosition > 100 ||
		(value.Font != nil && !validPageFont(*value.Font)) ||
		(value.BacklinksDisplay != nil && !validPageCustomizationDisplay(*value.BacklinksDisplay)) ||
		(value.PageCommentsDisplay != nil && !validPageCustomizationDisplay(*value.PageCommentsDisplay)) ||
		(value.TableOfContentsDisplay != nil && *value.TableOfContentsDisplay != "" && !validPageTableOfContentsDisplay(*value.TableOfContentsDisplay)) ||
		(value.InlineCommentsDisplay != nil && *value.InlineCommentsDisplay != "" && !validPageInlineCommentsDisplay(*value.InlineCommentsDisplay)) ||
		// A page an older archive kept without its kind is a page.
		(value.Kind != nil && *value.Kind != "" && *value.Kind != pageKindPage && *value.Kind != pageKindDatabase) ||
		(value.Position != nil && (math.IsNaN(*value.Position) || math.IsInf(*value.Position, 0))) ||
		(value.BodyPosition != nil && *value.BodyPosition < 0) ||
		!validWorkspaceImportCommentReactions(value.Reactions) {
		return false
	}
	if value.TrashedAt != nil {
		trashedAt := strings.TrimSpace(*value.TrashedAt)
		if !value.InTrash || trashedAt != *value.TrashedAt || len(trashedAt) > 64 {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, trashedAt); err != nil {
			return false
		}
	}
	return value.ArchivedAt == nil || validWorkspaceImportArchivedAt(*value.ArchivedAt)
}

// validWorkspaceImportArchivedAt accepts the archive time an export keeps
// for an archived page or row page.
func validWorkspaceImportArchivedAt(archivedAt string) bool {
	if archivedAt == "" || strings.TrimSpace(archivedAt) != archivedAt || len(archivedAt) > 64 {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, archivedAt)
	return err == nil
}

// archivePageKind keeps an archived page's kind; an archive from before
// kinds were kept has none.
func archivePageKind(kind *string) string {
	if kind == nil {
		return ""
	}
	return *kind
}

func validWorkspaceImportArchivePageIcon(icon, iconType string) bool {
	if utf8.RuneCountInString(icon) > maxPageIconRunes || utf8.RuneCountInString(iconType) > maxPageIconType {
		return false
	}
	if icon == "" {
		return iconType == "" || iconType == "none"
	}
	return validStoredPresentationIcon(icon, iconType)
}

// appendWorkspaceImportArchiveOperations writes a pass of an archive read
// whole, by the filter and items an archive prepared as it uploads takes
// (workspace_import_archive_prepare_transform.go).
func appendWorkspaceImportArchiveOperations(
	w *workspaceImportArchiveChunkWriter, db *sql.DB, pass workspaceImportArchiveTransformPass,
) error {
	passFilter, err := workspaceImportArchivePassFilter(pass)
	if err != nil {
		return err
	}
	levelFilter, levelArgs := workspaceImportArchiveLevelFilter(pass)
	rows, err := db.Query(`SELECT sequence, source_id, page_id, value_json FROM entities WHERE kind = ?`+
		passFilter+levelFilter+` ORDER BY page_id, sequence`, append([]any{pass.kind}, levelArgs...)...)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		row := workspaceImportArchiveTransformRow{}
		if rows.Scan(&row.sequence, &row.sourceID, &row.pageID, &row.encoded) != nil {
			return workspaceImportInputError()
		}
		items, _, err := workspaceImportArchiveTransformItems(db, pass, row)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := w.append(item); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

// workspaceImportArchiveBlockType reads an archived block's type.
func workspaceImportArchiveBlockType(encoded string) (string, error) {
	block := struct {
		Type string `json:"type"`
	}{}
	if json.Unmarshal([]byte(encoded), &block) != nil {
		return "", workspaceImportInputError()
	}
	return block.Type, nil
}

// workspaceImportArchiveTableRowItems are a table's rows, each under the
// table, in their order, to come right after it — a page's table or one in a
// database row's page.
func workspaceImportArchiveTableRowItems(
	db *sql.DB, kind, mode, tableSourceID, pageID, tableID string,
) ([]workspaceImportStagedItem, error) {
	rows, err := db.Query(`
		SELECT source_id, value_json FROM entities INDEXED BY entities_kind_parent
		WHERE kind = ? AND parent_id = ? ORDER BY position, source_id
	`, kind, tableSourceID)
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	type tableRow struct{ sourceID, encoded string }
	found := []tableRow{}
	for rows.Next() {
		var row tableRow
		if rows.Scan(&row.sourceID, &row.encoded) != nil {
			rows.Close()
			return nil, workspaceImportInputError()
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, workspaceImportStorageError(err)
	}
	rows.Close()
	items := make([]workspaceImportStagedItem, 0, len(found))
	for _, row := range found {
		var item workspaceImportStagedItem
		if kind == workspaceExportItemRowPageBlock {
			item, err = workspaceImportArchiveRowBlockOperation(db, mode, row.encoded)
		} else {
			item, err = workspaceImportArchiveOperation(db, kind, mode, row.sourceID, pageID, row.encoded)
		}
		if err != nil {
			return nil, err
		}
		if item.Operation == nil || item.Operation.Block == nil || item.Operation.Block.Type != graphBlockTypeTableRow {
			return nil, workspaceImportInputError()
		}
		parent := tableID
		item.Operation.Block.ParentID = &parent
		items = append(items, item)
	}
	return items, nil
}

// workspaceImportArchiveRootTableMode reports a pass that makes a page's
// blocks without their places: a table comes with its rows in it there, as a
// table can't be without rows nor a row outside its table.
func workspaceImportArchiveRootTableMode(kind, mode string) bool {
	return (kind == workspaceExportItemBlock || kind == workspaceExportItemRowPageBlock) &&
		(mode == "root_source" || mode == "root_remaining")
}

func workspaceImportArchiveOperation(
	db *sql.DB,
	kind, mode, sourceID, pageID, encoded string,
) (workspaceImportStagedItem, error) {
	switch kind {
	case workspaceExportItemDataSource:
		value := archiveDataSourceValue{}
		if json.Unmarshal([]byte(encoded), &value) != nil {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemDataSource)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		icon, iconType, err := workspaceImportArchivePresentationIcon(db, value.Icon, value.IconType, value.CustomEmoji)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		wiki, err := workspaceImportArchiveWikiSource(db, value)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		taskSettings, err := workspaceImportArchiveTaskSettings(db, value)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		// A database's cover comes in as a page's does.
		cover, coverPosition := value.Cover, 50
		if value.CoverPosition != nil {
			coverPosition = *value.CoverPosition
		}
		if utf8.RuneCountInString(cover) > maxPageCoverRunes || coverPosition < 0 || coverPosition > 100 {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		if strings.HasPrefix(cover, workspaceImportArchiveFileMarkerPrefix) {
			if cover, err = workspaceImportArchiveMarkedFileReference(db, cover); err != nil {
				return workspaceImportStagedItem{}, err
			}
		}
		description, descriptionHidden := value.Description, value.DescriptionHidden
		identity := importedDataSourceStarterIdentity(value.StarterIdentity)
		return workspaceImportStagedItem{PageID: pageID, WikiSource: wiki, TaskSettings: taskSettings, StarterIdentity: identity, Operation: &graphOperation{
			Kind: graphOperationKindUpsertDataSource,
			DataSource: &graphDataSource{
				ID: id, Title: value.Title, TitleSpans: value.TitleSpans, Icon: &icon, IconType: &iconType,
				Description: &description, DescriptionSpans: value.DescriptionSpans, DescriptionHidden: &descriptionHidden,
				Cover: &cover, CoverPosition: &coverPosition,
			},
		}}, nil
	case workspaceExportItemProperty:
		value := archivePropertyValue{}
		if json.Unmarshal([]byte(encoded), &value) != nil {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemProperty)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		source, err := workspaceImportArchiveReference(db, value.DataSourceID, workspaceExportItemDataSource)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		config, err := rewriteWorkspaceImportArchiveJSON(db, value.Config)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		if value.DeletedAt != nil && !validWorkspaceImportArchivedAt(*value.DeletedAt) {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		return workspaceImportStagedItem{PageID: pageID, PropertyDeletedAt: value.DeletedAt, Operation: &graphOperation{
			Kind:     graphOperationKindUpsertProperty,
			Property: &graphProperty{ID: id, DataSourceID: source, Type: value.Type, Name: value.Name, Position: value.Position, Config: config},
		}}, nil
	case workspaceExportItemView:
		value := archiveViewValue{}
		if json.Unmarshal([]byte(encoded), &value) != nil {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemView)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		source, err := workspaceImportArchiveReference(db, value.DataSourceID, workspaceExportItemDataSource)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		config, err := rewriteWorkspaceImportArchiveJSON(db, value.Config)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		return workspaceImportStagedItem{PageID: pageID, Operation: &graphOperation{
			Kind: graphOperationKindUpsertView,
			View: &graphDatabaseView{ID: id, DataSourceID: source, Type: value.Type, Name: value.Name, Position: value.Position, Config: config},
		}}, nil
	case workspaceExportItemRow:
		return workspaceImportArchiveRowOperation(db, mode, pageID, encoded)
	case workspaceExportItemBlock:
		return workspaceImportArchiveBlockOperation(db, mode, sourceID, pageID, encoded)
	default:
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
}

func workspaceImportArchiveRowOperation(db *sql.DB, mode, pageID, encoded string) (workspaceImportStagedItem, error) {
	value := archiveRowValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil || (mode != "skeleton" && mode != "full") {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemRow)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	source, err := workspaceImportArchiveReference(db, value.DataSourceID, workspaceExportItemDataSource)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	row := &graphDatabaseRow{ID: id, DataSourceID: source, Position: value.Position, Values: map[string]graphRowValue{}}
	var presentation *workspaceImportStagedRowPresentation
	if mode == "full" {
		if value.Icon, value.IconType, err = workspaceImportArchivePresentationIcon(
			db, value.Icon, value.IconType, value.CustomEmoji,
		); err != nil {
			return workspaceImportStagedItem{}, err
		}
		// A database page whose font no one chose has none in its archive,
		// and shows the default one.
		if value.Font != nil && *value.Font == "" {
			value.Font = nil
		}
		if value.ArchivedAt != nil && !validWorkspaceImportArchivedAt(*value.ArchivedAt) ||
			value.TrashedAt != nil && !validWorkspaceImportArchivedAt(*value.TrashedAt) ||
			utf8.RuneCountInString(value.Cover) > maxPageCoverRunes || value.CoverPosition < 0 || value.CoverPosition > 100 ||
			value.Font != nil && !validPageFont(*value.Font) || value.TableOfContentsDisplay != nil && *value.TableOfContentsDisplay != "" && !validPageTableOfContentsDisplay(*value.TableOfContentsDisplay) ||
			value.InlineCommentsDisplay != nil && *value.InlineCommentsDisplay != "" && !validPageInlineCommentsDisplay(*value.InlineCommentsDisplay) ||
			!validWorkspaceImportCommentReactions(value.Reactions) {
			return workspaceImportStagedItem{}, workspaceImportInputError()
		}
		if err := workspaceImportArchiveReactions(db, value.Reactions); err != nil {
			return workspaceImportStagedItem{}, err
		}
		// A database page's cover comes in as a page's does.
		if strings.HasPrefix(value.Cover, workspaceImportArchiveFileMarkerPrefix) {
			if value.Cover, err = workspaceImportArchiveMarkedFileReference(db, value.Cover); err != nil {
				return workspaceImportStagedItem{}, err
			}
		}
		presentation = &workspaceImportStagedRowPresentation{
			RowID: id, Icon: value.Icon, IconType: value.IconType, Cover: value.Cover,
			CoverPosition: value.CoverPosition, ArchivedAt: value.ArchivedAt, TrashedAt: value.TrashedAt,
			Font: normalizedPageFont(value.Font), SmallText: value.SmallText != nil && *value.SmallText,
			FullWidth: value.FullWidth != nil && *value.FullWidth, TableOfContentsDisplay: normalizedPageTableOfContentsDisplay(value.TableOfContentsDisplay),
			InlineCommentsDisplay: normalizedPageInlineCommentsDisplay(value.InlineCommentsDisplay),
			Reactions:             value.Reactions,
		}
		if presentation.SubPageIDs, err = workspaceImportArchiveRowSubPages(db, value.ID); err != nil {
			return workspaceImportStagedItem{}, err
		}
		if value.TrashedAt != nil {
			presentation.TrashRootID = id
			if value.TrashRootID != nil && *value.TrashRootID != value.ID {
				if presentation.TrashRootID, err = workspaceImportArchiveReference(
					db, *value.TrashRootID, workspaceExportItemRow,
				); err != nil {
					return workspaceImportStagedItem{}, err
				}
			}
		}
	}
	if mode == "full" && value.SubitemParentID != nil {
		parent, err := workspaceImportArchiveReference(db, *value.SubitemParentID, workspaceExportItemRow)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		row.SubitemParentID = &parent
	}
	for propertyID, rowValue := range value.Values {
		if mode == "skeleton" && rowValue.Type == graphValueTypeRelation {
			continue
		}
		mappedProperty, err := workspaceImportArchiveReference(db, propertyID, workspaceExportItemProperty)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		if rowValue.Type == graphValueTypeRelation {
			var relations []string
			if json.Unmarshal(rowValue.Value, &relations) != nil {
				return workspaceImportStagedItem{}, workspaceImportInputError()
			}
			for index, relation := range relations {
				relations[index], err = workspaceImportArchiveReference(db, relation, workspaceExportItemRow)
				if err != nil {
					return workspaceImportStagedItem{}, err
				}
			}
			rowValue.Value, _ = json.Marshal(relations)
		}
		if rowValue.Type == graphValueTypePerson {
			var people any
			if json.Unmarshal(rowValue.Value, &people) != nil {
				return workspaceImportStagedItem{}, workspaceImportInputError()
			}
			if people, err = workspaceImportArchivePersonValue(db, people); err != nil {
				return workspaceImportStagedItem{}, err
			}
			rowValue.Value, _ = json.Marshal(people)
		}
		rowValue.Value, err = rewriteWorkspaceImportArchiveJSON(db, rowValue.Value)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		if rowValue.Spans, err = rewriteWorkspaceImportArchiveSpans(db, rowValue.Spans); err != nil {
			return workspaceImportStagedItem{}, err
		}
		row.Values[mappedProperty] = rowValue
	}
	// A row keeps only the values someone set: one whose title was left or
	// made empty (a button's or an automation's new page, a cleared title)
	// has none, and comes in as it was, empty.
	return workspaceImportStagedItem{PageID: pageID, RowPresentation: presentation, Operation: &graphOperation{
		Kind: graphOperationKindUpsertRow, Row: row,
	}}, nil
}

func workspaceImportArchiveBlockOperation(db *sql.DB, mode, sourceID, pageID, encoded string) (workspaceImportStagedItem, error) {
	value := archiveBlockValue{}
	baseMode, _, validMode := workspaceImportArchiveBlockMode(mode)
	if json.Unmarshal([]byte(encoded), &value) != nil || !validMode {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	if value.BodyPosition != nil && (*value.BodyPosition < 0 || value.ParentID != nil) {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemBlock)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	var parent *string
	if baseMode == "full" && value.ParentID != nil {
		mapped, err := workspaceImportArchiveReference(db, *value.ParentID, workspaceExportItemBlock)
		if err != nil {
			return workspaceImportStagedItem{}, err
		}
		parent = &mapped
	}
	var afterID *string
	start := false
	if baseMode == "full" {
		parentID := ""
		if value.ParentID != nil {
			parentID = *value.ParentID
		}
		var predecessor string
		err := db.QueryRow(`
			SELECT source_id FROM entities INDEXED BY entities_block_order
			WHERE kind = ? AND page_id = ? AND parent_id = ?
				AND (position < ? OR (position = ? AND source_id < ?))
			ORDER BY position DESC, source_id DESC LIMIT 1
		`, workspaceExportItemBlock, value.PageID, parentID, value.Position, value.Position, value.ID).Scan(&predecessor)
		if errors.Is(err, sql.ErrNoRows) {
			start = true
		} else if err != nil {
			return workspaceImportStagedItem{}, workspaceImportStorageError(err)
		} else {
			mapped, err := workspaceImportArchiveReference(db, predecessor, workspaceExportItemBlock)
			if err != nil {
				return workspaceImportStagedItem{}, err
			}
			afterID = &mapped
		}
	}
	content, err := rewriteWorkspaceImportArchiveJSON(db, value.Content)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	if _, selected, err := workspaceImportArchiveLinkedViewSelection(db, value, id); err != nil {
		return workspaceImportStagedItem{}, err
	} else if selected != "" {
		if content, err = workspaceImportArchiveLinkedBlockContent(content, selected); err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	// A database holding its own source may also show sources other
	// databases hold, on pages sorted after its own; it comes in with its own
	// first, and with the others once every database holding a source has.
	if mode == "root_source" {
		if content, err = workspaceImportArchiveWithoutLinkedMembers(content); err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	text, delta := "", json.RawMessage(nil)
	if baseMode == "full" {
		if text, delta, err = workspaceImportArchiveBlockText(db, sourceID); err != nil {
			return workspaceImportStagedItem{}, err
		}
	}
	return workspaceImportStagedItem{PageID: pageID, Text: text, TextDelta: delta, Start: start, BodyPosition: value.BodyPosition, Operation: &graphOperation{
		Kind:  graphOperationKindUpsertBlock,
		Block: &graphBlock{ID: id, ParentID: parent, AfterID: afterID, Position: value.Position, Type: value.Type, Content: content},
	}}, nil
}

// workspaceImportArchiveWithoutLinkedMembers is a database block's content
// without the sources it shows from other databases.
func workspaceImportArchiveWithoutLinkedMembers(content json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(content, &fields) != nil {
		return nil, workspaceImportInputError()
	}
	if _, linked := fields["linkedMembers"]; !linked {
		return content, nil
	}
	delete(fields, "linkedMembers")
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	return encoded, nil
}

func workspaceImportArchiveBlockMode(mode string) (base string, sourceOnly bool, valid bool) {
	switch mode {
	case "root_source":
		return "root", true, true
	case "root_remaining":
		return "root", false, true
	case "full_source":
		return "full", true, true
	case "full_remaining":
		return "full", false, true
	default:
		return "", false, false
	}
}

// workspaceImportArchivePresentationIcon remaps a portable row or data-source
// icon to the imported file or custom emoji — or the emoji's picture
// (workspace_archive_custom_emoji_icon.go) — and normalizes an absent icon.
func workspaceImportArchivePresentationIcon(
	db *sql.DB, icon, iconType string, customEmoji *archiveCustomEmojiPicture,
) (string, string, error) {
	var err error
	if iconType == "image" && strings.HasPrefix(icon, workspaceImportArchiveFileMarkerPrefix) {
		if icon, err = workspaceImportArchiveMarkedFileReference(db, icon); err != nil {
			return "", "", err
		}
	}
	if iconType == "custom_emoji" {
		if icon, iconType, err = workspaceImportArchiveCustomEmojiIcon(db, icon, customEmoji); err != nil {
			return "", "", err
		}
	}
	if !validWorkspaceImportArchivePageIcon(icon, iconType) {
		return "", "", workspaceImportInputError()
	}
	if iconType == "" {
		iconType = "none"
	}
	return icon, iconType, nil
}

// workspaceImportArchiveRowHostPage is the imported page that holds the
// database of an archived database page, or the database page holding it,
// up to a page however deep; a cycle ends as an input error.
func workspaceImportArchiveRowHostPage(db *sql.DB, rowSourceID string) (string, error) {
	hostSourceID := rowSourceID
	for seen := map[string]bool{}; !seen[hostSourceID]; {
		seen[hostSourceID] = true
		if err := db.QueryRow(`SELECT page_id FROM entities WHERE kind = ? AND source_id = ?`,
			workspaceExportItemRow, hostSourceID).Scan(&hostSourceID); err != nil {
			return "", workspaceImportInputError()
		}
		if page, err := workspaceImportArchiveReference(db, hostSourceID, workspaceExportItemPage); err == nil {
			return page, nil
		}
	}
	return "", workspaceImportInputError()
}

// workspaceImportArchiveRowSubPages are the imported sub-pages of an archived
// database page, in the archive's order.
func workspaceImportArchiveRowSubPages(db *sql.DB, rowSourceID string) ([]string, error) {
	rows, err := db.Query(`SELECT source_id FROM entities WHERE kind = ? AND parent_id = ? ORDER BY sequence`,
		workspaceExportItemPage, rowSourceID)
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	defer rows.Close()
	sources := []string{}
	for rows.Next() {
		var source string
		if rows.Scan(&source) != nil {
			return nil, workspaceImportInputError()
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	pages := make([]string, 0, len(sources))
	for _, source := range sources {
		page, err := workspaceImportArchiveReference(db, source, workspaceExportItemPage)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}
