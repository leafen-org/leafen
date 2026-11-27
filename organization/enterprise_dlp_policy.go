package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/unicode/norm"
)

var ()

var dlpIgnoredContentKeys = map[string]struct{}{
	"action": {}, "id": {}, "ids": {}, "workspaceId": {}, "pageId": {},
	"databaseId": {}, "rowId": {}, "parentId": {}, "blockId": {},
	"commentId": {}, "uploadId": {}, "key": {}, "token": {},
	"expectedUpdatedAt": {}, "mutationId": {}, "baseCursor": {},
}

func defaultDLPPolicyResponse() leafen.DLPPolicyResponse {
	return leafen.DLPPolicyResponse{ContentScanMode: leafen.DLPContentScanBlock, SensitiveTerms: []string{}}
}

func loadOrganizationDLPPolicy(app core.App, organizationID string) (leafen.DLPPolicyResponse, error) {
	stored, err := loadStoredOrganizationDLPPolicy(app, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultDLPPolicyResponse(), nil
	}
	if err != nil {
		return leafen.DLPPolicyResponse{}, err
	}
	return stored.Response()
}

func loadStoredOrganizationDLPPolicy(app core.App, organizationID string) (leafen.StoredOrganizationDLPPolicy, error) {
	stored := leafen.StoredOrganizationDLPPolicy{}
	err := app.DB().NewQuery(`
		SELECT [[organization_id]], [[enabled]], [[content_scan_mode]], [[sensitive_terms_json]],
			[[block_public_sharing]], [[block_external_sharing]], [[block_file_downloads]],
			[[block_exports]], [[revision]], [[updated_at]]
		FROM {{leafen_organization_dlp_policies}}
		WHERE [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).One(&stored)
	return stored, err
}

func saveOrganizationDLPPolicy(
	app core.App,
	organization leafen.OrganizationAuthorization,
	input leafen.DLPPolicyInput,
) (leafen.DLPPolicyResponse, error) {
	normalized, err := leafen.NormalizeDLPPolicyInput(input)
	if err != nil {
		return leafen.DLPPolicyResponse{}, err
	}
	response := leafen.DLPPolicyResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, err := leafen.AuthorizeOrganization(
			txApp, organization.PrincipalID, organization.OrganizationID, leafen.PermissionOrganizationSecurityAdmin,
		)
		if err != nil {
			return err
		}
		current, err := loadOrganizationDLPPolicy(txApp, organization.OrganizationID)
		if err != nil {
			return err
		}
		if leafen.DLPPolicyMatchesInput(current, normalized) {
			response = current
			return nil
		}
		terms, err := json.Marshal(normalized.SensitiveTerms)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		revision := current.Revision + 1
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_organization_dlp_policies}} (
				[[organization_id]], [[enabled]], [[content_scan_mode]], [[sensitive_terms_json]],
				[[block_public_sharing]], [[block_external_sharing]], [[block_file_downloads]],
				[[block_exports]], [[revision]], [[updated_at]]
			) VALUES (
				{:organizationId}, {:enabled}, {:contentScanMode}, {:sensitiveTerms},
				{:blockPublicSharing}, {:blockExternalSharing}, {:blockFileDownloads},
				{:blockExports}, {:revision}, {:updatedAt}
			)
			ON CONFLICT ([[organization_id]]) DO UPDATE SET
				[[enabled]] = excluded.[[enabled]],
				[[content_scan_mode]] = excluded.[[content_scan_mode]],
				[[sensitive_terms_json]] = excluded.[[sensitive_terms_json]],
				[[block_public_sharing]] = excluded.[[block_public_sharing]],
				[[block_external_sharing]] = excluded.[[block_external_sharing]],
				[[block_file_downloads]] = excluded.[[block_file_downloads]],
				[[block_exports]] = excluded.[[block_exports]],
				[[revision]] = excluded.[[revision]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"organizationId": organization.OrganizationID, "enabled": leafen.BoolInt(normalized.Enabled),
			"contentScanMode": normalized.ContentScanMode, "sensitiveTerms": string(terms),
			"blockPublicSharing":   leafen.BoolInt(normalized.BlockPublicSharing),
			"blockExternalSharing": leafen.BoolInt(normalized.BlockExternalSharing),
			"blockFileDownloads":   leafen.BoolInt(normalized.BlockFileDownloads),
			"blockExports":         leafen.BoolInt(normalized.BlockExports), "revision": revision, "updatedAt": now,
		}).Execute()
		if err != nil {
			return err
		}
		if err := leafen.BumpOrganizationPolicy(txApp, organization.OrganizationID, now); err != nil {
			return err
		}
		if err := leafen.RecordOrganizationAudit(
			txApp, organization.OrganizationID, currentAuthorization.PrincipalID,
			"organization.dlp.updated", "dlp_policy", organization.OrganizationID+":"+strconv.Itoa(revision),
		); err != nil {
			return err
		}
		response = leafen.DLPPolicyResponse{
			Enabled: normalized.Enabled, ContentScanMode: normalized.ContentScanMode,
			SensitiveTerms:       slices.Clone(normalized.SensitiveTerms),
			BlockPublicSharing:   normalized.BlockPublicSharing,
			BlockExternalSharing: normalized.BlockExternalSharing,
			BlockFileDownloads:   normalized.BlockFileDownloads,
			BlockExports:         normalized.BlockExports, Revision: revision,
		}
		return nil
	})
	return response, err
}

func enforceDLPContent(app core.App, organizationID string, values ...any) error {
	scanner, err := newDLPContentScanner(app, organizationID)
	if err != nil {
		return err
	}
	return scanner.Enforce(values...)
}

type dlpContentScanner struct {
	organizationID string
	policy         leafen.DLPPolicyResponse
	terms          []string
}

func newDLPContentScanner(app core.App, organizationID string) (dlpContentScanner, error) {
	policy, err := loadOrganizationDLPPolicy(app, organizationID)
	if err != nil {
		return dlpContentScanner{}, errors.Join(leafen.ErrDLPPolicyUnavailable, err)
	}
	scanner := dlpContentScanner{organizationID: organizationID, policy: policy}
	if !policy.Enabled || policy.ContentScanMode != leafen.DLPContentScanBlock || len(policy.SensitiveTerms) == 0 {
		return scanner, nil
	}
	scanner.terms = make([]string, 0, len(policy.SensitiveTerms))
	for _, term := range policy.SensitiveTerms {
		scanner.terms = append(scanner.terms, leafen.DLPFold().String(norm.NFKC.String(term)))
	}
	return scanner, nil
}

func (scanner dlpContentScanner) Blocks() bool {
	return len(scanner.terms) > 0
}

func (scanner dlpContentScanner) Enforce(values ...any) error {
	if len(scanner.terms) == 0 {
		return nil
	}
	visited := 0
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return leafen.ErrDLPPolicyBlocked
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return leafen.ErrDLPPolicyBlocked
		}
		blocked, err := dlpValueContainsSensitiveTerm(decoded, scanner.terms, &visited)
		if err != nil || blocked {
			return leafen.ErrDLPPolicyBlocked
		}
	}
	return nil
}

func (scanner dlpContentScanner) RequireCurrentPolicy(app core.App, organizationID string) error {
	if organizationID != scanner.organizationID {
		return leafen.ErrDLPPolicyUnavailable
	}
	current, err := loadOrganizationDLPPolicy(app, organizationID)
	if err != nil {
		return errors.Join(leafen.ErrDLPPolicyUnavailable, err)
	}
	if current.Revision != scanner.policy.Revision {
		return leafen.ErrDLPPolicyUnavailable
	}
	return nil
}

func dlpValueContainsSensitiveTerm(value any, terms []string, visited *int) (bool, error) {
	*visited++
	// Past what a change the server takes can hold — a thousand operations,
	// a whole 100 x 300 table's cells among them (2026-11-10: ten thousand
	// refused such a paste with no sensitive term in it) — it can't be
	// checked, so it is blocked.
	if *visited > leafen.DLPMaxScannedValues {
		return false, leafen.ErrDLPPolicyBlocked
	}
	switch typed := value.(type) {
	case string:
		if utf8.RuneCountInString(typed) > leafen.DLPMaxScannedStringRunes {
			return false, leafen.ErrDLPPolicyBlocked
		}
		text := leafen.DLPFold().String(norm.NFKC.String(typed))
		for _, term := range terms {
			if strings.Contains(text, term) {
				return true, nil
			}
		}
	case []any:
		for _, item := range typed {
			blocked, err := dlpValueContainsSensitiveTerm(item, terms, visited)
			if err != nil || blocked {
				return blocked, err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if _, ignored := dlpIgnoredContentKeys[key]; ignored {
				continue
			}
			blocked, err := dlpValueContainsSensitiveTerm(item, terms, visited)
			if err != nil || blocked {
				return blocked, err
			}
		}
	}
	return false, nil
}

func dlpOutbound(app core.App, organizationID, action string) error {
	policy, err := loadOrganizationDLPPolicy(app, organizationID)
	if err != nil {
		return errors.Join(leafen.ErrDLPPolicyUnavailable, err)
	}
	if !policy.Enabled {
		return nil
	}
	blocked := map[string]bool{
		leafen.DLPOutboundPublicSharing:   policy.BlockPublicSharing,
		leafen.DLPOutboundExternalSharing: policy.BlockExternalSharing,
		leafen.DLPOutboundFileDownloads:   policy.BlockFileDownloads,
		leafen.DLPOutboundExports:         policy.BlockExports,
	}
	denied, known := blocked[action]
	if !known {
		return leafen.ErrDLPPolicyBlocked
	}
	if denied {
		return leafen.ErrDLPPolicyBlocked
	}
	return nil
}

func findOwnedOrganization(app core.App, principalID string) (leafen.OrganizationAuthorization, bool, error) {
	organizations, err := app.FindCollectionByNameOrId(leafen.OrganizationsCollection)
	if err != nil {
		return leafen.OrganizationAuthorization{}, false, err
	}
	organization, err := app.FindFirstRecordByData(organizations, "owner", principalID)
	if errors.Is(err, sql.ErrNoRows) {
		return leafen.OrganizationAuthorization{}, false, nil
	}
	if err != nil {
		return leafen.OrganizationAuthorization{}, false, err
	}
	authorization, err := leafen.AuthorizeOrganization(app, principalID, organization.Id, leafen.PermissionOrganizationSecurityAdmin)
	return authorization, err == nil, err
}

func (scanner dlpContentScanner) Revision() int {
	return scanner.policy.Revision
}

// organizationDLP answers the core's DLP checks.
type organizationDLP struct{}

func (organizationDLP) ContentScanner(app core.App, organizationID string) (leafen.OrganizationDLPContentScanner, error) {
	return newDLPContentScanner(app, organizationID)
}

func (organizationDLP) Outbound(app core.App, organizationID, action string) error {
	return dlpOutbound(app, organizationID, action)
}
