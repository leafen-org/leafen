package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedFormOptionAuthority struct {
	PermissionCursor int64 `db:"permission_cursor"`
	PersonRevision   int64 `db:"person_revision"`
	ContentRevision  int64 `db:"content_revision"`
}

type storedFormPersonCandidate struct {
	ID string `db:"user_id"`
}

type storedFormPersonOption struct {
	ID   string `db:"id"`
	Name string `db:"name"`
}

type storedFormRelationTarget struct {
	PageID string `db:"page_id"`
}

type storedFormRelationTitle struct {
	ID string `db:"id"`
}

type storedFormRelationOption struct {
	ID       string `db:"id"`
	Position string `db:"position"`
	Name     string `db:"name"`
	Icon     string `db:"icon"`
	IconType string `db:"icon_type"`
	// Whether its body holds anything, which its icon shows.
	HasContent bool `db:"has_content"`
}

type formOptionContext struct {
	Link          storedFormLink
	Source        formSource
	Property      storedFormProperty
	Revision      string
	TargetPageID  string
	TargetSource  string
	TitleProperty string
	Authority     storedFormOptionAuthority
}

func normalizeFormOptionsRequest(input formOptionsRequest) (formOptionsRequest, error) {
	input.Token = strings.TrimSpace(input.Token)
	input.Revision = strings.TrimSpace(input.Revision)
	input.Cursor = strings.TrimSpace(input.Cursor)
	input.Query = strings.TrimSpace(input.Query)
	propertyID, err := normalizeFormIdentifier(input.PropertyID)
	switch {
	case err != nil:
		return formOptionsRequest{}, formInvalid("propertyId is the question's propertyId, 1–%d characters", maxFormIdentifierCharacters)
	case len(input.Token) > maxFormTokenCharacters:
		return formOptionsRequest{}, formInvalid("token is the one in the form's link, /forms/{token}")
	case len(input.Revision) != 64:
		return formOptionsRequest{}, formInvalid("revision is the form's, as POST /forms {\"action\": \"definition\"} answers it")
	case len(input.Cursor) > maxFormCursorCharacters:
		return formOptionsRequest{}, formInvalid("cursor is the nextCursor a list answered with")
	case utf8.RuneCountInString(input.Query) > maxFormOptionQueryCharacters || !utf8.ValidString(input.Query):
		return formOptionsRequest{}, formInvalid("query is up to %d characters", maxFormOptionQueryCharacters)
	}
	switch input.PageFilter {
	case "", "all", "verified", "archived":
	default:
		return formOptionsRequest{}, formInvalid("pageFilter %q isn't all, verified or archived", input.PageFilter)
	}
	input.PropertyID = propertyID
	return input, nil
}

func readFormOptions(
	app core.App,
	principalID string,
	input formOptionsRequest,
) (formOptionsResponse, error) {
	input, err := normalizeFormOptionsRequest(input)
	if err != nil {
		return formOptionsResponse{}, err
	}
	response := formOptionsResponse{}
	err = runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		context, err := loadFormOptionContext(txApp, principalID, input)
		if err != nil {
			return err
		}
		snapshot := formOptionSnapshot{
			PrincipalID: principalID, WorkspaceID: context.Link.WorkspaceID,
			LinkID: context.Link.ID, PropertyID: context.Property.ID,
			PropertyType: context.Property.Type, DefinitionRevision: context.Revision,
			PermissionCursor: context.Authority.PermissionCursor, Query: input.Query, PageFilter: input.PageFilter,
		}
		if context.Property.Type == graphValueTypePerson {
			snapshot.PersonRevision = context.Authority.PersonRevision
		} else {
			snapshot.ContentRevision = context.Authority.ContentRevision
			snapshot.TargetPageID = context.TargetPageID
			snapshot.TargetSourceID = context.TargetSource
		}
		cursor := formOptionCursorPayload{FormatVersion: formOptionCursorFormatVersion, Snapshot: snapshot}
		if input.Cursor != "" {
			cursor, err = openFormOptionCursor(txApp, input.Cursor)
			if err != nil || cursor.Snapshot != snapshot {
				return refusedUnlessStorage(errFormConflict, err)
			}
		}
		if context.Property.Type == graphValueTypePerson {
			response, err = readPersonFormOptions(txApp, context, cursor)
		} else if context.TargetPageID == "" {
			response = formOptionsResponse{PropertyID: context.Property.ID, Type: context.Property.Type, Options: []formReferenceOption{}}
		} else {
			response, err = readRelationFormOptions(txApp, context, cursor)
		}
		return err
	})
	return response, err
}

