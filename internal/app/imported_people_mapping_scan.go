package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	importedPeopleScanPhaseValues   = "values"
	importedPeopleScanPhaseMentions = "mentions"
	importedPeopleScanPhaseMembers  = "members"
	// A scan reads this many bounded batches at most before it answers with
	// no people found yet, so a workspace of many blocks takes few answers.
	importedPeopleScanBatchBudget = 32
	// A batch of mentions reads this many pages that can name someone
	// imported, and a page's blocks this many at a time.
	importedPeopleMentionPages  = 8
	importedPeopleMentionBlocks = 256
)

type importedPeopleScanCursor struct {
	Version      int    `json:"v"`
	PrincipalID  string `json:"principalId"`
	WorkspaceID  string `json:"workspaceId"`
	Revision     int    `json:"revision"`
	Phase        string `json:"phase"`
	PageID       string `json:"pageId,omitempty"`
	RowID        string `json:"rowId,omitempty"`
	PropertyID   string `json:"propertyId,omitempty"`
	BlockID      string `json:"blockId,omitempty"`
	MemberID     string `json:"memberId,omitempty"`
	SourceOffset int    `json:"sourceOffset,omitzero"`
	PeopleOnly   bool   `json:"peopleOnly,omitzero"`
}

type importedPeopleValueRow struct {
	PageID     string `db:"page_id"`
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
	Spans      string `db:"spans"`
}

type importedPeopleBlockRow struct {
	PageID  string `db:"page_id"`
	ID      string `db:"id"`
	Content string `db:"content"`
}

