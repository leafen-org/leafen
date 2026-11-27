package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type databaseQueryPlan struct {
	Search        string
	FilterGroup   *databaseQueryFilterGroup
	Sorts         []databaseQuerySort
	Location      *time.Location
	ReferenceTime time.Time
}

func canonicalDatabaseQueryRequest(
	principalID, pageID string,
	input databaseQueryRequest,
) (databaseQueryRequest, string, error) {
	input.DataSourceID = strings.TrimSpace(input.DataSourceID)
	if input.ViewID != nil {
		value := strings.TrimSpace(*input.ViewID)
		input.ViewID = &value
	}
	if input.Search != nil {
		value := strings.TrimSpace(*input.Search)
		input.Search = &value
	}
	input.SubitemScope = strings.TrimSpace(input.SubitemScope)
	if input.SubitemScope == "" {
		input.SubitemScope = databaseSubitemScopeAll
	}
	if input.SubitemParentID != nil {
		value := strings.TrimSpace(*input.SubitemParentID)
		input.SubitemParentID = &value
	}
	input.ArchiveScope = strings.TrimSpace(input.ArchiveScope)
	if input.ArchiveScope == "" {
		input.ArchiveScope = databaseArchiveScopeActive
	}
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	if input.TimeZone == "" {
		input.TimeZone = "UTC"
	}
	if input.ReferenceTime != nil {
		value := strings.TrimSpace(*input.ReferenceTime)
		input.ReferenceTime = &value
	}
	if input.Limit == 0 {
		input.Limit = maxDatabaseQueryResultRows
	}
	input.Cursor = nil
	if input.DataSourceID == "" || len(input.DataSourceID) > maxGraphIdentifierLength ||
		input.Limit < 1 || input.Limit > maxDatabaseQueryResultRows ||
		input.ViewID != nil && (*input.ViewID == "" || len(*input.ViewID) > maxGraphIdentifierLength) ||
		input.Search != nil && len([]rune(*input.Search)) > maxDatabaseQuerySearchRunes ||
		(input.SubitemScope != databaseSubitemScopeAll &&
			input.SubitemScope != databaseSubitemScopeParents &&
			input.SubitemScope != databaseSubitemScopeChildren) ||
		(input.SubitemScope == databaseSubitemScopeChildren &&
			(input.SubitemParentID == nil || *input.SubitemParentID == "" || len(*input.SubitemParentID) > maxGraphIdentifierLength)) ||
		(input.SubitemScope != databaseSubitemScopeChildren && input.SubitemParentID != nil) ||
		(input.ArchiveScope != databaseArchiveScopeActive &&
			input.ArchiveScope != databaseArchiveScopeArchived &&
			input.ArchiveScope != databaseArchiveScopeAll) {
		return input, "", errors.New(errorCodeDatabaseQueryInvalid)
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return input, "", errors.New(errorCodeDatabaseQueryInvalid)
	}
	if input.ReferenceTime != nil {
		if _, err := time.Parse(time.RFC3339Nano, *input.ReferenceTime); err != nil {
			return input, "", errors.New(errorCodeDatabaseQueryInvalid)
		}
	}
	// The build names it too: a newer build never reuses an older one's
	// rows (server_build_identity.go).
	encoded, err := json.Marshal(struct {
		PrincipalID string               `json:"principalId"`
		PageID      string               `json:"pageId"`
		Input       databaseQueryRequest `json:"input"`
		Build       string               `json:"build"`
	}{PrincipalID: principalID, PageID: pageID, Input: input, Build: serverBuildIdentity})
	if err != nil {
		return input, "", err
	}
	digest := sha256.Sum256(encoded)
	return input, hex.EncodeToString(digest[:]), nil
}

func compileDatabaseQueryPlan(
	input databaseQueryRequest,
	context databaseQueryContext,
) (databaseQueryPlan, error) {
	plan := databaseQueryPlan{}
	if len(context.ViewConfig) > 0 {
		// The view's rows as its screen shows them, filter bar included.
		known := make([]string, 0, len(context.Properties))
		for _, property := range context.Properties {
			if property.DataSourceID == context.DataSourceID {
				known = append(known, property.ID)
			}
		}
		filterGroup, sorts, search, err := databaseViewQueryTerms(string(context.ViewConfig), known)
		if err != nil {
			return plan, errors.New(errorCodeDatabaseQueryInvalid)
		}
		// A view saved nested deeper before reads its first three levels, as
		// its screen shows them (queryBudget.ts boundedQueryConfig).
		plan.FilterGroup, plan.Sorts = databaseQueryFilterGroupWithin(filterGroup, 1), sorts
		if search != nil {
			plan.Search = strings.TrimSpace(*search)
		}
	}
	if input.Search != nil {
		plan.Search = strings.TrimSpace(*input.Search)
	}
	if input.FilterGroup != nil {
		plan.FilterGroup = input.FilterGroup
	}
	if len(input.Sorts) > 0 {
		plan.Sorts = input.Sorts
	}
	plan.Sorts, plan.FilterGroup = withoutDeletedDatabaseQueryProperties(plan.Sorts, plan.FilterGroup, context.DeletedPropertyIDs)
	plan.FilterGroup = pruneDatabaseQueryFilterGroup(plan.FilterGroup, func(filter databaseQueryFilter) bool {
		return !databaseQueryFilterAwaitsValue(filter)
	})
	if len([]rune(plan.Search)) > maxDatabaseQuerySearchRunes {
		return plan, errors.New(errorCodeDatabaseQueryInvalid)
	}
	for index := range plan.Sorts {
		sort := &plan.Sorts[index]
		switch sort.Direction {
		case "asc":
			sort.Direction = databaseQuerySortAscending
		case "desc":
			sort.Direction = databaseQuerySortDescending
		}
		if _, ok := context.PropertiesByID[sort.PropertyID]; !ok ||
			(sort.Direction != databaseQuerySortAscending && sort.Direction != databaseQuerySortDescending) {
			return plan, errors.New(errorCodeDatabaseQueryInvalid)
		}
	}
	if err := validateDatabaseQueryFilterGroup(plan.FilterGroup, context.PropertiesByID); err != nil {
		return plan, err
	}
	if err := context.refuseHiddenQueryProperties(plan); err != nil {
		return plan, err
	}
	location, err := time.LoadLocation(input.TimeZone)
	if err != nil {
		return plan, errors.New(errorCodeDatabaseQueryInvalid)
	}
	plan.Location = location
	plan.ReferenceTime = time.Now().UTC()
	if input.ReferenceTime != nil {
		plan.ReferenceTime, err = time.Parse(time.RFC3339Nano, *input.ReferenceTime)
		if err != nil {
			return plan, errors.New(errorCodeDatabaseQueryInvalid)
		}
	}
	return plan, nil
}

