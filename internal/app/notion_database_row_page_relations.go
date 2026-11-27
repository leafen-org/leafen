package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const maxNotionDatabaseRowPageRelations = 100

func notionDatabaseRowPageRelationTarget(
	property storedNotionDatabaseRowPageProperty,
) (string, error) {
	config := struct {
		DataSourceID string `json:"relationDatabaseId"`
	}{}
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	target := strings.TrimSpace(config.DataSourceID)
	if target == "" {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	return target, nil
}

func projectNotionDatabaseRowPageRelations(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	internalIDs []string,
) ([]map[string]any, error) {
	if len(internalIDs) > maxNotionDatabaseRowPageRelations {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	targetSource, err := notionDatabaseRowPageRelationTarget(property)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(internalIDs))
	result := make([]map[string]any, 0, len(internalIDs))
	for _, internalID := range internalIDs {
		internalID = strings.TrimSpace(internalID)
		if internalID == "" {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		if _, duplicate := seen[internalID]; duplicate {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		seen[internalID] = struct{}{}
		// A row the connection can't open, and one in the trash, are left
		// out (database_relation_closed_targets.go), as Notion's API leaves
		// out a trashed page (seen 2026-11-10); a write keeps them.
		target, open, err := notionRelationRowOpen(app, authorization.Identity, nil, internalID)
		if err != nil {
			return nil, err
		}
		if !open || target.Authority.TrashedAt != 0 {
			continue
		}
		if target.Authority.DataSourceID != targetSource {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		identities, err := ensureNotionDatabaseRowPagePublicIdentities(app, target)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"id": identities.PageID})
	}
	return result, nil
}

func notionDatabaseRowPageRequestRelations(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	raw json.RawMessage,
) ([]string, error) {
	items := []struct {
		ID string `json:"id"`
	}{}
	if json.Unmarshal(raw, &items) != nil || len(items) > maxNotionDatabaseRowPageRelations {
		return nil, notionDatabaseRowPageRequestInvalid(
			"database property %q relation is invalid", property.Name,
		)
	}
	targetSource, err := notionDatabaseRowPageRelationTarget(property)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		identity, err := resolveNotionObjectIdentity(
			app, notionObjectIdentityRowPage, strings.TrimSpace(item.ID),
		)
		if err != nil || identity.Workspace != authorization.Identity.WorkspaceID {
			return nil, notionRelationPageMissing(item.ID)
		}
		// A page named twice is related once, as Notion relates it.
		if _, duplicate := seen[identity.InternalID]; duplicate {
			continue
		}
		seen[identity.InternalID] = struct{}{}
		// A row the caller can't open — on a page it can't read, or one its
		// views leave out (an unlinked item) — is a page Notion can't find;
		// one in the trash it names.
		target, open, err := notionRelationRowOpen(app, authorization.Identity, nil, identity.InternalID)
		if err != nil {
			return nil, err
		}
		if !open {
			return nil, notionRelationPageMissing(item.ID)
		}
		if target.Authority.DataSourceID != targetSource {
			return nil, notionDatabaseRowPageRequestInvalid(
				"database property %q contains an inaccessible relation", property.Name,
			)
		}
		result = append(result, identity.InternalID)
	}
	return result, nil
}

// notionRelationPageMissing refuses a related page the caller can't open,
// as Notion's API does (seen 2026-11-09: 404 object_not_found, the page
// named and the pages it may be in to share with the integration), naming a
// UUID however it was written in
// its dashed form, as Notion's does (seen 2026-11-10).
func notionRelationPageMissing(id string) error {
	id = strings.TrimSpace(id)
	if dashed, ok := normalizeNotionUUIDRequest(id); ok {
		id = dashed
	}
	return &notionDatabaseRowPageRequestError{
		message:     "There's no page with ID: " + id + " that this integration can reach. If it exists, share the page or database it is in with the integration.",
		missingPage: id,
	}
}

// notionRelationPageNotFound answers a related page the caller can't open
// as Notion's API does, naming the integration.
func notionRelationPageNotFound(event *core.RequestEvent, id string) error {
	return notionNamedObjectNotFound(event, "page", id)
}

// notionNamedObjectNotFound answers a page or block a request names that
// the caller can't reach as Notion's API does, naming it and the integration.
func notionNamedObjectNotFound(event *core.RequestEvent, kind, id string) error {
	name, botID := notionTokenBot(event)
	integration := "the integration"
	if name != "" {
		integration += " \"" + name + "\""
	}
	var data map[string]any
	if botID != "" {
		data = map[string]any{"integration_id": botID}
	}
	return notionCompatibilityErrorWithData(event, http.StatusNotFound, notionErrorObjectNotFound,
		"There's no "+kind+" with ID: "+id+" that this integration can reach. If it exists, share the page or database it is in with "+integration+".", data, nil)
}
