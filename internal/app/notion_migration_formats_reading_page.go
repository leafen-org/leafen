package app

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Leafen's page the settings reading starts from, so that it shows live in
// Leafen's import window whatever address Leafen has. A Notion tab reaches
// no other server by its scripts, and an image from an http address — or
// from this computer's or the home network's own — is refused or held for
// the person's leave (seen 2026-11-23: img-src took https alone, and an
// image, a frame or a sound from 127.0.0.1 never left the tab). A Notion tab
// this page opened can still tell this page how far it is, by a message
// between the two windows, which no address rule stops; this page hands it
// on to its own server (notion_migration_formats_progress.go). The agent
// opens the page and presses its one button; the person does nothing more.
// The page is opened by the reader key's hash, as the progress is told. It
// also hands that tab the reader, without its key, when the agent's start
// line there asks for it (notion_migration_formats_reader_start.go).

const notionMigrationFormatsReadingPath = notionMigrationFormatsPath + "/reading"

// notionMigrationFormatsReadingWords are the page's words: what it does, its
// button, how far the reading is, and what it says once the key has ended.
var notionMigrationFormatsReadingWords = [2][4]string{
	{"Reading how your Notion looks, for Leafen. Notion opens from here; the reading runs in that tab and shows live in Leafen’s import window.",
		"Open Notion", "Read so far: %d, left: %d", "This reading has ended. Ask your agent for a new one."},
	{"리펜이 Notion의 꾸밈을 읽습니다. 여기서 Notion을 열고, 그 탭에서 읽는 동안 진행 상황이 리펜 가져오기 창에 실시간으로 보입니다.",
		"Notion 열기", "읽음 %d · 남음 %d", "이 읽기는 끝났습니다. 에이전트에게 새로 받으세요."},
}

func notionMigrationFormatsReadingRoute(event *core.RequestEvent) error {
	hash := strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("h")))
	found := struct {
		Owner   string `db:"owner"`
		Roots   string `db:"roots_json"`
		Expires int64  `db:"expires"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[migration.owner]],[[migration.roots_json]],
			COALESCE(json_extract([[reader.value]], '$.expires'), 0) AS [[expires]]
		FROM {{leafen_notion_migration_formats}} AS [[reader]]
		JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration.id]]=[[reader.migration_id]]
		WHERE [[reader.kind]]='reader' AND [[reader.notion_id]]='' AND json_extract([[reader.value]], '$.keyHash')={:hash}`).
		Bind(dbx.Params{"hash": hash}).One(&found)
	live := err == nil && len(hash) == 64 && found.Expires > time.Now().UnixMilli()
	language := notionMigrationFormatsLanguage(event.App, found.Owner, event.Request)
	words := notionMigrationFormatsReadingWords[language]
	// The tab opens on the move's first page, where the person is signed in.
	notion := "https://app.notion.com/"
	roots := []notionImportRoot{}
	if json.Unmarshal([]byte(found.Roots), &roots) == nil && len(roots) > 0 {
		notion += "p/" + notionMigrationSourceKey(roots[0].ID)
	}
	nonce := security.RandomString(24)
	header := event.Response.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	// Leafen's pages cut a window they open off from them (same-origin); this
	// one keeps the Notion tab it opens, or that tab could tell it nothing.
	header.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
	header.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; "+
		"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	encode := func(value any) string {
		encoded, _ := json.Marshal(value)
		return html.EscapeString(string(encoded))
	}
	body := `<p>` + html.EscapeString(words[3]) + `</p>`
	if live {
		// The reader goes as data, not run here: its key comes with the start line.
		reader, _ := json.Marshal(notionMigrationFormatsPageReader(roots, time.UnixMilli(found.Expires)))
		body = `<p>` + html.EscapeString(words[0]) + `</p><p><button id="open" type="button">` + html.EscapeString(words[1]) +
			`</button></p><p id="read" role="status"></p><script type="application/json" id="reader">` + string(reader) + `</script>` +
			`<script nonce="` + nonce + `" data-notion="` + encode(notion) + `" data-progress="` + encode(notionMigrationFormatsProgressPath) +
			`" data-said="` + encode(words[2]) + `">` + notionMigrationFormatsReadingScript + `</script>`
	}
	lang := [2]string{"en", "ko"}[language]
	return event.HTML(http.StatusOK, `<!doctype html><html lang="`+lang+`"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>Leafen</title>`+
		`<style nonce="`+nonce+`">body{margin:64px auto;max-width:420px;padding:0 16px;font:14px/1.5 -apple-system,system-ui,sans-serif;color:#37352f;background:#fff}`+
		`button{font:inherit;padding:6px 14px;border-radius:6px;border:1px solid #d3d1cb;background:#2383e2;color:#fff;cursor:pointer}`+
		`@media (prefers-color-scheme:dark){body{color:#d4d4d4;background:#191919}}</style></head><body>`+body+`</body></html>`)
}

// notionMigrationFormatsReadingScript opens Notion from the page's button,
// hands that tab the reader when its start line asks — with where it hands
// in, this page's own server — and hands on what the reader in that tab
// tells it — the same address an image would carry — to this server, from
// Notion's own addresses alone, answering so that the reader knows the page
// that opened it is this one.
const notionMigrationFormatsReadingScript = `(() => {
  const script = document.currentScript, notion = JSON.parse(script.dataset.notion), progress = JSON.parse(script.dataset.progress);
  const said = JSON.parse(script.dataset.said), read = document.getElementById("read");
  const endpoint = JSON.stringify(location.origin + location.pathname.replace(/\/reading$/, ""));
  const reader = JSON.parse(document.getElementById("reader").textContent).split("__ENDPOINT__").join(endpoint);
  let tab = null;
  document.getElementById("open").addEventListener("click", () => { tab = window.open(notion, "leafen-notion-reading"); });
  window.addEventListener("message", (event) => {
    if (!tab || event.source !== tab || !/^https:\/\/([a-z0-9-]+\.)*(notion\.so|notion\.com)$/.test(event.origin)) return;
    if (event.data && event.data.leafenReaderPlease) return tab.postMessage({ leafenReader: reader }, event.origin);
    const query = event.data && event.data.leafenReading;
    if (typeof query !== "string" || query.length > 4000) return;
    tab.postMessage({ leafenHeard: true }, event.origin); // The reader goes on by message, not by image.
    fetch(progress + "?" + query, { credentials: "omit", cache: "no-store" }).catch(() => {});
    const counts = new URLSearchParams(query), done = ["p", "d", "b"].reduce((sum, name) => sum + Number(counts.get(name) || 0), 0);
    read.textContent = said.replace("%d", String(done)).replace("%d", String(Number(counts.get("l") || 0)));
  });
})();`
