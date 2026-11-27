package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Sending a database item to someone (Leafen's own; the recommended
// structure's decision, 9 and 13): passing it inside the workspace makes no
// mail or chat. It names them in one of the item's person properties — its
// first, unless another is chosen — so they hear of it as a person property
// tells anyone it names, and leaves the word as a comment on the item; it
// can also make a task, in a database the sender names, for them to look at
// it. Someone the item doesn't open to is given it by a share of their own,
// as its share menu does, and only by someone who may share it (full
// access), from the screen: a key or an agent shares through the share
// itself first, which asks its own scope and confirmation. It works on any
// database with a person property; people do it from an item's menu, agents
// with leafen_send.

var (
	errRowSendInvalid     = errors.New(errorCodeRowSendInvalid)
	errRowSendNoProperty  = errors.New("the item's database has no person property to name them in")
	errRowSendNoTarget    = errors.New("send to a member of the workspace")
	errRowSendUnseen      = errors.New("they can't open this item, and only someone with full access to it can share it with them: put it where they can open it, or ask for it to be shared")
	errRowSendKeyShare    = errors.New("they can't open this item, and a key's or an agent's send doesn't share it: share it with them first, which asks its own scope and confirmation, or put it where they can open it")
	rowSendMessageRunes   = 2000
	rowSendTaskTitleRunes = 200
	// rowSendTargetsWindow is how many of the workspace's people the list
	// gives at a time; its nextCursor reads on to the last.
	rowSendTargetsWindow = 200
)

func registerRowSendRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+rowSendTargetsPath, readRowSendTargets)
	event.Router.POST(apiRoot+rowSendPath, sendRowRoute).Bind(apis.BodyLimit(16 << 10))
}

// rowSendProperties are the item's database's person properties, in its order.
func rowSendProperties(app core.App, dataSourceID string) ([]rowSendPropertyWire, error) {
	properties := []rowSendPropertyWire{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[name]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sourceId} AND [[type]] = {:person}
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"sourceId": dataSourceID, "person": graphValueTypePerson}).All(&properties)
	return properties, err
}

// rowSendMembers are a window of the workspace's people — its owner and
// members — by the name they go by (their email when they have none), from
// after on; only, when given, is the one person asked for.
func rowSendMembers(app core.App, workspaceID, only string, after textIDCursor, limit int) ([]rowSendTargetWire, error) {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return nil, err
	}
	people := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[name]] FROM (
			SELECT [[user]].[[id]], COALESCE(NULLIF(trim([[user]].[[name]]), ''), [[user]].[[email]], '') AS [[name]]
			FROM {{users}} [[user]]
			WHERE [[user]].[[id]] = {:ownerId} OR [[user]].[[id]] IN (
				SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId}))
		WHERE ({:only} = '' OR [[id]] = {:only})
			AND ({:afterId} = '' OR [[name]] > {:afterName} OR ([[name]] = {:afterName} AND [[id]] > {:afterId}))
		ORDER BY [[name]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"ownerId": workspacePermissionOwnerID(workspace), "workspaceId": workspaceID, "only": only,
		"afterName": after.At, "afterId": after.ID, "limit": limit,
	}).All(&people)
	targets := make([]rowSendTargetWire, 0, len(people))
	for _, person := range people {
		targets = append(targets, rowSendTargetWire{UserID: person.ID, Name: person.Name})
	}
	return targets, err
}

