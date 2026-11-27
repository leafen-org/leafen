package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A block copy or page duplication may take a database without its content:
// its properties, views and settings come along but its rows (with their
// page bodies and dependencies) do not. The choice rides in the job payload.

func durableJobOptionsPayload(withoutContent bool) string {
	if withoutContent {
		return `{"withoutContent":true}`
	}
	return "{}"
}

// durableJobWithoutContent reports whether the job copies databases without
// their rows.
func durableJobWithoutContent(app core.App, jobID string) (bool, error) {
	raw := ""
	err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	payload := struct {
		WithoutContent bool `json:"withoutContent"`
	}{}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.WithoutContent, nil
}

// pageDuplicationPayload is a page duplication's job payload: whether it
// leaves database rows behind and the placeholder its copy's block replaces.
func pageDuplicationPayload(withoutContent bool, placeholderID, teamspaceID string, private, quietMentions bool) string {
	payload := map[string]any{}
	if quietMentions {
		payload["quietMentions"] = true
	}
	if private {
		payload["toPrivatePages"] = true
	}
	if withoutContent {
		payload["withoutContent"] = true
	}
	if placeholderID != "" {
		payload["copyPlaceholderId"] = placeholderID
	}
	if teamspaceID != "" {
		payload["destinationTeamspaceId"] = teamspaceID
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

// pageDuplicationQuietMentions says whether the copy's mentions tell nobody,
// as a connection's copy doesn't in Notion; one made in the app does (seen
// 2026-11-05).
func pageDuplicationQuietMentions(app core.App, jobID string) (bool, error) {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw); err != nil {
		return false, err
	}
	payload := struct {
		Quiet bool `json:"quietMentions"`
	}{}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.Quiet, nil
}

// pageDuplicationPlaceholder is the block a page duplication's copy takes
// the place of, if any.
func pageDuplicationPlaceholder(app core.App, jobID string) (string, error) {
	raw := ""
	err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	payload := struct {
		Placeholder string `json:"copyPlaceholderId"`
	}{}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.Placeholder, nil
}

// databaseCopyKindCopied reports whether a copy that leaves rows behind still
// takes entities of this kind.
func databaseCopyKindCopied(kind string, withoutContent bool) bool {
	return !withoutContent || kind != "row" && kind != "dependency_edge"
}
