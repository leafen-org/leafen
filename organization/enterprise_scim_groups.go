package organization

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	filterParser "github.com/scim2/filter-parser/v2"
)

type enterpriseSCIMGroupHandler struct {
	app                 core.App
	organizationID      string
	diagnosticReference string
}

type storedSCIMGroup struct {
	ID               string `db:"id"`
	OrganizationID   string `db:"organization_id"`
	DisplayName      string `db:"display_name"`
	NormalizedName   string `db:"normalized_name"`
	ExternalID       string `db:"external_id"`
	ActiveGeneration int    `db:"active_generation"`
	CurrentPayload   string `db:"current_payload_hash"`
	DeletedAt        int64  `db:"deleted_at"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
}

var scimPendingMutationConflictResponse = scimErrors.ScimError{
	Detail: "A different mutation is already in progress for this group. Wait for the identified operation to complete, then retry.",
	Status: http.StatusConflict,
}

func (handler *enterpriseSCIMGroupHandler) Create(
	request *http.Request,
	attributes scim.ResourceAttributes,
) (scim.Resource, error) {
	payload, err := scimGroupPayload(attributes)
	if err != nil {
		return scim.Resource{}, err
	}
	if handler.groupNameExists(payload.DisplayName, "") {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorUniqueness, "Another group already has this displayName.")
	}
	groupID, err := newSCIMRecordID("lsg_")
	if err != nil {
		return scim.Resource{}, handler.storageError("group-id", err)
	}
	operationID, err := beginEnterpriseSCIMGroupOperation(
		handler.app, handler.organizationID, groupID, payload, true,
	)
	if err != nil {
		return scim.Resource{}, handler.storageError("group-create-admit", err)
	}
	turn, err := continueEnterpriseSCIMOperationContext(request.Context(), handler.app, operationID)
	if err != nil {
		return scim.Resource{}, handler.storageError("group-create-continue", err)
	}
	recordSCIMOperationTurn(request, turn)
	return handler.Get(nil, groupID)
}

func (handler *enterpriseSCIMGroupHandler) Get(_ *http.Request, id string) (scim.Resource, error) {
	group, err := handler.group(id)
	if err != nil {
		return scim.Resource{}, err
	}
	return handler.resource(group)
}

func (handler *enterpriseSCIMGroupHandler) GetAll(
	_ *http.Request,
	params scim.ListRequestParams,
) (scim.Page, error) {
	page, err := loadSCIMGroupList(handler.app, handler.organizationID, params)
	if err != nil {
		return scim.Page{}, handler.storageError("group-list", err)
	}
	return page, nil
}

func (handler *enterpriseSCIMGroupHandler) Replace(
	request *http.Request,
	id string,
	attributes scim.ResourceAttributes,
) (scim.Resource, error) {
	group, err := handler.group(id)
	if err != nil {
		return scim.Resource{}, err
	}
	payload, err := scimGroupPayload(attributes)
	if err != nil {
		return scim.Resource{}, err
	}
	if handler.groupNameExists(payload.DisplayName, id) {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorUniqueness, "Another group already has this displayName.")
	}
	if payload.ExternalID == "" {
		payload.ExternalID = group.ExternalID
	}
	return handler.apply(request, id, payload)
}

func (handler *enterpriseSCIMGroupHandler) Delete(request *http.Request, id string) error {
	group, err := handler.group(id)
	if err != nil {
		return err
	}
	payload := leafen.SCIMGroupMutationPayload{
		DisplayName: group.DisplayName, ExternalID: group.ExternalID, MemberIDs: []string{}, Deleted: true,
	}
	_, err = handler.apply(request, id, payload)
	return err
}

func (handler *enterpriseSCIMGroupHandler) Patch(
	request *http.Request,
	id string,
	operations []scim.PatchOperation,
) (scim.Resource, error) {
	if len(operations) > leafen.SCIMMaxPatchOperations {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorTooMany, fmt.Sprintf("A patch takes at most %d operations.", leafen.SCIMMaxPatchOperations))
	}
	group, err := handler.group(id)
	if err != nil {
		return scim.Resource{}, err
	}
	members, err := handler.memberIDs(group)
	if err != nil {
		return scim.Resource{}, handler.storageError("group-patch-members", err)
	}
	payload := leafen.SCIMGroupMutationPayload{
		DisplayName: group.DisplayName, ExternalID: group.ExternalID, MemberIDs: members,
	}
	for _, operation := range operations {
		if operation.Path == nil {
			values, ok := operation.Value.(map[string]interface{})
			if !ok || operation.Op == scim.PatchOperationRemove {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidPath, "A patch without path adds or replaces an object of attributes.")
			}
			if value, present := values["displayName"]; present {
				parsed, ok := value.(string)
				if !ok {
					return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName must be a string.")
				}
				payload.DisplayName = strings.TrimSpace(parsed)
			}
			if value, present := values["externalId"]; present {
				parsed, ok := value.(string)
				if !ok {
					return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "externalId must be a string.")
				}
				payload.ExternalID = strings.TrimSpace(parsed)
			}
			if value, present := values["members"]; present {
				parsed, err := scimGroupMemberIDs(value)
				if err != nil {
					return scim.Resource{}, err
				}
				if operation.Op == scim.PatchOperationAdd {
					payload.MemberIDs = append(payload.MemberIDs, parsed...)
				} else {
					payload.MemberIDs = parsed
				}
			}
			continue
		}
		switch operation.Path.AttributePath.AttributeName {
		case "displayName":
			value, ok := operation.Value.(string)
			if !ok || operation.Op == scim.PatchOperationRemove {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName must be a string and can't be removed.")
			}
			payload.DisplayName = strings.TrimSpace(value)
		case "externalId":
			if operation.Op == scim.PatchOperationRemove {
				payload.ExternalID = ""
				continue
			}
			value, ok := operation.Value.(string)
			if !ok {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "externalId must be a string.")
			}
			payload.ExternalID = strings.TrimSpace(value)
		case "members":
			values, err := scimGroupPatchMemberIDs(operation)
			if err != nil {
				return scim.Resource{}, err
			}
			switch operation.Op {
			case scim.PatchOperationReplace:
				payload.MemberIDs = values
			case scim.PatchOperationAdd:
				payload.MemberIDs = append(payload.MemberIDs, values...)
			case scim.PatchOperationRemove:
				payload.MemberIDs = removeSCIMMemberIDs(payload.MemberIDs, values)
			}
		default:
			return scim.Resource{}, scimRefusal(scimErrors.ScimErrorMutability, "A group's patch changes displayName, externalId or members.")
		}
	}
	if payload.DisplayName == "" {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName can't be empty.")
	}
	if handler.groupNameExists(payload.DisplayName, id) {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorUniqueness, "Another group already has this displayName.")
	}
	return handler.apply(request, id, payload)
}

func (handler *enterpriseSCIMGroupHandler) apply(
	request *http.Request,
	id string,
	payload leafen.SCIMGroupMutationPayload,
) (scim.Resource, error) {
	operationID, err := beginEnterpriseSCIMGroupOperation(
		handler.app, handler.organizationID, id, payload, false,
	)
	if err != nil {
		var conflict scimPendingOperationConflict
		if errors.As(err, &conflict) {
			recordSCIMOperationConflict(request, scimOperationTurnFromStored(conflict.Operation))
			return scim.Resource{}, scimPendingMutationConflictResponse
		}
		return scim.Resource{}, handler.storageError("group-operation-admit", err)
	}
	turn, err := continueEnterpriseSCIMOperationContext(request.Context(), handler.app, operationID)
	if err != nil {
		return scim.Resource{}, handler.storageError("group-operation-continue", err)
	}
	recordSCIMOperationTurn(request, turn)
	if payload.Deleted {
		return scim.Resource{}, nil
	}
	return handler.Get(nil, id)
}

func (handler *enterpriseSCIMGroupHandler) group(id string) (storedSCIMGroup, error) {
	row := storedSCIMGroup{}
	err := handler.app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[display_name]], [[normalized_name]],
			[[external_id]], [[active_generation]], [[current_payload_hash]],
			[[deleted_at]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_groups}}
		WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId} AND [[deleted_at]] = 0
	`).Bind(dbx.Params{"id": id, "organizationId": handler.organizationID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedSCIMGroup{}, scimErrors.ScimErrorResourceNotFound(id)
	}
	if err != nil {
		return storedSCIMGroup{}, handler.storageError("group-get", err)
	}
	return row, nil
}

