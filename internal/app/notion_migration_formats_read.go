package app

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// What Notion's API doesn't give a connection — each page's, row's and
// template's full width, small text and font; each database's page layout
// and the title its block keeps apart from its sources' names;
// each view's groups as set by hand (their order, which are hidden, whether
// empty ones hide, a board's column colors), its wrap, the order people put
// its rows in, its own default template and its conditions on properties
// people deleted; each form's title, description, questions (a checkbox's
// label among them) and who may answer; and which blocks have comments, with
// how many of their discussions are resolved — Leafen reads
// in the person's own signed-in Notion and matches inside the migration's run
// (decision 2026-11-16, "사람의 로그인된 브라우저에서 리펜이 읽어 실행 안에서
// 맞춘다"). The agent gets a short reader (notion_migration_formats_reader.js)
// with a key for this migration and runs it in a Notion tab of the person's
// with its browser tool. The reader reads settings only, never page text,
// with Notion's own web API, and hands them to Leafen itself as a form sent
// from that tab — Notion's page lets a form go anywhere but a script's
// request only to Notion — so what it read never passes through the agent.
// Leafen keeps them by Notion id (notion_migration_formats_schema.go), and
// the run matches them as one of its steps (notion_migration_formats_apply.go);
// once the run is done, a reading matches at once. What it couldn't read or
// match is listed item by item (notion_migration_formats_left.go).

//go:embed notion_migration_formats_reader.js
var notionMigrationFormatsReaderSource string

//go:embed notion_migration_formats_reader_hand_in.js
var notionMigrationFormatsReaderHandIn string

const (
	notionMigrationFormatsPath = apiRoot + "/notion-migration-formats"
	// One reading is taken up to this size and this many pages, databases,
	// views and items it couldn't read; past either it is refused, saying so
	// in the person's tab and to the agent (notion_migration_formats_refused.go).
	notionMigrationFormatsMaxBytes  = 16 << 20
	notionMigrationFormatsMostItems = 200000
)

// notionMigrationFormatsRead is what the reader hands in, by Notion id: each
// page's and row's [full width, small text, font], each database's page
// layout, each view's settings, and what it couldn't read.
type notionMigrationFormatsRead struct {
	Format    int                                      `json:"format"`
	Pages     map[string][]any                         `json:"pages"`
	Databases map[string]notionMigrationDatabaseFormat `json:"databases"`
	Views     map[string]notionMigrationViewFormat     `json:"views"`
	Unread    []notionMigrationFormatUnread            `json:"unread"`
	// The bodies read whole and their blocks with comments
	// (notion_migration_comment_discussions.go).
	Discussions *notionMigrationDiscussions `json:"discussions"`
}

type notionMigrationPageFormat struct {
	FullWidth bool   `json:"fullWidth"`
	SmallText bool   `json:"smallText"`
	Font      string `json:"font"`
	// Matched is set once the run gave it to what it made of the page.
	Matched bool `json:"matched,omitzero"`
}

// A database's page layout: its properties in order (Notion's property ids)
// and each one's visibility (show, hide or hide_if_empty); and the title its
// block keeps of its own, "" for none (nil: a reader before it read none,
// notion_migration_formats_database_title.go).
type notionMigrationDatabaseFormat struct {
	Order      []string          `json:"order"`
	Visibility map[string]string `json:"visibility"`
	Title      *string           `json:"title,omitempty"`
	Matched    bool              `json:"matched,omitzero"`
	// Like names a database of the same reading with the same layout, as the
	// reader sends it once (notion_migration_formats_parts.go).
	Like string `json:"like,omitempty"`
}

