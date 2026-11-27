package organization

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	errOrganizationDiscoveryInvalid       = errors.New(leafen.ErrorCodeOrganizationDiscoveryInvalid)
	errOrganizationDiscoveryConflict      = errors.New("organization discovery mutation conflicts with its receipt")
	errOrganizationDiscoveryNotFound      = errors.New(leafen.ErrorCodeOrganizationDiscoveryNotFound)
	errOrganizationDiscoveryTooLarge      = errors.New(leafen.ErrorCodeOrganizationDiscoveryTooLarge)
	errOrganizationDiscoverySourceChanged = errors.New(leafen.ErrorCodeOrganizationDiscoverySourceChanged)
	errOrganizationDiscoveryUnavailable   = errors.New(leafen.ErrorCodeOrganizationDiscoveryUnavailable)
)

type storedOrganizationDiscoveryExport struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	OwnerID        string `db:"owner_id"`
	MutationID     string `db:"mutation_id"`
	RequestHash    string `db:"request_hash"`
	Format         string `db:"format"`
	FilterJSON     string `db:"filter_json"`
	Status         string `db:"status"`
	Phase          string `db:"phase"`
	Revision       int    `db:"revision"`
	ItemCount      int    `db:"item_count"`
	ByteCount      int64  `db:"byte_count"`
	ChunkCount     int    `db:"chunk_count"`
	ContentSHA256  string `db:"content_sha256"`
	SourceRevision int    `db:"source_revision"`
	CreatedAt      int64  `db:"created_at"`
	CompletedAt    int64  `db:"completed_at"`
}

type organizationDiscoveryWorkspace struct {
	ID   string `db:"id"`
	Name string `db:"name"`
}

var organizationDiscoveryFold = cases.Fold()

func normalizeOrganizationDiscoveryInput(
	input leafen.OrganizationDiscoveryExportCreateInput,
) (leafen.OrganizationDiscoveryExportCreateInput, leafen.OrganizationDiscoveryFilter, string, error) {
	input.MutationID = strings.TrimSpace(input.MutationID)
	input.Query = strings.TrimSpace(norm.NFKC.String(input.Query))
	input.Format = strings.ToLower(strings.TrimSpace(input.Format))
	if input.Format == "" {
		input.Format = leafen.OrganizationDiscoveryExportFormatJSONL
	}
	if input.MutationID == "" || len([]rune(input.MutationID)) > leafen.OrganizationDiscoveryIdentifierMaxRunes ||
		len([]rune(input.Query)) > leafen.OrganizationDiscoveryQueryMaxRunes ||
		(input.Format != leafen.OrganizationDiscoveryExportFormatJSONL && input.Format != leafen.OrganizationDiscoveryExportFormatJSON) {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", errOrganizationDiscoveryInvalid
	}
	var err error
	input.UserIDs, err = normalizeOrganizationDiscoveryIdentifiers(input.UserIDs, leafen.OrganizationDiscoveryCustodianMaxItems)
	if err != nil {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", err
	}
	sort.Strings(input.UserIDs)
	input.WorkspaceIDs, err = normalizeOrganizationDiscoveryIdentifiers(input.WorkspaceIDs, leafen.OrganizationDiscoveryWorkspaceMaxItems)
	if err != nil {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", err
	}
	sort.Strings(input.WorkspaceIDs)
	input.Since, err = normalizeOrganizationDiscoveryTimestamp(input.Since)
	if err != nil {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", err
	}
	input.Until, err = normalizeOrganizationDiscoveryTimestamp(input.Until)
	if err != nil || (input.Since != "" && input.Until != "" && input.Since > input.Until) {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", errOrganizationDiscoveryInvalid
	}
	includeTrashed := true
	if input.IncludeTrashed != nil {
		includeTrashed = *input.IncludeTrashed
	}
	input.IncludeTrashed = &includeTrashed
	filter := leafen.OrganizationDiscoveryFilter{
		Query: input.Query, UserIDs: input.UserIDs, WorkspaceIDs: input.WorkspaceIDs,
		Since: input.Since, Until: input.Until, IncludeTrashed: includeTrashed,
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return leafen.OrganizationDiscoveryExportCreateInput{}, leafen.OrganizationDiscoveryFilter{}, "", errOrganizationDiscoveryInvalid
	}
	digest := sha256.Sum256(canonical)
	return input, filter, hex.EncodeToString(digest[:]), nil
}