func readRowSendTargets(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	item, err := authorizeDatabaseRowPage(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")), permissionWorkspaceRead)
	if err != nil {
		return rowSendError(event, err, "item")
	}
	properties, err := rowSendProperties(event.App, item.DataSourceID)
	if err != nil {
		return rowSendError(event, err, "properties")
	}
	after, err := decodeTextIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeRowSendInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	targets, err := rowSendMembers(event.App, item.WorkspaceID, "", after, rowSendTargetsWindow+1)
	if err != nil {
		return rowSendError(event, err, "members")
	}
	next := ""
	if len(targets) > rowSendTargetsWindow {
		targets = targets[:rowSendTargetsWindow]
		next = encodeTextIDCursor(targets[len(targets)-1].Name, targets[len(targets)-1].UserID)
	}
	for index := range targets {
		role, err := databaseRowPageAccessRole(event.App, targets[index].UserID, item)
		if err != nil {
			return rowSendError(event, err, "reach")
		}
		targets[index].CanSee = role >= pageAccessRoleComment
	}
	role, err := databaseRowPageAccessRole(event.App, user.Id, item)
	if err != nil {
		return rowSendError(event, err, "role")
	}
	return event.JSON(http.StatusOK, rowSendTargetsResponse{
		Properties: properties, Targets: targets, CanShare: role >= pageAccessRoleFullAccess, NextCursor: next,
	})
}

func rowSendStoredValue(app core.App, rowID, propertyID string) json.RawMessage {
	var value sql.NullString
	_ = app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}`).
		Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID}).Row(&value)
	if !value.Valid {
		return nil
	}
	return json.RawMessage(value.String)
}

// rowSendKorean is whether the sender's words a send writes — its task's
// title, its comment — are Korean, as the server names what it makes for
// them (generatedNameLanguage): a screen's send in its request's language
// for one following their device, an agent's as with no request.
func rowSendKorean(ctx context.Context, app core.App, userID string) bool {
	return generatedNameLanguage(ctx, app, userID) == accountLanguageKo
}

// rowSendTask is the task a send makes for its recipient to look at the
// item: in the database named, titled after the item; and the property of
// that database's first person property, which names them once they can open it.
func rowSendTask(ctx context.Context, app core.App, actorID, account string, item databaseRowPageAuthority, sourceID, title string, now time.Time) (graphOperation, string, string, string, error) {
	source := struct {
		WorkspaceID string `db:"workspace_id"`
		PageID      string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[workspace_id]], [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:sourceId}`).
		Bind(dbx.Params{"sourceId": sourceID}).One(&source); err != nil || source.WorkspaceID != item.WorkspaceID || source.PageID == "" {
		return graphOperation{}, "", "", "", refusedUnlessStorage(errRowSendInvalid, err)
	}
	properties, err := rowSendProperties(app, sourceID)
	if err != nil {
		return graphOperation{}, "", "", "", err
	}
	var titleID string
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:sourceId} AND [[type]] = 'title' LIMIT 1`).
		Bind(dbx.Params{"sourceId": sourceID}).Row(&titleID); err != nil {
		return graphOperation{}, "", "", "", refusedUnlessStorage(errRowSendInvalid, err)
	}
	taskTitle := "Look at: " + title
	if rowSendKorean(ctx, app, actorID) {
		taskTitle = "확인: " + title
	}
	if runes := []rune(taskTitle); len(runes) > rowSendTaskTitleRunes {
		taskTitle = string(runes[:rowSendTaskTitleRunes])
	}
	encodedTitle, _ := json.Marshal(taskTitle)
	values := map[string]graphRowValue{titleID: {Type: graphValueTypeTitle, Value: encodedTitle}}
	person := ""
	if len(properties) > 0 {
		person = properties[0].ID
	}
	taskID := newPageID()
	return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: taskID, DataSourceID: sourceID, Position: "zsend" + taskID, Values: values,
	}}, taskID, source.PageID, person, nil
}

// sendRow sends a database item to a member: names them in a person
// property, shares it with them first when they can't open it and the
// sender may share it (mayShare: a send from the screen), makes the task to
// look at it when asked, and leaves the word as a comment.
func sendRow(ctx context.Context, app core.App, actorID, rowID string, input rowSendRequest, mayShare bool, now time.Time) (rowSendResponse, error) {
	item, err := authorizeDatabaseRowPage(app, actorID, strings.TrimSpace(rowID), permissionWorkspaceWrite)
	if err != nil {
		return rowSendResponse{}, err
	}
	message := strings.TrimSpace(input.Message)
	if len([]rune(message)) > rowSendMessageRunes {
		return rowSendResponse{}, errRowSendInvalid
	}
	properties, err := rowSendProperties(app, item.DataSourceID)
	if err != nil {
		return rowSendResponse{}, err
	}
	property := ""
	for _, candidate := range properties {
		if property == "" && (input.Property == "" || candidate.ID == input.Property || candidate.Name == input.Property) {
			property = candidate.ID
		}
	}
	if property == "" {
		return rowSendResponse{}, errRowSendNoProperty
	}
	account, name := strings.TrimSpace(input.To), ""
	if account != "" {
		members, err := rowSendMembers(app, item.WorkspaceID, account, textIDCursor{}, 1)
		if err != nil {
			return rowSendResponse{}, err
		}
		for _, member := range members {
			name = member.Name
		}
	}
	if account == "" || name == "" {
		return rowSendResponse{}, errRowSendNoTarget
	}
	// Who can't open it gets it shared by someone who may share it.
	role, err := databaseRowPageAccessRole(app, account, item)
	if err != nil {
		return rowSendResponse{}, err
	}
	share := account != actorID && role < pageAccessRoleComment
	if share {
		if sender, err := databaseRowPageAccessRole(app, actorID, item); err != nil || sender < pageAccessRoleFullAccess {
			return rowSendResponse{}, errors.Join(errRowSendUnseen, err)
		}
		if !mayShare {
			return rowSendResponse{}, errRowSendKeyShare
		}
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return rowSendResponse{}, errors.New("collaboration storage unavailable")
	}
	response := rowSendResponse{}
	if share {
		kind := "comment"
		if rowSendTaskLike(app, item.DataSourceID) {
			kind = "edit"
		}
		if err := changeDatabaseRowPagePermissionNotifying(app, actorID, item.RowID,
			databaseRowPagePermissionInput{Action: "grant", PrincipalID: account, Role: kind}, now, false); err != nil {
			return response, err
		}
		response.Shared = true
	}
	// They join the people the property names.
	named := graphRelationRowIDs(rowSendStoredValue(app, item.RowID, property))
	present := false
	for _, id := range named {
		present = present || id == account
	}
	if !present {
		named = append(named, account)
	}
	encoded, _ := json.Marshal(named)
	operations := []graphOperation{{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: item.RowID, DataSourceID: item.DataSourceID, Position: item.Position, SubitemParentID: storedRowParent(app, item.RowID),
		Values: map[string]graphRowValue{property: {Type: graphValueTypePerson, Value: encoded}},
	}}}
	hash := ""
	apply := func(pageID string, change []graphOperation) error {
		body, _ := json.Marshal(change)
		digest := sha256.Sum256(append([]byte("leafen:row-send\x00"+actorID+"\x00"+strconv.FormatInt(now.UnixNano(), 10)+"\x00"), body...))
		changeHash := hex.EncodeToString(digest[:])
		if hash == "" {
			hash = changeHash
		}
		_, err := persistence.applyContentGraphMutationContext(ctx, actorID, pageID,
			graphMutationInput{MutationID: "row-send-" + changeHash[:32]},
			contentGraphMutationHooks{
				// Someone the item alone is shared with to edit changes it as on its own page.
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					authorization, err := authorizePageBody(txApp, actorID, pageID, permissionWorkspaceWrite)
					if isPermissionDenied(err) && pageID == item.ContainerPageID && len(change) == 1 &&
						change[0].Row != nil && change[0].Row.ID == item.RowID {
						if _, page, rowErr := databaseRowPageMutationAuthorization(txApp, actorID, item.RowID, permissionWorkspaceWrite); rowErr == nil {
							return page, nil
						}
					}
					return authorization, err
				},
				ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) { return changeHash, nil },
				Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
					return contentGraphMutationPreparation{Operations: change}, nil
				},
			})
		return err
	}
	// The task, made on its database's page: with the item's change when that is the item's page.
	changes := map[string][]graphOperation{item.ContainerPageID: operations}
	pages := []string{item.ContainerPageID}
	title := databaseRowPageTitle(app, item.RowID)
	taskSource, taskPage, taskPerson := "", "", ""
	if source := strings.TrimSpace(input.ConfirmTaskIn); source != "" {
		task, taskID, page, person, err := rowSendTask(ctx, app, actorID, account, item, source, title, now)
		if err != nil {
			return response, err
		}
		if _, known := changes[page]; !known {
			pages = append(pages, page)
		}
		changes[page] = append(changes[page], task)
		response.TaskID, taskSource, taskPage, taskPerson = taskID, source, page, person
	}
	for _, pageID := range pages {
		if err := apply(pageID, changes[pageID]); err != nil {
			return response, err
		}
	}
	response.Notified = account != actorID
	// The task too, where its database doesn't open to them, from a sender
	// who may share it; then it names them, so they hear of it.
	if response.TaskID != "" && account != actorID {
		if task, err := authorizeDatabaseRowPage(app, actorID, response.TaskID, permissionWorkspaceRead); err == nil {
			if taskRole, err := databaseRowPageAccessRole(app, account, task); err == nil && taskRole < pageAccessRoleComment {
				if sender, err := databaseRowPageAccessRole(app, actorID, task); err == nil && sender >= pageAccessRoleFullAccess {
					if err := changeDatabaseRowPagePermissionNotifying(app, actorID, response.TaskID,
						databaseRowPagePermissionInput{Action: "grant", PrincipalID: account, Role: "edit"}, now, false); err != nil {
						return response, err
					}
				}
			}
		}
	}
	if response.TaskID != "" && taskPerson != "" {
		people, _ := json.Marshal([]string{account})
		if err := apply(taskPage, []graphOperation{{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: response.TaskID, DataSourceID: taskSource, Position: "zsend" + response.TaskID,
			Values: map[string]graphRowValue{taskPerson: {Type: graphValueTypePerson, Value: people}},
		}}}); err != nil {
			return response, err
		}
	}
	// The word, on the item.
	if message == "" {
		message = map[bool]string{true: "보냈습니다.", false: "Sent to you."}[rowSendKorean(ctx, app, actorID)]
	}
	commentBody, err := encodeNotionStoredCommentBody(notionStoredCommentBody{Type: "rich_text", Rich: []mcpCommentSpan{{Text: "→ " + name + ": " + message}}})
	if err != nil {
		return response, err
	}
	authorize := func(app core.App) (commentAuthorization, error) {
		authorization, _, err := authorizeDatabaseRowCommentTarget(app, actorID, item.RowID, permissionWorkspaceWrite)
		return authorization, err
	}
	current, err := authorize(app)
	if err != nil {
		return response, err
	}
	if _, err := createCommentRecordForTarget(app, actorID, current.Target,
		commentCreateInput{ID: hash[:15], RequestID: "send-" + hash[:20], Body: commentBody}, authorize); err != nil {
		return response, err
	}
	return response, nil
}

// rowSendTaskLike is whether an item is something to do: its database has a
// status, so one sent it gets to change it, not only to answer on it.
func rowSendTaskLike(app core.App, dataSourceID string) bool {
	var count int
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:sourceId} AND [[type]] = 'status'`).
		Bind(dbx.Params{"sourceId": dataSourceID}).Row(&count)
	return count > 0
}

func sendRowRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := rowSendRequest{}
	if err := event.BindBody(&input); err != nil {
		return rowSendError(event, errRowSendInvalid, "body")
	}
	_, keyed := userKeyOf(event)
	response, err := sendRow(withRequestLanguage(event.Request.Context(), event.Request), event.App, user.Id, event.Request.PathValue("rowId"), input, !keyed, time.Now().UTC())
	if err != nil {
		return rowSendError(event, err, "send")
	}
	return event.JSON(http.StatusOK, response)
}

func rowSendError(event *core.RequestEvent, err error, step string) error {
	switch {
	// The word goes on the item as a comment, which the organization's DLP
	// policy checks; a refusal says so (2026-11-06: it read "Couldn't send it").
	case isDLPPolicyDecisionError(err):
		return writeDLPPolicyError(event, err, "row-send")
	case isPermissionDenied(err), errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeRowSendNotFound, "Not found.")
	case errors.Is(err, errRowSendNoProperty), errors.Is(err, errRowSendNoTarget):
		return apiError(event, http.StatusBadRequest, errorCodeRowSendInvalid, err.Error()+".")
	case errors.Is(err, errRowSendUnseen), errors.Is(err, errRowSendKeyShare):
		return apiError(event, http.StatusForbidden, errorCodeRowSendUnseen, rowSendRefusal(err)+".")
	case errors.Is(err, errRowSendInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeRowSendInvalid, "Check whom to send it to, the person property, the task's database and the word, at most 2,000 characters.")
	}
	var graphErr *graphRequestError
	if errors.As(err, &graphErr) {
		return apiError(event, graphErr.status, graphErr.code, graphErr.message)
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeRowSendFailed, "Couldn't send it.", "row-send", step, errorCodeRowSendFailed, err)
}