// A view's groups in order with which are hidden (nil: Notion keeps none
// set by hand), whether empty ones hide and a board's columns are colored,
// whether it wraps (nil: never set), and its rows in the order people put
// them (nil: none).
type notionMigrationViewFormat struct {
	Database string                       `json:"database"`
	Type     string                       `json:"type"`
	Groups   []notionMigrationGroupFormat `json:"groups"`
	// Whether its groups with no rows hide, and a board's columns are colored.
	HideEmpty *bool    `json:"hideEmpty"`
	Colors    *bool    `json:"colors"`
	Wrap      *bool    `json:"wrap"`
	Rows      []string `json:"rows"`
	// DefaultTemplate is the template the view makes its new pages from, when
	// it has one of its own (notion_migration_formats_templates.go).
	DefaultTemplate string `json:"defaultTemplate,omitempty"`
	// Form is a form's title, description and questions
	// (notion_migration_formats_form.go).
	Form *notionMigrationFormFormat `json:"form,omitempty"`
	// DeletedFilters are its filter's conditions on properties people deleted,
	// which Notion's API leaves out (notion_migration_formats_deleted_filters.go).
	DeletedFilters []notionMigrationDeletedFilter `json:"deletedFilters,omitempty"`
	// Left names what the run couldn't match: groups, wrap, rows, or a
	// form's questions.
	Left    []string `json:"left,omitempty"`
	Matched bool     `json:"matched,omitzero"`
	// Unsaved says the run couldn't save the view (someone changed or took
	// it away since, or Leafen refused it): all it read is left.
	Unsaved bool `json:"unsaved,omitzero"`
	// As the reader sends it (notion_migration_formats_parts.go): Like names
	// a view of the same reading with the same settings, Ordered says its
	// rows' order is in the reading's orders; RowsNotTaken, that it had one
	// Leafen didn't take.
	Like         string `json:"like,omitempty"`
	Ordered      bool   `json:"ordered,omitzero"`
	RowsNotTaken bool   `json:"rowsNotTaken,omitzero"`
	// RowsOf names its rows' order as Leafen keeps it, once for the views
	// that have it (takeNotionMigrationFormats).
	RowsOf string `json:"rowsOf,omitempty"`
}

type notionMigrationGroupFormat struct {
	Property string `json:"property"`
	Type     string `json:"type"`
	Value    any    `json:"value"`
	Hidden   bool   `json:"hidden"`
}

type notionMigrationFormatUnread struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Database string `json:"database,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// notionMigrationFormatsReader is the reader's own row: its key
// (notion_migration_formats_key.go) — the hash, the key sealed, when Leafen
// first gave it and when it ends, and once a reading came in with it, its
// hash and when — when a reading came in and when the run matched it.
type notionMigrationFormatsReader struct {
	KeyHash   string                        `json:"keyHash"`
	KeySealed string                        `json:"keySealed,omitempty"`
	GivenAt   int64                         `json:"givenAt,omitzero"`
	Expires   int64                         `json:"expires"`
	UsedHash  string                        `json:"usedHash,omitempty"`
	UsedAt    int64                         `json:"usedAt,omitzero"`
	ReadAt    int64                         `json:"readAt"`
	AppliedAt int64                         `json:"appliedAt"`
	Counts    notionMigrationFormatsCounts  `json:"counts"`
	Unread    []notionMigrationFormatUnread `json:"unread"`
	// Why the last reading sent was refused and when, when it came after the
	// last one kept (notion_migration_formats_refused.go).
	Refused   string `json:"refused,omitempty"`
	RefusedAt int64  `json:"refusedAt,omitzero"`
	// The parts of the last reading sent Leafen didn't take, with their sizes:
	// all of them when it refused it, else those the reading kept without.
	NotTaken []notionMigrationFormatsPart `json:"notTaken,omitempty"`
	// Which blocks of the bodies it read have comments, as read.
	Discussions *notionMigrationDiscussions `json:"discussions,omitempty"`
}

type notionMigrationFormatsCounts struct {
	Pages     int `json:"pages"`
	Databases int `json:"databases"`
	Views     int `json:"views"`
	Unread    int `json:"unread"`
}

func loadNotionMigrationFormatsReader(app core.App, migrationID string) (notionMigrationFormatsReader, bool, error) {
	reader := notionMigrationFormatsReader{}
	var value string
	err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]='reader' AND [[notion_id]]=''`).
		Bind(dbx.Params{"id": migrationID}).Row(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return reader, false, nil
		}
		return reader, false, err
	}
	return reader, true, json.Unmarshal([]byte(value), &reader)
}

