package app

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a token can't reach is said naming what Notion's API names (seen
// 2026-11-05), in Leafen's words: which object, its ID and where to look, so
// someone connecting an app knows to share the page with it.

var notionPathObjectKinds = map[string]string{
	"pages": "page", "blocks": "block", "databases": "database", "data_sources": "data_source",
	"file_uploads": "file_upload", "users": "user", "comments": "comment",
	"views": "view", "async_tasks": "async_task",
}

type notionAskedObject struct{ Kind, ID string }

const notionAskedObjectKey = "notionAskedObject"

// noteNotionAskedObject names what a request asks for when its address doesn't,
// as a new page's parent.
func noteNotionAskedObject(event *core.RequestEvent, kind, id string) {
	event.Set(notionAskedObjectKey, notionAskedObject{Kind: kind, ID: strings.TrimSpace(id)})
}

func notionObjectAsked(event *core.RequestEvent) notionAskedObject {
	parts := strings.Split(strings.Trim(event.Request.URL.Path, "/"), "/")
	// A view's query is what its address asks for, named as a body's ID is
	// (seen 2026-11-12: a view_query it finds none of, whatever view its
	// address names).
	if len(parts) == 5 && parts[1] == "views" && parts[3] == "queries" {
		return notionAskedObject{Kind: "view_query", ID: notionBodyIDShown(parts[4])}
	}
	if len(parts) >= 3 && notionPathObjectKinds[parts[1]] != "" {
		return notionAskedObject{Kind: notionPathObjectKinds[parts[1]], ID: notionDashedUUID(parts[2])}
	}
	asked, _ := event.Get(notionAskedObjectKey).(notionAskedObject)
	return asked
}

// notionTokenBot is the name and ID of the connection or token asking.
func notionTokenBot(event *core.RequestEvent) (string, string) {
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil || !ok {
		return "", ""
	}
	var bot map[string]any
	if identity.ClientID == "personal-access-token" {
		bot, err = notionPersonalAccessTokenBotUser(event.App, identity.GrantID, map[string]any{})
	} else {
		bot, err = notionOAuthBotUser(event.App, identity.GrantID, map[string]any{})
	}
	if err != nil {
		return "", ""
	}
	name, _ := bot["name"].(string)
	id, _ := bot["id"].(string)
	return name, id
}

// notionDashedUUID is an ID of 32 hexadecimal digits with its dashes, as
// Notion names an object in its address or a new page's parent that it
// can't find (seen 2026-11-10), letters as written; any other ID as it is.
func notionDashedUUID(id string) string {
	if len(id) != 32 || strings.Trim(id, "0123456789abcdefABCDEF") != "" {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

// An ID in the address shaped as neither a UUID nor Leafen's own is refused
// as Notion refuses it, not as an object it can't find (seen 2026-11-07: a
// path that isn't valid, path.page_id not being a valid uuid, for every
// object but an async task, which it looks for) —
// a UUID as its bodies take one (seen 2026-11-12: 32 capital digits are
// refused so, dashed capitals are an object it can't find).
var (
	leafenOwnID = regexp.MustCompile(`^[a-z0-9]{15}$`)
	// notionBodyUUID is a UUID as Notion's request bodies take one (seen
	// 2026-11-11 on page, user and custom emoji mentions, page and block
	// icons and a new page's parent): dashed, letters either case, or 32
	// lowercase hexadecimal digits; undashed capitals and stray dashes are
	// refused as no UUID.
	notionBodyUUID = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-f]{32})$`)
)

func notionPathIDRefused(event *core.RequestEvent, asked notionAskedObject) (string, bool) {
	parts := strings.Split(strings.Trim(event.Request.URL.Path, "/"), "/")
	if len(parts) < 3 || notionPathObjectKinds[parts[1]] != asked.Kind || notionDashedUUID(parts[2]) != asked.ID ||
		asked.Kind == "async_task" {
		return "", false
	}
	return notionPathIDShapeRefused(asked.Kind, parts[2])
}

// notionPathIDShapeRefused is Notion's refusal of an ID in the address
// named name ("view", "query") that is no UUID nor Leafen's own.
func notionPathIDShapeRefused(name, id string) (string, bool) {
	if notionBodyUUID.MatchString(id) || leafenOwnID.MatchString(id) {
		return "", false
	}
	quoted, _ := json.Marshal(id)
	return notionFaultSaid("path", notionFaultLine("path."+name+"_id", []string{"a valid uuid"}, string(quoted))), true
}

// notionNotFoundSaid is a not-found answer's status, code, message and data,
// as Notion's.
func notionNotFoundSaid(event *core.RequestEvent, status int, code, message string, data map[string]any) (int, string, string, map[string]any) {
	if status != http.StatusNotFound || code != notionErrorObjectNotFound ||
		!strings.HasSuffix(message, "with the requested ID.") && !strings.HasSuffix(message, "in the connected workspace.") {
		return status, code, message, data
	}
	asked := notionObjectAsked(event)
	if asked.Kind == "" || asked.ID == "" {
		return status, code, message, data
	}
	if refusal, refused := notionPathIDRefused(event, asked); refused {
		return http.StatusBadRequest, notionErrorValidation, refusal, nil
	}
	name, botID := notionTokenBot(event)
	found := "There's no " + asked.Kind + " with ID: " + asked.ID + " that this integration can reach."
	switch asked.Kind {
	case "user":
		message = found + " Reading people takes its capability to read user information, " +
			"and even then it sees only the members and guests of its own workspace."
	case "file_upload":
		message = found + " Check that the upload's status is `uploaded` and that the integration may update or insert content."
	default:
		integration := "the integration"
		if name != "" {
			integration += " \"" + name + "\""
		}
		message = found + " If it exists, share the page or database it is in with " + integration + "."
	}
	if botID != "" && data == nil {
		data = map[string]any{"integration_id": botID}
	}
	return status, code, message, data
}
