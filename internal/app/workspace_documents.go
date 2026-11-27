package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The library's "문서" tab (document apps decision of 2026-10-11, 12):
// the docx, xlsx, pptx, pdf, HWP and HWPX file blocks on the pages
// a person can read, and those a database page's file properties hold, latest change or name first, found by name or by the
// words the server read from them, one kind at a time. The list walks the
// workspace's file blocks in that order and leaves out what the person
// can't read — a page they can't open, one in the trash or on its way out,
// a template — and hands back a cursor after the last block it looked at,
// so a long list reads to its end a stretch at a time and nothing is cut off.

const (
	workspaceDocumentsScanBatch  = 200
	workspaceDocumentsScanBudget = 2000
)

// workspaceDocumentKindSQL names a file block's kind of document by its
// type, else by its name's extension; other files have none.
const workspaceDocumentKindSQL = `CASE
	WHEN [[ct]] = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' OR [[nm]] LIKE '%.docx' THEN 'docx'
	WHEN [[ct]] = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' OR [[nm]] LIKE '%.xlsx' THEN 'xlsx'
	WHEN [[ct]] = 'application/vnd.openxmlformats-officedocument.presentationml.presentation' OR [[nm]] LIKE '%.pptx' THEN 'pptx'
	WHEN [[ct]] LIKE 'application/pdf%' OR [[nm]] LIKE '%.pdf' THEN 'pdf'
	WHEN [[ct]] IN ('application/hwp+zip', 'application/vnd.hancom.hwpx', 'application/haansofthwpx') OR [[nm]] LIKE '%.hwpx' THEN 'hwpx'
	WHEN [[ct]] IN ('application/x-hwp', 'application/haansofthwp', 'application/vnd.hancom.hwp') OR [[nm]] LIKE '%.hwp' THEN 'hwp'
END`

type workspaceDocumentsFilter struct {
	Query string
	// The kinds asked for, in order; none is every kind.
	Kinds []string
	Sort  string
}

func (filter workspaceDocumentsFilter) key() string {
	digest := sha256.Sum256([]byte(filter.Sort + "\x00" + strings.Join(filter.Kinds, ",") + "\x00" + filter.Query))
	return hex.EncodeToString(digest[:8])
}

// workspaceDocumentsCursor is the last block a list looked at, for the
// person, workspace and filter it was made for.
type workspaceDocumentsCursor struct {
	PrincipalID string `json:"p"`
	WorkspaceID string `json:"w"`
	Filter      string `json:"f"`
	Updated     int64  `json:"u"`
	Name        string `json:"n"`
	BlockID     string `json:"b"`
}

type storedWorkspaceDocument struct {
	BlockID   string `db:"block_id"`
	PageID    string `db:"page_id"`
	Content   string `db:"content"`
	CreatedBy string `db:"created_by"`
	Updated   int64  `db:"updated"`
	NameKey   string `db:"nm"`
	Kind      string `db:"kind"`
	Text      string `db:"text"`
	// A file property's document names its property; a file block's none.
	PropertyID string `db:"property_id"`
	Partial    string `db:"partial"`
}