func storeNotionMigrationFormat(app core.App, migrationID, workspaceID, kind, notionID string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_formats}} ([[migration_id]],[[workspace_id]],[[kind]],[[notion_id]],[[value]])
		VALUES ({:migrationId},{:workspaceId},{:kind},{:notionId},{:value})
		ON CONFLICT ([[migration_id]],[[kind]],[[notion_id]]) DO UPDATE SET [[value]]=excluded.[[value]]
	`).Bind(dbx.Params{"migrationId": migrationID, "workspaceId": workspaceID, "kind": kind, "notionId": notionID,
		"value": string(encoded)}).Execute()
	return err
}

// notionMigrationFormatsReaderScript gives the reader for a migration with
// its key (giveNotionMigrationFormatsKey: the same one until a reading comes
// in with it), and the reader's row as kept.
func notionMigrationFormatsReaderScript(
	app core.App, stored storedNotionMigration, leafenURL string,
) (string, notionMigrationFormatsKey, notionMigrationFormatsReader, error) {
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil {
		return "", notionMigrationFormatsKey{}, notionMigrationFormatsReader{}, err
	}
	given, reader, err := giveNotionMigrationFormatsKey(app, stored)
	if err != nil {
		return "", given, reader, err
	}
	encode := func(value any) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	// It hands in what it has by the time its key nears its end, in the
	// server's clock.
	return notionMigrationFormatsReaderCode(encode(leafenURL), encode(given.Key), roots, given.Expires), given, reader, nil
}

// notionMigrationFormatsReaderCode is the reader with where it hands in and
// its key as the JavaScript that gives them (a string, or a name its runner
// binds, notion_migration_formats_reader_start.go), the roots it reads from,
// and what it hands in by: its key's end, less a margin, in the server's
// clock.
func notionMigrationFormatsReaderCode(endpoint, key string, roots []notionImportRoot, expires time.Time) string {
	// The agent copies it whole, so it goes without its heading comment.
	code := notionMigrationFormatsReaderSource
	if start := strings.Index(code, "(async () => {"); start >= 0 {
		code = code[start:]
	}
	handIn := notionMigrationFormatsReaderHandIn
	if start := strings.Index(handIn, "  const handIn"); start >= 0 {
		handIn = handIn[start:]
	}
	code = strings.Replace(code, "  /* __HAND_IN__ */\n", handIn, 1)
	encode := func(value any) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	until := expires.Add(-notionMigrationFormatsKeyMargin).UnixMilli()
	return strings.NewReplacer(
		"__ENDPOINT__", endpoint,
		"__KEY__", key,
		"__ROOTS__", encode(roots),
		"__UNTIL__", encode(until),
	).Replace(code)
}

// The route reads its own body (no body limit before it): a reading too big
// is answered in the person's tab, not with the server's bare 413. A call
// is refused before it (notionMigrationFormatsCallsRefused).
func registerNotionMigrationFormatsRoutes(event *core.ServeEvent) {
	event.Router.POST(notionMigrationFormatsPath, receiveNotionMigrationFormatsRoute).Unbind(apis.DefaultBodyLimitMiddlewareId).BindFunc(notionMigrationFormatsCallsRefused)
	event.Router.GET(notionMigrationFormatsProgressPath, notionMigrationFormatsProgressRoute)
	event.Router.GET(notionMigrationFormatsReadingPath, notionMigrationFormatsReadingRoute)
}

// receiveNotionMigrationFormatsRoute takes a reading the reader sends from
// the person's Notion tab, by its key alone (the tab carries no Leafen
// sign-in), keeps it, matches it at once when the run is done, and sends the
// tab back to the Notion page it came from. A reading past Leafen's size or
// count is refused with why, in the tab and for the agent. Either way the
// key has taken its one reading (notion_migration_formats_key.go).
func receiveNotionMigrationFormatsRoute(event *core.RequestEvent) error {
	language := notionMigrationFormatsLanguage(event.App, "", event.Request)
	form, cut, err := readNotionMigrationFormatsForm(event.Request)
	if err != nil {
		return notionMigrationFormatsAnswer(event, http.StatusBadRequest, "", language, notionMigrationFormatsUnreadableWords)
	}
	key := strings.TrimSpace(form.Get("key"))
	back := notionMigrationFormatsBack(form.Get("back"))
	read, notTaken, err := notionMigrationFormatsParts(form, cut)
	tooBig := errors.Is(err, errNotionMigrationFormatsPartTooBig)
	if key == "" || err != nil && !tooBig {
		return notionMigrationFormatsAnswer(event, http.StatusBadRequest, back, language, notionMigrationFormatsUnreadableWords)
	}
	hash := notionMigrationFormatsKeyHash(key)
	found := struct {
		MigrationID string `db:"migration_id"`
		WorkspaceID string `db:"workspace_id"`
		Owner       string `db:"owner"`
	}{}
	err = event.App.DB().NewQuery(`SELECT [[reader.migration_id]],[[reader.workspace_id]],COALESCE([[migration.owner]],'') AS [[owner]]
		FROM {{leafen_notion_migration_formats}} AS [[reader]]
		LEFT JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration.id]]=[[reader.migration_id]]
		WHERE [[reader.kind]]='reader' AND [[reader.notion_id]]=''
			AND (json_extract([[reader.value]], '$.keyHash')={:hash} OR json_extract([[reader.value]], '$.usedHash')={:hash})`).
		Bind(dbx.Params{"hash": hash}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return notionMigrationFormatsAnswer(event, http.StatusForbidden, back, language, notionMigrationFormatsEndedWords)
	}
	if err != nil {
		return notionMigrationFormatsFailure(event, back, language, notionMigrationFormatsKeyUnreadableWords, err)
	}
	language = notionMigrationFormatsLanguage(event.App, found.Owner, event.Request)
	release := acquireProductOperationLock(notionMigrationLockNamespace, found.MigrationID)
	defer release()
	refused := notionMigrationFormatsRefusal(tooBig, read)
	took, reader, err := takeNotionMigrationFormats(event.App, found.MigrationID, found.WorkspaceID, hash, refused, read, notTaken)
	switch {
	case err != nil:
		return notionMigrationFormatsFailure(event, back, language, notionMigrationFormatsKeepFailedWords, err)
	case took == notionMigrationFormatsKeyUsed:
		at := time.UnixMilli(reader.UsedAt).In(personLocation(event.App, found.Owner)).Format("2006-01-02 15:04")
		words := notionMigrationFormatsUsedWords
		return notionMigrationFormatsAnswer(event, http.StatusConflict, back, language,
			notionMigrationFormatsWords{fmt.Sprintf(words[0], at), fmt.Sprintf(words[1], at)})
	case took != notionMigrationFormatsKeyLive:
		return notionMigrationFormatsAnswer(event, http.StatusForbidden, back, language, notionMigrationFormatsEndedWords)
	case refused != "":
		return notionMigrationFormatsAnswer(event, http.StatusRequestEntityTooLarge, back, language, notionMigrationFormatsRefusedPage(refused, notTaken, read.Discussions != nil))
	}
	if err := applyNotionMigrationFormatsWhenDone(event.App, found.MigrationID); err != nil {
		event.App.Logger().Warn("notion migration formats: matching after the run failed", "error", err)
	}
	return notionMigrationFormatsAnswer(event, http.StatusOK, back, language, notionMigrationFormatsTakenPage(notTaken))
}

// takeNotionMigrationFormats takes a reading handed in with the key whose
// hash is given, while that key is the reader's live one: it keeps the
// reading in place of the earlier one, or keeps why it refused it, and ends
// the key. It says what the key was to the reader's row and gives the row.
func takeNotionMigrationFormats(
	app core.App, migrationID, workspaceID, hash, refused string, read notionMigrationFormatsRead, notTaken []notionMigrationFormatsPart,
) (notionMigrationFormatsKeyState, notionMigrationFormatsReader, error) {
	took, reader := notionMigrationFormatsKeyEnded, notionMigrationFormatsReader{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		if reader, _, err = loadNotionMigrationFormatsReader(txApp, migrationID); err != nil {
			return err
		}
		if took = notionMigrationFormatsKeyStateOf(reader, hash); took != notionMigrationFormatsKeyLive {
			return nil
		}
		endNotionMigrationFormatsKey(&reader)
		reader.NotTaken = notTaken
		if refused != "" {
			reader.Refused, reader.RefusedAt = refused, reader.UsedAt
			// Which blocks have comments, when that came whole (notion_migration_formats_parts.go).
			if read.Discussions != nil {
				reader.Discussions = notionMigrationDiscussionsKept(read.Discussions)
			}
			return storeNotionMigrationFormat(txApp, migrationID, workspaceID, "reader", "", reader)
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_notion_migration_formats}}
			WHERE [[migration_id]]={:id} AND [[kind]]!='reader'`).Bind(dbx.Params{"id": migrationID}).Execute(); err != nil {
			return err
		}
		for id, values := range read.Pages {
			page := notionMigrationPageFormat{}
			if len(values) == 3 {
				page.FullWidth, _ = values[0].(bool)
				page.SmallText, _ = values[1].(bool)
				page.Font, _ = values[2].(string)
			}
			if !validPageFont(page.Font) {
				page.Font = pageFontDefault
			}
			if err := storeNotionMigrationFormat(txApp, migrationID, workspaceID, "page", canonicalNotionID(id), page); err != nil {
				return err
			}
		}
		for id, database := range read.Databases {
			if err := storeNotionMigrationFormat(txApp, migrationID, workspaceID, "database", canonicalNotionID(id), database); err != nil {
				return err
			}
		}
		orders := map[string]bool{}
		for id, view := range read.Views {
			view.Database = canonicalNotionID(view.Database)
			view.DeletedFilters = notionMigrationDeletedFiltersKept(view.DeletedFilters)
			// Each rows' order once, the views naming it (notion_migration_formats_parts.go).
			if view.Rows != nil {
				view.RowsOf = notionMigrationFormatsOrderID(view.Rows)
				if !orders[view.RowsOf] {
					orders[view.RowsOf] = true
					if err := storeNotionMigrationFormat(txApp, migrationID, workspaceID, "order", view.RowsOf, view.Rows); err != nil {
						return err
					}
				}
				view.Rows = nil
			}
			if err := storeNotionMigrationFormat(txApp, migrationID, workspaceID, "view", canonicalNotionID(id), view); err != nil {
				return err
			}
		}
		// Every item it couldn't read is kept and counted: a reading holds
		// at most notionMigrationFormatsMostItems in all.
		reader.ReadAt, reader.Unread, reader.Discussions = reader.UsedAt, read.Unread, notionMigrationDiscussionsKept(read.Discussions)
		reader.Refused, reader.RefusedAt = "", 0
		reader.Counts = notionMigrationFormatsCounts{
			Pages: len(read.Pages), Databases: len(read.Databases), Views: len(read.Views), Unread: len(read.Unread),
		}
		return storeNotionMigrationFormat(txApp, migrationID, workspaceID, "reader", "", reader)
	})
	return took, reader, err
}