func loadFormOptionContext(
	app core.App,
	principalID string,
	input formOptionsRequest,
) (formOptionContext, error) {
	context := formOptionContext{}
	link, err := loadStoredFormLinkByToken(app, input.Token)
	if err != nil {
		return context, err
	}
	if link.Audience != formAudienceWorkspace {
		return context, errFormNotFound
	}
	if err := authorizeFormDefinition(app, principalID, link); err != nil {
		return context, err
	}
	source, err := loadFormSource(
		app, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil,
	)
	if err != nil || source.ViewConfig["leafenFormAudience"] != formAudienceWorkspace ||
		source.ViewConfig["type"] != graphViewTypeForm {
		if err != nil && !errors.Is(err, errFormInvalid) {
			return context, err
		}
		return context, refusedUnlessStorage(errFormNotFound, err)
	}
	revision, err := formDefinitionRevision(link, source)
	if err != nil {
		return context, err
	}
	if revision != input.Revision {
		return context, errFormConflict
	}
	property := storedFormProperty{}
	found := false
	for index, question := range source.Form.Questions {
		if question.PropertyID == input.PropertyID && !question.Hidden {
			property = source.Properties[index]
			found = true
			break
		}
	}
	if !found || property.ID != input.PropertyID || property.Type != graphValueTypePerson && property.Type != graphValueTypeRelation {
		return context, refuse(errFormInvalid, "propertyId names a person or relation question of the form: a select question's options come with its definition")
	}
	authority, err := readFormOptionAuthority(app, link.WorkspaceID)
	if err != nil {
		return context, err
	}
	context = formOptionContext{
		Link: link, Source: source, Property: property, Revision: revision, Authority: authority,
	}
	if property.Type == graphValueTypeRelation {
		context.TargetPageID, context.TargetSource, context.TitleProperty, err =
			loadFormRelationTarget(app, principalID, link, property)
		if errors.Is(err, errFormRelationUnseen) {
			err = nil
		}
		if err != nil {
			return formOptionContext{}, err
		}
	}
	return context, nil
}

