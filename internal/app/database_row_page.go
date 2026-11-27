package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var (
	errDatabaseRowPageNotFound        = errors.New("database row page not found")
	errDatabaseRowPageCursorInvalid   = errors.New(errorCodeDatabaseRowPageCursorInvalid)
	errDatabaseRowPageRevisionChanged = errors.New(errorCodeDatabaseRowPageRevisionChanged)
	errDatabaseRowPageConflict        = errors.New(errorCodeDatabaseRowPageConflict)
	errDatabaseRowPageInTrash         = errors.New(errorCodeDatabaseRowPageInTrash)
	// errDatabaseRowPageReadOnly is a row the person reads but may not change
	// (view access, or a database an app keeps for itself). Where a write is
	// answered it is refused as forbidden; to callers that only hide what
	// they can't reach it is still a row not found.
	errDatabaseRowPageReadOnly = fmt.Errorf("%w: read only", errDatabaseRowPageNotFound)
	// errDatabaseRowPageMoveDenied is a move of a row the person changes to
	// another page, refused for want of full access to it or of edit access
	// where it goes (2026-10-31). The move is answered with that reason;
	// to callers that only hide what they can't reach it is still a denial.
	errDatabaseRowPageMoveDenied = fmt.Errorf("move needs full access to the row and edit access where it goes: %w", permissionDenied())
)

type databaseRowPageAuthority struct {
	RowID                  string `db:"row_id"`
	OwnerID                string `db:"owner"`
	WorkspaceID            string `db:"workspace_id"`
	ContainerPageID        string `db:"container_page_id"`
	DataSourceID           string `db:"data_source_id"`
	Position               string `db:"position"`
	SubitemParentID        string `db:"subitem_parent_id"`
	AuthorityRevision      int    `db:"authority_revision"`
	Icon                   string `db:"icon"`
	IconType               string `db:"icon_type"`
	Cover                  string `db:"cover"`
	CoverPosition          int    `db:"cover_position"`
	Font                   string `db:"font"`
	SmallText              bool   `db:"small_text"`
	FullWidth              bool   `db:"full_width"`
	TableOfContentsDisplay string `db:"table_of_contents_display"`
	Locked                 bool   `db:"is_locked"`
	LockedBy               string `db:"locked_by"`
	ArchivedAt             string `db:"archived_at"`
	ArchivedBy             string `db:"archived_by"`
	CreatedBy              string `db:"created_by"`
	LastEditedBy           string `db:"last_edited_by"`
	CreatedAt              int64  `db:"created"`
	UpdatedAt              int64  `db:"updated"`
	BodySnapshotRevision   int    `db:"body_snapshot_revision"`
	// A trashed row keeps its authority; TrashRootID names the row whose
	// trash event it belongs to.
	TrashedAt   int64  `db:"trashed_at"`
	TrashedBy   string `db:"trashed_by"`
	TrashRootID string `db:"trash_root_id"`
	// A template opened as a row page (database_template_row_page.go).
	Template bool `db:"template"`
}

type databaseRowPageCursor struct {
	PrincipalID               string `json:"principalId"`
	RowID                     string `json:"rowId"`
	ContainerSnapshotRevision int    `json:"containerSnapshotRevision"`
	AuthorityRevision         int    `json:"authorityRevision"`
	BodySnapshotRevision      int    `json:"bodySnapshotRevision"`
	SortKey                   string `json:"sortKey"`
	Limit                     int    `json:"limit"`
}

type storedDatabaseRowPageBodyBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	SortKey  string `db:"sort_key"`
}

// databaseRowPageValuesMaxBodyBytes holds an item's title as long as a page's
// written with its formatting — its words twice, in the value and its spans,
// four bytes a character — or any other property's value in 256 KiB.
const databaseRowPageValuesMaxBodyBytes = 8*maxGraphTitleRunes + 256*1024

func registerDatabaseRowPageRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+databaseRowPageReadPath, readDatabaseRowPage)
	event.Router.PUT(apiRoot+databaseRowPageFavoritePath, setDatabaseRowPageFavorite).
		Bind(apis.BodyLimit(1024))
	event.Router.POST(apiRoot+databaseRowPageDuplicatePath, ygo.duplicateDatabaseRowPage).
		Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+databaseRowPageMovePath, ygo.moveDatabaseRowPage).
		Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+databaseRowPageTrashPath, ygo.trashDatabaseRowPage).
		Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+databaseRowPageRestorePath, ygo.restoreDatabaseRowPage).
		Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+databaseRowPageDeletePermanentlyPath, deleteDatabaseRowPagePermanently).
		Bind(apis.BodyLimit(4096))
	event.Router.GET(apiRoot+databaseRowTrashListPath, readDatabaseRowTrash)
	event.Router.PUT(apiRoot+databaseRowPageArchivePath, setDatabaseRowPageArchive).
		Bind(apis.BodyLimit(1024))
	event.Router.PUT(apiRoot+databaseRowPagePresentationPath, setDatabaseRowPagePresentation).
		Bind(apis.BodyLimit(8 * 1024))
	event.Router.GET(apiRoot+databaseRowPagePermissionListPath, readDatabaseRowPagePermissions)
	event.Router.POST(apiRoot+databaseRowPagePermissionMutatePath, mutateDatabaseRowPagePermission).
		Bind(apis.BodyLimit(1024))
	event.Router.POST(apiRoot+databaseRowPageValuesPath, writeDatabaseRowPageValues(ygo)).
		Bind(apis.BodyLimit(databaseRowPageValuesMaxBodyBytes))
	event.Router.POST(apiRoot+databaseRowValuesBatchPath, writeDatabaseRowValuesBatch(ygo)).
		Bind(apis.BodyLimit(databaseRowPageMaxBodyBytes))
	event.Router.POST(apiRoot+databaseRowPageFromPagePath, ygo.moveDatabaseRowPageIn).
		Bind(apis.BodyLimit(4096))
	// A wiki page's change can rename it with a title as long as a page's.
	event.Router.POST(apiRoot+databaseRowPageWikiChangePath, ygo.changeWikiPageRoute).
		Bind(apis.BodyLimit(4*maxGraphTitleRunes + 16*1024))
}

func databaseRowPageNotFoundError() error {
	return errDatabaseRowPageNotFound
}

// authorizeDatabaseRowPage reads a row page, trashed or not; a trashed row
// refuses writes until it is restored.
func authorizeDatabaseRowPage(
	app core.App,
	principalID string,
	rowID string,
	requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	authority, err := authorizeDatabaseRowPageInTrash(app, principalID, rowID, requirement)
	if err == nil && requirement != permissionWorkspaceRead && authority.TrashedAt != 0 {
		return databaseRowPageAuthority{}, errDatabaseRowPageInTrash
	}
	return authority, err
}

// authorizeDatabaseRowPageInTrash also lets a trashed row be written, for
// the trash actions themselves.
func authorizeDatabaseRowPageInTrash(
	app core.App,
	principalID string,
	rowID string,
	requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	authority, err := loadDatabaseRowPageAuthority(app, rowID)
	if err != nil {
		return databaseRowPageAuthority{}, err
	}
	return authorizeDatabaseRowPageAuthority(app, principalID, authority, requirement)
}

// authorizeDatabaseRowPageAuthority authorizes a person on a row page whose
// authority is read.
func authorizeDatabaseRowPageAuthority(
	app core.App,
	principalID string,
	authority databaseRowPageAuthority,
	requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	// A row's page authorizes it, through its data source's page or its
	// own shares.
	page, err := authorizeDatabaseRowPageRecord(app, principalID, authority, pageAccessRoleForWorkspaceRequirement(requirement))
	if isPermissionDenied(err) {
		if requirement != permissionWorkspaceRead {
			if _, readErr := authorizeDatabaseRowPageRecord(app, principalID, authority,
				pageAccessRoleForWorkspaceRequirement(permissionWorkspaceRead)); readErr == nil {
				return databaseRowPageAuthority{}, errDatabaseRowPageReadOnly
			}
		}
		return databaseRowPageAuthority{}, refusalKeepingHold(err, databaseRowPageNotFoundError())
	}
	if err != nil {
		return databaseRowPageAuthority{}, err
	}
	if page.Workspace.OwnerID != authority.OwnerID ||
		page.Workspace.WorkspaceID != authority.WorkspaceID {
		return databaseRowPageAuthority{}, databaseRowPageNotFoundError()
	}
	return authority, nil
}

// loadDatabaseRowPageAuthority reads a row's page, trashed or not, without
// asking who may reach it.
func loadDatabaseRowPageAuthority(app core.App, rowID string) (databaseRowPageAuthority, error) {
	// Read once a read scope: a database item's graph read read it for
	// reading, editing and suggesting (2026-10-17).
	return remembered(app, rowPageAuthorityMemoKey{rowID}, func() (databaseRowPageAuthority, error) {
		return readDatabaseRowPageAuthority(app, rowID)
	})
}

type rowPageAuthorityMemoKey struct{ rowID string }

func readDatabaseRowPageAuthority(app core.App, rowID string) (databaseRowPageAuthority, error) {
	return readDatabaseRowPageAuthorityWhere(app, rowID, databaseSourceNotTrashedSQL("[[page]].[[data_source_id]]"))
}