type leafenMCPSendInput struct {
	Item          string `json:"item" jsonschema:"The database item to pass on: its ID or /pages/ URL"`
	To            string `json:"to" jsonschema:"The member to send it to: their user ID, as notion-get-users lists, or a user:// link"`
	Property      string `json:"property,omitempty" jsonschema:"The person property to name them in: its name or ID; the database's first person property when left out"`
	Message       string `json:"message,omitempty" jsonschema:"A word for them, left as a comment on the item"`
	ConfirmTaskIn string `json:"confirm_task_in,omitempty" jsonschema:"A database to also make them a task in, to look at the item: its data source ID"`
}

func registerMCPSendTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_send", Title: "Pass a database item to someone",
		Description: "Passes a database item to a member of the workspace, as its menu's \"Send\" does: names them in one of its person " +
			"properties (the database's first unless you name one), so they hear of it as a person property tells anyone it names, " +
			"leaves the message as a comment on it, and with confirm_task_in makes them a task in that database to look at it. Someone " +
			"the item doesn't open to isn't sent it: share it with them first, or put it where they can open it. The role relations " +
			"(From, To and the like) stay as they are. Nothing leaves Leafen: it makes no mail or chat.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPSendInput) (*mcp.CallToolResult, rowSendResponse, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, rowSendResponse{}, err
		}
		item := mcpLocalID(app, input.Item)
		if _, err := authorizeDatabaseRowPage(app, authority.PrincipalID, item, permissionWorkspaceRead); err != nil {
			return nil, rowSendResponse{}, mcpDenied("this account can't open that item, or it is gone")
		}
		// Agents know people by the IDs the Notion-compatible tools give.
		to := leafenPersonReference(input.To)
		response, err := sendRow(ctx, app, authority.PrincipalID, item, rowSendRequest{
			To: to, Property: input.Property, Message: input.Message, ConfirmTaskIn: mcpReferenceID(input.ConfirmTaskIn),
		}, false, time.Now().UTC())
		if err != nil {
			if errors.Is(err, errRowSendInvalid) {
				return nil, rowSendResponse{}, fmt.Errorf("%w: confirm_task_in has to be a database of this workspace the person can open, with a person property", errRowSendInvalid)
			}
			for _, known := range []error{errRowSendNoProperty, errRowSendNoTarget, errRowSendUnseen, errRowSendKeyShare} {
				if errors.Is(err, known) {
					return nil, rowSendResponse{}, known
				}
			}
			return nil, rowSendResponse{}, mcpReasoned(err)
		}
		return nil, response, nil
	})
}

// rowSendRefusal is why a send that would share is refused.
func rowSendRefusal(err error) string {
	if errors.Is(err, errRowSendKeyShare) {
		return errRowSendKeyShare.Error()
	}
	return errRowSendUnseen.Error()
}
