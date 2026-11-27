package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceActivityPageState struct {
	Title        string
	ParentID     string
	PrivateOwner string
	TeamspaceID  string
	Trash        bool
	Presentation string
}

func captureWorkspaceActivityPage(page *core.Record) workspaceActivityPageState {
	presentation, _ := json.Marshal([]any{page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"), page.GetInt("cover_position"),
		page.GetBool("is_locked"), page.GetBool("full_width"), page.GetBool("small_text"), page.GetString("font"), page.GetString("backlinks_display"), page.GetString("page_comments_display"), page.GetString("table_of_contents_display"), page.GetString("inline_comments_display"), page.GetBool("is_archived")})
	return workspaceActivityPageState{Title: page.GetString("title"), ParentID: page.GetString("parent_id"), PrivateOwner: page.GetString(privatePageOwnerField),
		TeamspaceID: page.GetString("teamspace_id"), Trash: page.GetBool("in_trash"), Presentation: string(presentation)}
}

func recordWorkspacePageCreated(app core.App, page *core.Record, actor string, at int64) error {
	return recordWorkspaceActivity(app, page, actor, "created", "page", page.Id, "create", workspaceActivityMetadata{Name: page.GetString("title"), LocationID: page.GetString("parent_id")}, at)
}

// recordWorkspacePageMetadata records a change of a page's details; version
// is the one it recorded in the page's history, 0 for none.
func recordWorkspacePageMetadata(app core.App, page *core.Record, before workspaceActivityPageState, actor string, at int64, version int) error {
	after := captureWorkspaceActivityPage(page)
	origin := "metadata:" + strconv.Itoa(page.GetInt("cursor"))
	metadata := workspaceActivityMetadata{Name: after.Title, LocationID: after.ParentID}
	write := func(action string) error {
		return recordWorkspaceActivity(app, page, actor, origin, "page", page.Id, action, metadata, at)
	}
	changed := false
	if before.Trash != after.Trash {
		changed = true
		action := "restore"
		if after.Trash {
			action = "delete"
			metadata.LocationID = before.ParentID
		} else {
			metadata.Kind = "trashRestore"
		}
		// A sub-page put in the trash is its block deleted from the page that
		// holds it, whose line names it ("〈x〉 블록을 삭제했습니다",
		// workspace_activity_graph.go); Notion's updates give it no page line
		// besides (seen 2026-11-13). A page at the top and a database's row
		// keep theirs.
		subPage := after.Trash && before.ParentID != "" && !isDatabaseRowPageRecord(page)
		if !subPage {
			if err := write(action); err != nil {
				return err
			}
		}
		// A page at the top put in the trash is its block deleted too
		// (workspace_activity_page_block.go).
		if after.Trash && before.ParentID == "" && !isDatabaseRowPageRecord(page) {
			if err := recordWorkspacePageBlockDeleted(app, page, actor, origin, at); err != nil {
				return err
			}
		}
		metadata.Kind = ""
	}
	// Spaces at a title's ends are no new name (writeWorkspaceActivity).
	// A new title is an edit of the page, of the version it recorded
	// (workspace_activity_title_change.go).
	if workspaceActivityName(before.Title) != workspaceActivityName(after.Title) {
		changed = true
		metadata.PreviousName = before.Title
		if err := recordWorkspaceActivityOfVersion(app, page, actor, origin, "page", page.Id, "rename", metadata, at, version); err != nil {
			return err
		}
		metadata.PreviousName = ""
	}
	if before.ParentID != after.ParentID || before.PrivateOwner != after.PrivateOwner || before.TeamspaceID != after.TeamspaceID {
		changed = true
		if err := write("move"); err != nil {
			return err
		}
	}
	if !changed && before.Presentation != after.Presentation {
		return write("edit")
	}
	return nil
}

func recordWorkspacePageHistoryActivity(app core.App, page *core.Record, version storedPageHistoryVersion, payload []byte) error {
	action := "edit"
	switch version.Source {
	case pageHistoryEventRestore:
		action = "restore"
		if err := sealWorkspaceActivityEdits(app, page, version.CreatedAt, version.VersionCursor); err != nil {
			return err
		}
	case pageHistoryEventCollaboration:
		var envelope pageHistoryEventEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return err
		}
		if len(envelope.TextDeltas) == 0 {
			return nil
		}
	case pageHistoryEventPage, pageHistoryEventRewrite:
	default:
		return nil
	}
	name, err := workspaceActivityPageName(app, page)
	if err != nil {
		return err
	}
	// A person's edits of a page are one activity a clock minute, as the
	// history shows them: one for each keystroke the realtime editor sent
	// was a row of the organization's journal each (2026-10-15).
	origin := "history:" + version.ID
	if action == "edit" {
		origin = fmt.Sprintf("history-edit:%s:%d", version.ActorID, version.CreatedAt/60000)
	}
	return recordWorkspaceActivityOfVersion(app, page, version.ActorID, origin, "page", page.Id, action,
		workspaceActivityMetadata{Name: name, LocationID: page.GetString("parent_id")}, version.CreatedAt, version.VersionCursor)
}

// sealWorkspaceActivityEdits ends the edits of a page open in the window a
// restore of it falls in, each person's: their groups take an id of their
// own, so an edit after the restore starts a group of its own. A group
// across a restore read its page before its first version and after its
// last, the restore between them as if its person's edit (2026-11-13: a
// group of versions 6 to 74, restored at 66).
func sealWorkspaceActivityEdits(app core.App, page *core.Record, at int64, restore int) error {
	window := workspaceActivityWindow.Milliseconds()
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_workspace_activity}} INDEXED BY [[idx_leafen_workspace_activity_feed]]
		WHERE [[workspace_id]]={:workspace} AND [[last_at]]>={:start} AND [[page_id]]={:page}
			AND [[subject_type]]='page' AND [[subject_id]]={:page} AND [[action]]='edit'`).
		Bind(dbx.Params{"workspace": page.GetString("workspace"), "start": at - at%window, "page": page.Id}).Column(&ids); err != nil {
		return err
	}
	for _, id := range ids {
		digest := sha256.Sum256([]byte(id + "\x00restored\x00" + strconv.Itoa(restore)))
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_workspace_activity}} SET [[id]]={:sealed} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"sealed": hex.EncodeToString(digest[:16]), "id": id}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
