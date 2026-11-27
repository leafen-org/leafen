package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	notionAdminPATDefaultPageSize = 100
	notionAdminPATMaximumPageSize = 100
	notionAdminPATCursorVersion   = 1
	notionAdminPATCursorPrefix    = "hnapat1"
	notionAdminPATCursorDomain    = "leafen:notion-admin-personal-access-token-cursor:v1"
	notionAdminPATCursorMaxLength = 2048
	maxNotionAdminPATFilterValues = notionAdminTokenMaxResourceIDs
	maxNotionAdminPATSearchRunes  = notionAdminTokenMaxResourceIDRunes
)

var errInvalidNotionAdminPATCursor = notionAdminInvalid(errors.New("invalid Notion Admin personal access token cursor"), "start_cursor isn't one this list gave.")

type notionAdminPATFilters struct {
	Statuses   []string `json:"s"`
	CreatorIDs []string `json:"c"`
	Search     string   `json:"q"`
}

type notionAdminPATCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	WorkspaceID    string `json:"w"`
	Revision       int64  `json:"r"`
	AsOf           int64  `json:"n"`
	AfterID        string `json:"a"`
	FiltersHash    string `json:"f"`
}

func notionAdminPATListQuery(request *http.Request) (int, string, notionAdminPATFilters, error) {
	query := request.URL.Query()
	allowed := map[string]bool{
		"cursor": true, "page_size": true, "status": true, "status[]": true,
		"creator_ids": true, "creator_ids[]": true, "search": true,
	}
	for key := range query {
		if !allowed[key] {
			return 0, "", notionAdminPATFilters{}, errInvalidNotionAdminPATCursor
		}
	}
	for _, scalar := range []string{"cursor", "page_size", "search"} {
		if values, exists := query[scalar]; exists && len(values) != 1 {
			return 0, "", notionAdminPATFilters{}, errInvalidNotionAdminPATCursor
		}
	}
	pageSize := notionAdminPATDefaultPageSize
	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminPATMaximumPageSize {
			return 0, "", notionAdminPATFilters{}, errInvalidNotionAdminPATCursor
		}
		pageSize = parsed
	}
	statuses, err := notionAdminPATQueryValues(query["status"], query["status[]"], true)
	if err != nil {
		return 0, "", notionAdminPATFilters{}, err
	}
	for _, status := range statuses {
		if status != "active" && status != "expired" && status != "revoked" {
			return 0, "", notionAdminPATFilters{}, errInvalidNotionAdminPATCursor
		}
	}
	publicCreators, err := notionAdminPATQueryValues(
		query["creator_ids"], query["creator_ids[]"], false,
	)
	if err != nil {
		return 0, "", notionAdminPATFilters{}, err
	}
	creatorIDs := make([]string, 0, len(publicCreators))
	for _, publicID := range publicCreators {
		internalID, err := resolveNotionCompatibleUserID(publicID)
		if err != nil {
			return 0, "", notionAdminPATFilters{}, refusedUnlessStorage(errInvalidNotionAdminPATCursor, err)
		}
		creatorIDs = append(creatorIDs, internalID)
	}
	search := strings.TrimSpace(query.Get("search"))
	if utf8.RuneCountInString(search) > maxNotionAdminPATSearchRunes {
		return 0, "", notionAdminPATFilters{}, errInvalidNotionAdminPATCursor
	}
	return pageSize, query.Get("cursor"), notionAdminPATFilters{
		Statuses: statuses, CreatorIDs: creatorIDs, Search: search,
	}, nil
}

func notionAdminPATQueryValues(primary, bracketed []string, normalizeStatus bool) ([]string, error) {
	if len(primary) > 1 || len(primary) > 0 && len(bracketed) > 0 ||
		len(primary)+len(bracketed) > maxNotionAdminPATFilterValues {
		return nil, errInvalidNotionAdminPATCursor
	}
	values := append(append([]string(nil), primary...), bracketed...)
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if normalizeStatus {
			value = strings.ToLower(value)
		}
		if value == "" {
			return nil, errInvalidNotionAdminPATCursor
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func notionAdminPATFiltersHash(filters notionAdminPATFilters) (string, error) {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func encodeNotionAdminPATCursor(
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	revision, asOf int64,
	afterID string,
	filters notionAdminPATFilters,
) (string, error) {
	filterHash, err := notionAdminPATFiltersHash(filters)
	if err != nil {
		return "", err
	}
	return encodeNotionAdminLegalHoldScopeCursor(
		secret, notionAdminPATCursorPrefix, notionAdminPATCursorDomain,
		notionAdminPATCursorMaxLength, notionAdminPATCursor{
			Version: notionAdminPATCursorVersion, OrganizationID: identity.OrganizationID,
			TokenID: identity.TokenID, WorkspaceID: workspaceID, Revision: revision,
			AsOf: asOf, AfterID: afterID, FiltersHash: filterHash,
		},
	)
}

func decodeNotionAdminPATCursor(
	raw, secret string,
	identity notionAdminIdentity,
	workspaceID string,
	filters notionAdminPATFilters,
) (notionAdminPATCursor, error) {
	if raw == "" {
		return notionAdminPATCursor{}, nil
	}
	filterHash, err := notionAdminPATFiltersHash(filters)
	if err != nil {
		return notionAdminPATCursor{}, errInvalidNotionAdminPATCursor
	}
	cursor := notionAdminPATCursor{}
	if err := decodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionAdminPATCursorPrefix, notionAdminPATCursorDomain,
		notionAdminPATCursorMaxLength, &cursor,
	); err != nil || cursor.Version != notionAdminPATCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.WorkspaceID != workspaceID || cursor.Revision < 1 || cursor.AsOf < 1 ||
		cursor.FiltersHash != filterHash || !validNotionCompatibleInternalID(cursor.AfterID) {
		return notionAdminPATCursor{}, errInvalidNotionAdminPATCursor
	}
	return cursor, nil
}
