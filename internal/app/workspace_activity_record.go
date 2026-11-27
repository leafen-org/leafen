package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceActivityPrefix = "workspace.activity."

// workspaceActivityWindow groups a person's changes: those of one kind to
// one thing within it are one row, at most six an hour for a page someone
// keeps editing. Notion's own window isn't shown; it too lists one
// person's edits of a page as one "edited" entry.
const workspaceActivityWindow = 10 * time.Minute

// The organization journal owns these facts too. Activity metadata contains
// names and references only, never a copy of document or property contents.
type workspaceActivityMetadata struct {
	PageID       string `json:"pageId"`
	BlockID      string `json:"blockId,omitempty"`
	Name         string `json:"name"`
	PreviousName string `json:"previousName,omitempty"`
	Kind         string `json:"kind,omitempty"`
	LocationID   string `json:"locationId,omitempty"`
	SourceID     string `json:"sourceId,omitempty"`
	// DatabaseName is a view's or a property's database as it was named
	// then, for when it is gone; the feed names it as it is now.
	DatabaseName   string `json:"databaseName,omitempty"`
	SubjectPageID  string `json:"subjectPageId,omitempty"`
	OperationJobID string `json:"operationJobId,omitempty"`
	// CopyJobs are the copies of blocks into a page its edit holds, each
	// group keeping all its own (workspace_activity_copy_merge.go).
	CopyJobs []string `json:"copyJobs,omitempty"`
	// Through is the connection the person made the change through, as the
	// page's history names it beside them (workspaceActivityThrough).
	Through string `json:"through,omitempty"`
	// Part is which of a database page's edits a group is: its record — its
	// properties — or its body, which Notion's updates list as two
	// (workspaceActivityRowPart); "" for any other page's, and for a group
	// from before they were two, which holds both.
	Part string `json:"part,omitempty"`
	// Made is a sub-page's making read as an edit of it, its name its change
	// (workspace_activity_sub_page.go); its group keeps it.
	Made bool `json:"made,omitempty"`
}

const (
	workspaceActivityPartRecord = "record"
	workspaceActivityPartBody   = "body"
)

// workspaceActivityRowPart is the part of a database page an edit of it
// belongs to: its properties' change (the row's own, kind database_row) its
// record, any other — its blocks, its look — its body. Notion's updates list
// a row's record and its body as two updates even where they were edited in
// turn (seen 2026-11-13).
func workspaceActivityRowPart(page *core.Record, entry workspaceActivityEntry) string {
	if entry.EntityType != "page" || entry.Action != "edit" || entry.EntityID != page.Id || !isDatabaseRowPageRecord(page) {
		return ""
	}
	if entry.Metadata.Kind == "database_row" {
		return workspaceActivityPartRecord
	}
	return workspaceActivityPartBody
}

// workspaceActivityEntry is one change as the feed groups it.
type workspaceActivityEntry struct {
	OrganizationID string
	WorkspaceID    string
	ActorID        string
	EntityType     string
	EntityID       string
	Action         string
	Metadata       workspaceActivityMetadata
	At             int64
	// Version is the change's version in its page's own history, 0 when
	// it has none there.
	Version int
	// Times is how many changes it counts in its group, 1 when unset.
	Times int
	// TitleEdit is a page's new title, an edit of it
	// (workspace_activity_title_change.go).
	TitleEdit bool
	// SubPageMade is a sub-page's making, and SubPageNamed one whose name
	// is an edit of it (workspace_activity_sub_page.go).
	SubPageMade, SubPageNamed bool
}

func recordWorkspaceActivity(app core.App, page *core.Record, actorID, origin, entityType, entityID, action string, metadata workspaceActivityMetadata, at int64) error {
	return recordWorkspaceActivityOfVersion(app, page, actorID, origin, entityType, entityID, action, metadata, at, 0)
}

// recordWorkspaceActivityOfVersion records a change that is a version of
// the page's own history, so its group knows its versions.
func recordWorkspaceActivityOfVersion(app core.App, page *core.Record, actorID, origin, entityType, entityID, action string, metadata workspaceActivityMetadata, at int64, version int) error {
	return recordWorkspaceActivityTimes(app, page, actorID, origin, entityType, entityID, action, metadata, at, version, 1)
}

