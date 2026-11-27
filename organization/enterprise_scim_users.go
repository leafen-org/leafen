package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	filterParser "github.com/scim2/filter-parser/v2"
)

// The shared authority owners for this adapter are organizationMembershipsCollection,
// organizationPolicyStateTable and organizationAuditEventsTable. SCIM does not
// create a parallel account, role, session or policy authority.
type enterpriseSCIMUserHandler struct {
	app                 core.App
	organizationID      string
	diagnosticReference string
}

func (handler *enterpriseSCIMUserHandler) Create(
	request *http.Request,
	attributes scim.ResourceAttributes,
) (scim.Resource, error) {
	ctx := context.Background()
	if request != nil {
		ctx = request.Context()
	}
	email, ok := scimStringAttribute(attributes, "userName")
	if !ok {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "userName is required: the user's email address.")
	}
	email, ok = leafen.NormalizeEmailIdentity(email)
	if !ok {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "userName must be an email address.")
	}
	if err := requireVerifiedOrganizationDomain(handler.app, handler.organizationID, email); errors.Is(err, errOrganizationDomainNotVerified) {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, scimUnverifiedDomainDetail(email))
	} else if err != nil {
		return scim.Resource{}, err
	}
	name, _ := scimStringAttribute(attributes, "displayName")
	externalID, _ := scimStringAttribute(attributes, "externalId")
	active := scimBoolAttribute(attributes, "active", true)
	var emailAddresses []leafen.AccountEmailAddress
	if value, present := attributes["emails"]; present {
		var err error
		emailAddresses, err = parseSCIMEmails(value)
		if err != nil {
			return scim.Resource{}, err
		}
	}
	var membership *core.Record
	unknown := scimUnknownPassword(handler.app, email)
	err := handler.app.RunInTransaction(func(txApp core.App) error {
		user, err := findOrCreateSCIMUser(txApp, email, name, unknown)
		if err != nil {
			return err
		}
		if emailAddresses != nil {
			state, err := leafen.AccountEmailSnapshot(txApp, user)
			if err != nil {
				return err
			}
			if err := handler.applyEmails(txApp, user, &scimEmailPlan{Addresses: emailAddresses, Revision: state.Revision}); err != nil {
				return err
			}
		}
		if name != "" && user.GetString("name") != name {
			if _, err := leafen.MutateAccountProfileAuthority(
				txApp, user, name, user.GetString("profile_avatar"),
			); err != nil {
				return err
			}
		}
		collection, err := txApp.FindCollectionByNameOrId(leafen.OrganizationMembershipsCollection)
		if err != nil {
			return err
		}
		existing, err := txApp.FindFirstRecordByFilter(
			collection, "organization = {:organization} && user = {:user}",
			dbx.Params{"organization": handler.organizationID, "user": user.Id},
		)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && existing != nil {
			// A member who came by an invitation or company sign-in comes
			// under the directory, as Notion's SCIM takes an existing account,
			// so the directory can end their access later. One it already
			// manages is a duplicate, and an owner or a guest stays as they are.
			if existing.GetBool("scim_managed") || existing.GetString("role") == leafen.OrganizationRoleOwner ||
				existing.GetString("role") == leafen.OrganizationRoleGuest {
				return scimRefusal(scimErrors.ScimErrorUniqueness, "This user is already a member of this organization.")
			}
			membership = existing
		} else {
			id, err := newSCIMRecordID("lom_")
			if err != nil {
				return err
			}
			membership = core.NewRecord(collection)
			membership.Id = id
			membership.Set("organization", handler.organizationID)
			membership.Set("user", user.Id)
			membership.Set("role", leafen.OrganizationRoleMember)
		}
		membership.Set("status", scimMembershipStatus(active))
		membership.Set("scim_external_id", externalID)
		membership.Set("scim_managed", true)
		if err := txApp.Save(membership); err != nil {
			return err
		}
		if err := leafen.ActivateSCIMListResource(
			ctx, txApp, handler.organizationID, leafen.SCIMListAccessResourceTypeUser,
			leafen.SCIMListAccessAllFilterKey, "", membership.Id,
		); err != nil {
			return err
		}
		if !active {
			if err := leafen.RevokeAllBrowserSessions(txApp, user.Id); err != nil {
				return err
			}
		}
		now := time.Now().UTC().UnixMilli()
		if err := leafen.BumpOrganizationPolicy(txApp, handler.organizationID, now); err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, handler.organizationID, "scim", "organization.scim.user.created",
			"organization_membership", membership.Id,
		)
	})
	if err != nil {
		return scim.Resource{}, handler.storageError("user-create", err)
	}
	return handler.resource(membership)
}

