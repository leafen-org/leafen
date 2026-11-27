package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The workspace's People database (schema_baseline.json): a full-page
// database the server makes and keeps, private to the server itself — Leafen's
// own author, leafenSystemAuthorID, which the app names "Leafen" with its
// mark where Notion names its own "Notion App" — so it is in no one's
// sidebar or export, and what made its pages; search finds it and its rows,
// as Notion's finds its People and its people's pages (seen 2026-11-15), to
// whoever its own access lets read them, which is people_database_access.go's. Its five own properties are
// Notion's People's — Name, Email, About, Membership Type and Person (seen
// 2026-10-12, named in English in both languages as Notion's are) — and its
// views its two, "Directory View" and "Card View". Each person People lists
// — its owner, members, guests and the deactivated — has a row, made by the
// server when they first show there and kept when they leave, whose Name,
// About, Membership Type and Person follow theirs; its Email is read from
// their account where People is shown (the address stays the reader's to
// see, peopleEmailShown), never kept. The properties its owner adds are
// the database's own, of any kind a database takes.

const (
	peopleDatabaseTitle   = "People"
	peopleSyncTimeout     = time.Minute
	peopleRowPositionSize = 8
)

type peopleDatabase struct {
	WorkspaceID          string `db:"workspace_id"`
	PageID               string `db:"page_id"`
	DataSourceID         string `db:"data_source_id"`
	NamePropertyID       string `db:"name_property_id"`
	EmailPropertyID      string `db:"email_property_id"`
	AboutPropertyID      string `db:"about_property_id"`
	MembershipPropertyID string `db:"membership_property_id"`
	PersonPropertyID     string `db:"person_property_id"`
}

// peopleMemberships are Membership Type's options, as Notion's (seen 2026-10-12).
var peopleMemberships = []struct{ ID, Name, Color string }{
	{"owner", "Workspace owner", "red"}, {"member", "Member", "blue"}, {"restricted", "Restricted member", "yellow"},
	{"guest", "Guest", "yellow"}, {"deactivated", "Deactivated", "default"},
}

func loadPeopleDatabase(app core.App, workspaceID string) (peopleDatabase, bool, error) {
	return loadPeopleDatabaseBy(app, "workspace_id", workspaceID)
}

// newPeopleDatabase is the IDs a workspace's People database takes, the
// same each time, so a making that stopped half way is finished by the next.
func newPeopleDatabase(workspaceID string) peopleDatabase {
	id := func(part string) string { return derivedPageID("people-database", workspaceID, part) }
	return peopleDatabase{
		WorkspaceID: workspaceID, PageID: id("page"), DataSourceID: id("source"),
		NamePropertyID: id("name"), EmailPropertyID: id("email"), AboutPropertyID: id("about"),
		MembershipPropertyID: id("membership"), PersonPropertyID: id("person"),
	}
}

// peopleRowID is a person's row in a workspace's People.
func peopleRowID(workspaceID, userID string) string {
	return derivedPageID("people-row", workspaceID, userID)
}