// workspaceBlockActivity is one block's change in a change of a page's blocks.
type workspaceBlockActivity struct {
	BlockID  string
	Action   string
	Metadata workspaceActivityMetadata
	// JoinsEdit is a change that is an edit of its page only where the
	// person's edit of it is already a line: a sub-page's block deleted,
	// whose own line says so (workspace_activity_graph.go).
	JoinsEdit bool
}

// recordWorkspaceBlockActivities records the changes of a page's blocks one
// version made. Those that fold into the page's edit (foldWorkspaceBlockActivity)
// are one entry, written once where the first of them was and counted as
// many: written for each, a 20,100-block deletion wrote its page's one edit
// 20,101 times, about a third of the 5.5 s it held the one write connection
// (2026-11-08).
func recordWorkspaceBlockActivities(app core.App, page *core.Record, actorID, origin string, changes []workspaceBlockActivity, at int64, version int) error {
	folds := make([]bool, len(changes))
	folded, joining := 0, 0
	for index, change := range changes {
		folds[index] = foldWorkspaceBlockActivity(workspaceActivityEntry{EntityType: "block", Metadata: change.Metadata}) != nil
		if folds[index] {
			folded++
			if change.JoinsEdit {
				joining++
			}
		}
	}
	for index, change := range changes {
		times := 1
		if folds[index] {
			if folded == 0 {
				continue
			}
			// Changes that only join the page's edit make no line of it.
			if joining == folded {
				joined, err := workspaceActivityEditShown(app, page, actorID, change.Metadata, at)
				if err != nil || !joined {
					return err
				}
			}
			times, folded = folded, 0
		}
		if err := recordWorkspaceActivityTimes(app, page, actorID, origin, "block", change.BlockID, change.Action, change.Metadata, at, version, times); err != nil {
			return err
		}
	}
	return nil
}

// workspaceActivityEditShown says whether a person's edit of a page is a
// line in the window a change at `at` falls in.
func workspaceActivityEditShown(app core.App, page *core.Record, actorID string, metadata workspaceActivityMetadata, at int64) (bool, error) {
	entry, err := workspaceActivityEntryOf(app, page, actorID, "block", "", "edit", metadata, at, 0, 1)
	if err != nil || entry == nil {
		return false, err
	}
	var found int
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_activity}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": workspaceActivityGroupID(*entry)}).Row(&found)
	return found > 0, err
}

// recordWorkspaceActivityTimes records a change counted times in its group.
func recordWorkspaceActivityTimes(app core.App, page *core.Record, actorID, origin, entityType, entityID, action string, metadata workspaceActivityMetadata, at int64, version, times int) error {
	if origin == "" || entityID == "" {
		return errors.New(errorCodeWorkspaceActivityInvalid)
	}
	entry, err := workspaceActivityEntryOf(app, page, actorID, entityType, entityID, action, metadata, at, version, times)
	if err != nil || entry == nil {
		return err
	}
	_, err = writeWorkspaceActivity(app, *entry, true)
	return err
}

// workspaceActivityEntryOf is the entry a change writes to its group — a
// block's change folded into its page's edit — or none for a change no one
// made or a page being imported.
func workspaceActivityEntryOf(app core.App, page *core.Record, actorID, entityType, entityID, action string, metadata workspaceActivityMetadata, at int64, version, times int) (*workspaceActivityEntry, error) {
	if page.GetBool("notion_import_staging") || actorID == "" {
		return nil, nil
	}
	workspace, err := app.FindRecordById(workspacesCollection, page.GetString("workspace"))
	if err != nil {
		return nil, err
	}
	organizationID := workspace.GetString("organization")
	if organizationID == "" {
		return nil, errors.New(errorCodeWorkspaceActivityInvalid)
	}
	if !validWorkspaceActivityKind(action, entityType) {
		return nil, errors.New(errorCodeWorkspaceActivityInvalid)
	}
	metadata.PageID = page.Id
	entry := workspaceActivityEntry{OrganizationID: organizationID, WorkspaceID: workspace.Id, ActorID: actorID,
		EntityType: entityType, EntityID: entityID, Action: action, Metadata: metadata, At: at, Version: version, Times: times}
	if folded := foldWorkspaceBlockActivity(entry); folded != nil {
		name, err := workspaceActivityPageName(app, page)
		if err != nil {
			return nil, err
		}
		folded.Metadata.Name, folded.Metadata.LocationID = name, page.GetString("parent_id")
		entry = *folded
	}
	entry.Metadata.Part = workspaceActivityRowPart(page, entry)
	// A page's new title is an edit of it, a database page's of its record
	// (workspace_activity_title_change.go).
	if entry.TitleEdit = action == "rename" && entityType == "page" && workspaceActivityTitleIsEdit(page); entry.TitleEdit && isDatabaseRowPageRecord(page) {
		entry.Metadata.Part = workspaceActivityPartRecord
	}
	if entry.SubPageMade = workspaceActivitySubPageMade(page, entityType, entityID, action); entry.SubPageMade {
		entry.SubPageNamed = workspaceActivityTitleIsEdit(page)
	}
	if entry.Metadata.Through, err = workspaceActivityThrough(app, actorID); err != nil {
		return nil, err
	}
	return &entry, nil
}

