package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A published page shows a workspace emoji in its text as its picture, as
// Notion's public pages do. A visitor has no session to look the emoji up
// with, so while projecting the page's text the server seals, for each
// workspace emoji the page shows, a token naming the link, the emoji and its
// picture's file, for an hour. The picture is served for the token only while
// the link still shows the workspace and the emoji still has that picture,
// under the outbound rules a public page's files go by. An emoji the page
// doesn't show gets no token, so no other picture is reachable. The titles a
// read shows — its page's own, the pages it lists, a page link's target's —
// show their emoji so too, as a member's screen does.

const (
	publicCustomEmojiSpanKey          = "customEmojiPublic"
	publicCustomEmojiTokenFormat      = 1
	publicCustomEmojiTokenTTL         = time.Hour
	publicCustomEmojiTokenMinChars    = 64
	publicCustomEmojiTokenMaxChars    = 1024
	publicCustomEmojiProjectionTarget = 512
)

type publicCustomEmojiToken struct {
	FormatVersion int    `json:"formatVersion"`
	Source        string `json:"source"`
	AuthorityID   string `json:"authorityId"`
	WorkspaceID   string `json:"workspaceId"`
	EmojiID       string `json:"emojiId"`
	FileID        string `json:"fileId"`
	ExpiresAt     int64  `json:"expiresAt"`
}

func registerPublicCustomEmojiRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/public-content/{source}/{key}/emoji/{token}", servePublicCustomEmoji)
}

func publicCustomEmojiCipherKey(secret string) string {
	digest := sha256.Sum256([]byte("leafen-public-custom-emoji-v1\x00" + secret))
	return string(digest[:])
}

// projectPublicCustomEmoji gives each workspace emoji span of the projected
// items the token its picture is served for: the spans of their text and of
// the names they show (mapPublicNameSpans). The emoji are read in groups of
// the projection's bound; the items already hold a bounded window.
func projectPublicCustomEmoji(
	app core.App,
	source string,
	authorization publicVisibilityAuthorization,
	items []graphReadItem,
	now time.Time,
) ([]graphReadItem, error) {
	ids := []string{}
	seen := map[string]bool{}
	collect := func(span map[string]any) (map[string]any, bool) {
		if id := publicCustomEmojiSpanID(span); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		return nil, false
	}
	for _, item := range items {
		mapPublicItemSpans(item, collect)
		mapPublicNameSpans(item, collect)
	}
	if len(ids) == 0 {
		return items, nil
	}
	tokens, err := sealPublicCustomEmojiTokens(app, source, authorization, ids, now)
	if err != nil {
		return nil, err
	}
	tokened := func(span map[string]any) (map[string]any, bool) {
		token := tokens[publicCustomEmojiSpanID(span)]
		if token == "" {
			return nil, false
		}
		next := make(map[string]any, len(span)+1)
		for key, current := range span {
			next[key] = current
		}
		next[publicCustomEmojiSpanKey] = token
		return next, true
	}
	projected := append([]graphReadItem(nil), items...)
	for index := range projected {
		if value, changed := mapPublicItemSpans(projected[index], tokened); changed {
			projected[index].Value = value
		}
		if value, changed := mapPublicNameSpans(projected[index], tokened); changed {
			projected[index].Value = value
		}
	}
	return projected, nil
}

// publicNameSpanFields are the spans of the names a public page's graph
// shows beside its text: a page link's target's title
// (public_content_page_links.go), a database block's name for its page and
// a linked database's own, a database's name and description.
var publicNameSpanFields = map[string][]string{
	graphReadKindBlock:      {publicPageLinkTitleSpansKey, "pageTitleSpans", "linkedTitleSpans"},
	graphReadKindDataSource: {"titleSpans", "descriptionSpans"},
}

// mapPublicNameSpans applies mapSpan to the spans of the names an item
// shows (publicNameSpanFields), and returns a copy of the item's value
// holding the changed spans.
func mapPublicNameSpans(item graphReadItem, mapSpan func(map[string]any) (map[string]any, bool)) (map[string]any, bool) {
	holder := item.Value
	if item.Kind == graphReadKindBlock {
		content, ok := item.Value["content"].(map[string]any)
		if !ok {
			return nil, false
		}
		holder = content
	}
	var next map[string]any
	for _, field := range publicNameSpanFields[item.Kind] {
		spans, changed := mapPublicSpans(publicSpanList(holder[field]), mapSpan)
		if !changed {
			continue
		}
		if next == nil {
			next = maps.Clone(holder)
		}
		next[field] = spans
	}
	if next == nil {
		return nil, false
	}
	if item.Kind != graphReadKindBlock {
		return next, true
	}
	value := maps.Clone(item.Value)
	value["content"] = next
	return value, true
}

