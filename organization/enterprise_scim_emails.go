package organization

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	filter "github.com/scim2/filter-parser/v2"
)

type scimEmailPlan struct {
	Addresses []leafen.AccountEmailAddress
	Revision  string
}

func parseSCIMEmails(value any) ([]leafen.AccountEmailAddress, error) {
	values, ok := value.([]interface{})
	if !ok || len(values) > leafen.AccountSecondaryEmailLimit+1 {
		return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, fmt.Sprintf("emails must be a list of at most %d addresses.", leafen.AccountSecondaryEmailLimit+1))
	}
	addresses := []leafen.AccountEmailAddress{}
	seen := map[string]bool{}
	primary := false
	for _, raw := range values {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "Each entry of emails must be an object.")
		}
		address, ok := entry["value"].(string)
		if !ok {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "Each entry of emails needs value: an email address.")
		}
		email, valid := leafen.NormalizeEmailIdentity(address)
		if !valid || seen[email] {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "Each entry of emails needs a valid email address, each once.")
		}
		isPrimary, _ := entry["primary"].(bool)
		if isPrimary && primary {
			return nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "Only one entry of emails can be primary.")
		}
		primary = primary || isPrimary
		seen[email] = true
		addresses = append(addresses, leafen.AccountEmailAddress{Email: email, Primary: isPrimary, Verified: true})
	}
	return addresses, nil
}

func (handler *enterpriseSCIMUserHandler) emailPlan(user *core.Record, value any) (*scimEmailPlan, error) {
	addresses, err := parseSCIMEmails(value)
	if err != nil {
		return nil, err
	}
	state, err := leafen.AccountEmailSnapshot(handler.app, user)
	if err != nil {
		return nil, err
	}
	return &scimEmailPlan{Addresses: addresses, Revision: state.Revision}, nil
}