type leafenMCPNotionMigrationFormatsOutput struct {
	Reader string `json:"reader"`
	// A line that runs `reader` in the Notion tab reader_page opened, taking
	// it from that page (notion_migration_formats_reader_start.go).
	ReaderStart string `json:"reader_start"`
	// As the reading starts, its line for the person with the address of
	// their live view (notion_migration_stage_lines.go).
	StageLine string `json:"stage_line,omitempty"`
	// When Leafen first gave this reader: it gives the same one again until
	// a reading comes in with it (notion_migration_formats_key.go).
	ReaderGivenAt string                        `json:"reader_given_at"`
	Read          *notionMigrationFormatsCounts `json:"read,omitempty"`
	ReadAt        string                        `json:"read_at,omitempty"`
	// Why Leafen refused the last reading sent, when it did.
	Refused string `json:"refused,omitempty"`
	// Where the person watches the reading live (notion_migration_wait_next.go),
	// and Leafen's page the reading starts from (notion_migration_formats_reading_page.go).
	Link       string `json:"link,omitempty"`
	ReaderPage string `json:"reader_page,omitempty"`
	Next       string `json:"next"`
}

func registerMCPNotionMigrationFormatsTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_formats", Title: "Read what Notion's API doesn't give",
		Description: "Gives the reader that reads in the person's logged-in Notion what Notion's API doesn't give a connection — " +
			"each page's, row's and template's full width, small text and font, each database's page layout, " +
			"each view's groups as set by hand, its wrap, the order people put its rows in, its default template and its conditions on properties people deleted (Notion's API leaves those out of the view's filter), " +
			"and each form's title, description, questions and who may answer — " +
			"and hands it straight to Leafen, which matches it in the migration's run (at once when the run is done already, writing its report again). " +
			"`reader_page` is Leafen's page for it: opened in a tab of your own, its button opens Notion where the person is logged in. In that Notion tab, `reader_start` — a line of a few hundred characters, to run with your browser tool's JavaScript — " +
			"takes the reader from Leafen's page and runs it with its key, answering once it has handed in; `reader` is the same reader whole (about 27,000 characters, with its comments), for a person to read, or to run in a Notion tab opened otherwise. " +
			"It tells Leafen how far it is as it reads; the tab goes to Leafen and back. It reads settings, and which blocks have comments (and how many of their discussions are resolved) so the run asks only those, never page text, and what it reads doesn't pass through you. " +
			"Then this tool's `read` says what Leafen got. Until a reading comes in, it gives the same `reader` and `reader_start` and doesn't stop the one reading.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationScanInput) (*mcp.CallToolResult, leafenMCPNotionMigrationFormatsOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationFormatsOutput{}, err
		}
		stored, err := loadNotionMigration(app, authority.owner(), workspace.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationFormatsOutput{}, mcpInvalid(mcpNotionMigrationIDAsked)
		}
		if stored.SourceLabel != "" {
			return nil, leafenMCPNotionMigrationFormatsOutput{}, mcpInvalid("this migration moves another tool's bundle, which keeps how its pages look: there is nothing to read in Notion")
		}
		endpoint, err := mcpServerURL(authority.Resource, notionMigrationFormatsPath)
		if err != nil {
			return nil, leafenMCPNotionMigrationFormatsOutput{}, failedBecause(errMCPToolFailed, err)
		}
		script, given, reader, err := notionMigrationFormatsReaderScript(app, stored, endpoint)
		if err != nil {
			return nil, leafenMCPNotionMigrationFormatsOutput{}, failedBecause(errMCPToolFailed, err)
		}
		output := notionMigrationFormatsToolOutput(script, given, reader, personLocation(app, authority.owner()))
		output.ReaderStart = notionMigrationFormatsReaderStart(given.Key)
		output.Link, output.ReaderPage = notionMigrationLiveLink(app, authority, workspace.WorkspaceID), endpoint+"/reading?h="+notionMigrationFormatsKeyHash(given.Key)
		language, _ := serverTextLanguage(app, authority.owner(), nil)
		if strings.Contains(output.Next, notionMigrationLiveWords) {
			output.StageLine = notionMigrationStageLineOf(language, notionMigrationStageFormats, output.Link, 0)
		}
		output.Next = notionMigrationNamesLink(notionMigrationWithStageLine(output.Next, language, notionMigrationStageFormats), output.Link)
		return nil, output, nil
	})
}

// notionMigrationFormatsScanNext is what a scanned Notion migration's agent
// does with the settings reading the run matches: explained in the one
// question, run after its yes, before the run (2026-11-20, the user: an agent
// opened Notion for it with no word of why — notionMigrationReadingAsk).
func notionMigrationFormatsScanNext(source string) string {
	if source != "" {
		return ""
	}
	return " With a browser to run it in, the one question also explains the settings reading — " + notionMigrationReadingAsk +
		" — and on its yes, before the run, have Leafen read them with leafen_notion_migration_formats, so the run matches them."
}

// notionMigrationReadingAsk is what the one question says of the settings
// reading, in plain words, before the agent opens Notion for it (decision
// change 3's additions, 2026-11-20 05:0x and 05:1x).
const notionMigrationReadingAsk = "that you'll open Notion in your browser to read how their pages and databases look (full width, font, page layouts, " +
	"board groups, rows put in order by hand, forms) and which blocks have comments, so Leafen sets them as in Notion; that it reads no page text; " +
	"that without it they come in as Leafen's defaults, listed in the report, and the comments take far longer, since Notion gives comments block " +
	"by block and Leafen then asks about every block (about three hours more for 30,000 blocks) instead of only those with open comments; " +
	"their yes to the question covers it"