// readDatabaseRowPageAuthorityWhere reads a row's page whose data source
// meets sourceCondition: one out of the trash, or any, for the Public API's
// reads of rows in the trash (notion_trash_reads.go).
func readDatabaseRowPageAuthorityWhere(app core.App, rowID, sourceCondition string) (databaseRowPageAuthority, error) {
	authority := databaseRowPageAuthority{}
	err := app.DB().NewQuery(databaseRowPageAuthoritySQL + `
		WHERE [[page]].[[row_id]]={:rowId} AND ` + sourceCondition + `
	`).Bind(dbx.Params{"rowId": rowID}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseRowPageAuthority{}, databaseRowPageNotFoundError()
	}
	return authority, err
}

// readDatabaseRowPageAuthorities reads the pages of rows at once, as
// readDatabaseRowPageAuthorityWhere reads each; a row it finds none for is
// left out.
func readDatabaseRowPageAuthorities(app core.App, rowIDs []string, sourceCondition string) ([]databaseRowPageAuthority, error) {
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	authorities := []databaseRowPageAuthority{}
	err = app.DB().NewQuery(databaseRowPageAuthoritySQL + `
		WHERE [[page]].[[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds})) AND ` + sourceCondition + `
	`).Bind(dbx.Params{"rowIds": string(encoded)}).All(&authorities)
	return authorities, err
}

// databaseRowPageAuthoritySQL reads what a row's page is under: its row, its
// record unless a deletion holds it, and its place in the trash. The row is
// found by its id; its owner, workspace, page and source are read after (+),
// or SQLite, which keeps no statistics, reads its source's every row.
var databaseRowPageAuthoritySQL = `
		SELECT [[page]].[[row_id]], [[page]].[[owner]], [[page]].[[workspace_id]],
			[[page]].[[container_page_id]], [[page]].[[data_source_id]],
			[[row]].[[position]], [[row]].[[subitem_parent_id]],
			[[page]].[[authority_revision]], [[row_record]].[[icon]], [[row_record]].[[icon_type]],
			[[row_record]].[[cover]], [[row_record]].[[cover_position]], [[row_record]].[[is_locked]], [[row_record]].[[locked_by]],
			[[row_record]].[[font]], [[row_record]].[[small_text]], [[row_record]].[[full_width]], [[row_record]].[[table_of_contents_display]],
			[[row_record]].[[archived_at]], [[row_record]].[[archived_by]],
			[[row_record]].[[created_by]], [[row_record]].[[last_edited_by]],
			[[page]].[[created]], [[page]].[[updated]],
			[[page]].[[body_snapshot_revision]],
			COALESCE([[trash]].[[trashed_at]], 0) AS [[trashed_at]],
			COALESCE([[trash]].[[trashed_by]], '') AS [[trashed_by]],
			COALESCE([[trash]].[[root_row_id]], '') AS [[trash_root_id]], FALSE AS [[template]]
		FROM {{leafen_database_row_pages}} [[page]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]]=[[page]].[[row_id]]
			AND +[[row]].[[owner]]=[[page]].[[owner]]
			AND +[[row]].[[workspace_id]]=[[page]].[[workspace_id]]
			AND +[[row]].[[page_id]]=[[page]].[[container_page_id]]
			AND +[[row]].[[data_source_id]]=[[page]].[[data_source_id]]
		JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]]=[[page]].[[row_id]]
			AND [[row_record]].[[deletion_job_id]] NOT LIKE '` + pageDeletionHoldFencePrefix + `%'
		LEFT JOIN {{leafen_database_row_trash}} [[trash]] ON [[trash]].[[row_id]]=[[page]].[[row_id]]`

func readDatabaseRowPage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	query := event.Request.URL.Query()
	limit, err := parseDatabaseRowPageLimit(query.Get("limit"))
	// "body=first" reads only the body's first block and the first block
	// inside it, what a card's "페이지 콘텐츠" draws; it has no next window.
	firstBlock := query.Get("body") == "first"
	if err != nil || rowID == "" || query.Get("body") != "" && (!firstBlock || query.Get("cursor") != "") {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the database row query range.")
	}
	response, err := loadDatabaseRowPageWindowFor(
		event.Request.Context(), event.App, user.Id, rowID, query.Get("cursor"), limit, firstBlock,
	)
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageCursorInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageCursorInvalid, "Check the database row query position.")
	case errors.Is(err, errDatabaseRowPageRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageRevisionChanged, "The database rows changed. Load them again from the start.")
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row body is outside the allowed read range.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't load the database rows.", "database_row_page", "read",
			errorCodeDatabaseRowPageFailed, err,
		)
	default:
		// What names a page closed to this reader (content_graph_reader_mentions.go).
		rowPageAsRead(event.App, user.Id, &response)
		// An agent's read takes a column's options short (database_row_page_agent_options.go).
		databaseRowPageOptionsForAgent(event, rowID, query.Get("options") == "all", &response)
		return event.JSON(http.StatusOK, response)
	}
}