func validateDatabaseQueryFilterGroup(
	root *databaseQueryFilterGroup,
	properties map[string]storedDatabaseQueryProperty,
) error {
	if root == nil {
		return nil
	}
	type entry struct {
		group *databaseQueryFilterGroup
		depth int
	}
	stack := []entry{{group: root, depth: 1}}
	// As many conditions and groups as a view holds, nested three levels at
	// most, as Notion's advanced filter (seen 2026-11-03 and 2026-11-19).
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.depth > maxDatabaseQueryFilterDepth ||
			(current.group.Conjunction != databaseQueryConjunctionAnd && current.group.Conjunction != databaseQueryConjunctionOr) {
			return errors.New(errorCodeDatabaseQueryInvalid)
		}
		for _, filter := range current.group.Filters {
			if filter.PropertyID == "" {
				return errors.New(errorCodeDatabaseQueryInvalid)
			}
			property, ok := properties[filter.PropertyID]
			if !ok || !databaseQueryFilterOperatorAllowed(property.Type, filter.Operator) {
				return errors.New(errorCodeDatabaseQueryInvalid)
			}
			if filter.Operator == "relative_to_today" {
				if _, ok := databaseQueryRelativeDateInput(databaseQueryRawValue(filter.Value)); !ok {
					return errors.New(errorCodeDatabaseQueryInvalid)
				}
			}
		}
		for index := range current.group.Groups {
			stack = append(stack, entry{group: &current.group.Groups[index], depth: current.depth + 1})
		}
	}
	return nil
}

// withoutDeletedDatabaseQueryProperties leaves out the sorts and filters on
// properties in Deleted properties; a view keeps them for a restore.
func withoutDeletedDatabaseQueryProperties(
	sorts []databaseQuerySort, group *databaseQueryFilterGroup, deleted map[string]struct{},
) ([]databaseQuerySort, *databaseQueryFilterGroup) {
	if len(deleted) == 0 {
		return sorts, group
	}
	kept := make([]databaseQuerySort, 0, len(sorts))
	for _, sort := range sorts {
		if _, gone := deleted[sort.PropertyID]; !gone {
			kept = append(kept, sort)
		}
	}
	return kept, pruneDatabaseQueryFilterGroup(group, func(filter databaseQueryFilter) bool {
		_, gone := deleted[filter.PropertyID]
		return !gone
	})
}

// pruneDatabaseQueryFilterGroup keeps the filters keep accepts and leaves out
// the groups left without any.
func pruneDatabaseQueryFilterGroup(
	group *databaseQueryFilterGroup, keep func(databaseQueryFilter) bool,
) *databaseQueryFilterGroup {
	if group == nil {
		return nil
	}
	next := &databaseQueryFilterGroup{Conjunction: group.Conjunction}
	for _, filter := range group.Filters {
		if keep(filter) {
			next.Filters = append(next.Filters, filter)
		}
	}
	for index := range group.Groups {
		if child := pruneDatabaseQueryFilterGroup(&group.Groups[index], keep); child != nil {
			next.Groups = append(next.Groups, *child)
		}
	}
	if len(next.Filters) == 0 && len(next.Groups) == 0 {
		return nil
	}
	return next
}

// databaseQueryFilterGroupWithin is a filter group, at the depth given,
// without the groups nested past maxDatabaseQueryFilterDepth.
func databaseQueryFilterGroupWithin(group *databaseQueryFilterGroup, depth int) *databaseQueryFilterGroup {
	if group == nil {
		return nil
	}
	within := *group
	within.Groups, within.Order = nil, nil
	if depth >= maxDatabaseQueryFilterDepth {
		return &within
	}
	within.Groups, within.Order = make([]databaseQueryFilterGroup, 0, len(group.Groups)), group.Order
	for index := range group.Groups {
		within.Groups = append(within.Groups, *databaseQueryFilterGroupWithin(&group.Groups[index], depth+1))
	}
	return &within
}

// databaseQueryFilterAwaitsValue reports a filter whose value is still empty:
// it doesn't filter yet, as a new filter in Notion shows every row until a
// value is chosen (seen 2026-09-30). "Empty" and "not empty" and the fixed
// relative dates ("this week") need no value.
func databaseQueryFilterAwaitsValue(filter databaseQueryFilter) bool {
	switch filter.Operator {
	case "is_empty", "is_not_empty":
		return false
	case "is_between":
		return !databaseQueryDateRangeHasDay(databaseQueryRawValue(filter.Value))
	}
	if _, relative := databaseQueryRelativeOperators[filter.Operator]; relative && filter.Operator != "relative_to_today" {
		return false
	}
	switch value := databaseQueryRawValue(filter.Value).(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(value) == ""
	case []any:
		return len(value) == 0
	}
	return false
}
