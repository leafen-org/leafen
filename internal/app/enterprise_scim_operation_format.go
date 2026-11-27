package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a SCIM group operation keeps its members and metadata. The organization
// module's SCIM writes operations in this form, and the core's migration that
// moved members out of the payload (enterprise_scim_operation_members_schema.go)
// wrote them in it too.

const scimOperationBatchSize = 256

type scimGroupMutationPayload struct {
	DisplayName string   `json:"displayName"`
	ExternalID  string   `json:"externalId"`
	MemberIDs   []string `json:"memberIds"`
	Deleted     bool     `json:"deleted"`
}

type scimGroupOperationMetadata struct {
	DisplayName string `json:"displayName"`
	ExternalID  string `json:"externalId"`
	Deleted     bool   `json:"deleted"`
}

func scimOperationMetadata(payload scimGroupMutationPayload) scimGroupOperationMetadata {
	return scimGroupOperationMetadata{
		DisplayName: payload.DisplayName,
		ExternalID:  payload.ExternalID,
		Deleted:     payload.Deleted,
	}
}

func insertSCIMOperationMemberRange(
	app core.App,
	operationID string,
	memberIDs []string,
	startPosition int,
	now int64,
) error {
	for start := 0; start < len(memberIDs); start += scimOperationBatchSize {
		end := min(start+scimOperationBatchSize, len(memberIDs))
		params := dbx.Params{"operationId": operationID, "now": now}
		values := make([]string, 0, end-start)
		for position := start; position < end; position++ {
			key := fmt.Sprintf("member%d", position-start)
			params[key] = memberIDs[position]
			values = append(values, fmt.Sprintf(
				"({:operationId}, %d, {:%s}, {:now})", startPosition+position, key,
			))
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_scim_operation_members}}
				([[operation_id]], [[position]], [[organization_member_id]], [[created_at]])
			VALUES ` + strings.Join(values, ", ") + `
			ON CONFLICT ([[operation_id]], [[position]]) DO NOTHING
		`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func uniqueSortedSCIMMemberIDs(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