type importedPeopleMemberRow struct {
	UserID string `db:"user_id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Role   string `db:"role"`
}

// scanImportedPeople reads a window of the imported people a workspace's
// pages name, then its members to map them to; asked for people only — the
// setting lists the members itself — it ends with the people.
func scanImportedPeople(app core.App, principalID, workspaceID, rawCursor string, peopleOnly bool) (notionImportedPeopleScanResult, error) {
	cursor, err := decodeImportedPeopleScanCursor(rawCursor, principalID, workspaceID, peopleOnly)
	if err != nil {
		return notionImportedPeopleScanResult{}, err
	}
	result := notionImportedPeopleScanResult{People: []notionImportedPerson{}, Members: []notionImportedPeopleMember{}}
	err = runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, principalID, workspaceID, importedPeopleMappingPermission)
		if err != nil {
			return err
		}
		var revision int
		if err := txApp.DB().NewQuery(`SELECT [[revision]] FROM {{leafen_workspace_content_revisions}} WHERE [[workspace_id]]={:workspaceId}`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision); err != nil {
			return err
		}
		if cursor.Revision < 0 {
			cursor.Revision = revision
		} else if cursor.Revision != revision {
			return errImportedPeopleCursorInvalid
		}
		// A batch that found no one is followed by the next, within the
		// budget, rather than answered empty.
		batches := 0
		access := importedPeoplePageAccess{}
	scanPhases:
		for len(result.People) < notionImportedPeopleItems && cursor.Phase != importedPeopleScanPhaseMembers {
			batches++
			switch cursor.Phase {
			case importedPeopleScanPhaseValues:
				done, err := scanImportedPeopleValues(txApp, &cursor, &result, access)
				if err != nil {
					return err
				}
				if !done {
					if len(result.People) == 0 && batches < importedPeopleScanBatchBudget {
						continue
					}
					break scanPhases
				}
				if len(result.People) == notionImportedPeopleItems {
					break scanPhases
				}
				cursor = importedPeopleScanCursor{Version: importedPeopleScanCursorVersion, PrincipalID: principalID, WorkspaceID: workspaceID, Revision: revision, Phase: importedPeopleScanPhaseMentions, PeopleOnly: peopleOnly}
			case importedPeopleScanPhaseMentions:
				done, err := scanImportedPeopleMentions(txApp, &cursor, &result, access)
				if err != nil {
					return err
				}
				if !done {
					if len(result.People) == 0 && batches < importedPeopleScanBatchBudget {
						continue
					}
					break scanPhases
				}
				if len(result.People) == notionImportedPeopleItems {
					break scanPhases
				}
				cursor = importedPeopleScanCursor{Version: importedPeopleScanCursorVersion, PrincipalID: principalID, WorkspaceID: workspaceID, Revision: revision, Phase: importedPeopleScanPhaseMembers, PeopleOnly: peopleOnly}
			default:
				return errImportedPeopleCursorInvalid
			}
		}
		if len(result.People) > 0 {
			if err := suggestImportedPeopleMembers(txApp, authorization, result.People); err != nil {
				return err
			}
		}
		if cursor.Phase == importedPeopleScanPhaseMembers && peopleOnly {
			return nil
		}
		if cursor.Phase == importedPeopleScanPhaseMembers && len(result.People) == 0 {
			return scanImportedPeopleMembers(txApp, authorization, &cursor, &result)
		}
		result.HasMore = true
		result.NextCursor, err = encodeImportedPeopleScanCursor(cursor)
		return err
	})
	return result, err
}

// importedPeoplePageAccess remembers which pages the scanning person may
// change: only those name the people they can map.
type importedPeoplePageAccess map[string]bool

func (access importedPeoplePageAccess) editable(app core.App, principalID, pageID string) (bool, error) {
	if editable, known := access[pageID]; known {
		return editable, nil
	}
	_, err := authorizePageBody(app, principalID, pageID, importedPeopleMappingPermission)
	if err != nil && !isPermissionDenied(err) {
		return false, err
	}
	access[pageID] = err == nil
	return err == nil, nil
}

func scanImportedPeopleValues(app core.App, cursor *importedPeopleScanCursor, result *notionImportedPeopleScanResult, access importedPeoplePageAccess) (bool, error) {
	rows := []importedPeopleValueRow{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], [[row_id]], [[property_id]], [[type]], [[value]], [[spans]]
		FROM {{leafen_database_row_values}}
		WHERE [[workspace_id]]={:workspaceId}
			AND ([[type]]='person' AND ` + importedPeopleMarkedSQL("[[value]]") + ` OR ` + importedPeopleMarkedSQL("[[spans]]") + `)
			AND (([[page_id]]>{:pageId}) OR ([[page_id]]={:pageId} AND [[row_id]]>{:rowId})
				OR ([[page_id]]={:pageId} AND [[row_id]]={:rowId} AND [[property_id]]>={:propertyId}))
		ORDER BY [[page_id]], [[row_id]], [[property_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": cursor.WorkspaceID, "pageId": cursor.PageID, "rowId": cursor.RowID, "propertyId": cursor.PropertyID, "limit": notionImportedPeopleItems + 1}).All(&rows)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		same := row.PageID == cursor.PageID && row.RowID == cursor.RowID && row.PropertyID == cursor.PropertyID
		if same && cursor.SourceOffset == 0 {
			continue
		}
		if editable, err := access.editable(app, cursor.PrincipalID, row.PageID); err != nil {
			return false, err
		} else if !editable {
			cursor.PageID, cursor.RowID, cursor.PropertyID, cursor.SourceOffset = row.PageID, row.RowID, row.PropertyID, 0
			continue
		}
		// A person value holds imported people; a formatted text cell
		// mentions them in its spans.
		found := map[string]importedPeopleSource{}
		property := row.Type == graphValueTypePerson
		if property {
			var value any
			if json.Unmarshal([]byte(row.Value), &value) != nil {
				return false, errImportedPeopleMappingFailed
			}
			collectImportedPeopleSources(value, found)
		} else if err := collectImportedPeopleContentSources(row.Spans, true, found); err != nil {
			return false, err
		}
		sources := orderedImportedPeopleSources(found)
		offset := 0
		if same {
			offset = cursor.SourceOffset
		}
		consumed := appendImportedPeopleSources(result, sources, offset, property)
		cursor.PageID, cursor.RowID, cursor.PropertyID = row.PageID, row.RowID, row.PropertyID
		if offset+consumed < len(sources) {
			cursor.SourceOffset = offset + consumed
			return false, nil
		}
		cursor.SourceOffset = 0
		if len(result.People) == notionImportedPeopleItems {
			return false, nil
		}
	}
	return len(rows) <= notionImportedPeopleItems, nil
}

// scanImportedPeopleMentions reads the people the workspace's block text
// mentions: the cursor's page on from its block, then the next pages that
// can name an imported person (importedPeoplePageMarkedSQL), each page's
// document read once; the others' documents are not read.
func scanImportedPeopleMentions(app core.App, cursor *importedPeopleScanCursor, result *notionImportedPeopleScanResult, access importedPeoplePageAccess) (bool, error) {
	if cursor.PageID != "" {
		if done, err := scanImportedPeoplePageMentions(app, cursor, result, access, cursor.PageID); err != nil || !done {
			return false, err
		}
	}
	pages := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[workspace]]={:workspaceId} AND [[page]].[[id]]>{:pageId}
			AND ` + importedPeoplePageMarkedSQL("[[page]].[[id]]") + `
		ORDER BY [[page]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": cursor.WorkspaceID, "pageId": cursor.PageID, "limit": importedPeopleMentionPages}).Column(&pages); err != nil {
		return false, err
	}
	for _, pageID := range pages {
		if done, err := scanImportedPeoplePageMentions(app, cursor, result, access, pageID); err != nil || !done {
			return false, err
		}
	}
	return len(pages) < importedPeopleMentionPages, nil
}

// scanImportedPeoplePageMentions reads one page's blocks from the cursor on,
// a window at a time; false when the result filled before the page ended.
// Done, the cursor names the page, so the next pages follow it.
func scanImportedPeoplePageMentions(app core.App, cursor *importedPeopleScanCursor, result *notionImportedPeopleScanResult, access importedPeoplePageAccess, pageID string) (bool, error) {
	if pageID != cursor.PageID {
		cursor.PageID, cursor.BlockID, cursor.SourceOffset = pageID, "", 0
	}
	if editable, err := access.editable(app, cursor.PrincipalID, pageID); err != nil || !editable {
		cursor.SourceOffset = 0
		return err == nil, err
	}
	var doc *crdt.Doc
	for {
		rows := []importedPeopleBlockRow{}
		if err := app.DB().NewQuery(`
			SELECT [[page_id]], [[id]], [[content]] FROM {{leafen_blocks}}
			WHERE [[page_id]]={:pageId} AND [[id]]>={:blockId} ORDER BY [[id]] LIMIT {:limit}
		`).Bind(dbx.Params{"pageId": pageID, "blockId": cursor.BlockID, "limit": importedPeopleMentionBlocks}).All(&rows); err != nil {
			return false, err
		}
		for _, row := range rows {
			same := row.ID == cursor.BlockID
			if same && cursor.SourceOffset == 0 {
				continue
			}
			if doc == nil {
				loaded, _, err := loadYgoDoc(app, pageID)
				if err != nil {
					return false, err
				}
				doc = loaded
			}
			found := importedPeopleMentionSources(doc.GetText(ygoBlockTextRootPrefix + row.ID).ToDelta())
			if err := collectImportedPeopleContentSources(row.Content, false, found); err != nil {
				return false, err
			}
			sources := orderedImportedPeopleSources(found)
			offset := 0
			if same {
				offset = cursor.SourceOffset
			}
			consumed := appendImportedPeopleSources(result, sources, offset, false)
			cursor.BlockID = row.ID
			if offset+consumed < len(sources) {
				cursor.SourceOffset = offset + consumed
				return false, nil
			}
			cursor.SourceOffset = 0
			if len(result.People) == notionImportedPeopleItems {
				return false, nil
			}
		}
		if len(rows) < importedPeopleMentionBlocks {
			return true, nil
		}
	}
}

func importedPeopleMentionSources(deltas []crdt.Delta) map[string]importedPeopleSource {
	found := map[string]importedPeopleSource{}
	for _, delta := range deltas {
		if delta.Op != crdt.DeltaOpInsert || delta.Attributes == nil {
			continue
		}
		sourceID, _ := delta.Attributes["userId"].(string)
		if !validImportedPeopleSourceID(sourceID) {
			continue
		}
		source := importedPeopleSource{SourceID: sourceID, Count: 1}
		for _, name := range []string{"notionUser", "leafenUser"} {
			if person, ok := delta.Attributes[name].(map[string]any); ok {
				source.DisplayName = importedPeopleOptionalString(person["displayName"])
				source.Email = importedPeopleOptionalString(person["email"])
			}
		}
		mergeImportedPeopleSource(found, source)
	}
	return found
}

func orderedImportedPeopleSources(found map[string]importedPeopleSource) []importedPeopleSource {
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]importedPeopleSource, 0, len(ids))
	for _, id := range ids {
		result = append(result, found[id])
	}
	return result
}

func appendImportedPeopleSources(result *notionImportedPeopleScanResult, sources []importedPeopleSource, offset int, property bool) int {
	room := notionImportedPeopleItems - len(result.People)
	if offset >= len(sources) || room == 0 {
		return 0
	}
	end := offset + room
	if end > len(sources) {
		end = len(sources)
	}
	for _, source := range sources[offset:end] {
		person := notionImportedPerson{SourceID: source.SourceID, DisplayName: source.DisplayName, Email: source.Email}
		if property {
			person.PropertyValueCount = source.Count
		} else {
			person.MentionCount = source.Count
		}
		result.People = append(result.People, person)
	}
	return end - offset
}

func scanImportedPeopleMembers(app core.App, authorization workspaceAuthorization, cursor *importedPeopleScanCursor, result *notionImportedPeopleScanResult) error {
	rows := []importedPeopleMemberRow{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]], [[user]].[[email]],
			CASE WHEN [[user]].[[id]]=[[workspace]].[[workspace_owner]] THEN 'owner' ELSE 'member' END AS [[role]]
		FROM {{users}} [[user]] JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]]=[[workspace]].[[organization]]
			AND [[organization_member]].[[user]]=[[user]].[[id]] AND [[organization_member]].[[status]]='active'
		WHERE [[user]].[[id]]>{:afterId} AND ([[user]].[[id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
			WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]] AND [[member]].[[user_id]]=[[user]].[[id]] AND [[member]].[[role]]='member'
		)) ORDER BY [[user]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "afterId": cursor.MemberID, "limit": notionImportedPeopleMembers + 1}).All(&rows)
	if err != nil {
		return err
	}
	result.HasMore = len(rows) > notionImportedPeopleMembers
	if result.HasMore {
		rows = rows[:notionImportedPeopleMembers]
	}
	for _, row := range rows {
		result.Members = append(result.Members, notionImportedPeopleMember{UserID: row.UserID, DisplayName: importedPeopleOptionalString(row.Name), Email: importedPeopleOptionalString(strings.ToLower(row.Email)), Role: row.Role})
	}
	if result.HasMore {
		cursor.MemberID = rows[len(rows)-1].UserID
		var err error
		result.NextCursor, err = encodeImportedPeopleScanCursor(*cursor)
		return err
	}
	return nil
}

func suggestImportedPeopleMembers(app core.App, authorization workspaceAuthorization, people []notionImportedPerson) error {
	emails := map[string]struct{}{}
	for _, person := range people {
		if person.Email != nil {
			emails[strings.ToLower(*person.Email)] = struct{}{}
		}
	}
	if len(emails) == 0 {
		return nil
	}
	params := dbx.Params{"workspaceId": authorization.WorkspaceID}
	placeholders := make([]string, 0, len(emails))
	for email := range emails {
		key := "email" + string(rune('A'+len(placeholders)))
		params[key] = email
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []importedPeopleMemberRow{}
	if err := app.DB().NewQuery(`SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]], lower([[user]].[[email]]) AS [[email]]
		FROM {{users}} [[user]] JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]] ON [[organization_member]].[[organization]]=[[workspace]].[[organization]]
			AND [[organization_member]].[[user]]=[[user]].[[id]] AND [[organization_member]].[[status]]='active'
		WHERE lower([[user]].[[email]]) IN (` + joinSQL(placeholders) + `) AND ([[user]].[[id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[member]] WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]]
			AND [[member]].[[user_id]]=[[user]].[[id]] AND [[member]].[[role]]='member'))`).Bind(params).All(&rows); err != nil {
		return err
	}
	byEmail := map[string]importedPeopleMemberRow{}
	for _, row := range rows {
		byEmail[row.Email] = row
	}
	for index := range people {
		if people[index].Email == nil {
			continue
		}
		if row, ok := byEmail[strings.ToLower(*people[index].Email)]; ok {
			people[index].SuggestedUserID = &row.UserID
			people[index].SuggestedDisplayName = importedPeopleOptionalString(row.Name)
		}
	}
	return nil
}

func decodeImportedPeopleScanCursor(raw, principalID, workspaceID string, peopleOnly bool) (importedPeopleScanCursor, error) {
	empty := importedPeopleScanCursor{Version: importedPeopleScanCursorVersion, PrincipalID: principalID, WorkspaceID: workspaceID, Revision: -1, Phase: importedPeopleScanPhaseValues, PeopleOnly: peopleOnly}
	if strings.TrimSpace(raw) == "" {
		return empty, nil
	}
	if len(raw) > importedPeopleCursorMaxBytes {
		return importedPeopleScanCursor{}, errImportedPeopleCursorInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	var cursor importedPeopleScanCursor
	if err != nil || len(data) > importedPeopleCursorMaxBytes || json.Unmarshal(data, &cursor) != nil ||
		cursor.Version != importedPeopleScanCursorVersion || cursor.PrincipalID != principalID || cursor.WorkspaceID != workspaceID ||
		cursor.PeopleOnly != peopleOnly ||
		cursor.Revision < 0 || cursor.SourceOffset < 0 || cursor.SourceOffset > notionImportedPeopleItems ||
		(cursor.Phase != importedPeopleScanPhaseValues && cursor.Phase != importedPeopleScanPhaseMentions && cursor.Phase != importedPeopleScanPhaseMembers) {
		return importedPeopleScanCursor{}, errImportedPeopleCursorInvalid
	}
	return cursor, nil
}

func encodeImportedPeopleScanCursor(cursor importedPeopleScanCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(encoded) > importedPeopleCursorMaxBytes {
		return "", errImportedPeopleCursorInvalid
	}
	return encoded, nil
}
