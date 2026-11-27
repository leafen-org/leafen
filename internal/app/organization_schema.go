package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationsCollection           = "leafen_organizations"
	organizationMembershipsCollection = "leafen_organization_memberships"
	organizationPolicyStateTable      = "leafen_organization_policy_state"
	organizationAuditEventsTable      = "leafen_organization_audit_events"
)

// ensureOwnerOrganization is the owner's organization, made where they have
// none and named in the language given (defaultOrganizationName).
func ensureOwnerOrganization(app core.App, ownerID, language string) (*core.Record, error) {
	organizations, err := app.FindCollectionByNameOrId(organizationsCollection)
	if err != nil {
		return nil, err
	}
	organization, err := app.FindFirstRecordByData(organizations, "owner", ownerID)
	created := false
	if errors.Is(err, sql.ErrNoRows) {
		owner, findErr := app.FindRecordById("users", ownerID)
		if findErr != nil {
			return nil, findErr
		}
		organization = core.NewRecord(organizations)
		organization.Set("owner", ownerID)
		organization.Set("name", defaultOrganizationName(owner, language))
		if err := app.Save(organization); err != nil {
			return nil, err
		}
		created = true
	} else if err != nil {
		return nil, err
	}
	if organization.GetString("owner") != ownerID {
		return nil, errors.New("organization owner mismatch")
	}
	if err := ensureOrganizationMembership(
		app, organization.Id, ownerID, organizationRoleOwner, organizationMemberStatusActive,
	); err != nil {
		return nil, err
	}
	if err := ensureOrganizationPolicyState(app, organization.Id); err != nil {
		return nil, err
	}
	if err := ensureOrganizationSharingSecurityPolicy(
		app, organization.Id, time.Now().UTC().UnixMilli(),
	); err != nil {
		return nil, err
	}
	if created {
		if err := ensureOrganizationStorageUsageForNewOrganization(
			app, organization.Id, time.Now().UTC().UnixMilli(),
		); err != nil {
			return nil, err
		}
	}
	if created {
		if err := recordOrganizationAudit(
			app, organization.Id, ownerID, "organization.created", "organization", organization.Id,
		); err != nil {
			return nil, err
		}
	}
	return organization, nil
}

// defaultOrganizationName names a new organization after its owner in the
// language the server names things in (generatedNameLanguage), as their first
// workspace is ("〈이름〉님의 워크스페이스"; 2026-10-19: "〈이름〉
// Organization" stood in Korean settings; 2026-10-27: the first setup made
// it before the chosen language was saved, in English).
func defaultOrganizationName(owner *core.Record, language string) string {
	name := strings.TrimSpace(owner.GetString("name"))
	if name == "" {
		name = strings.TrimSpace(strings.SplitN(owner.GetString("email"), "@", 2)[0])
	}
	korean := language == accountLanguageKo
	switch {
	case korean && name == "":
		return "개인 조직"
	case korean:
		return name + "님의 조직"
	case name == "":
		return "Personal Organization"
	default:
		return name + "’s Organization"
	}
}

func ensureOrganizationMembership(app core.App, organizationID, userID, role, status string) error {
	collection, err := app.FindCollectionByNameOrId(organizationMembershipsCollection)
	if err != nil {
		return err
	}
	record, err := app.FindFirstRecordByFilter(
		collection, "organization = {:organization} && user = {:user}",
		dbx.Params{"organization": organizationID, "user": userID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		record = core.NewRecord(collection)
		record.Set("organization", organizationID)
		record.Set("user", userID)
		record.Set("role", role)
		record.Set("status", status)
		return app.Save(record)
	}
	if err != nil {
		return err
	}
	if record.GetString("role") != role || record.GetString("status") != status {
		return fmt.Errorf("organization membership conflicts with required %s/%s", role, status)
	}
	return nil
}

func ensureOrganizationPolicyState(app core.App, organizationID string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_policy_state}} ([[organization_id]], [[version]], [[updated]])
		VALUES ({:organizationId}, 0, {:updated})
		ON CONFLICT ([[organization_id]]) DO NOTHING
	`).Bind(dbx.Params{"organizationId": organizationID, "updated": time.Now().UTC().UnixMilli()}).Execute()
	return err
}

func organizationAuditID(organizationID, action, subjectID string) string {
	digest := sha256.Sum256([]byte(organizationID + "\x00" + action + "\x00" + subjectID))
	return hex.EncodeToString(digest[:16])
}

// organizationAuditEventID names one audit event of its own; callers that must
// fold a retried write into one row pass an explicit ID instead.
func organizationAuditEventID(organizationID, action, subjectID string, created int64) string {
	return organizationAuditID(
		organizationID, action,
		subjectID+"@"+strconv.FormatInt(created, 10)+"#"+core.GenerateDefaultRandomId(),
	)
}

func recordOrganizationAudit(
	app core.App,
	organizationID, actorID, action, subjectType, subjectID string,
) error {
	return recordOrganizationAuditAt(
		app, organizationID, actorID, action, subjectType, subjectID,
		time.Now().UTC().UnixMilli(),
	)
}

func recordOrganizationAuditAt(
	app core.App,
	organizationID, actorID, action, subjectType, subjectID string,
	created int64,
) error {
	return recordOrganizationAuditAtContext(
		context.Background(), app, organizationID, actorID, action,
		subjectType, subjectID, created,
	)
}

func recordOrganizationAuditAtContext(
	ctx context.Context,
	app core.App,
	organizationID, actorID, action, subjectType, subjectID string,
	created int64,
) error {
	// Each change is its own event, a repeated change to the same subject too,
	// even two in the same millisecond. Every write sits in the transaction of
	// the change it records, so a retry never writes it twice.
	id := organizationAuditEventID(organizationID, action, subjectID, created)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_audit_events}}
			([[id]], [[organization_id]], [[actor_id]], [[action]], [[subject_type]], [[subject_id]], [[created]])
		VALUES ({:id}, {:organizationId}, {:actorId}, {:action}, {:subjectType}, {:subjectId}, {:created})
		ON CONFLICT ([[id]]) DO NOTHING
	`).WithContext(ctx).Bind(dbx.Params{
		"id": id, "organizationId": organizationID,
		"actorId": actorID, "action": action, "subjectType": subjectType, "subjectId": subjectID,
		"created": created,
	}).Execute()
	return err
}

func recordOrganizationAuditEvent(app core.App, event organizationAuditEvent) error {
	return recordOrganizationAuditEventContext(context.Background(), app, event)
}

func recordOrganizationAuditEventContext(
	ctx context.Context,
	app core.App,
	event organizationAuditEvent,
) error {
	return insertOrganizationAuditEventContext(ctx, app, event)
}