func (handler *enterpriseSCIMUserHandler) applyEmails(app core.App, user *core.Record, plan *scimEmailPlan) error {
	if plan == nil {
		return nil
	}
	if _, err := leafen.CurrentAccountEmailMutation(app, user.Id, plan.Revision); err != nil {
		return scimRefusal(scimErrors.ScimErrorInvalidValue, "The user's email addresses changed while this request ran; send it again.")
	}
	addresses, err := leafen.VerifiedAccountEmails(app, user.Id)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	primary := leafen.NormalizePageShareEmail(user.Email())
	for _, address := range plan.Addresses {
		if err := requireVerifiedOrganizationDomain(app, handler.organizationID, address.Email); errors.Is(err, errOrganizationDomainNotVerified) {
			return scimRefusal(scimErrors.ScimErrorInvalidValue, scimUnverifiedDomainDetail(address.Email))
		} else if err != nil {
			return err
		}
		wanted[address.Email] = true
		if address.Primary {
			primary = address.Email
		}
	}
	if !wanted[leafen.NormalizePageShareEmail(user.Email())] && primary == leafen.NormalizePageShareEmail(user.Email()) &&
		requireVerifiedOrganizationDomain(app, handler.organizationID, primary) == nil {
		return scimRefusal(scimErrors.ScimErrorMutability, "The user's primary email is in a verified domain and can't be removed; give another primary first.")
	}
	// Only this organization's verified addresses are mutable by its SCIM
	// credential. Another organization's and personal addresses stay intact.
	for _, email := range addresses {
		if email == leafen.NormalizePageShareEmail(user.Email()) || wanted[email] || requireVerifiedOrganizationDomain(app, handler.organizationID, email) != nil {
			continue
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND [[email]] = {:email}`).
			Bind(dbx.Params{"userId": user.Id, "email": email}).Execute(); err != nil {
			return err
		}
		if err := leafen.AuditAccountEmailChange(app, user, email, "remove"); err != nil {
			return err
		}
	}
	if primary != leafen.NormalizePageShareEmail(user.Email()) {
		old := leafen.NormalizePageShareEmail(user.Email())
		retained, err := retainedManagedPrimary(app, user, handler.organizationID, wanted[old])
		if err != nil {
			return scimRefusal(scimErrors.ScimErrorMutability, "The user's current primary email can't be given up here.")
		}
		if owner, err := leafen.FindAccountByEmail(app, primary); err == nil && owner.Id != user.Id {
			return scimRefusal(scimErrors.ScimErrorUniqueness, "Another account already uses this primary email.")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Replacing a full set's primary frees the old claim in the users
		// trigger before additions are counted. The transaction rolls back
		// if the final set would exceed the shared limit.
		user.SetEmail(primary)
		user.SetVerified(true)
		if err := app.Save(user); err != nil {
			return err
		}
		if retained != "" {
			if err := leafen.AddVerifiedAccountEmail(app, user, retained); err != nil {
				return err
			}
		}
		if err := leafen.RevokeAllBrowserSessions(app, user.Id); err != nil {
			return err
		}
	}
	for _, address := range plan.Addresses {
		if err := leafen.AddVerifiedAccountEmail(app, user, address.Email); err != nil {
			if errors.Is(err, leafen.ErrRegistrationAccountExists) {
				return scimRefusal(scimErrors.ScimErrorUniqueness, "Another account already uses one of these email addresses.")
			}
			return err
		}
	}
	if err := leafen.AccountEmailsChanged(app, user); err != nil {
		return err
	}
	return leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{OrganizationID: handler.organizationID, ActorID: "scim",
		Action: "account_email.scim", TargetType: "user", TargetID: user.Id, OccurredAt: time.Now().UTC().UnixMilli()})
}

// Whole-field add/replace and filtered removal/primary changes are parsed
// into one bounded final plan; membership and identity commit together.
func (handler *enterpriseSCIMUserHandler) splitEmailPatch(user *core.Record, operations []scim.PatchOperation) ([]scim.PatchOperation, *scimEmailPlan, error) {
	state, err := leafen.AccountEmailSnapshot(handler.app, user)
	if err != nil {
		return nil, nil, err
	}
	remaining := []scim.PatchOperation{}
	addresses := []leafen.AccountEmailAddress{}
	for _, address := range state.Emails {
		if requireVerifiedOrganizationDomain(handler.app, handler.organizationID, address.Email) == nil {
			addresses = append(addresses, address)
		}
	}
	changed := false
	for _, operation := range operations {
		if operation.Path == nil {
			attributes, ok := operation.Value.(map[string]interface{})
			if !ok {
				return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "A patch without path takes an object of attributes as its value.")
			}
			value, present := attributes["emails"]
			if !present {
				remaining = append(remaining, operation)
				continue
			}
			copy := map[string]interface{}{}
			for key, value := range attributes {
				if key != "emails" {
					copy[key] = value
				}
			}
			operation.Value = copy
			if len(copy) > 0 {
				remaining = append(remaining, operation)
			}
			operation.Path = &filter.Path{AttributePath: filter.AttributePath{AttributeName: "emails"}}
			operation.Value = value
		}
		if !strings.EqualFold(operation.Path.AttributePath.AttributeName, "emails") {
			remaining = append(remaining, operation)
			continue
		}
		changed = true
		if operation.Path.ValueExpression != nil {
			match, ok := operation.Path.ValueExpression.(*filter.AttributeExpression)
			if !ok || match.Operator != filter.EQ || !strings.EqualFold(match.AttributePath.AttributeName, "value") {
				return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidPath, "The path can filter emails only by value eq.")
			}
			email, ok := match.CompareValue.(string)
			if !ok {
				return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "The emails filter compares value with a string.")
			}
			found := false
			next := []leafen.AccountEmailAddress{}
			for _, address := range addresses {
				if strings.EqualFold(address.Email, strings.TrimSpace(email)) {
					found = true
					if operation.Op == scim.PatchOperationRemove && operation.Path.SubAttributeName() == "" {
						continue
					}
					if operation.Path.SubAttributeName() != "primary" || operation.Op == scim.PatchOperationRemove {
						return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidPath, "Within one email the path can only set primary.")
					}
					primary, ok := operation.Value.(bool)
					if !ok || !primary {
						return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidValue, "primary can only be set to true.")
					}
					for index := range next {
						next[index].Primary = false
					}
					address.Primary = true
				} else if found {
					address.Primary = false
				}
				next = append(next, address)
			}
			if !found {
				return nil, nil, scimRefusal(scimErrors.ScimErrorNoTarget, "No email of this user matches the path's filter.")
			}
			addresses = next
			continue
		}
		if operation.Path.SubAttributeName() != "" || operation.Path.AttributePath.SubAttribute != nil {
			return nil, nil, scimRefusal(scimErrors.ScimErrorInvalidPath, "The emails path takes no sub-attribute other than primary within a filter.")
		}
		if operation.Op == scim.PatchOperationRemove {
			addresses = []leafen.AccountEmailAddress{}
			continue
		}
		parsed, err := parseSCIMEmails(operation.Value)
		if err != nil {
			return nil, nil, err
		}
		if operation.Op == scim.PatchOperationReplace {
			addresses = parsed
			continue
		}
		for _, address := range parsed {
			found := false
			for index := range addresses {
				if address.Primary {
					addresses[index].Primary = false
				}
				if addresses[index].Email == address.Email {
					addresses[index] = address
					found = true
				}
			}
			if !found {
				addresses = append(addresses, address)
			}
		}
		if len(addresses) > leafen.AccountSecondaryEmailLimit+1 {
			return nil, nil, scimRefusal(scimErrors.ScimErrorTooMany, fmt.Sprintf("A user can have at most %d email addresses.", leafen.AccountSecondaryEmailLimit+1))
		}
	}
	if !changed {
		return remaining, nil, nil
	}
	return remaining, &scimEmailPlan{Addresses: addresses, Revision: state.Revision}, nil
}
