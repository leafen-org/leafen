//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a run changed besides page history, for the person's list of what
// the app did (app_platform_run_undo.go): where they started its action —
// a page's or a row's menu, or the "/" menu — the comments it left, and how
// many of its calls changed something, so a run that changed only what page
// history doesn't keep isn't listed as having changed nothing (2026-10-16:
// a menu's action read "from the app's screen" and its two comments were
// missing).

const appRunCommentPreview = 80

// The places a menu names in an action's input.
var appRunPlaces = map[string]bool{"pageMenu": true, "rowMenu": true, "slash": true}

var appRunCommentPath = regexp.MustCompile(`^/pages/([^/?]+)/(?:comments|suggestions)$`)

// appRunPlace is where the person started an action, as its menu said.
func appRunPlace(run appRun) string {
	if run.Trigger != "action" {
		return ""
	}
	call := struct {
		Input struct {
			Place string `json:"place"`
		} `json:"input"`
	}{}
	if json.Unmarshal(run.Input, &call) != nil || !appRunPlaces[call.Input.Place] {
		return ""
	}
	return call.Input.Place
}

type appRunEffect struct {
	Kind      string `json:"kind"`
	PageID    string `json:"pageId"`
	CommentID string `json:"commentId"`
}

// appRunEffects gathers a run's changes as its calls answer.
type appRunEffects struct {
	writes int
	list   []appRunEffect
}

// note keeps a call that changed something, and a comment it left by name.
func (effects *appRunEffects) note(method, path string, result nativeOperationResult) {
	if strings.EqualFold(method, http.MethodGet) || requestOnlyReads(path) || result.Status < 200 || result.Status > 299 {
		return
	}
	effects.writes++
	match := appRunCommentPath.FindStringSubmatch(path)
	if match == nil || !strings.EqualFold(method, http.MethodPost) {
		return
	}
	body, _ := result.Body.(map[string]any)
	comment, _ := body["comment"].(map[string]any)
	if id, _ := comment["id"].(string); id != "" {
		effects.list = append(effects.list, appRunEffect{Kind: "comment", PageID: match[1], CommentID: id})
	}
}

func (effects *appRunEffects) encoded() string {
	if len(effects.list) == 0 {
		return ""
	}
	raw, _ := json.Marshal(effects.list)
	return string(raw)
}

// appRunComments are a window of the comments a run left that are still
// there, on pages the person may read, from the effect at from on; one they
// wrote themselves they may delete from here. The next is the effect the
// next window starts at, 0 once the last comment is looked at.
func appRunComments(app core.App, userID, encoded string, from int) ([]appRunCommentWire, int) {
	comments := []appRunCommentWire{}
	effects := []appRunEffect{}
	if encoded == "" || json.Unmarshal([]byte(encoded), &effects) != nil {
		return comments, 0
	}
	looked := 0
	for index := from; index < len(effects); index++ {
		effect := effects[index]
		if effect.Kind != "comment" {
			continue
		}
		if looked == appRunHistoryComments {
			return comments, index
		}
		looked++
		row := struct {
			PageID   string `db:"page_id"`
			Author   string `db:"author_id"`
			Body     string `db:"body_json"`
			Revision int    `db:"revision"`
			Title    string `db:"title"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[comment]].[[page_id]], [[comment]].[[author_id]], [[comment]].[[body_json]], [[comment]].[[revision]],
				COALESCE([[page]].[[title]], '') AS [[title]]
			FROM {{leafen_comments}} [[comment]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[comment]].[[page_id]]
			WHERE [[comment]].[[id]] = {:id} AND [[comment]].[[page_id]] = {:page}`).
			Bind(dbx.Params{"id": effect.CommentID, "page": effect.PageID}).One(&row); err != nil {
			continue
		}
		if _, err := authorizePageBody(app, userID, row.PageID, permissionWorkspaceRead); err != nil {
			continue
		}
		comments = append(comments, appRunCommentWire{PageID: row.PageID, CommentID: effect.CommentID, PageTitle: row.Title,
			Text: appRunCommentText(row.Body), Revision: row.Revision, Deletable: row.Author == userID})
	}
	return comments, 0
}

// appRunCommentText is the start of a comment's text: its body's text, or
// its runs' text.
func appRunCommentText(raw string) string {
	var text string
	if json.Unmarshal([]byte(raw), &text) != nil {
		body := struct {
			Rich []struct {
				Text string `json:"text"`
			} `json:"rich"`
		}{}
		_ = json.Unmarshal([]byte(raw), &body)
		var joined strings.Builder
		for _, span := range body.Rich {
			joined.WriteString(span.Text)
		}
		text = joined.String()
	}
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > appRunCommentPreview {
		text = string(runes[:appRunCommentPreview]) + "…"
	}
	return text
}

// appRunChangedRowNames names the first few of the database rows a run
// changed (appRunChangedRowIDs).
func appRunChangedRowNames(app core.App, ids []string) []appRunRowWire {
	rows := []appRunRowWire{}
	for _, id := range ids {
		if len(rows) >= appRunHistoryRowNames {
			break
		}
		database := ""
		_ = app.DB().NewQuery(`SELECT COALESCE([[source]].[[title]], '') FROM {{leafen_database_rows}} [[row]]
			LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] WHERE [[row]].[[id]] = {:row}`).
			Bind(dbx.Params{"row": id}).Row(&database)
		rows = append(rows, appRunRowWire{RowID: id, Title: databaseRowPageTitle(app, id), Database: database})
	}
	return rows
}

// appRunChangedRowIDs are the database rows a run's versions of a page
// changed — each version's history event lists them — in the order it first
// changed them.
func appRunChangedRowIDs(app core.App, pageID string, scope appRunVersionScope) []string {
	payloads, params := []string{}, dbx.Params{"page": pageID}
	if err := app.DB().NewQuery(`SELECT [[event]].[[payload]] FROM {{leafen_page_history_events}} [[event]]
		JOIN {{leafen_page_history_versions}} [[version]] ON [[version]].[[page_id]] = [[event]].[[page_id]] AND [[version]].[[version_cursor]] = [[event]].[[version_cursor]]
		WHERE [[event]].[[page_id]] = {:page} AND ` + scope.where(params) + `
		ORDER BY [[event]].[[version_cursor]]`).
		Bind(params).Column(&payloads); err != nil {
		return nil
	}
	ids, seen := []string{}, map[string]bool{}
	for _, payload := range payloads {
		event := struct {
			Rows []struct {
				ID string `json:"id"`
			} `json:"rows"`
		}{}
		raw, err := unpackPageHistoryText(payload)
		if err != nil || json.Unmarshal([]byte(raw), &event) != nil {
			continue
		}
		for _, changed := range event.Rows {
			if changed.ID != "" && !seen[changed.ID] {
				seen[changed.ID] = true
				ids = append(ids, changed.ID)
			}
		}
	}
	return ids
}