func readFormOptionAuthority(app core.App, workspaceID string) (storedFormOptionAuthority, error) {
	authority := storedFormOptionAuthority{}
	err := app.DB().NewQuery(`
		SELECT COALESCE([[permission]].[[cursor]], 0) AS [[permission_cursor]],
			[[frontier]].[[person_revision]], [[content]].[[revision]] AS [[content_revision]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_form_option_frontiers}} [[frontier]]
			ON [[frontier]].[[workspace_id]] = [[workspace]].[[id]]
		JOIN {{leafen_workspace_content_revisions}} [[content]]
			ON [[content]].[[workspace_id]] = [[workspace]].[[id]]
		LEFT JOIN {{leafen_workspace_permission_state}} [[permission]]
			ON [[permission]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&authority)
	return authority, err
}

// errFormRelationUnseen is a related database the respondent can't open.
var errFormRelationUnseen = errors.New("form relation target unseen")

func loadFormRelationTarget(
	app core.App,
	principalID string,
	link storedFormLink,
	property storedFormProperty,
) (string, string, string, error) {
	targetSource := graphRelationTarget(property.Type, json.RawMessage(property.Config))
	if targetSource == "" {
		targetSource = link.DataSourceID
	}
	targets := []storedFormRelationTarget{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]] = {:sourceId} AND [[source]].[[owner]] = {:owner}
			AND [[source]].[[workspace_id]] = {:workspaceId}
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
		LIMIT 2
	`).Bind(dbx.Params{
		"sourceId": targetSource, "owner": link.Owner, "workspaceId": link.WorkspaceID,
	}).All(&targets)
	if err != nil {
		return "", "", "", err
	}
	if len(targets) != 1 {
		return "", "", "", errFormConflict
	}
	authorization, trashed, err := authorizeFormRelationPage(app, principalID, targets[0].PageID)
	// A respondent who can't open the related database finds none of its pages.
	if isPermissionDenied(err) {
		return "", "", "", errFormRelationUnseen
	}
	if err != nil || authorization.Workspace.WorkspaceID != link.WorkspaceID ||
		authorization.Workspace.OwnerID != link.Owner || trashed {
		if err != nil {
			return "", "", "", err
		}
		return "", "", "", errFormConflict
	}
	titles := []storedFormRelationTitle{}
	err = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:sourceId} AND [[type]] = 'title'
		ORDER BY [[position]], [[id]] LIMIT 2
	`).Bind(dbx.Params{
		"owner": link.Owner, "workspaceId": link.WorkspaceID,
		"pageId": targets[0].PageID, "sourceId": targetSource,
	}).All(&titles)
	if err != nil {
		return "", "", "", err
	}
	if len(titles) != 1 {
		return "", "", "", errFormConflict
	}
	return targets[0].PageID, targetSource, titles[0].ID, nil
}

func readPersonFormOptions(
	app core.App,
	context formOptionContext,
	cursor formOptionCursorPayload,
) (formOptionsResponse, error) {
	candidates := []storedFormPersonCandidate{}
	// What the respondent typed narrows the people to those whose name has it.
	named := ""
	if cursor.Snapshot.Query != "" {
		named = ` WHERE [[candidate]].[[user_id]] IN (SELECT [[id]] FROM {{users}} WHERE lower([[name]]) LIKE {:search} ESCAPE '\')`
	}
	err := app.DB().NewQuery(`
		SELECT [[candidate]].[[user_id]] FROM (
			SELECT [[workspace_owner]] AS [[user_id]] FROM {{leafen_workspaces}}
			WHERE [[id]] = {:workspaceId} AND [[workspace_owner]] > {:afterId}
			UNION
			SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] > {:afterId}
		) [[candidate]]` + named + `
		ORDER BY [[candidate]].[[user_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": context.Link.WorkspaceID, "afterId": cursor.LastID,
		"limit": maxFormOptionWindow + 1, "search": formOptionSearch(cursor.Snapshot.Query),
	}).All(&candidates)
	if err != nil {
		return formOptionsResponse{}, err
	}
	hasMore := len(candidates) > maxFormOptionWindow
	if hasMore {
		candidates = candidates[:maxFormOptionWindow]
	}
	metadata, err := readPersonFormOptionMetadata(app, context, candidates)
	if err != nil {
		return formOptionsResponse{}, err
	}
	options := make([]formReferenceOption, 0, len(candidates))
	me := cursor.Snapshot.PrincipalID
	for _, candidate := range candidates {
		if option, ok := metadata[candidate.ID]; ok {
			name := strings.TrimSpace(option.Name)
			if name == "" {
				name = option.ID
			}
			// The respondent comes first, as Notion's (seen 2026-11-04); the
			// walk reaches them in id order otherwise, so a later window may
			// still do.
			entry := formReferenceOption{ID: option.ID, Name: name}
			if option.ID == me {
				options = append([]formReferenceOption{entry}, options...)
			} else {
				options = append(options, entry)
			}
		}
	}
	return finishFormOptionWindow(app, context, cursor, options, hasMore, "", candidates)
}

func readPersonFormOptionMetadata(
	app core.App,
	context formOptionContext,
	candidates []storedFormPersonCandidate,
) (map[string]storedFormPersonOption, error) {
	if len(candidates) == 0 {
		return map[string]storedFormPersonOption{}, nil
	}
	params := dbx.Params{"workspaceId": context.Link.WorkspaceID}
	placeholders := make([]string, len(candidates))
	for index, candidate := range candidates {
		key := "candidate" + string(rune('A'+index))
		params[key] = candidate.ID
		placeholders[index] = "{:" + key + "}"
	}
	rows := []storedFormPersonOption{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]]
		FROM {{users}} [[user]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_member]].[[user]] = [[user]].[[id]]
			AND [[organization_member]].[[status]] = 'active'
		WHERE [[user]].[[id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[user]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	result := make(map[string]storedFormPersonOption, len(rows))
	for _, row := range rows {
		if _, duplicate := result[row.ID]; duplicate {
			return nil, errFormConflict
		}
		result[row.ID] = row
	}
	return result, nil
}

func readRelationFormOptions(
	app core.App,
	context formOptionContext,
	cursor formOptionCursorPayload,
) (formOptionsResponse, error) {
	rows := []storedFormRelationOption{}
	// What the respondent typed narrows the pages to those whose title has it.
	titled := ""
	if cursor.Snapshot.Query != "" {
		titled = ` AND lower(COALESCE(json_extract([[title]].[[value]], '$'), '')) LIKE {:search} ESCAPE '\'`
	}
	// The list's filter, as Notion's (seen 2026-11-05): the pages not archived,
	// all, the verified or the archived ones.
	switch cursor.Snapshot.PageFilter {
	case "all":
		titled += databaseRowArchiveScopeSQL(databaseArchiveScopeAll, "[[page]]")
	case "archived":
		titled += databaseRowArchiveScopeSQL(databaseArchiveScopeArchived, "[[page]]")
	case "verified":
		titled += databaseRowArchiveScopeSQL("", "[[page]]") + ` AND COALESCE([[page]].[[verified_at]], '') != ''`
	default:
		titled += databaseRowArchiveScopeSQL("", "[[page]]")
	}
	err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]],
			COALESCE(json_extract([[title]].[[value]], '$'), '') AS [[name]],
			COALESCE([[page]].[[icon]], '') AS [[icon]], COALESCE([[page]].[[icon_type]], '') AS [[icon_type]],
			` + pageHasContentSQL + ` AS [[has_content]]
		FROM {{leafen_database_rows}} [[row]]
		LEFT JOIN {{leafen_database_row_values}} [[title]]
			ON [[title]].[[row_id]] = [[row]].[[id]]
			AND [[title]].[[property_id]] = {:titleProperty} AND [[title]].[[type]] = 'title'
		LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[row]].[[id]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId}
			AND [[row]].[[data_source_id]] = {:sourceId}
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
			AND ([[row]].[[position]] > {:afterPosition}
				OR ([[row]].[[position]] = {:afterPosition} AND [[row]].[[id]] > {:afterId}))` + titled + `
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"titleProperty": context.TitleProperty, "owner": context.Link.Owner,
		"pageId": context.TargetPageID, "sourceId": context.TargetSource,
		"afterPosition": cursor.LastPosition, "afterId": cursor.LastID,
		"limit": maxFormOptionWindow + 1, "search": formOptionSearch(cursor.Snapshot.Query),
	}).All(&rows)
	if err != nil {
		return formOptionsResponse{}, err
	}
	hasMore := len(rows) > maxFormOptionWindow
	if hasMore {
		rows = rows[:maxFormOptionWindow]
	}
	// An unlinked item the person has no role in isn't offered
	// (database_row_unlinked_access.go); the window still walks past it.
	items, err := loadDatabaseRowAccess(app, cursor.Snapshot.PrincipalID, []string{context.TargetSource})
	if err != nil {
		return formOptionsResponse{}, err
	}
	options := make([]formReferenceOption, 0, len(rows))
	for _, row := range rows {
		if items.hides(row.ID) {
			continue
		}
		// An untitled page goes nameless; the form names it as its database
		// does ("새 작업" in a task database, else "새 페이지").
		options = append(options, formReferenceOption{
			ID: row.ID, Name: strings.TrimSpace(row.Name), Icon: row.Icon, IconType: row.IconType, HasContent: row.HasContent,
		})
	}
	response, err := finishRelationFormOptionWindow(app, context, cursor, options, hasMore, rows)
	if err != nil {
		return response, err
	}
	// The list says where it searches: "〈데이터베이스〉 내 검색".
	if err := nameFormOptionTarget(app, context.TargetSource, &response); err != nil {
		return response, err
	}
	response.TaskDatabase, err = databaseIsTaskDatabase(app, context.TargetSource)
	return response, err
}

