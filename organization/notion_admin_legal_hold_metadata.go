package organization

import (
	"context"
	"errors"
	"sort"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldMetadata struct {
	Version         int
	StartDate       float64
	EndDate         *float64
	InteractionMask int
	Description     *string
	Icon            *string
	TokenID         string
}

type notionAdminLegalHoldNativeOverride struct {
	Description *string
	Icon        *string
}

func notionAdminLegalHoldNativeOverrideFromStored(
	stored storedLegalHold,
) (notionAdminLegalHoldNativeOverride, error) {
	if stored.NotionAdminVersion != 0 {
		if stored.NotionAdminNativeDescription.Valid || stored.NotionAdminNativeIcon.Valid {
			return notionAdminLegalHoldNativeOverride{}, leafen.ErrLegalHoldUnavailable
		}
		return notionAdminLegalHoldNativeOverride{}, nil
	}
	override := notionAdminLegalHoldNativeOverride{}
	if stored.NotionAdminNativeDescription.Valid {
		value := stored.NotionAdminNativeDescription.String
		override.Description = &value
	}
	if stored.NotionAdminNativeIcon.Valid {
		value := stored.NotionAdminNativeIcon.String
		override.Icon = &value
	}
	return override, nil
}

func notionAdminLegalHoldMetadataFromStored(
	stored storedLegalHold,
) (notionAdminLegalHoldMetadata, bool, error) {
	if stored.NotionAdminVersion == 0 {
		if stored.NotionAdminStartDate != 0 || stored.NotionAdminEndDate.Valid ||
			stored.NotionAdminInteractionMask != 0 || stored.NotionAdminDescription.Valid ||
			stored.NotionAdminIcon.Valid || stored.NotionAdminTokenID != "" {
			return notionAdminLegalHoldMetadata{}, false, leafen.ErrLegalHoldUnavailable
		}
		return notionAdminLegalHoldMetadata{}, false, nil
	}
	if stored.NotionAdminVersion != leafen.NotionAdminLegalHoldMetadataVersion ||
		stored.NotionAdminInteractionMask < 1 || stored.NotionAdminInteractionMask > 7 ||
		stored.NotionAdminTokenID == "" ||
		stored.NotionAdminEndDate.Valid &&
			stored.NotionAdminEndDate.Float64 < stored.NotionAdminStartDate {
		return notionAdminLegalHoldMetadata{}, false, leafen.ErrLegalHoldUnavailable
	}
	metadata := notionAdminLegalHoldMetadata{
		Version: stored.NotionAdminVersion, StartDate: stored.NotionAdminStartDate,
		InteractionMask: stored.NotionAdminInteractionMask,
		TokenID:         stored.NotionAdminTokenID,
	}
	if stored.NotionAdminEndDate.Valid {
		value := stored.NotionAdminEndDate.Float64
		metadata.EndDate = &value
	}
	if stored.NotionAdminDescription.Valid {
		value := stored.NotionAdminDescription.String
		metadata.Description = &value
	}
	if stored.NotionAdminIcon.Valid {
		value := stored.NotionAdminIcon.String
		metadata.Icon = &value
	}
	return metadata, true, nil
}

func notionAdminLegalHoldInteractionMask(values []string) (int, bool) {
	mask := 0
	for _, value := range values {
		switch value {
		case "page.created":
			mask |= 1
		case "page.edited":
			mask |= 2
		case "page.viewed":
			mask |= 4
		default:
			return 0, false
		}
	}
	return mask, mask != 0
}

func notionAdminLegalHoldInteractions(mask int) ([]string, error) {
	if mask < 1 || mask > 7 {
		return nil, leafen.ErrLegalHoldUnavailable
	}
	values := make([]string, 0, 3)
	if mask&1 != 0 {
		values = append(values, "page.created")
	}
	if mask&2 != 0 {
		values = append(values, "page.edited")
	}
	if mask&4 != 0 {
		values = append(values, "page.viewed")
	}
	return values, nil
}

func loadNotionAdminWorkspaceIDs(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
) ([]string, error) {
	params := dbx.Params{"organizationId": identity.OrganizationID}
	query := `SELECT [[id]] FROM {{leafen_workspaces}}
		WHERE [[organization]] = {:organizationId}`
	allowed := make([]string, 0, len(identity.WorkspaceIDs()))
	if _, wildcard := identity.WorkspaceIDs()["*"]; !wildcard {
		for id := range identity.WorkspaceIDs() {
			allowed = append(allowed, id)
		}
		sort.Strings(allowed)
		if len(allowed) == 0 {
			return []string{}, nil
		}
		query += ` AND [[id]] IN (` + strings.Join(
			bindLegalHoldIDs(params, "workspace", allowed), ", ",
		) + `)`
	}
	query += ` ORDER BY [[id]]`
	rows := []struct {
		ID string `db:"id"`
	}{}
	if err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		publicID, err := leafen.NotionCompatibleWorkspaceID(row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, publicID)
	}
	return result, nil
}

func notionAdminIdentityHasOnlyWildcard(values map[string]struct{}) bool {
	if len(values) != 1 {
		return false
	}
	_, wildcard := values["*"]
	return wildcard
}