func setDatabaseRowPageFavorite(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPageFavoriteInput{}
	if rowID == "" || event.BindBody(&input) != nil || input.ExpectedAuthorityRevision < 1 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the favorite change details.")
	}
	response, err := updateDatabaseRowPageFavorite(event.App, user.Id, rowID, input)
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't change the favorite.", "database_row_page", "favorite",
			errorCodeDatabaseRowPageFailed, err,
		)
	default:
		return event.JSON(http.StatusOK, response)
	}
}

func updateDatabaseRowPageFavorite(
	app core.App,
	principalID, rowID string,
	input databaseRowPageFavoriteInput,
) (databaseRowPageActionResponse, error) {
	stripe := pageFavoriteStripe(principalID, rowID)
	stripe.Lock()
	defer stripe.Unlock()
	response := databaseRowPageActionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowOrTemplatePage(
			txApp, principalID, rowID, permissionWorkspaceRead,
		)
		if err != nil {
			return err
		}
		if !authority.Template && authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		// A database page's favorite is its page record's favorite, a template's too.
		state, err := loadPageFavoriteState(txApp, principalID, rowID)
		if err != nil {
			return err
		}
		if state.Revision == 0 || state.Favorite != input.Favorite {
			requestID := "row-page-favorite:" + security.RandomString(24)
			err = storePageFavorite(
				txApp, rowID, authority.WorkspaceID, principalID, input.Favorite, state.Revision+1, requestID,
				pageFavoriteRequestHash(pageFavoriteMutationInput{
					RequestID: requestID, Favorite: input.Favorite, ExpectedRevision: state.Revision,
				}),
			)
		}
		if err == nil {
			response = databaseRowPageActionResponse{
				RowID: rowID, ContainerPageID: authority.ContainerPageID,
				AuthorityRevision: authority.AuthorityRevision,
			}
		}
		return err
	})
	return response, err
}

func parseDatabaseRowPageLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return databaseRowPageBodyPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > databaseRowPageBodyPageSize {
		return 0, errDatabaseRowPageCursorInvalid
	}
	return limit, nil
}

func databaseRowPageSnapshotIdentity(rowID string, containerRevision, authorityRevision, bodyRevision int) string {
	digest := sha256.Sum256([]byte(
		rowID + ":" + strconv.Itoa(containerRevision) + ":" +
			strconv.Itoa(authorityRevision) + ":" + strconv.Itoa(bodyRevision),
	))
	return hex.EncodeToString(digest[:])
}

func encodeDatabaseRowPageCursor(cursor databaseRowPageCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil || len(encoded) > databaseRowPageCursorBytes {
		return "", errDatabaseRowPageCursorInvalid
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeDatabaseRowPageCursor(raw, principalID, rowID string, limit int) (databaseRowPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return databaseRowPageCursor{
			PrincipalID: principalID, RowID: rowID, Limit: limit,
		}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > databaseRowPageCursorBytes {
		return databaseRowPageCursor{}, errDatabaseRowPageCursorInvalid
	}
	cursor := databaseRowPageCursor{}
	if json.Unmarshal(decoded, &cursor) != nil || cursor.PrincipalID != principalID ||
		cursor.RowID != rowID || cursor.Limit != limit || cursor.SortKey == "" ||
		cursor.ContainerSnapshotRevision < 0 || cursor.AuthorityRevision < 1 ||
		cursor.BodySnapshotRevision < 0 {
		return databaseRowPageCursor{}, errDatabaseRowPageCursorInvalid
	}
	return cursor, nil
}

func loadDatabaseRowPageWindow(
	app core.App, principalID string, rowID string, rawCursor string, limit int, firstBlock bool,
) (databaseRowPageSnapshot, error) {
	return loadDatabaseRowPageWindowFor(context.Background(), app, principalID, rowID, rawCursor, limit, firstBlock)
}

// loadDatabaseRowPageWindowFor is loadDatabaseRowPageWindow for the reader
// whose own request ctx carries.
func loadDatabaseRowPageWindowFor(
	ctx context.Context,
	app core.App,
	principalID string,
	rowID string,
	rawCursor string,
	limit int,
	firstBlock bool,
) (databaseRowPageSnapshot, error) {
	response := databaseRowPageSnapshot{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		response, err = loadDatabaseRowPageWindowInTransactionFor(
			ctx, txApp, principalID, rowID, rawCursor, limit, firstBlock,
		)
		return err
	})
	return response, err
}
