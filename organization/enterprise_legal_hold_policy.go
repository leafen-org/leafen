package organization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

var (
	errLegalHoldInvalid        = leafen.NewLegalHoldFailure(leafen.ErrorCodeLegalHoldInvalid)
	errLegalHoldTargetNotFound = errors.New("legal hold target not found")
	errLegalHoldNotFound       = leafen.NewLegalHoldFailure(leafen.ErrorCodeLegalHoldNotFound)
)

func normalizeLegalHoldCreateInput(
	input leafen.LegalHoldCreateInput,
) (leafen.LegalHoldCreateInput, string, error) {
	name := strings.TrimSpace(input.Name)
	reason := strings.TrimSpace(input.Reason)
	if name == "" || len([]rune(name)) > leafen.LegalHoldMaxNameRunes {
		return leafen.LegalHoldCreateInput{}, "", leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("name is required, up to %d characters.", leafen.LegalHoldMaxNameRunes))
	}
	if reason == "" || len([]rune(reason)) > leafen.LegalHoldMaxReasonRunes {
		return leafen.LegalHoldCreateInput{}, "", leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("reason is required, up to %d characters.", leafen.LegalHoldMaxReasonRunes))
	}
	scope, err := normalizeLegalHoldScope(input.Scope)
	if err != nil {
		return leafen.LegalHoldCreateInput{}, "", err
	}
	hash, err := legalHoldHash(struct {
		Name   string                        `json:"name"`
		Reason string                        `json:"reason"`
		Scope  leafen.LegalHoldScopeResponse `json:"scope"`
	}{Name: name, Reason: reason, Scope: scope})
	if err != nil {
		return leafen.LegalHoldCreateInput{}, "", err
	}
	mutationID, err := normalizeLegalHoldMutationID(input.MutationID, hash)
	if err != nil {
		return leafen.LegalHoldCreateInput{}, "", err
	}
	return leafen.LegalHoldCreateInput{
		MutationID: mutationID, Name: name, Reason: reason,
		Scope: leafen.LegalHoldScopeInput{
			All: scope.All, WorkspaceIDs: scope.WorkspaceIDs, PageIDs: scope.PageIDs,
			CustodianUserIDs: scope.CustodianUserIDs,
		},
	}, hash, nil
}

func normalizeLegalHoldReleaseInput(
	holdID string,
	input leafen.LegalHoldReleaseInput,
) (leafen.LegalHoldReleaseInput, string, error) {
	holdID = strings.TrimSpace(holdID)
	if len(holdID) != 32 {
		return leafen.LegalHoldReleaseInput{}, "", errLegalHoldNotFound
	}
	hash, err := legalHoldHash(struct {
		HoldID string `json:"legalHoldId"`
	}{HoldID: holdID})
	if err != nil {
		return leafen.LegalHoldReleaseInput{}, "", err
	}
	mutationID, err := normalizeLegalHoldMutationID(input.MutationID, hash)
	if err != nil {
		return leafen.LegalHoldReleaseInput{}, "", err
	}
	return leafen.LegalHoldReleaseInput{MutationID: mutationID}, hash, nil
}

func normalizeLegalHoldMutationID(value, requestHash string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "content:" + requestHash
	}
	if len([]rune(value)) > leafen.LegalHoldMaxMutationIDRunes {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("The idempotency key is longer than %d characters.", leafen.LegalHoldMaxMutationIDRunes))
	}
	return value, nil
}

func normalizeLegalHoldScope(input leafen.LegalHoldScopeInput) (leafen.LegalHoldScopeResponse, error) {
	workspaces, err := normalizeLegalHoldIDs(input.WorkspaceIDs)
	if err != nil {
		return leafen.LegalHoldScopeResponse{}, err
	}
	pages, err := normalizeLegalHoldIDs(input.PageIDs)
	if err != nil {
		return leafen.LegalHoldScopeResponse{}, err
	}
	users, err := normalizeLegalHoldIDs(input.CustodianUserIDs)
	if err != nil {
		return leafen.LegalHoldScopeResponse{}, err
	}
	all := input.All || len(workspaces)+len(pages)+len(users) == 0
	if all && len(workspaces)+len(pages)+len(users) != 0 {
		return leafen.LegalHoldScopeResponse{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "Choose all, or particular workspaces, pages or users, not both.")
	}
	return leafen.LegalHoldScopeResponse{
		All: all, WorkspaceIDs: workspaces, PageIDs: pages, CustodianUserIDs: users,
	}, nil
}