// ensurePeopleDatabase makes the workspace's People database when it has none.
func ensurePeopleDatabase(app core.App, workspaceID string) (peopleDatabase, error) {
	if found, has, err := loadPeopleDatabase(app, workspaceID); err != nil || has {
		return found, err
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok {
		return peopleDatabase{}, errors.New("people database: no page store")
	}
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return peopleDatabase{}, err
	}
	database := newPeopleDatabase(workspaceID)
	move, _, err := loadPeopleDatabaseMove(app, workspaceID)
	if err != nil {
		return peopleDatabase{}, err
	}
	operations, err := peopleDatabaseOperations(database, move)
	if err != nil {
		return peopleDatabase{}, err
	}
	if err := savePeopleDatabasePage(app, workspace, database.PageID); err != nil {
		return peopleDatabase{}, err
	}
	kind := pageKindDatabase
	ctx, cancel := context.WithTimeout(context.Background(), peopleSyncTimeout)
	defer cancel()
	_, err = persistence.applyContentGraphMutationContext(ctx, workspacePermissionOwnerID(workspace), database.PageID,
		graphMutationInput{MutationID: "people-database:" + workspaceID, PageKind: &kind},
		contentGraphMutationHooks{
			// The server makes it; nobody made or named anything in it.
			SuppressNotifications: true, SuppressAutomationEvents: true, LeafenOwn: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return peopleDigest("people-database", workspaceID), nil
			},
			Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				_, err := txApp.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_workspace_people_databases}}
					([[workspace_id]], [[page_id]], [[data_source_id]], [[name_property_id]], [[email_property_id]],
						[[about_property_id]], [[membership_property_id]], [[person_property_id]], [[created_at]])
					VALUES ({:workspaceId}, {:pageId}, {:sourceId}, {:name}, {:email}, {:about}, {:membership}, {:person}, {:now})`).
					Bind(dbx.Params{"workspaceId": workspaceID, "pageId": database.PageID, "sourceId": database.DataSourceID,
						"name": database.NamePropertyID, "email": database.EmailPropertyID, "about": database.AboutPropertyID,
						"membership": database.MembershipPropertyID, "person": database.PersonPropertyID,
						"now": time.Now().UTC().UnixMilli()}).Execute()
				return err
			},
		})
	if err != nil {
		return peopleDatabase{}, err
	}
	// Made under its owner's authority, it is still no one's edit: its last
	// editor and its first version are Leafen's, as its people's pages are,
	// so it is not in its owner's Home "최근 편집" (seen 2026-11-16).
	if err := markLeafenSystemEdited(app, database.PageID, workspacePermissionOwnerID(workspace)); err != nil {
		return peopleDatabase{}, err
	}
	return database, nil
}

// savePeopleDatabasePage keeps the page People's database is: the
// workspace's, at its top, private to the server, made by it.
func savePeopleDatabasePage(app core.App, workspace *core.Record, pageID string) error {
	if _, err := app.FindRecordById(pagesCollection, pageID); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	pages, err := app.FindCollectionByNameOrId(pagesCollection)
	if err != nil {
		return err
	}
	cursor, err := nextWorkspaceUserStateCursor(app, workspace.GetString("owner"))
	if err != nil {
		return err
	}
	page := core.NewRecord(pages)
	page.Id = pageID
	page.Set("owner", workspace.GetString("owner"))
	page.Set("workspace", workspace.Id)
	page.Set("teamspace_id", "")
	page.Set("title", peopleDatabaseTitle)
	page.Set("content", "")
	page.Set("revision", 0)
	page.Set("cursor", cursor)
	page.Set("parent_id", "")
	page.Set("kind", pageKindDatabase)
	page.Set(privatePageOwnerField, leafenSystemAuthorID)
	applyPageTrashState(page, false, "")
	initializePageStyleMetadata(page)
	initializePageCustomizationMetadata(page)
	setNewPageCustodianProvenance(page, leafenSystemAuthorID)
	// One another write made meanwhile — People's read, as the work after
	// the workspace's request makes it — is the one it goes on with.
	if err := app.Save(page); err != nil {
		if _, findErr := app.FindRecordById(pagesCollection, pageID); findErr != nil {
			return err
		}
	}
	return nil
}

// peopleDatabaseOperations make the database: its block, its data source,
// its five own properties, and its two views — or the views People kept
// before, with the properties its owner added (people_database_moves.go).
func peopleDatabaseOperations(database peopleDatabase, move peopleDatabaseMove) ([]graphOperation, error) {
	blockID, directoryID, cardsID := derivedPageID("people-database", database.WorkspaceID, "block"),
		derivedPageID("people-database", database.WorkspaceID, "directory"), derivedPageID("people-database", database.WorkspaceID, "cards")
	moved, firstView, err := peopleMovedOperations(database, move)
	if err != nil {
		return nil, err
	}
	shownFirst := directoryID
	if firstView != "" {
		shownFirst = firstView
	}
	content, _ := json.Marshal(map[string]string{"dataSourceId": database.DataSourceID, "viewId": shownFirst})
	options := make([]map[string]string, 0, len(peopleMemberships))
	for _, option := range peopleMemberships {
		options = append(options, map[string]string{"id": option.ID, "name": option.Name, "color": option.Color})
	}
	membershipConfig, _ := json.Marshal(map[string]any{"options": options})
	property := func(id, kind, name, position string, config json.RawMessage) graphOperation {
		if config == nil {
			config = json.RawMessage(`{}`)
		}
		return graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: id, DataSourceID: database.DataSourceID, Type: kind, Name: name, Position: position, Config: config,
		}}
	}
	order := []string{database.NamePropertyID, database.EmailPropertyID, database.AboutPropertyID, database.MembershipPropertyID, database.PersonPropertyID}
	// Its views list the workspace's owner and members by name, as Notion's
	// (seen 2026-10-12), guests and the deactivated left out until their
	// filter is changed, its Person hidden (seen 2026-11-14).
	query := map[string]any{
		"sorts":        []map[string]string{{"propertyId": database.NamePropertyID, "direction": "asc"}},
		"quickFilters": []map[string]any{{"propertyId": database.MembershipPropertyID, "operator": "does_not_equal", "value": []string{"guest", "deactivated"}}},
		"rowOrder":     []string{},
	}
	view := func(id, kind, name, position string, extra map[string]any) graphOperation {
		config := map[string]any{}
		for key, value := range query {
			config[key] = value
		}
		for key, value := range extra {
			config[key] = value
		}
		encoded, _ := json.Marshal(config)
		return graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: database.DataSourceID, Type: kind, Name: name, Position: position, Config: encoded,
		}}
	}
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{ID: blockID, Position: "a0", Type: "inline_database", Content: content}},
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSourceWrite{ID: database.DataSourceID, PageID: database.PageID, Title: peopleDatabaseTitle}},
		property(database.NamePropertyID, graphValueTypeTitle, "Name", "a0", nil),
		property(database.EmailPropertyID, graphValueTypeEmail, "Email", "a1", nil),
		property(database.AboutPropertyID, graphValueTypeRichText, "About", "a2", nil),
		property(database.MembershipPropertyID, graphValueTypeSelect, "Membership Type", "a3", membershipConfig),
		property(database.PersonPropertyID, graphValueTypePerson, "Person", "a4", nil),
	}
	if firstView == "" {
		operations = append(operations, view(directoryID, "table", "Directory View", "a0", map[string]any{
			"icon": "user-circle:gray", "propertyOrder": order, "hiddenProperties": []string{database.PersonPropertyID},
		}), view(cardsID, "gallery", "Card View", "a1", map[string]any{
			"icon": "target:gray", "coverProperty": "__none", "visibleProperties": []string{database.MembershipPropertyID},
		}))
	}
	return append(operations, moved...), nil
}

// peoplePerson is a person as People's row shows them.
type peoplePerson struct {
	UserID      string `db:"user_id"`
	DisplayName string `db:"display_name"`
	Avatar      string `db:"avatar"`
	Email       string `db:"email"`
	About       string `db:"about"`
	Membership  string `db:"membership"`
}

// syncPeopleDatabase makes the workspace's People database when it has
// none, a row for each person People lists who has none, and sets each
// row's Name, About, Membership Type and Person to the person's — as the
// server, keeping each row's last edit (an About's own edit is stamped
// where it is written, savePersonAbout) — and sets off its automations with
// what it wrote, as the change of whoever made it
// (admitPeopleRowAutomationEvents); a database it just made is People as it
// was, no one's change. A run that fails leaves who made the changes to the
// run after it.
func syncPeopleDatabase(app core.App, workspaceID string) (database peopleDatabase, err error) {
	actors := takePeopleChangeActors(workspaceID)
	defer func() { keepPeopleChangeActorsIf(err != nil, workspaceID, actors) }()
	_, existed, err := loadPeopleDatabase(app, workspaceID)
	if err != nil {
		return peopleDatabase{}, err
	}
	database, err = ensurePeopleDatabase(app, workspaceID)
	if err != nil {
		return database, err
	}
	people := []peoplePerson{}
	if err := app.DB().NewQuery(peopleDirectorySQL(` AND NOT `+integrationAccountSQL("[[user]].[[id]]")) + `
		ORDER BY [[person]].[[user_id]]`).
		Bind(dbx.Params{"workspaceId": workspaceID, "ownerId": peopleWorkspaceOwner(app, workspaceID), "readerId": ""}).All(&people); err != nil {
		return database, err
	}
	joined := map[string]int64{}
	stamps := []struct {
		UserID   string `db:"user_id"`
		JoinedAt int64  `db:"joined_at"`
	}{}
	if err := app.DB().NewQuery(peopleJoinedSQL()).Bind(dbx.Params{"workspaceId": workspaceID}).All(&stamps); err != nil {
		return database, err
	}
	for _, stamp := range stamps {
		joined[stamp.UserID] = stamp.JoinedAt
	}
	kept, err := loadPeopleRowValues(app, database)
	if err != nil {
		return database, err
	}
	move, moving, err := loadPeopleDatabaseMove(app, workspaceID)
	if err != nil {
		return database, err
	}
	owned := map[string]bool{}
	for _, raw := range move.Properties {
		if id, _ := peopleMovedPropertyID(raw); id != "" {
			owned[id] = true
		}
	}
	madeAt := map[string]peopleRowMade{}
	for _, person := range people {
		made := peopleRowMade{Created: joined[person.UserID]}
		if row, has := move.Rows[person.UserID]; has {
			if row.JoinedAt > 0 {
				made.Created = row.JoinedAt
			}
			made.Edited, made.EditedBy = row.EditedAt, row.EditedBy
		}
		joined[person.UserID], madeAt[person.UserID] = made.Created, made
	}
	// The people who have no row yet come in the order they joined, as
	// Notion's ID numbers them (seen 2026-11-15).
	sort.SliceStable(people, func(i, j int) bool {
		left, right := joined[people[i].UserID], joined[people[j].UserID]
		if (left == 0) != (right == 0) {
			return right == 0
		}
		return left < right
	})
	operations, added, events := []graphOperation{}, []string{}, []peopleRowEvent{}
	for _, person := range people {
		want := peopleRowValues(database, person)
		row, has := kept[person.UserID]
		if !has {
			rowID := peopleRowID(workspaceID, person.UserID)
			peopleMovedRowValues(move, person.UserID, owned, want)
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
				ID: rowID, DataSourceID: database.DataSourceID, Position: peopleRowPosition(joined[person.UserID], person.UserID), Values: want,
			}})
			added = append(added, person.UserID)
			events = append(events, peopleRowEvent{UserID: person.UserID, RowID: rowID, Added: true, Changed: slices.Collect(maps.Keys(want))})
			continue
		}
		changed := map[string]graphRowValue{}
		for id, value := range want {
			if string(row.Values[id]) != string(value.Value) {
				changed[id] = value
			}
		}
		if len(changed) > 0 {
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
				ID: row.RowID, DataSourceID: database.DataSourceID, Position: row.Position, Values: changed,
			}})
			events = append(events, peopleRowEvent{UserID: person.UserID, RowID: row.RowID, Changed: slices.Collect(maps.Keys(changed))})
		}
	}
	if !existed {
		events = nil
	}
	if len(operations) == 0 {
		if moving {
			return database, finishPeopleDatabaseMove(app, workspaceID)
		}
		return database, nil
	}
	return database, applyPeopleRowOperations(app, database, operations, added, madeAt, moving, events, actors)
}

// peopleRowMade is when a person's row was made — when they joined — and
// last edited, and by whom (none: the server, when it was made).
type peopleRowMade struct {
	Created, Edited int64
	EditedBy        string
}

// applyPeopleRowOperations writes People's rows as the server, names the
// rows it made the people's, made by the server when they joined, and sets
// off the database's automations with them.
func applyPeopleRowOperations(app core.App, database peopleDatabase, operations []graphOperation, added []string, stamps map[string]peopleRowMade,
	moving bool, events []peopleRowEvent, actors map[string]string) error {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok {
		return errors.New("people database: no page store")
	}
	workspace, err := app.FindRecordById(workspacesCollection, database.WorkspaceID)
	if err != nil {
		return err
	}
	// Each run is a change of its own: the same values written again later are written again.
	encoded, _ := json.Marshal(operations)
	hash := peopleDigest("people-rows", database.WorkspaceID, newPageID(), string(encoded))
	ctx, cancel := context.WithTimeout(context.Background(), peopleSyncTimeout)
	defer cancel()
	// The rows the server writes are a version of People that is Leafen's, not
	// its owner's (contentGraphMutationHooks.LeafenOwn).
	_, err = persistence.applyContentGraphMutationContext(ctx, workspacePermissionOwnerID(workspace), database.PageID,
		graphMutationInput{MutationID: "people-rows:" + hash[:32]},
		contentGraphMutationHooks{
			SuppressNotifications: true, SuppressAutomationEvents: true, PreserveRowEditStamps: true, LeafenOwn: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) { return hash, nil },
			Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			AfterApply: func(txApp core.App, place pageAuthorization, _ *crdt.Doc) error {
				for _, userID := range added {
					if err := stampPeopleRowMade(txApp, database.WorkspaceID, userID, stamps[userID]); err != nil {
						return err
					}
				}
				if err := admitPeopleRowAutomationEvents(txApp, place, database, events, actors); err != nil {
					return err
				}
				if moving {
					return finishPeopleDatabaseMove(txApp, database.WorkspaceID)
				}
				return nil
			},
		})
	return err
}

type peopleRowKept struct {
	RowID, Position string
	Values          map[string]json.RawMessage
}

// loadPeopleRowValues reads each person's row and the values the server keeps in it.
func loadPeopleRowValues(app core.App, database peopleDatabase) (map[string]peopleRowKept, error) {
	stored := []struct {
		UserID     string         `db:"user_id"`
		RowID      string         `db:"row_id"`
		Position   sql.NullString `db:"position"`
		PropertyID sql.NullString `db:"property_id"`
		Value      sql.NullString `db:"value"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[person]].[[user_id]], [[person]].[[row_id]], [[row]].[[position]], [[value]].[[property_id]], [[value]].[[value]]
		FROM {{leafen_workspace_people_rows}} [[person]]
		LEFT JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[person]].[[row_id]]
		LEFT JOIN {{leafen_database_row_values}} [[value]] ON [[value]].[[row_id]] = [[person]].[[row_id]]
			AND [[value]].[[property_id]] IN ({:name}, {:about}, {:membership}, {:person})
		WHERE [[person]].[[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": database.WorkspaceID, "name": database.NamePropertyID, "about": database.AboutPropertyID,
			"membership": database.MembershipPropertyID, "person": database.PersonPropertyID}).All(&stored); err != nil {
		return nil, err
	}
	kept := map[string]peopleRowKept{}
	for _, row := range stored {
		entry, has := kept[row.UserID]
		if !has {
			entry = peopleRowKept{RowID: row.RowID, Position: row.Position.String, Values: map[string]json.RawMessage{}}
		}
		if row.PropertyID.Valid {
			entry.Values[row.PropertyID.String] = json.RawMessage(row.Value.String)
		}
		kept[row.UserID] = entry
	}
	return kept, nil
}

// peopleRowValues are the values a person's row holds of them.
func peopleRowValues(database peopleDatabase, person peoplePerson) map[string]graphRowValue {
	encode := func(value any) json.RawMessage {
		raw, _ := json.Marshal(value)
		return raw
	}
	return map[string]graphRowValue{
		database.NamePropertyID:       {Type: graphValueTypeTitle, Value: encode(person.DisplayName)},
		database.AboutPropertyID:      {Type: graphValueTypeRichText, Value: encode(person.About)},
		database.MembershipPropertyID: {Type: graphValueTypeSelect, Value: encode(person.Membership)},
		database.PersonPropertyID:     {Type: graphValueTypePerson, Value: encode([]string{person.UserID})},
	}
}

// peopleRowPosition places a row by when the person joined.
func peopleRowPosition(joinedAt int64, userID string) string {
	if joinedAt <= 0 {
		return "z" + userID
	}
	return fmt.Sprintf("a%0*d", peopleRowPositionSize*2, joinedAt)
}

func peopleWorkspaceOwner(app core.App, workspaceID string) string {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return ""
	}
	return workspacePermissionOwnerID(workspace)
}

func peopleDigest(parts ...string) string {
	digest := sha256.Sum256([]byte("leafen:" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
