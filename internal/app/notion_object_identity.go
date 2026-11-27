package app

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionObjectIdentityDatabase   = "database"
	notionObjectIdentityDataSource = "data_source"
	notionObjectIdentityRowPage    = "row_page"
	notionObjectIdentityRowBlock   = "row_block"
	notionObjectIdentityComment    = "comment"
	notionObjectIdentityFileUpload = "file_upload"
	notionObjectIdentityView       = "view"
)

var errInvalidNotionObjectIdentity = errors.New("invalid Notion-compatible object identity")

type notionObjectIdentity struct {
	PublicID   string `db:"public_id"`
	ObjectKind string `db:"object_kind"`
	Workspace  string `db:"workspace_id"`
	InternalID string `db:"internal_id"`
}

func notionCompatibleObjectID(objectKind, workspaceID, internalID string) (string, error) {
	objectKind = strings.TrimSpace(objectKind)
	workspaceID = strings.TrimSpace(workspaceID)
	internalID = strings.TrimSpace(internalID)
	if !validNotionObjectIdentityKind(objectKind) || workspaceID == "" || internalID == "" ||
		len(workspaceID) > maxGraphIdentifierLength || len(internalID) > maxGraphIdentifierLength {
		return "", errInvalidNotionObjectIdentity
	}
	digest := sha256.Sum256([]byte(
		"leafen:notion-object-identity:v1\x00" + objectKind + "\x00" + workspaceID + "\x00" + internalID,
	))
	// Scheme nibble 2 makes these UUIDv8 identities disjoint from the reversible
	// ordinary-page codec, whose first payload nibble is scheme 1.
	digest[0] = digest[0]&0x0f | 0x20
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	var encoded [16]byte
	copy(encoded[:], digest[:16])
	return formatNotionUserUUID(encoded), nil
}