// publicTitleCustomEmoji is, for the workspace emoji the titles a public
// read shows hold (its page's, the pages it lists), the token each one's
// picture is served for, by the emoji's ID: the titles' spans are the
// page's own, which carry no token, and a visitor's screen draws a title's
// emoji by them (publicContentDocument.ts), as a member's does.
func publicTitleCustomEmoji(
	app core.App,
	source string,
	authorization publicVisibilityAuthorization,
	now time.Time,
	titles ...[]graphTextSpan,
) (map[string]string, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, spans := range titles {
		for _, span := range spans {
			if span.Mention == "custom_emoji" && validCustomEmojiID(span.CustomEmojiID) && !seen[span.CustomEmojiID] {
				seen[span.CustomEmojiID] = true
				ids = append(ids, span.CustomEmojiID)
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	tokens, err := sealPublicCustomEmojiTokens(app, source, authorization, ids, now)
	if err != nil || len(tokens) == 0 {
		return nil, err
	}
	return tokens, nil
}

// sealPublicCustomEmojiTokens seals, for each of the emoji the workspace
// has, the token its picture is served for through the link, read in groups
// of the projection's bound.
func sealPublicCustomEmojiTokens(
	app core.App,
	source string,
	authorization publicVisibilityAuthorization,
	ids []string,
	now time.Time,
) (map[string]string, error) {
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return nil, err
	}
	tokens := make(map[string]string, len(ids))
	for start := 0; start < len(ids); start += publicCustomEmojiProjectionTarget {
		group := ids[start:min(len(ids), start+publicCustomEmojiProjectionTarget)]
		values := make([]any, len(group))
		for index, id := range group {
			values[index] = id
		}
		rows := []struct {
			ID     string `db:"id"`
			FileID string `db:"file_id"`
		}{}
		if err := app.DB().Select("id", "file_id").From(customEmojisTable).
			Where(dbx.HashExp{"workspace_id": authorization.WorkspaceID, "workspace_import_job_id": ""}).
			AndWhere(dbx.In("id", values...)).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			token, err := sealPublicCustomEmojiToken(secret, publicCustomEmojiToken{
				FormatVersion: publicCustomEmojiTokenFormat, Source: source, AuthorityID: authorization.AuthorityID,
				WorkspaceID: authorization.WorkspaceID, EmojiID: row.ID, FileID: row.FileID,
				ExpiresAt: now.Add(publicCustomEmojiTokenTTL).UnixMilli(),
			})
			if err != nil {
				return nil, err
			}
			tokens[row.ID] = token
		}
	}
	return tokens, nil
}

func publicCustomEmojiSpanID(span map[string]any) string {
	if span["mention"] != "custom_emoji" {
		return ""
	}
	id, _ := span["customEmojiId"].(string)
	if !validCustomEmojiID(id) {
		return ""
	}
	return id
}

func sealPublicCustomEmojiToken(secret string, payload publicCustomEmojiToken) (string, error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	cipher, err := security.Encrypt(plain, publicCustomEmojiCipherKey(secret))
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) < publicCustomEmojiTokenMinChars || len(token) > publicCustomEmojiTokenMaxChars {
		return "", errors.New(errorCodePublicContentFailed)
	}
	return token, nil
}

func openPublicCustomEmojiToken(secret, token string, now time.Time) (publicCustomEmojiToken, bool) {
	if len(token) < publicCustomEmojiTokenMinChars || len(token) > publicCustomEmojiTokenMaxChars {
		return publicCustomEmojiToken{}, false
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return publicCustomEmojiToken{}, false
	}
	plain, err := security.Decrypt(string(cipher), publicCustomEmojiCipherKey(secret))
	if err != nil {
		return publicCustomEmojiToken{}, false
	}
	payload := publicCustomEmojiToken{}
	if json.Unmarshal(plain, &payload) != nil || payload.FormatVersion != publicCustomEmojiTokenFormat ||
		!validCustomEmojiID(payload.EmojiID) || !workspaceExportFileRecordIDPattern.MatchString(payload.FileID) ||
		payload.AuthorityID == "" || payload.WorkspaceID == "" || now.UnixMilli() >= payload.ExpiresAt {
		return publicCustomEmojiToken{}, false
	}
	return payload, true
}

func servePublicCustomEmoji(event *core.RequestEvent) error {
	source := event.Request.PathValue("source")
	key := strings.TrimSpace(event.Request.PathValue("key"))
	if key == "" || len(key) > maxPublicContentTokenCharacters {
		return publicContentFileNotFound(event)
	}
	now := time.Now().UTC()
	secret, err := fileDownloadCapabilitySecret(event.App)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.emoji", "secret", errorCodePublicContentFailed, err)
	}
	token, ok := openPublicCustomEmojiToken(secret, event.Request.PathValue("token"), now)
	if !ok || token.Source != source {
		return publicContentFileNotFound(event)
	}
	authorization, err := authorizePublicContentLink(event.App, source, key, now)
	if errors.Is(err, errPublicVisibilityNotFound) {
		return publicContentFileNotFound(event)
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.emoji", "authorize", errorCodePublicContentFailed, err)
	}
	if authorization.AuthorityID != token.AuthorityID || authorization.WorkspaceID != token.WorkspaceID {
		return publicContentFileNotFound(event)
	}
	var kept int
	if err := event.App.DB().Select("COUNT(*)").From(customEmojisTable).Where(dbx.HashExp{
		"id": token.EmojiID, "workspace_id": token.WorkspaceID, "file_id": token.FileID, "workspace_import_job_id": "",
	}).Row(&kept); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.emoji", "read", errorCodePublicContentFailed, err)
	}
	if kept != 1 {
		return publicContentFileNotFound(event)
	}
	dlpActions := []string{dlpOutboundPublicSharing, dlpOutboundFileDownloads}
	if source == publicContentSourceView {
		dlpActions = []string{dlpOutboundFileDownloads}
	}
	if err := servePublicProductFile(event, authorization.Workspace, token.FileID, nil, dlpActions, "private, max-age=300"); err != nil ||
		event.Status() >= http.StatusBadRequest {
		return err
	}
	recordContentReads(event.App, authorization.WorkspaceID, contentReader{Via: contentReadViaShare, Client: source + ":" + authorization.AuthorityID},
		[]contentRead{{Kind: "file", ID: token.FileID, How: contentReadOpen}})
	return nil
}
