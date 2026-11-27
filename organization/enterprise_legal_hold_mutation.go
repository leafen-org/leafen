package organization

import (
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

type legalHoldCreateAuthority struct {
	OrganizationID string
	MutationActor  string
	CreatedBy      string
	AuditActor     string
	AuditAction    string
}

type legalHoldReleaseAuthority struct {
	OrganizationID string
	ReleaseActor   string
	AuditActor     string
	AuditAction    string
	AllowReleased  bool
}

func createOrganizationLegalHold(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	input leafen.LegalHoldCreateInput,
) (leafen.LegalHoldResponse, error) {
	normalized, requestHash, err := normalizeLegalHoldCreateInput(input)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	response := leafen.LegalHoldResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID,
			leafen.PermissionOrganizationLegalHolds,
		)
		if err != nil {
			return err
		}
		response, err = createLegalHoldInTransaction(
			txApp,
			legalHoldCreateAuthority{
				OrganizationID: current.OrganizationID,
				MutationActor:  current.PrincipalID,
				CreatedBy:      current.PrincipalID,
				AuditActor:     current.PrincipalID,
				AuditAction:    "organization.legal_hold.created",
			},
			normalized,
			requestHash,
			nil,
		)
		return err
	})
	return response, err
}

func createLegalHoldInTransaction(
	txApp core.App,
	authority legalHoldCreateAuthority,
	normalized leafen.LegalHoldCreateInput,
	requestHash string,
	metadata *notionAdminLegalHoldMetadata,
) (leafen.LegalHoldResponse, error) {
	if authority.OrganizationID == "" || authority.MutationActor == "" ||
		authority.CreatedBy == "" || authority.AuditActor == "" ||
		authority.AuditAction == "" || requestHash == "" {
		return leafen.LegalHoldResponse{}, errLegalHoldInvalid
	}
	existing, found, err := loadLegalHoldMutation(
		txApp, authority.OrganizationID, authority.MutationActor,
		legalHoldScopeCreateOperation, normalized.MutationID,
	)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	if found {
		if existing.RequestHash != requestHash {
			return leafen.LegalHoldResponse{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "This idempotency key was used for a different request.")
		}
		response, err := loadLegalHold(txApp, authority.OrganizationID, existing.HoldID)
		response.Duplicate = true
		return response, err
	}
	scope, err := normalizeLegalHoldScope(normalized.Scope)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	if err := validateLegalHoldScopeTargets(txApp, authority.OrganizationID, scope); err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	now := time.Now().UTC().UnixMilli()
	holdID := legalHoldMutationRecordID(
		authority.OrganizationID, authority.MutationActor,
		legalHoldScopeCreateOperation, normalized.MutationID,
	)
	response := leafen.LegalHoldResponse{
		ID: holdID, OrganizationID: authority.OrganizationID, Name: normalized.Name,
		Status: leafen.LegalHoldStatusActive, Reason: normalized.Reason, Scope: scope,
		CreatedBy: authority.CreatedBy, CreatedAt: now,
	}
	if err := insertLegalHold(txApp, response, scope, metadata); err != nil {
		return leafen.LegalHoldResponse{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if err := insertLegalHoldMutation(
		txApp, authority.OrganizationID, authority.MutationActor,
		legalHoldScopeCreateOperation, normalized.MutationID, requestHash, holdID, now,
	); err != nil {
		return leafen.LegalHoldResponse{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if err := leafen.BumpOrganizationPolicy(txApp, authority.OrganizationID, now); err != nil {
		return leafen.LegalHoldResponse{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if err := recordLegalHoldAudit(
		txApp, authority.OrganizationID, authority.AuditActor, authority.AuditAction, holdID, now,
	); err != nil {
		return leafen.LegalHoldResponse{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	return response, nil
}

func releaseOrganizationLegalHold(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	holdID string,
	input leafen.LegalHoldReleaseInput,
) (leafen.LegalHoldResponse, error) {
	normalized, requestHash, err := normalizeLegalHoldReleaseInput(holdID, input)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	response := leafen.LegalHoldResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID,
			leafen.PermissionOrganizationLegalHolds,
		)
		if err != nil {
			return err
		}
		existing, found, err := loadLegalHoldMutation(
			txApp, current.OrganizationID, current.PrincipalID,
			legalHoldScopeReleaseOperation, normalized.MutationID,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestHash != requestHash || existing.HoldID != holdID {
				return leafen.NotionAdminInvalid(errLegalHoldInvalid, "This idempotency key was used for a different request.")
			}
			response, err = loadLegalHold(txApp, current.OrganizationID, holdID)
			response.Duplicate = true
			return err
		}
		now := time.Now().UTC().UnixMilli()
		response, _, err = releaseLegalHoldInTransaction(
			txApp,
			legalHoldReleaseAuthority{
				OrganizationID: current.OrganizationID,
				ReleaseActor:   current.PrincipalID,
				AuditActor:     current.PrincipalID,
				AuditAction:    "organization.legal_hold.released",
			},
			holdID,
			now,
		)
		if err != nil {
			return err
		}
		if err := insertLegalHoldMutation(
			txApp, current.OrganizationID, current.PrincipalID,
			legalHoldScopeReleaseOperation, normalized.MutationID, requestHash, holdID, now,
		); err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		return nil
	})
	return response, err
}

func releaseLegalHoldInTransaction(
	txApp core.App,
	authority legalHoldReleaseAuthority,
	holdID string,
	now int64,
) (leafen.LegalHoldResponse, bool, error) {
	if authority.OrganizationID == "" || authority.ReleaseActor == "" ||
		authority.AuditActor == "" || authority.AuditAction == "" || holdID == "" || now <= 0 {
		return leafen.LegalHoldResponse{}, false, errLegalHoldInvalid
	}
	current, err := loadLegalHold(txApp, authority.OrganizationID, holdID)
	if err != nil {
		return leafen.LegalHoldResponse{}, false, err
	}
	if current.Status == leafen.LegalHoldStatusReleased && authority.AllowReleased {
		return current, false, nil
	}
	if current.Status != leafen.LegalHoldStatusActive {
		return leafen.LegalHoldResponse{}, false, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold is no longer active.")
	}
	if err := markLegalHoldReleased(
		txApp, authority.OrganizationID, holdID, authority.ReleaseActor, now,
	); err != nil {
		return leafen.LegalHoldResponse{}, false, err
	}
	if err := leafen.BumpOrganizationPolicy(txApp, authority.OrganizationID, now); err != nil {
		return leafen.LegalHoldResponse{}, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if err := recordLegalHoldAudit(
		txApp, authority.OrganizationID, authority.AuditActor, authority.AuditAction, holdID, now,
	); err != nil {
		return leafen.LegalHoldResponse{}, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	response, err := loadLegalHold(txApp, authority.OrganizationID, holdID)
	return response, true, err
}

const (
	legalHoldScopeCreateOperation  = "create"
	legalHoldScopeReleaseOperation = "release"
)
