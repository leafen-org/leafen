package app

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Links Leafen hands an agent for the person to open — a screen, the
// migration's top page and report, an app's screen — go by where the person
// sees Leafen (person_window.go; user 2026-11-19, the first day's same cause:
// in the desktop app a http://127.0.0.1 address opens a browser that isn't
// signed in, while the app's window is). In the app such a link is a leafen:
// link to the app's own address, which the app takes and opens in its
// signed-in window, the window coming to the front (the scheme the packaged
// app registers; web/src/lib/packagedLinks.ts reads leafen://open?url=);
// elsewhere it is the address itself. An address an agent's own tools read
// (a page's url in a tool's answer) stays as it is; the inbox's note tells
// the app's agent how to give one to the person (personLinksNote).

// appWindowLink is the leafen: link that opens path on the desktop app's own
// server in its window, or "" where this isn't the app's server.
func appWindowLink(app core.App, path string) string {
	owner := localOwnerOf(app)
	if owner == nil {
		return ""
	}
	return "leafen://open?url=" + url.QueryEscape(owner.Origin()+path)
}

// personLinkAt is path on this server as the person opens it: in the app, its
// window; else the address the agent reached Leafen at.
func personLinkAt(app core.App, authority mcpAuthority, path string) string {
	if personWindowOf(app, authority.owner()) == personWindowApp {
		if link := appWindowLink(app, path); link != "" {
			return link
		}
	}
	address, err := mcpServerURL(authority.Resource, path)
	if err != nil {
		return path
	}
	return address
}

// personPublicLink is path as the person opens it where a link must reach
// them anywhere: in the app, its window; else at the server's public
// address, or none without one.
func personPublicLink(app core.App, person, path string) string {
	if personWindowOf(app, person) == personWindowApp {
		if link := appWindowLink(app, path); link != "" {
			return link
		}
	}
	if origin := configuredPublicOrigin(); origin != "" {
		return origin + path
	}
	return ""
}

// personLinksNote tells the desktop app's agent how to give its person a
// link to open; "" elsewhere.
func personLinksNote(app core.App, person string) string {
	if personWindowOf(app, person) != personWindowApp {
		return ""
	}
	return " Leafen here is the app on the person's computer: a link to a Leafen page you give them to open goes as " +
		"leafen://open?url=<the page's url, URL-encoded> — it opens in the app's signed-in window, where a http://127.0.0.1 address " +
		"opens a browser that isn't logged in. Your own tools keep the plain url."
}

// A guide's Leafen screen for the person, written `<Leafen's address>/path`.
var guideLeafenScreen = regexp.MustCompile("`<Leafen's address>(/[^`]*)`")

// withPersonLinks writes a guide's Leafen screens as the person opens them
// (personLinkAt); an API address, which another service is given, stays as
// written.
func withPersonLinks(ctx context.Context, app core.App, guide string) string {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return guide
	}
	return guideLeafenScreen.ReplaceAllStringFunc(guide, func(found string) string {
		path := guideLeafenScreen.FindStringSubmatch(found)[1]
		if strings.HasPrefix(path, apiRoot+"/") || strings.HasPrefix(path, "/api/") {
			return found
		}
		return "`" + personLinkAt(app, authority, path) + "`"
	})
}

// A link Leafen gives a person to a screen of one workspace — a migration's
// import screen, its pages and report, its notices — names that workspace,
// so the app opens it there whichever workspace it shows (2026-11-22: the
// import screen's link opened the person's other workspace, without the
// run; web/src/lib/workspaceAddress.ts), and so the person sees which by the
// link alone: ?workspace= its domain, or else its name, then its ID, as
// Notion's page address puts the page's title before its ID
// (app.notion.com/p/Page-title-<id>, seen 2026-11-22).

// workspaceLinkLabelRunes is how much of a name a link carries.
const workspaceLinkLabelRunes = 40

// workspacePath is path with the workspace it is of, before any fragment.
func workspacePath(app core.App, workspaceID, path string) string {
	if workspaceID == "" {
		return path
	}
	fragment := ""
	if at := strings.IndexByte(path, '#'); at >= 0 {
		path, fragment = path[:at], path[at:]
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "workspace=" + workspaceLinkKey(app, workspaceID) + fragment
}

// workspaceLinkKey is a workspace as a link names it: its domain or else
// its name, a hyphen, and its ID; its ID alone with neither.
func workspaceLinkKey(app core.App, workspaceID string) string {
	var row struct {
		Name   string `db:"name"`
		Domain string `db:"domain"`
	}
	if err := app.DB().NewQuery(`SELECT [[name]], [[domain]] FROM {{` + workspacesCollection + `}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": workspaceID}).One(&row); err != nil {
		return url.QueryEscape(workspaceID)
	}
	label := strings.TrimSpace(row.Domain)
	if label == "" {
		label = workspaceLinkLabel(row.Name)
	}
	if label == "" {
		return url.QueryEscape(workspaceID)
	}
	return label + "-" + url.QueryEscape(workspaceID)
}

// workspaceLinkLabel is a name as a link carries it: its letters and digits,
// each run of anything else one hyphen, at most workspaceLinkLabelRunes —
// none of what an address escapes, so it reads as it is.
func workspaceLinkLabel(name string) string {
	var label strings.Builder
	runes := 0
	for _, character := range name {
		if runes == workspaceLinkLabelRunes {
			break
		}
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			label.WriteRune(character)
			runes++
			continue
		}
		if label.Len() > 0 && !strings.HasSuffix(label.String(), "-") {
			label.WriteByte('-')
			runes++
		}
	}
	return strings.Trim(label.String(), "-")
}