func (handler *enterpriseSCIMUserHandler) Get(_ *http.Request, id string) (scim.Resource, error) {
	membership, err := handler.membership(id)
	if err != nil {
		return scim.Resource{}, handler.storageError("user-get", err)
	}
	return handler.resource(membership)
}

func (handler *enterpriseSCIMUserHandler) GetAll(
	_ *http.Request,
	params scim.ListRequestParams,
) (scim.Page, error) {
	if params.FilterValidator != nil {
		resource, found, err := handler.filteredUser(params.FilterValidator.GetFilter())
		if err != nil {
			return scim.Page{}, err
		}
		resources := []scim.Resource{}
		if found && params.Count > 0 && params.StartIndex <= 1 {
			resources = append(resources, resource)
		}
		return scim.Page{TotalResults: leafen.BoolInt(found), Resources: resources}, nil
	}
	page, err := loadSCIMUserList(handler.app, handler.organizationID, params)
	if err != nil {
		return scim.Page{}, handler.storageError("user-list", err)
	}
	return page, nil
}

func (handler *enterpriseSCIMUserHandler) Replace(
	_ *http.Request,
	id string,
	attributes scim.ResourceAttributes,
) (scim.Resource, error) {
	membership, err := handler.membership(id)
	if err != nil {
		return scim.Resource{}, handler.storageError("user-replace-load", err)
	}
	user, err := handler.app.FindRecordById("users", membership.GetString("user"))
	if err != nil {
		return scim.Resource{}, handler.storageError("user-replace-account", err)
	}
	email, ok := scimStringAttribute(attributes, "userName")
	if !ok || !strings.EqualFold(strings.TrimSpace(email), user.Email()) {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorMutability, "userName can't change; it is the user's email address.")
	}
	name, _ := scimStringAttribute(attributes, "displayName")
	externalID, _ := scimStringAttribute(attributes, "externalId")
	active := scimBoolAttribute(attributes, "active", true)
	var emailPlan *scimEmailPlan
	if value, present := attributes["emails"]; present {
		emailPlan, err = handler.emailPlan(user, value)
		if err != nil {
			return scim.Resource{}, err
		}
	}
	changed, err := handler.updateMembership(membership, user, active, name, externalID, emailPlan)
	if err != nil {
		return scim.Resource{}, err
	}
	if !changed {
		return scim.Resource{}, nil
	}
	return handler.resource(membership)
}

func (handler *enterpriseSCIMUserHandler) Delete(_ *http.Request, id string) error {
	membership, err := handler.membership(id)
	if err != nil {
		return handler.storageError("user-delete-load", err)
	}
	user, err := handler.app.FindRecordById("users", membership.GetString("user"))
	if err != nil {
		return handler.storageError("user-delete-account", err)
	}
	_, err = handler.updateMembership(membership, user, false, user.GetString("name"), membership.GetString("scim_external_id"))
	return err
}