// scanWorkspaceDocuments reads the next document blocks after the cursor
// in the filter's order, whether or not the person can read their pages.
func scanWorkspaceDocuments(app core.App, workspaceID string, filter workspaceDocumentsFilter, after workspaceDocumentsCursor) ([]storedWorkspaceDocument, error) {
	params := dbx.Params{"workspaceId": workspaceID, "batch": workspaceDocumentsScanBatch}
	where := []string{"[[kind]] IS NOT NULL"}
	if len(filter.Kinds) > 0 {
		kinds := make([]string, len(filter.Kinds))
		for index, kind := range filter.Kinds {
			kinds[index] = "{:kind" + strconv.Itoa(index) + "}"
			params["kind"+strconv.Itoa(index)] = kind
		}
		where = append(where, "[[kind]] IN ("+strings.Join(kinds, ", ")+")")
	}
	if filter.Query != "" {
		where = append(where, "(instr([[nm]], {:query}) > 0 OR ([[extract]].[[status]] = 'done' AND instr(lower([[extract]].[[text]]), {:query}) > 0))")
		params["query"] = filter.Query
	}
	order := "[[updated]] DESC, [[block_id]] DESC"
	if filter.Sort == workspaceDocumentSortName {
		order = "[[nm]] ASC, [[block_id]] ASC"
		if after.BlockID != "" {
			where = append(where, "([[nm]] > {:afterName} OR ([[nm]] = {:afterName} AND [[block_id]] > {:afterBlock}))")
		}
	} else if after.BlockID != "" {
		where = append(where, "([[updated]] < {:afterUpdated} OR ([[updated]] = {:afterUpdated} AND [[block_id]] < {:afterBlock}))")
	}
	params["afterName"], params["afterUpdated"], params["afterBlock"] = after.Name, after.Updated, after.BlockID
	rows := []storedWorkspaceDocument{}
	err := app.DB().NewQuery(`
		WITH [[candidate]] AS (
			SELECT [[block]].[[id]] AS [[block_id]], [[block]].[[page_id]] AS [[page_id]], [[block]].[[content]] AS [[content]],
				[[block]].[[created_by]] AS [[created_by]], [[block]].[[updated]] AS [[updated]],
				COALESCE(json_extract([[block]].[[content]], '$.fileId'), '') AS [[file_id]],
				lower(COALESCE(json_extract([[block]].[[content]], '$.name'), '')) AS [[nm]],
				lower(trim(COALESCE(json_extract([[block]].[[content]], '$.contentType'), ''))) AS [[ct]]
				, '' AS [[property_id]]
			FROM {{leafen_blocks}} [[block]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[block]].[[page_id]]
			WHERE [[block]].[[workspace_id]] = {:workspaceId} AND [[block]].[[type]] IN ('file', 'pdf')
				AND json_valid([[block]].[[content]])
				AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[in_trash]] = FALSE
				AND [[page]].[[database_template]] = FALSE AND [[page]].[[deletion_job_id]] = ''
				AND [[page]].[[notion_import_staging]] = FALSE
			UNION ALL
			-- A database page's file properties hold documents too, each named
			-- by its page, property and place in the value.
			SELECT [[value]].[[row_id]] || ':' || [[value]].[[property_id]] || ':' || [[attachment]].[[key]] AS [[block_id]],
				[[value]].[[row_id]] AS [[page_id]], [[attachment]].[[value]] AS [[content]],
				[[row_record]].[[created_by]] AS [[created_by]], [[value]].[[updated]] AS [[updated]],
				COALESCE(json_extract([[attachment]].[[value]], '$.fileId'), '') AS [[file_id]],
				lower(COALESCE(json_extract([[attachment]].[[value]], '$.name'), '')) AS [[nm]],
				lower(trim(COALESCE(json_extract([[attachment]].[[value]], '$.contentType'), ''))) AS [[ct]],
				[[value]].[[property_id]] AS [[property_id]]
			FROM {{leafen_database_row_values}} [[value]] INDEXED BY [[idx_leafen_row_values_workspace_files]]
			JOIN json_each(CASE WHEN json_valid([[value]].[[value]]) THEN [[value]].[[value]] ELSE '[]' END) AS [[attachment]]
			JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]] = [[value]].[[row_id]]
			WHERE [[value]].[[workspace_id]] = {:workspaceId} AND [[value]].[[type]] = 'files'
				AND json_type([[attachment]].[[value]]) = 'object'
				AND [[row_record]].[[workspace]] = {:workspaceId} AND [[row_record]].[[in_trash]] = FALSE
				AND [[row_record]].[[database_template]] = FALSE AND [[row_record]].[[deletion_job_id]] = ''
				AND [[row_record]].[[notion_import_staging]] = FALSE
				AND ` + databaseRowNotTrashedSQL("[[value]].[[row_id]]") + `
				AND ` + databasePropertyNotTrashedSQL("[[value]].[[property_id]]") + `
		), [[typed]] AS (
			SELECT *, ` + workspaceDocumentKindSQL + ` AS [[kind]] FROM [[candidate]] WHERE [[file_id]] != ''
		)
		SELECT [[typed]].[[block_id]], [[typed]].[[page_id]], [[typed]].[[content]], [[typed]].[[created_by]],
			[[typed]].[[updated]], [[typed]].[[nm]], [[typed]].[[kind]], [[typed]].[[property_id]],
			CASE WHEN [[extract]].[[status]] = 'done' THEN substr([[extract]].[[text]], 1, 2000) ELSE '' END AS [[text]],
			COALESCE([[extract]].[[partial]], '') AS [[partial]]
		FROM [[typed]]
		LEFT JOIN {{leafen_file_text_extracts}} [[extract]]
			ON [[extract]].[[file_id]] = [[typed]].[[file_id]]
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + `
		LIMIT {:batch}
	`).Bind(params).All(&rows)
	return rows, err
}

// workspaceDocumentPage is what the list says of a document's page, and
// whether the person may change the page, for each page asked about once.
type workspaceDocumentPage struct {
	Readable, Editable    bool
	Title, Icon, IconType string
	ContainerPageID       string
}

func readWorkspaceDocumentPage(app core.App, principalID, workspaceID, pageID string) (workspaceDocumentPage, error) {
	pageWorkspaceID, rowPage, err := pageVisitWorkspace(app, principalID, pageID)
	if isPermissionDenied(err) || (err == nil && pageWorkspaceID != workspaceID) {
		return workspaceDocumentPage{}, nil
	}
	if err != nil {
		return workspaceDocumentPage{}, err
	}
	result := workspaceDocumentPage{Readable: true}
	// A page under a page in the trash is in the trash, and a row's page
	// with its database's page.
	readThrough := pageID
	if rowPage {
		authority, err := loadDatabaseRowPageAuthority(app, pageID)
		if err != nil {
			return workspaceDocumentPage{}, err
		}
		readThrough = authority.ContainerPageID
		result.Title, result.Icon, result.IconType = databaseRowPageTitle(app, pageID), authority.Icon, authority.IconType
		result.ContainerPageID = authority.ContainerPageID
	} else {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return workspaceDocumentPage{}, err
		}
		result.Title, result.Icon, result.IconType = page.GetString("title"), page.GetString("icon"), page.GetString("icon_type")
	}
	trashed, err := loadPageTrashEffects(app, []string{readThrough})
	if err != nil {
		return workspaceDocumentPage{}, err
	}
	if _, inTrash := trashed[readThrough]; inTrash {
		return workspaceDocumentPage{}, nil
	}
	if _, err := authorizePageBody(app, principalID, pageID, permissionWorkspaceWrite); err == nil {
		result.Editable = true
	} else if !isPermissionDenied(err) {
		return workspaceDocumentPage{}, err
	}
	return result, nil
}

func registerWorkspaceDocumentRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceDocumentsListPath, listWorkspaceDocumentsRoute)
	registerWorkspaceDocumentCreateRoute(event)
}

func workspaceDocumentsFailure(event *core.RequestEvent, err error) error {
	event.App.Logger().Error("workspace documents failed", "error", err)
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceDocumentsFailed, "Couldn't read the documents.", err)
}

func listWorkspaceDocumentsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	values := event.Request.URL.Query()
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	filter := workspaceDocumentsFilter{
		Query: strings.ToLower(strings.TrimSpace(values.Get("q"))), Sort: strings.TrimSpace(values.Get("sort")),
	}
	if filter.Sort == "" {
		filter.Sort = workspaceDocumentSortRecent
	}
	limit := workspaceDocumentsDefaultLimit
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > workspaceDocumentsMaxLimit {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceDocumentsInvalid, "Check how many documents to read.")
		}
		limit = parsed
	}
	// Each kind once, in the order kinds come in, so one choice has one key.
	for _, kind := range []string{workspaceDocumentWord, workspaceDocumentSheet, workspaceDocumentSlides, workspaceDocumentPDF, workspaceDocumentHWP, workspaceDocumentHWPX} {
		for _, asked := range values["kind"] {
			if asked == kind {
				filter.Kinds = append(filter.Kinds, kind)
				break
			}
		}
	}
	validKind := len(filter.Kinds) == len(values["kind"])
	if workspaceID == "" || !validKind || utf8.RuneCountInString(filter.Query) > workspaceDocumentsQueryRunes ||
		(filter.Sort != workspaceDocumentSortRecent && filter.Sort != workspaceDocumentSortName) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceDocumentsInvalid, "Check the documents asked for.")
	}
	after := workspaceDocumentsCursor{}
	if raw := values.Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(raw) > workspaceDocumentsCursorBytes || json.Unmarshal(decoded, &after) != nil ||
			after.PrincipalID != user.Id || after.WorkspaceID != workspaceID || after.Filter != filter.key() || after.BlockID == "" {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceDocumentsInvalid, "Check the documents' position.")
		}
	}
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return workspaceDocumentsFailure(event, err)
	}

	result := workspaceDocumentsWire{Documents: []workspaceDocumentWire{}, Actors: []workspaceDocumentActorWire{}}
	pages := map[string]workspaceDocumentPage{}
	creators := []string{}
	scanned, finished := 0, false
	for len(result.Documents) < limit && scanned < workspaceDocumentsScanBudget && !finished {
		batch, err := scanWorkspaceDocuments(event.App, workspaceID, filter, after)
		if err != nil {
			return workspaceDocumentsFailure(event, err)
		}
		finished = len(batch) < workspaceDocumentsScanBatch
		for index, stored := range batch {
			scanned++
			after = workspaceDocumentsCursor{Updated: stored.Updated, Name: stored.NameKey, BlockID: stored.BlockID}
			page, known := pages[stored.PageID]
			if !known {
				if page, err = readWorkspaceDocumentPage(event.App, user.Id, workspaceID, stored.PageID); err != nil {
					return workspaceDocumentsFailure(event, err)
				}
				pages[stored.PageID] = page
			}
			content, parseErr := parseGraphFileBlockContent(json.RawMessage(stored.Content))
			if !page.Readable || parseErr != nil || content.FileID == "" {
				continue
			}
			// A file property's document is the database page's to change, not an app's.
			result.Documents = append(result.Documents, workspaceDocumentWire{
				BlockID: stored.BlockID, PageID: stored.PageID, FileID: content.FileID, Name: content.Name,
				ContentType: content.ContentType, Size: content.Size, Version: mcpFileBaseVersion(content), Kind: stored.Kind,
				Preview: workspaceDocumentPreview(stored.Text), CanEdit: page.Editable && stored.PropertyID == "", CreatedBy: stored.CreatedBy,
				UpdatedAt: time.UnixMilli(stored.Updated).UTC().Format(time.RFC3339Nano),
				PageTitle: page.Title, PageIcon: page.Icon, PageIconType: page.IconType, ContainerPageID: page.ContainerPageID,
				PropertyID: stored.PropertyID, Partial: stored.Partial,
			})
			creators = append(creators, stored.CreatedBy)
			if len(result.Documents) == limit {
				// The blocks after this one are read again from here.
				finished = finished && index == len(batch)-1
				break
			}
		}
	}
	if !finished {
		after.PrincipalID, after.WorkspaceID, after.Filter = user.Id, workspaceID, filter.key()
		encoded, err := json.Marshal(after)
		if err != nil {
			return workspaceDocumentsFailure(event, err)
		}
		result.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	profiles, err := pageHistoryActorProfiles(event.App, creators)
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	for _, profile := range profiles {
		result.Actors = append(result.Actors, workspaceDocumentActorWire{UserID: profile.UserID, Name: profile.DisplayName, Avatar: profile.Avatar})
	}
	return event.JSON(http.StatusOK, result)
}

// workspaceDocumentPreview is the start of a document's words on one line,
// without what a reader made of bytes that weren't words — none at all when
// those are a quarter of it, as from a font the reader couldn't decode.
func workspaceDocumentPreview(text string) string {
	unreadable, read := 0, 0
	words := strings.Fields(strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			unreadable++
			return -1
		}
		if !unicode.IsSpace(r) {
			read++
		}
		return r
	}, text))
	if unreadable*3 > read {
		return ""
	}
	preview := strings.Join(words, " ")
	if utf8.RuneCountInString(preview) <= workspaceDocumentsPreviewRunes {
		return preview
	}
	return string([]rune(preview)[:workspaceDocumentsPreviewRunes])
}