func normalizeOrganizationDiscoveryIdentifiers(values []string, maximum int) ([]string, error) {
	if len(values) > maximum {
		return nil, errOrganizationDiscoveryInvalid
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > leafen.OrganizationDiscoveryIdentifierMaxRunes {
			return nil, errOrganizationDiscoveryInvalid
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeOrganizationDiscoveryTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", errOrganizationDiscoveryInvalid
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func validateOrganizationDiscoveryFilter(filter leafen.OrganizationDiscoveryFilter) error {
	if len([]rune(filter.Query)) > leafen.OrganizationDiscoveryQueryMaxRunes ||
		filter.Query != strings.TrimSpace(norm.NFKC.String(filter.Query)) ||
		!validOrganizationDiscoveryIdentifiers(filter.UserIDs, leafen.OrganizationDiscoveryCustodianMaxItems, true) ||
		!validOrganizationDiscoveryIdentifiers(filter.WorkspaceIDs, leafen.OrganizationDiscoveryWorkspaceMaxItems, true) {
		return errOrganizationDiscoveryUnavailable
	}
	since, sinceErr := normalizeOrganizationDiscoveryTimestamp(filter.Since)
	until, untilErr := normalizeOrganizationDiscoveryTimestamp(filter.Until)
	if sinceErr != nil || untilErr != nil || since != filter.Since || until != filter.Until ||
		(since != "" && until != "" && since > until) {
		return errOrganizationDiscoveryUnavailable
	}
	return nil
}

func validOrganizationDiscoveryIdentifiers(values []string, maximum int, sorted bool) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	previous := ""
	for index, value := range values {
		if value == "" || value != strings.TrimSpace(value) ||
			len([]rune(value)) > leafen.OrganizationDiscoveryIdentifierMaxRunes {
			return false
		}
		if _, duplicate := seen[value]; duplicate || (sorted && index > 0 && value <= previous) {
			return false
		}
		seen[value] = struct{}{}
		previous = value
	}
	return true
}

func assertOrganizationDiscoveryWorkspaces(app core.App, organizationID string, workspaceIDs []string) error {
	if len(workspaceIDs) == 0 {
		return nil
	}
	params := dbx.Params{"organizationId": organizationID}
	identities := make([]string, 0, len(workspaceIDs))
	for index, workspaceID := range workspaceIDs {
		key := fmt.Sprintf("workspace%d", index)
		params[key] = workspaceID
		identities = append(identities, "{:"+key+"}")
	}
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspaces}}
		WHERE [[organization]]={:organizationId} AND [[id]] IN (` + strings.Join(identities, ",") + `)
	`).Bind(params).Row(&count)
	if err != nil {
		return errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	if count != len(workspaceIDs) {
		return errOrganizationDiscoveryInvalid
	}
	return nil
}

func loadOrganizationDiscoveryRevision(app core.App, organizationID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT [[discovery_revision]] FROM {{leafen_organization_policy_state}}
		WHERE [[organization_id]]={:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).Row(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errOrganizationDiscoveryUnavailable
	}
	if err != nil || revision < 0 {
		return 0, errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	return revision, nil
}

const organizationDiscoveryExportSelect = `SELECT [[id]],[[organization_id]],[[owner_id]],
	[[mutation_id]],[[request_hash]],[[format]],[[filter_json]],[[status]],[[phase]],[[revision]],
	[[item_count]],[[byte_count]],[[chunk_count]],[[content_sha256]],[[source_revision]],
	[[created_at]],[[completed_at]] FROM {{leafen_organization_discovery_exports}}`

func loadStoredOrganizationDiscoveryExport(
	app core.App, organizationID, exportID string,
) (storedOrganizationDiscoveryExport, error) {
	row := storedOrganizationDiscoveryExport{}
	err := app.DB().NewQuery(organizationDiscoveryExportSelect + `
		WHERE [[organization_id]]={:organizationId} AND [[id]]={:exportId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "exportId": strings.TrimSpace(exportID),
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, errOrganizationDiscoveryNotFound
	}
	if err != nil {
		return row, errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	return row, nil
}

func findOrganizationDiscoveryExportByMutation(
	app core.App, organizationID, ownerID, mutationID string,
) (storedOrganizationDiscoveryExport, bool, error) {
	row := storedOrganizationDiscoveryExport{}
	err := app.DB().NewQuery(organizationDiscoveryExportSelect + `
		WHERE [[organization_id]]={:organizationId} AND [[owner_id]]={:ownerId}
			AND [[mutation_id]]={:mutationId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "ownerId": ownerID, "mutationId": mutationID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	return row, true, nil
}

func organizationDiscoveryExportFromStored(
	row storedOrganizationDiscoveryExport, organizationID string, duplicate bool,
) (leafen.OrganizationDiscoveryExportResponse, error) {
	filter := leafen.OrganizationDiscoveryFilter{}
	if row.OrganizationID != organizationID || len(row.ID) != 32 || row.OwnerID == "" ||
		row.Status != leafen.OrganizationDiscoveryExportStatusCompleted ||
		row.Phase != leafen.OrganizationDiscoveryExportPhaseCompleted || row.Revision < 1 ||
		(row.Format != leafen.OrganizationDiscoveryExportFormatJSONL && row.Format != leafen.OrganizationDiscoveryExportFormatJSON) ||
		row.ItemCount < 0 || row.ItemCount > leafen.OrganizationDiscoveryMaximumItems ||
		row.ByteCount < 0 || row.ByteCount > leafen.OrganizationDiscoveryMaximumBytes || row.ChunkCount < 0 ||
		len(row.RequestHash) != 64 || len(row.ContentSHA256) != 64 || row.SourceRevision < 0 ||
		row.CreatedAt < 0 || row.CompletedAt < row.CreatedAt ||
		json.Unmarshal([]byte(row.FilterJSON), &filter) != nil {
		return leafen.OrganizationDiscoveryExportResponse{}, errOrganizationDiscoveryUnavailable
	}
	if err := validateOrganizationDiscoveryFilter(filter); err != nil {
		return leafen.OrganizationDiscoveryExportResponse{}, errOrganizationDiscoveryUnavailable
	}
	return leafen.OrganizationDiscoveryExportResponse{
		ID: row.ID, OrganizationID: row.OrganizationID, Status: row.Status, Phase: row.Phase,
		Format: row.Format, Filter: filter, Revision: row.Revision, ItemCount: row.ItemCount,
		ByteCount: row.ByteCount, ChunkCount: row.ChunkCount, CreatedBy: row.OwnerID,
		CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt,
		ContentSHA256: row.ContentSHA256, Duplicate: duplicate,
	}, nil
}

func listStoredOrganizationDiscoveryExports(
	app core.App, organizationID, cursor string, limit int,
) (leafen.OrganizationDiscoveryExportListResponse, error) {
	cursorCompleted, cursorID, err := parseOrganizationDiscoveryCursor(strings.TrimSpace(cursor))
	if err != nil || limit < 1 || limit > leafen.OrganizationDiscoveryExportsPerPage {
		return leafen.OrganizationDiscoveryExportListResponse{}, errOrganizationDiscoveryInvalid
	}
	params := dbx.Params{"organizationId": organizationID, "limit": limit + 1}
	condition := ""
	if cursorID != "" {
		params["completedAt"], params["cursorId"] = cursorCompleted, cursorID
		condition = " AND ([[completed_at]] < {:completedAt} OR ([[completed_at]] = {:completedAt} AND [[id]] < {:cursorId}))"
	}
	rows := []storedOrganizationDiscoveryExport{}
	if err := app.DB().NewQuery(organizationDiscoveryExportSelect + `
		WHERE [[organization_id]]={:organizationId}` + condition + `
		ORDER BY [[completed_at]] DESC,[[id]] DESC LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return leafen.OrganizationDiscoveryExportListResponse{}, errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]leafen.OrganizationDiscoveryExportResponse, 0, len(rows))
	for _, row := range rows {
		item, err := organizationDiscoveryExportFromStored(row, organizationID, false)
		if err != nil {
			return leafen.OrganizationDiscoveryExportListResponse{}, err
		}
		items = append(items, item)
	}
	response := leafen.OrganizationDiscoveryExportListResponse{DiscoveryExports: items}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.NextCursor = fmt.Sprintf("%d:%s", last.CompletedAt, last.ID)
	}
	return response, nil
}