func (handler *enterpriseSCIMUserHandler) Patch(
	_ *http.Request,
	id string,
	operations []scim.PatchOperation,
) (scim.Resource, error) {
	if len(operations) > leafen.SCIMMaxPatchOperations {
		return scim.Resource{}, scimRefusal(scimErrors.ScimErrorTooMany, fmt.Sprintf("A patch takes at most %d operations.", leafen.SCIMMaxPatchOperations))
	}
	membership, err := handler.membership(id)
	if err != nil {
		return scim.Resource{}, handler.storageError("user-patch-load", err)
	}
	user, err := handler.app.FindRecordById("users", membership.GetString("user"))
	if err != nil {
		return scim.Resource{}, handler.storageError("user-patch-account", err)
	}
	remaining, emailPlan, err := handler.splitEmailPatch(user, operations)
	if err != nil {
		return scim.Resource{}, err
	}
	operations = remaining
	active := membership.GetString("status") == leafen.OrganizationMemberStatusActive
	name := user.GetString("name")
	externalID := membership.GetString("scim_external_id")
	for _, operation := range operations {
		if operation.Path == nil {
			values, ok := operation.Value.(map[string]interface{})
			if !ok || operation.Op == scim.PatchOperationRemove {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidPath, "A patch without path adds or replaces an object of attributes.")
			}
			for key, value := range values {
				switch key {
				case "active":
					parsed, ok := value.(bool)
					if !ok {
						return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "active must be true or false.")
					}
					active = parsed
				case "displayName":
					parsed, ok := value.(string)
					if !ok {
						return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName must be a string.")
					}
					name = strings.TrimSpace(parsed)
				case "externalId":
					parsed, ok := value.(string)
					if !ok {
						return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "externalId must be a string.")
					}
					externalID = strings.TrimSpace(parsed)
				case "userName":
					parsed, ok := value.(string)
					if !ok || !strings.EqualFold(strings.TrimSpace(parsed), user.Email()) {
						return scim.Resource{}, scimRefusal(scimErrors.ScimErrorMutability, "userName can't change; it is the user's email address.")
					}
				default:
					return scim.Resource{}, scimRefusal(scimErrors.ScimErrorMutability, "A user's patch changes active, displayName, externalId or emails.")
				}
			}
			continue
		}
		switch operation.Path.AttributePath.AttributeName {
		case "active":
			if operation.Op == scim.PatchOperationRemove {
				active = true
				continue
			}
			value, ok := operation.Value.(bool)
			if !ok {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "active must be true or false.")
			}
			active = value
		case "displayName":
			if operation.Op == scim.PatchOperationRemove {
				name = ""
				continue
			}
			value, ok := operation.Value.(string)
			if !ok {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "displayName must be a string.")
			}
			name = strings.TrimSpace(value)
		case "externalId":
			if operation.Op == scim.PatchOperationRemove {
				externalID = ""
				continue
			}
			value, ok := operation.Value.(string)
			if !ok {
				return scim.Resource{}, scimRefusal(scimErrors.ScimErrorInvalidValue, "externalId must be a string.")
			}
			externalID = strings.TrimSpace(value)
		default:
			return scim.Resource{}, scimRefusal(scimErrors.ScimErrorMutability, "A user's patch changes active, displayName, externalId or emails.")
		}
	}
	changed, err := handler.updateMembership(membership, user, active, name, externalID, emailPlan)
	if err != nil {
		return scim.Resource{}, err
	}
	if !changed {
		return scim.Resource{}, nil
	}
	return handler.resource(membership)
}

func (handler *enterpriseSCIMUserHandler) membership(id string) (*core.Record, error) {
	record, err := handler.app.FindRecordById(leafen.OrganizationMembershipsCollection, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil &&
		(record.GetString("organization") != handler.organizationID || !record.GetBool("scim_managed"))) {
		return nil, scimErrors.ScimErrorResourceNotFound(id)
	}
	return record, err
}

func (handler *enterpriseSCIMUserHandler) resource(membership *core.Record) (scim.Resource, error) {
	return loadSCIMUserResource(handler.app, handler.organizationID, membership.Id)
}