func normalizeLegalHoldIDs(values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > leafen.LegalHoldMaxScopeIDRunes {
			return nil, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("Each workspace, page or user ID must be non-empty and at most %d characters.", leafen.LegalHoldMaxScopeIDRunes))
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func legalHoldScopeRows(
	holdID, organizationID string,
	scope leafen.LegalHoldScopeResponse,
) []storedLegalHoldScope {
	if scope.All {
		return []storedLegalHoldScope{{
			HoldID: holdID, OrganizationID: organizationID, Kind: leafen.LegalHoldScopeAll,
		}}
	}
	rows := make([]storedLegalHoldScope, 0,
		len(scope.WorkspaceIDs)+len(scope.PageIDs)+len(scope.CustodianUserIDs))
	for _, item := range []struct {
		kind string
		ids  []string
	}{
		{leafen.LegalHoldScopeWorkspace, scope.WorkspaceIDs},
		{leafen.LegalHoldScopePage, scope.PageIDs},
		{leafen.LegalHoldScopeCustodian, scope.CustodianUserIDs},
	} {
		for _, id := range item.ids {
			rows = append(rows, storedLegalHoldScope{
				HoldID: holdID, OrganizationID: organizationID, Kind: item.kind, TargetID: id,
			})
		}
	}
	return rows
}

func legalHoldHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func legalHoldMutationRecordID(
	organizationID, actorID, operation, mutationID string,
) string {
	digest := sha256.Sum256([]byte(
		organizationID + "\x00" + actorID + "\x00" + operation + "\x00" + mutationID,
	))
	return hex.EncodeToString(digest[:16])
}

func activeLegalHoldPreventsDeletion(
	app core.App,
	organizationID string,
	target leafen.LegalHoldDeletionTarget,
) error {
	if organizationID == "" || target.WorkspaceID == "" {
		return leafen.ErrLegalHoldUnavailable
	}
	pageIDs, err := normalizeLegalHoldIDs(target.PageIDs)
	if err != nil {
		return leafen.ErrLegalHoldUnavailable
	}
	custodianIDs, err := normalizeLegalHoldIDs(target.CustodianUserIDs)
	if err != nil {
		return leafen.ErrLegalHoldUnavailable
	}
	target.PageIDs, target.CustodianUserIDs = pageIDs, custodianIDs
	matchedScope, found, err := findDirectActiveLegalHoldScope(app, organizationID, target)
	if err != nil {
		return err
	}
	if found && legalHoldAppliesToDeletion(legalHoldScopeForMatch(matchedScope), target) {
		return leafen.ErrLegalHoldPreventsPermanentDelete
	}
	if target.EntireWorkspace {
		matchedScope, found, err = findContainedActiveLegalHoldScope(
			app, organizationID, target.WorkspaceID,
		)
		if err != nil {
			return err
		}
		if found {
			containedTarget := target
			switch matchedScope.Kind {
			case leafen.LegalHoldScopePage:
				containedTarget.PageIDs = []string{matchedScope.TargetID}
			case leafen.LegalHoldScopeCustodian:
				containedTarget.CustodianUserIDs = []string{matchedScope.TargetID}
			default:
				return leafen.ErrLegalHoldUnavailable
			}
			if legalHoldAppliesToDeletion(
				legalHoldScopeForMatch(matchedScope), containedTarget,
			) {
				return leafen.ErrLegalHoldPreventsPermanentDelete
			}
			return leafen.ErrLegalHoldUnavailable
		}
	}
	return nil
}

func assertLegalHoldDeleteTransaction(app core.App) error {
	if !app.IsTransactional() {
		return leafen.ErrLegalHoldUnavailable
	}
	return nil
}

func legalHoldAppliesToDeletion(
	scope leafen.LegalHoldScopeResponse,
	target leafen.LegalHoldDeletionTarget,
) bool {
	if scope.All || containsLegalHoldID(scope.WorkspaceIDs, target.WorkspaceID) {
		return true
	}
	return intersectsLegalHoldIDs(scope.PageIDs, target.PageIDs) ||
		intersectsLegalHoldIDs(scope.CustodianUserIDs, target.CustodianUserIDs)
}

func legalHoldScopeForMatch(row storedLegalHoldScope) leafen.LegalHoldScopeResponse {
	scope := leafen.LegalHoldScopeResponse{
		WorkspaceIDs: []string{}, PageIDs: []string{}, CustodianUserIDs: []string{},
	}
	switch row.Kind {
	case leafen.LegalHoldScopeAll:
		scope.All = true
	case leafen.LegalHoldScopeWorkspace:
		scope.WorkspaceIDs = []string{row.TargetID}
	case leafen.LegalHoldScopePage:
		scope.PageIDs = []string{row.TargetID}
	case leafen.LegalHoldScopeCustodian:
		scope.CustodianUserIDs = []string{row.TargetID}
	}
	return scope
}

func containsLegalHoldID(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}

func intersectsLegalHoldIDs(left, right []string) bool {
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		switch strings.Compare(left[leftIndex], right[rightIndex]) {
		case -1:
			leftIndex++
		case 1:
			rightIndex++
		default:
			return true
		}
	}
	return false
}

// retentionHoldSQL is true of a record an active legal hold of its
// organization covers: all of it, its workspace, a page it is about or a
// person whose doing or reading it is.
func retentionHoldSQL(record leafen.LegalHoldRecordSQL) string {
	scope := func(kind, target string) string {
		return `([[held_scope]].[[kind]] = '` + kind + `' AND [[held_scope]].[[target_id]] = ` + target + `)`
	}
	covers := []string{`[[held_scope]].[[kind]] = '` + leafen.LegalHoldScopeAll + `'`}
	if record.Workspace != "" {
		covers = append(covers, scope(leafen.LegalHoldScopeWorkspace, record.Workspace))
	}
	for _, page := range record.Pages {
		covers = append(covers, scope(leafen.LegalHoldScopePage, page))
	}
	for _, person := range record.People {
		covers = append(covers, scope(leafen.LegalHoldScopeCustodian, person))
	}
	return `EXISTS (SELECT 1 FROM {{leafen_organization_legal_hold_scopes}} [[held_scope]]
		JOIN {{leafen_organization_legal_holds}} [[held]]
			ON [[held]].[[id]] = [[held_scope]].[[hold_id]] AND [[held]].[[organization_id]] = [[held_scope]].[[organization_id]]
		WHERE [[held_scope]].[[organization_id]] = ` + record.Organization + ` AND [[held]].[[status]] = '` + leafen.LegalHoldStatusActive + `'
			AND (` + strings.Join(covers, " OR ") + `))`
}