func (handler *enterpriseSCIMGroupHandler) resource(group storedSCIMGroup) (scim.Resource, error) {
	members, err := handler.memberIDs(group)
	if err != nil {
		return scim.Resource{}, err
	}
	return scimGroupResource(group, members), nil
}

func (handler *enterpriseSCIMGroupHandler) memberIDs(group storedSCIMGroup) ([]string, error) {
	rows := []struct {
		ID string `db:"organization_member_id"`
	}{}
	err := handler.app.DB().NewQuery(`
		SELECT [[organization_member_id]] FROM {{leafen_scim_group_members}}
		WHERE [[group_id]] = {:groupId} AND [[generation]] = {:generation}
		ORDER BY [[organization_member_id]]
	`).Bind(dbx.Params{"groupId": group.ID, "generation": group.ActiveGeneration}).All(&rows)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ID)
	}
	return result, err
}

func (handler *enterpriseSCIMGroupHandler) groupNameExists(displayName string, exceptID string) bool {
	var count int
	err := handler.app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_scim_groups}}
		WHERE [[organization_id]] = {:organizationId} AND [[normalized_name]] = {:name}
			AND [[deleted_at]] = 0 AND [[id]] != {:exceptId}
	`).Bind(dbx.Params{
		"organizationId": handler.organizationID, "name": normalizeSCIMGroupName(displayName),
		"exceptId": exceptID,
	}).Row(&count)
	return err == nil && count > 0
}

func scimGroupPayload(attributes scim.ResourceAttributes) (leafen.SCIMGroupMutationPayload, error) {
	displayName, ok := scimStringAttribute(attributes, "displayName")
	if !ok {
		return leafen.SCIMGroupMutationPayload{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName is required.")
	}
	externalID, _ := scimStringAttribute(attributes, "externalId")
	members, err := scimGroupMemberIDs(attributes["members"])
	if err != nil {
		return leafen.SCIMGroupMutationPayload{}, err
	}
	return leafen.SCIMGroupMutationPayload{
		DisplayName: displayName, ExternalID: externalID, MemberIDs: members,
	}, nil
}

func scimGroupMemberIDs(value interface{}) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	values, ok := value.([]interface{})
	if !ok {
		return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "members must be a list.")
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		member, ok := raw.(map[string]interface{})
		id, idOK := member["value"].(string)
		if !ok || !idOK || strings.TrimSpace(id) == "" {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "Each member needs value: the id of a user this connection manages.")
		}
		result = append(result, strings.TrimSpace(id))
	}
	return leafen.UniqueSortedSCIMMemberIDs(result), nil
}

func scimGroupPatchMemberIDs(operation scim.PatchOperation) ([]string, error) {
	if operation.Op == scim.PatchOperationRemove && operation.Value == nil &&
		operation.Path != nil && operation.Path.ValueExpression != nil {
		expression, ok := operation.Path.ValueExpression.(*filterParser.AttributeExpression)
		if !ok || expression.Operator != filterParser.EQ || expression.AttributePath.AttributeName != "value" {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidPath, "The path can filter members only by value eq.")
		}
		value, ok := expression.CompareValue.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "The members filter compares value with a non-empty string.")
		}
		return []string{strings.TrimSpace(value)}, nil
	}
	return scimGroupMemberIDs(operation.Value)
}

func removeSCIMMemberIDs(current []string, removed []string) []string {
	remove := make(map[string]struct{}, len(removed))
	for _, id := range removed {
		remove[id] = struct{}{}
	}
	result := make([]string, 0, len(current))
	for _, id := range current {
		if _, exists := remove[id]; !exists {
			result = append(result, id)
		}
	}
	return result
}

func normalizeSCIMGroupName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func (handler *enterpriseSCIMGroupHandler) storageError(stage string, err error) error {
	return normalizeSCIMStorageError(handler.app, handler.diagnosticReference, stage, err)
}