func (handler *enterpriseSCIMUserHandler) updateMembership(
	membership *core.Record,
	user *core.Record,
	active bool,
	name string,
	externalID string,
	emailPlans ...*scimEmailPlan,
) (bool, error) {
	var emailPlan *scimEmailPlan
	if len(emailPlans) > 0 {
		emailPlan = emailPlans[0]
	}
	wantedStatus := scimMembershipStatus(active)
	name = strings.TrimSpace(name)
	externalID = strings.TrimSpace(externalID)
	changed := membership.GetString("status") != wantedStatus ||
		membership.GetString("scim_external_id") != externalID || user.GetString("name") != name || emailPlan != nil
	if !changed {
		return false, nil
	}
	err := handler.app.RunInTransaction(func(txApp core.App) error {
		storedMembership, err := txApp.FindRecordById(leafen.OrganizationMembershipsCollection, membership.Id)
		if err != nil {
			return err
		}
		storedUser, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		if err := handler.applyEmails(txApp, storedUser, emailPlan); err != nil {
			return err
		}
		storedMembership.Set("status", wantedStatus)
		storedMembership.Set("scim_external_id", externalID)
		if _, err := leafen.MutateAccountProfileAuthority(
			txApp, storedUser, name, storedUser.GetString("profile_avatar"),
		); err != nil {
			return err
		}
		if err := txApp.Save(storedMembership); err != nil {
			return err
		}
		if !active && membership.GetString("status") == leafen.OrganizationMemberStatusActive {
			if err := leafen.RevokeAllBrowserSessions(txApp, user.Id); err != nil {
				return err
			}
		}
		now := time.Now().UTC().UnixMilli()
		if err := leafen.BumpOrganizationPolicy(txApp, handler.organizationID, now); err != nil {
			return err
		}
		// The line names whom it changed, by account too (the name goes from
		// the line with the account), and, when it turned them off or on, the
		// status before and after (2026-11-12: it named no one).
		metadata := map[string]any{"name": name, "userId": storedUser.Id}
		if name == "" {
			metadata["name"] = storedUser.GetString("email")
		}
		if was := membership.GetString("status"); was != wantedStatus {
			metadata["from"], metadata["to"] = was, wantedStatus
		}
		if was := user.GetString("name"); was != name {
			metadata["renamedFrom"] = was
		}
		return leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			OrganizationID: handler.organizationID, ActorID: "scim", Action: "organization.scim.user.updated",
			TargetType: "organization_membership", TargetID: membership.Id, Metadata: metadata, OccurredAt: now,
		})
	})
	if err != nil {
		return false, handler.storageError("user-update", err)
	}
	refreshed, err := handler.app.FindRecordById(leafen.OrganizationMembershipsCollection, membership.Id)
	if err == nil {
		*membership = *refreshed
	}
	return true, err
}

func (handler *enterpriseSCIMUserHandler) filteredUser(expression filterParser.Expression) (scim.Resource, bool, error) {
	attribute, ok := expression.(*filterParser.AttributeExpression)
	if !ok || attribute.Operator != filterParser.EQ {
		return scim.Resource{}, false, scimRefusal(scimErrors.ScimErrorTooMany, "The filter takes one eq comparison.")
	}
	value, ok := attribute.CompareValue.(string)
	if !ok {
		return scim.Resource{}, false, scimRefusal(scimErrors.ScimErrorInvalidFilter, "The filter compares with a string.")
	}
	var membership *core.Record
	var err error
	switch attribute.AttributePath.AttributeName {
	case "id":
		membership, err = handler.membership(value)
	case "externalId":
		if value == "" {
			// An empty externalId finds no user, though PocketBase's filter
			// matches it to the users without one (since v0.40.2).
			return scim.Resource{}, false, nil
		}
		membership, err = handler.app.FindFirstRecordByFilter(
			leafen.OrganizationMembershipsCollection,
			"organization = {:organization} && scim_managed = true && scim_external_id = {:externalId}",
			dbx.Params{"organization": handler.organizationID, "externalId": value},
		)
	case "userName":
		email, valid := leafen.NormalizeEmailIdentity(value)
		if !valid {
			return scim.Resource{}, false, nil
		}
		user, findErr := leafen.FindAccountByEmail(handler.app, email)
		if findErr != nil {
			err = findErr
			break
		}
		membership, err = handler.app.FindFirstRecordByFilter(
			leafen.OrganizationMembershipsCollection,
			"organization = {:organization} && user = {:user} && scim_managed = true",
			dbx.Params{"organization": handler.organizationID, "user": user.Id},
		)
	default:
		return scim.Resource{}, false, scimRefusal(scimErrors.ScimErrorTooMany, "The filter takes id, userName or externalId.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return scim.Resource{}, false, nil
	}
	if err != nil {
		return scim.Resource{}, false, handler.storageError("user-filter", err)
	}
	resource, err := handler.resource(membership)
	return resource, err == nil, err
}

func (handler *enterpriseSCIMUserHandler) storageError(stage string, err error) error {
	return normalizeSCIMStorageError(handler.app, handler.diagnosticReference, stage, err)
}