func ensureNotionObjectIdentity(
	app core.App,
	objectKind, workspaceID, internalID string,
) (string, error) {
	if strings.TrimSpace(workspaceID) != "" {
		if own, ok, err := objectOwnPublicID(app, objectKind, internalID); err != nil || ok {
			return own, err
		}
	}
	// One moved to another workspace keeps the identity it had there
	// (notion_object_identity_moved.go).
	if kept, ok, err := movedNotionObjectIdentity(app, objectKind, workspaceID, internalID); err != nil || ok {
		return kept, err
	}
	publicID, err := notionCompatibleObjectID(objectKind, workspaceID, internalID)
	if err != nil {
		return "", err
	}
	// A read snapshot names it now and stores it once it ends, before its
	// answer goes out (notionDeferredIdentities): its connection refuses
	// writes. Inside a snapshot or a transaction app is that one's: the
	// server's own app would write on the one write connection, which a
	// transaction holds already — it then waits for itself (2026-11-22: an
	// MCP fetch of a data source held every write of a server without the
	// snapshots' pool) — and a snapshot's read would wait out other writes.
	if snapshot, ok := app.(*readSnapshotApp); ok && snapshot.identities != nil {
		return snapshot.identities.ensure(app, objectKind, workspaceID, internalID, publicID)
	}
	// One stored already is read, not written again: an insert that adds
	// nothing still waits for the write connection (2026-11-14: every read
	// of a database waited out a long write here).
	stored := notionObjectIdentity{}
	err = app.DB().NewQuery(`
		SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]]
		FROM {{leafen_notion_object_identities}}
		WHERE [[object_kind]]={:objectKind} AND [[workspace_id]]={:workspaceId}
			AND [[internal_id]]={:internalId}
	`).Bind(dbx.Params{
		"objectKind": objectKind, "workspaceId": workspaceID, "internalId": internalID,
	}).One(&stored)
	switch {
	case err == nil && stored.PublicID == publicID:
		return publicID, nil
	case err == nil:
		return "", errInvalidNotionObjectIdentity
	case !errors.Is(err, sql.ErrNoRows):
		return "", err
	}
	if _, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_notion_object_identities}}
			([[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]], [[created]])
		VALUES ({:publicId}, {:objectKind}, {:workspaceId}, {:internalId}, {:created})
	`).Bind(dbx.Params{
		"publicId": publicID, "objectKind": objectKind, "workspaceId": workspaceID,
		"internalId": internalID, "created": time.Now().UTC().UnixMilli(),
	}).Execute(); err != nil {
		return "", err
	}
	err = app.DB().NewQuery(`
		SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]]
		FROM {{leafen_notion_object_identities}}
		WHERE [[object_kind]]={:objectKind} AND [[workspace_id]]={:workspaceId}
			AND [[internal_id]]={:internalId}
	`).Bind(dbx.Params{
		"objectKind": objectKind, "workspaceId": workspaceID, "internalId": internalID,
	}).One(&stored)
	if err != nil || stored.PublicID != publicID {
		return "", refusedUnlessStorage(errInvalidNotionObjectIdentity, err)
	}
	return publicID, nil
}

// notionDeferredIdentities are the public identities a read snapshot names
// (runReadSnapshot), whose connections refuse writes: one stored already is
// read once and kept, one not stored yet is named as it will be stored — its
// ID is worked out from what it names — and stored with the others, as
// ensureNotionObjectIdentity stores one, once the snapshot ends and before
// its answer goes out.
type notionDeferredIdentities struct {
	known   map[notionDeferredIdentityKey]string
	pending []notionObjectIdentity
}

type notionDeferredIdentityKey struct {
	objectKind, workspaceID, internalID string
}

func newNotionDeferredIdentities() *notionDeferredIdentities {
	return &notionDeferredIdentities{known: map[notionDeferredIdentityKey]string{}}
}

func (deferred *notionDeferredIdentities) ensure(
	app core.App,
	objectKind, workspaceID, internalID, publicID string,
) (string, error) {
	key := notionDeferredIdentityKey{objectKind, workspaceID, internalID}
	if known, ok := deferred.known[key]; ok {
		return known, nil
	}
	stored := notionObjectIdentity{}
	err := app.DB().NewQuery(`
		SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]]
		FROM {{leafen_notion_object_identities}}
		WHERE [[object_kind]]={:objectKind} AND [[workspace_id]]={:workspaceId}
			AND [[internal_id]]={:internalId}
	`).Bind(dbx.Params{
		"objectKind": objectKind, "workspaceId": workspaceID, "internalId": internalID,
	}).One(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		deferred.pending = append(deferred.pending, notionObjectIdentity{
			PublicID: publicID, ObjectKind: objectKind, Workspace: workspaceID, InternalID: internalID,
		})
	case err != nil:
		return "", err
	case stored.PublicID != publicID:
		return "", errInvalidNotionObjectIdentity
	}
	deferred.known[key] = publicID
	return publicID, nil
}

// store writes the identities named that weren't stored, in one transaction.
func (deferred *notionDeferredIdentities) store(app core.App) error {
	if len(deferred.pending) == 0 {
		return nil
	}
	return app.RunInTransaction(func(txApp core.App) error {
		for _, identity := range deferred.pending {
			publicID, err := ensureNotionObjectIdentity(
				txApp, identity.ObjectKind, identity.Workspace, identity.InternalID,
			)
			if err != nil {
				return err
			}
			if publicID != identity.PublicID {
				return errInvalidNotionObjectIdentity
			}
		}
		return nil
	})
}

func resolveNotionObjectIdentity(
	app core.App,
	objectKind, publicID string,
) (notionObjectIdentity, error) {
	given := strings.TrimSpace(publicID)
	publicID, ok := normalizeNotionUUIDRequest(publicID)
	if !ok && objectKind == notionObjectIdentityDataSource {
		return notionDataSourceIdentityByInternalID(app, given)
	}
	if !ok && objectKind == notionObjectIdentityRowPage {
		return notionRowPageIdentityByInternalID(app, given)
	}
	if !ok || !validNotionObjectIdentityKind(objectKind) {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	stored := notionObjectIdentity{}
	err := app.DB().NewQuery(`
		SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]]
		FROM {{leafen_notion_object_identities}}
		WHERE [[public_id]]={:publicId} AND [[object_kind]]={:objectKind}
	`).Bind(dbx.Params{"publicId": publicID, "objectKind": objectKind}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) && objectKind == notionObjectIdentityRowPage {
		// A database page's own ID names it: a UUID, or the page codec's ID
		// for one of 15 characters (rowPageOwnPublicID).
		if recordID, ownErr := resolvePageOwnPublicID(publicID); ownErr == nil {
			return notionRowPageIdentityByInternalID(app, recordID)
		}
		return notionRowPageIdentityByInternalID(app, publicID)
	}
	if errors.Is(err, sql.ErrNoRows) && objectKind == notionObjectIdentityDataSource {
		return notionDataSourceIdentityByInternalID(app, publicID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notionObjectIdentityByOwnID(app, objectKind, publicID)
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	expected, err := notionCompatibleObjectID(stored.ObjectKind, stored.Workspace, stored.InternalID)
	if err != nil || expected != stored.PublicID {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	// The identity made for the API that a database page, a database or a
	// view answered with before still names it; it answers with its own ID
	// now.
	if own, ok, err := objectOwnPublicID(app, objectKind, stored.InternalID); err != nil {
		return notionObjectIdentity{}, err
	} else if ok {
		stored.PublicID = own
	}
	return stored, nil
}

// rowPageOwnPublicID is the public ID a database page answers with, a
// database template's page too: the ID in its address, as a page's
// (pageOwnPublicID) — its own ID, a UUID, or the page codec's ID for one of
// 15 characters — as Notion's database page has one ID in its address and
// its API, and keeps it as it turns into a page or back. Database pages
// answered with a row identity (UUIDv8) before 2026-11-16; one named so
// still resolves to its page (resolveNotionObjectIdentity), and a page
// whose ID has no page ID of its own (a UUIDv8 shape) answers with it still.
func rowPageOwnPublicID(internalID string) (string, bool) {
	own, err := pageOwnPublicID(strings.TrimSpace(internalID))
	return own, err == nil
}

// notionDataSourceIdentityByInternalID reads the ID MCP names a data source
// by, collection://ID, which names it here too, as Notion's are one.
func notionDataSourceIdentityByInternalID(app core.App, internalID string) (notionObjectIdentity, error) {
	workspaceID := ""
	err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": internalID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID == "" {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	publicID, err := notionCompatibleObjectID(notionObjectIdentityDataSource, workspaceID, internalID)
	if err != nil {
		return notionObjectIdentity{}, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityDataSource, Workspace: workspaceID, InternalID: internalID,
	}, nil
}

// notionRowPageIdentityByInternalID reads the ID in a database item's address,
// its own ID, as the browser's address bar, this server's links and MCP give
// it (pageRecordAtAddress). A database template's page answers as one of its
// data source's pages does (notion_database_template_page.go), so the ID of
// the template its database's address opens (?tp=) names it too.
func notionRowPageIdentityByInternalID(app core.App, internalID string) (notionObjectIdentity, error) {
	workspaceID := ""
	err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:id}`).
		Bind(dbx.Params{"id": internalID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]]={:id} AND [[database_template]]`).
			Bind(dbx.Params{"id": internalID}).Row(&workspaceID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, internalID)
	if err != nil {
		return notionObjectIdentity{}, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityRowPage, Workspace: workspaceID, InternalID: internalID,
	}, nil
}

func notionObjectIdentityPublicID(
	app core.App,
	objectKind, workspaceID, internalID string,
) (string, error) {
	if !validNotionObjectIdentityKind(objectKind) || strings.TrimSpace(workspaceID) == "" ||
		strings.TrimSpace(internalID) == "" {
		return "", errInvalidNotionObjectIdentity
	}
	if own, ok, err := objectOwnPublicID(app, objectKind, internalID); err != nil || ok {
		return own, err
	}
	stored := notionObjectIdentity{}
	err := app.DB().NewQuery(`
		SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]]
		FROM {{leafen_notion_object_identities}}
		WHERE [[object_kind]]={:objectKind} AND [[workspace_id]]={:workspaceId}
			AND [[internal_id]]={:internalId}
	`).Bind(dbx.Params{
		"objectKind": objectKind, "workspaceId": workspaceID, "internalId": internalID,
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errInvalidNotionObjectIdentity
	}
	if err != nil {
		return "", err
	}
	expected, err := notionCompatibleObjectID(objectKind, workspaceID, internalID)
	if err != nil || stored.PublicID != expected {
		return "", errInvalidNotionObjectIdentity
	}
	return stored.PublicID, nil
}

func validNotionObjectIdentityKind(value string) bool {
	switch value {
	case notionObjectIdentityDatabase, notionObjectIdentityDataSource,
		notionObjectIdentityRowPage, notionObjectIdentityRowBlock,
		notionObjectIdentityComment, notionObjectIdentityFileUpload, notionObjectIdentityView,
		notionObjectIdentityBlock:
		return true
	default:
		return false
	}
}