func parseOrganizationDiscoveryCursor(value string) (int64, string, error) {
	if value == "" {
		return 0, "", nil
	}
	if len(value) > 512 {
		return 0, "", errOrganizationDiscoveryInvalid
	}
	created, id, ok := strings.Cut(value, ":")
	timestamp, err := strconv.ParseInt(created, 10, 64)
	if !ok || id == "" || err != nil || timestamp < 0 {
		return 0, "", errOrganizationDiscoveryInvalid
	}
	return timestamp, id, nil
}

func organizationDiscoveryMatches(item discoveryItem, filter leafen.OrganizationDiscoveryFilter) bool {
	switch item.Type {
	case "page", "block", "comment", "file":
	default:
		return false
	}
	if len(filter.UserIDs) > 0 {
		accepted := false
		for _, custodian := range item.CustodianUserIDs {
			index := sort.SearchStrings(filter.UserIDs, custodian)
			if index < len(filter.UserIDs) && filter.UserIDs[index] == custodian {
				accepted = true
				break
			}
		}
		if !accepted {
			return false
		}
	}
	occurred := item.UpdatedAt
	if occurred == "" {
		occurred = item.CreatedAt
	}
	if filter.Since != "" && (occurred == "" || occurred < filter.Since) {
		return false
	}
	if filter.Until != "" && (occurred == "" || occurred > filter.Until) {
		return false
	}
	if filter.Query == "" {
		return true
	}
	content, err := json.Marshal(item.Content)
	return err == nil && strings.Contains(
		organizationDiscoveryFold.String(norm.NFKC.String(string(content))),
		organizationDiscoveryFold.String(filter.Query),
	)
}