// workspaceActivityThrough is the connection a change is written through
// that the page's history names beside its person — an app's screen or an
// agent connected as them ("〈person〉 through 〈app〉", pageHistoryConnectionOf
// as the version records it) — and "" for a change made by hand. An
// integration's change is named by it alone and joins its person's group, as
// Notion's updates name it beside them (workspaceActivityIntegrations); an
// app's own account is the app, named by its account alone.
func workspaceActivityThrough(app core.App, actorID string) (string, error) {
	connection := pageHistoryConnectionOf(app)
	if connection.Name == "" || pageHistoryConnectionKind(connection.ID) == "integration" {
		return "", nil
	}
	accounts, err := integrationAccountUsers(app, []string{actorID})
	if err != nil || accounts[actorID] {
		return "", err
	}
	return connection.Name, nil
}

func validWorkspaceActivityKind(action, entityType string) bool {
	switch action {
	case "create", "edit", "delete", "rename", "move", "restore":
	default:
		return false
	}
	switch entityType {
	case "page", "block", "dataSource", "view", "property":
		return true
	}
	return false
}

// foldWorkspaceBlockActivity reads a change to one of a page's own blocks —
// made, edited, moved or deleted — as an edit of the page, as Notion's
// updates list them; a sub-page's block stays its own. The caller names the
// page and where it is.
func foldWorkspaceBlockActivity(entry workspaceActivityEntry) *workspaceActivityEntry {
	if entry.EntityType != "block" || entry.Metadata.SubjectPageID != "" {
		return nil
	}
	entry.EntityType, entry.EntityID, entry.Action = "page", entry.Metadata.PageID, "edit"
	entry.Metadata = workspaceActivityMetadata{PageID: entry.Metadata.PageID, OperationJobID: entry.Metadata.OperationJobID,
		CopyJobs: entry.Metadata.CopyJobs}
	return &entry
}