func finishFormOptionWindow(
	app core.App,
	context formOptionContext,
	cursor formOptionCursorPayload,
	options []formReferenceOption,
	hasMore bool,
	lastPosition string,
	candidates []storedFormPersonCandidate,
) (formOptionsResponse, error) {
	var next *string
	if hasMore {
		cursor.LastPosition = lastPosition
		cursor.LastID = candidates[len(candidates)-1].ID
		sealed, err := sealFormOptionCursor(app, cursor)
		if err != nil {
			return formOptionsResponse{}, err
		}
		next = &sealed
	}
	return formOptionsResponse{
		PropertyID: context.Property.ID, Type: context.Property.Type,
		Options: options, NextCursor: next, HasMore: hasMore,
	}, nil
}

func finishRelationFormOptionWindow(
	app core.App,
	context formOptionContext,
	cursor formOptionCursorPayload,
	options []formReferenceOption,
	hasMore bool,
	rows []storedFormRelationOption,
) (formOptionsResponse, error) {
	var next *string
	if hasMore {
		last := rows[len(rows)-1]
		cursor.LastPosition = last.Position
		cursor.LastID = last.ID
		sealed, err := sealFormOptionCursor(app, cursor)
		if err != nil {
			return formOptionsResponse{}, err
		}
		next = &sealed
	}
	return formOptionsResponse{
		PropertyID: context.Property.ID, Type: context.Property.Type,
		Options: options, NextCursor: next, HasMore: hasMore,
	}, nil
}

// formOptionSearch is a lower-cased LIKE pattern holding what was typed.
func formOptionSearch(query string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(query)) + "%"
}
