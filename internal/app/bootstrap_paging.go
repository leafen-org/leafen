package app

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

// The start-up read's rows and its paging: the workspaces and pages it reads,
// how many at a time, and the cursor that carries a person's place between
// windows.

type bootstrapCursor struct {
	PrincipalID      string `json:"principalId"`
	WorkspacesDone   bool   `json:"workspacesDone"`
	WorkspaceCreated string `json:"workspaceCreated,omitempty"`
	WorkspaceID      string `json:"workspaceId,omitempty"`
	PagesDone        bool   `json:"pagesDone"`
	// The page windows go workspace by workspace and part by part, a
	// teamspace or the person's private pages (bootstrap_page_access.go).
	PageWorkspaceCreated string `json:"pageWorkspaceCreated,omitempty"`
	PageWorkspace        string `json:"pageWorkspace,omitempty"`
	PageTeamspace        string `json:"pageTeamspace,omitempty"`
	PageCreated          string `json:"pageCreated,omitempty"`
	PageID               string `json:"pageId,omitempty"`
	SharedDone           bool   `json:"sharedDone"`
	SharedCreated        string `json:"sharedCreated,omitempty"`
	SharedPageID         string `json:"sharedPageId,omitempty"`
}

type storedBootstrapWorkspace struct {
	ID               string `db:"id"`
	OrganizationID   string `db:"organization"`
	Name             string `db:"name"`
	Icon             string `db:"icon"`
	Domain           string `db:"domain"`
	SettingsRevision int    `db:"settings_revision"`
	OwnerID          string `db:"owner"`
	Role             string `db:"role"`
	Restricted       bool   `db:"restricted"`
	// NoPrivatePages is a restricted member kept from making private pages
	// (organization_request_controls.go).
	NoPrivatePages bool   `db:"no_private_pages"`
	Created        string `db:"created"`
	// MemberCount is the owner and the members, as the workspace menu
	// counts them.
	MemberCount       int    `db:"member_count"`
	LandingPageID     string `db:"landing_page_id"`
	AppInstallID      string `db:"app_install_id"`
	AppSlug           string `db:"app_slug"`
	AnalyticsDisabled bool   `db:"page_analytics_disabled"`
	AnonymousDisabled bool   `db:"page_analytics_anonymous_tracking_disabled"`
}

type storedBootstrapPage struct {
	ID                     string  `db:"id"`
	WorkspaceID            string  `db:"workspace"`
	TeamspaceID            string  `db:"teamspace_id"`
	Title                  string  `db:"title"`
	Content                string  `db:"content"`
	Revision               int     `db:"revision"`
	Cursor                 int     `db:"cursor"`
	ParentID               string  `db:"parent_id"`
	Position               float64 `db:"position"`
	IsLocked               bool    `db:"is_locked"`
	LockedBy               string  `db:"locked_by"`
	InTrash                bool    `db:"in_trash"`
	TrashedAt              string  `db:"trashed_at"`
	ArchivedAt             string  `db:"archived_at"`
	ArchivedBy             string  `db:"archived_by"`
	Icon                   string  `db:"icon"`
	IconType               string  `db:"icon_type"`
	Cover                  string  `db:"cover"`
	CoverPosition          int     `db:"cover_position"`
	Font                   string  `db:"font"`
	SmallText              bool    `db:"small_text"`
	FullWidth              bool    `db:"full_width"`
	BacklinksDisplay       string  `db:"backlinks_display"`
	PageCommentsDisplay    string  `db:"page_comments_display"`
	TableOfContentsDisplay string  `db:"table_of_contents_display"`
	InlineCommentsDisplay  string  `db:"inline_comments_display"`
	Kind                   string  `db:"kind"`
	CreatedBy              string  `db:"created_by"`
	LastEditedBy           string  `db:"last_edited_by"`
	UpdatedAt              string  `db:"updated"`
	Created                string  `db:"created"`
	Favorite               bool    `db:"favorite"`
	FavoriteRevision       int     `db:"favorite_revision"`
	MovedDatabase          bool    `db:"moved_database"`
	ViewBlock              bool    `db:"view_block"`
	// DatabaseContainerID is a full-page database's own database, which a
	// mention of it names (the "@" menu).
	DatabaseContainerID string `db:"database_container_id"`
	// DatabaseBlockID is a full-page database's database block, linked or
	// not, whose ID its address names (fullPageDatabaseBlockSQL).
	DatabaseBlockID string `db:"database_block_id"`
	HasContent      bool   `db:"has_content"`
	// WikiRevision is a page's own, which a page that was a wiki keeps, so
	// the next change to its wiki state starts from it.
	WikiRevision int `db:"wiki_revision"`
	// A page's verification is its own, in a wiki or not: one verified
	// outside a wiki shows it as one in a wiki does (pageRecordResponse).
	VerifiedAt            string `db:"verified_at"`
	VerifiedBy            string `db:"verified_by"`
	VerificationExpiresAt string `db:"verification_expires_at"`
}

func bootstrapLimit(raw string, maximum int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return maximum, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maximum {
		return 0, permissionDenied()
	}
	return value, nil
}

func decodeBootstrapCursor(raw string, principalID string) (bootstrapCursor, error) {
	cursor := bootstrapCursor{PrincipalID: principalID}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cursor, nil
	}
	if len(raw) > maxBootstrapCursorSize {
		return bootstrapCursor{}, permissionDenied()
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > maxBootstrapCursorSize || json.Unmarshal(data, &cursor) != nil {
		return bootstrapCursor{}, permissionDenied()
	}
	if cursor.PrincipalID != principalID || (cursor.WorkspacesDone && (cursor.WorkspaceCreated != "" || cursor.WorkspaceID != "")) ||
		(cursor.PagesDone && (cursor.PageCreated != "" || cursor.PageID != "")) ||
		(cursor.SharedDone && (cursor.SharedCreated != "" || cursor.SharedPageID != "")) {
		return bootstrapCursor{}, permissionDenied()
	}
	return cursor, nil
}

func encodeBootstrapCursor(cursor bootstrapCursor) (string, error) {
	if cursor.WorkspacesDone {
		cursor.WorkspaceCreated = ""
		cursor.WorkspaceID = ""
	}
	if cursor.PagesDone {
		cursor.PageCreated = ""
		cursor.PageID = ""
	}
	if cursor.SharedDone {
		cursor.SharedCreated = ""
		cursor.SharedPageID = ""
	}
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