// workspaceActivityGroupID names an entry's group: the person, the thing,
// the kind of change, the operation it belongs to and its window — and the
// connection they made it through, so what they did through an app and by
// hand are two groups, as the page's history names them apart, and a
// database page's record and body (workspaceActivityRowPart).
func workspaceActivityGroupID(entry workspaceActivityEntry) string {
	window := entry.At / workspaceActivityWindow.Milliseconds()
	parts := []string{"activity-group", entry.WorkspaceID, entry.Metadata.PageID, entry.ActorID,
		entry.EntityType, entry.EntityID, entry.Action, entry.Metadata.OperationJobID, strconv.FormatInt(window, 10)}
	if entry.Metadata.Through != "" {
		parts = append(parts, "through", entry.Metadata.Through)
	}
	if entry.Metadata.Part != "" {
		parts = append(parts, "part", entry.Metadata.Part)
	}
	// A page's block deleted or the page back from the trash is a line each
	// (workspace_activity_page_block.go).
	if workspaceActivityOneEach(entry) {
		parts = append(parts, "at", strconv.FormatInt(entry.At, 10))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}

// writeWorkspaceActivity adds an entry to its group, counting it (Times) and
// keeping the latest metadata, the first previous name — an empty one too,
// a thing that had none — whether it holds a sub-page's making, and its
// first and last version. A new group also
// goes into the organization's audit log when audit is set; it says whether
// the group is new. A name changed only by spaces at its ends is no rename,
// and naming a thing its maker just made is its making
// (foldWorkspaceActivityNaming). A database, a view or a property renamed
// otherwise is edited, in one line with its other edits, as Notion's updates
// say it ("〈DB〉의 〈new name〉 속성을 편집했습니다.", seen 2026-11-13), and a
// page's new title is always an edit of it, its making's line named too
// (workspace_activity_title_change.go); the audit log still keeps it renamed.
// A sub-page's making is no line, or an edit of it named
// (workspace_activity_sub_page.go); the audit log keeps it made.
func writeWorkspaceActivity(app core.App, entry workspaceActivityEntry, audit bool) (bool, error) {
	times := max(1, entry.Times)
	entry.Metadata.Name = workspaceActivityName(entry.Metadata.Name)
	entry.Metadata.PreviousName = workspaceActivityName(entry.Metadata.PreviousName)
	// The entry as it was made: the audit log keeps it so where the feed
	// reads it otherwise, a rename as an edit or a sub-page's making.
	renamed := entry
	if !readWorkspaceActivitySubPageMade(&entry) {
		if audit {
			return false, auditWorkspaceActivityGroup(app, workspaceActivityGroupID(entry), entry)
		}
		return false, nil
	}
	if entry.Action == "rename" {
		if entry.Metadata.PreviousName == entry.Metadata.Name {
			return false, nil
		}
		folded, err := foldWorkspaceActivityNaming(app, entry)
		if err != nil || folded && !entry.TitleEdit {
			if err == nil && audit {
				err = auditWorkspaceActivityGroup(app, workspaceActivityGroupID(entry), entry)
			}
			return false, err
		}
		if entry.TitleEdit || entry.EntityType == "dataSource" || entry.EntityType == "view" || entry.EntityType == "property" {
			entry.Action, entry.Metadata.PreviousName = "edit", ""
		}
	}
	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return false, err
	}
	id := workspaceActivityGroupID(entry)
	var version any
	if entry.Version > 0 {
		version = entry.Version
	}
	var changes int
	if err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_activity}} ([[id]], [[organization_id]], [[workspace_id]], [[page_id]], [[actor_id]],
			[[action]], [[subject_type]], [[subject_id]], [[metadata_json]], [[changes]], [[created]], [[last_at]], [[first_version]], [[last_version]])
		VALUES ({:id}, {:organization}, {:workspace}, {:page}, {:actor}, {:action}, {:type}, {:subject}, {:metadata}, {:times}, {:at}, {:at}, {:version}, {:version})
		ON CONFLICT ([[id]]) DO UPDATE SET
			[[changes]] = [[changes]] + excluded.[[changes]],
			[[created]] = MIN([[created]], excluded.[[created]]),
			[[last_at]] = MAX([[last_at]], excluded.[[last_at]]),
			[[first_version]] = COALESCE(MIN([[first_version]], excluded.[[first_version]]), [[first_version]], excluded.[[first_version]]),
			[[last_version]] = COALESCE(MAX([[last_version]], excluded.[[last_version]]), [[last_version]], excluded.[[last_version]]),
			[[metadata_json]] = ` + workspaceActivityCopyJobsKept(`CASE WHEN excluded.[[last_at]] >= [[last_at]] THEN
				CASE WHEN [[action]] = 'rename'
					THEN json_set(excluded.[[metadata_json]], '$.previousName', COALESCE(json_extract([[metadata_json]], '$.previousName'), ''))
					WHEN json_extract([[metadata_json]], '$.made') THEN json_set(excluded.[[metadata_json]], '$.made', json('true'))
					ELSE excluded.[[metadata_json]] END
			ELSE
				CASE WHEN [[action]] = 'rename'
					THEN json_set([[metadata_json]], '$.previousName', COALESCE(json_extract(excluded.[[metadata_json]], '$.previousName'), ''))
					ELSE [[metadata_json]] END
			END`) + `
		RETURNING [[changes]]
	`).Bind(dbx.Params{"id": id, "organization": entry.OrganizationID, "workspace": entry.WorkspaceID, "page": entry.Metadata.PageID,
		"actor": entry.ActorID, "action": entry.Action, "type": entry.EntityType, "subject": entry.EntityID,
		"metadata": string(metadata), "at": entry.At, "version": version, "times": times}).Row(&changes); err != nil {
		return false, err
	}
	// A group it made counts only its changes; one it joined counted some before.
	created := changes == times
	if renamed.Action != entry.Action && audit {
		return created, auditWorkspaceActivityGroup(app, workspaceActivityGroupID(renamed), renamed)
	}
	if created && audit {
		if err := auditWorkspaceActivityGroup(app, id, entry); err != nil {
			return false, err
		}
	}
	return created, nil
}

// foldWorkspaceActivityNaming names a thing in the line of its making when
// the person who made it renames it within the window of its making — the
// "created" line takes the name, and no "renamed" line follows from the
// name it had while being typed (2026-11-13: a new row whose title was
// typed in four goes listed "w" as its old name, and each space between its
// words an edit). Notion lists a thing made and named at once by its final
// name alone, never the names typed on the way (seen 2026-11-13: a new row
// is "생성했습니다" and "편집했습니다 · Aa 이름 〈final〉", a new sub-page
// "편집했습니다 · Aa 제목 〈final〉"), which a page's new title is besides
// (workspace_activity_title_change.go). The making's line keeps its time; the
// audit log keeps the rename as before. It says whether it named one.
func foldWorkspaceActivityNaming(app core.App, entry workspaceActivityEntry) (bool, error) {
	// Its making's group is this window's or the one before, made by hand or
	// by the operation that renames it. A making is no part of a database
	// page's (workspaceActivityRowPart parts its edits), though a row's new
	// title is its record's: looked for in its record, a row's making was
	// never named (2026-11-18).
	window := workspaceActivityWindow.Milliseconds()
	jobs := []string{""}
	if entry.Metadata.OperationJobID != "" {
		jobs = append(jobs, entry.Metadata.OperationJobID)
	}
	ids := []any{}
	for _, job := range jobs {
		for _, at := range []int64{entry.At, entry.At - window} {
			making := entry
			making.Action, making.At, making.Metadata.OperationJobID, making.Metadata.Part = "create", at, job, ""
			ids = append(ids, workspaceActivityGroupID(making))
		}
	}
	result, err := app.DB().Update("leafen_workspace_activity", dbx.Params{"metadata_json": dbx.NewExp(`json_set([[metadata_json]], '$.name', {:name})`, dbx.Params{"name": entry.Metadata.Name})},
		dbx.And(dbx.In("id", ids...), dbx.NewExp("[[created]] >= {:since}", dbx.Params{"since": entry.At - window}))).Execute()
	if err != nil {
		return false, err
	}
	named, err := result.RowsAffected()
	return named > 0, err
}

// auditWorkspaceActivityGroup puts a group in the organization's audit log
// once, as a change to a page or a database's part. A rename's line keeps the
// name its group was last given and the name it had first (2026-11-15: a
// name typed in Korean was saved at its first letter, "멤", which its line
// kept while the group went on to the name typed whole).
func auditWorkspaceActivityGroup(app core.App, id string, entry workspaceActivityEntry) error {
	m := entry.Metadata
	return insertOrganizationAuditEvent(app, organizationAuditEvent{
		ID: id, OrganizationID: entry.OrganizationID,
		WorkspaceID: entry.WorkspaceID, ActorID: entry.ActorID, Action: workspaceActivityPrefix + entry.Action,
		TargetType: entry.EntityType, TargetID: entry.EntityID, OccurredAt: entry.At, KeepsLatestName: entry.Action == "rename",
		Metadata: map[string]any{"pageId": m.PageID, "blockId": m.BlockID, "name": workspaceActivityName(m.Name), "previousName": workspaceActivityName(m.PreviousName), "kind": m.Kind, "locationId": m.LocationID, "sourceId": m.SourceID, "subjectPageId": m.SubjectPageID, "operationJobId": m.OperationJobID},
	})
}

func workspaceActivityName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) > 512 {
		name = string(runes[:512])
	}
	return name
}
