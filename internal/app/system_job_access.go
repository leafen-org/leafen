package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// The server's own work on a workspace's pages — the trash's 30 days ending,
// a deleted database's turns, a property's conversion, numbering an ID, a
// two-way relation's other side, an invitee's values, a discussion's mark in
// a value, a published page's files copied for its visitor — is no one's
// act: its permission subject is the system, not the workspace's owner as a
// person. A workspace's owner reads a teamspace's pages only as anyone in
// their place does and has no part in another member's private pages
// (page_access_policy.go); asked as that person, the work there was refused
// and put off over and over (2026-11-18: a trashed database in a private
// teamspace its owner had not joined was never deleted).
//
// The system reaches every page its workspace keeps with full access, under
// the fences a page's state sets for anyone (page_access_state.go), and works
// under its workspace owner's workspace authority, in whose name the work is
// recorded as before. Only the server's own work makes it, from a workspace
// it read itself (systemJobSubjectFor): no request, token, connected tool or
// person's ID stands for it. A step either may take — holding a page for its
// 30 days, admitting a permanent deletion or a database's deletion, changing
// a page's place — asks the subject its caller names (pageAccessSubject); a
// person is asked as everywhere else.

// pageAccessSubject is whose permission such a step asks, and whose name it
// records.
type pageAccessSubject interface {
	actorID() string
	authorizeWorkspace(app core.App, workspaceID string, requirement permissionRequirement) (workspaceAuthorization, error)
	authorizePage(app core.App, pageID string, requirement permissionRequirement) (pageAuthorization, error)
	authorizePageBody(app core.App, pageID string, requirement permissionRequirement) (pageAuthorization, error)
	readWorkspaceFile(app core.App, fileID string) (*core.Record, error)
}

// personAccessSubject is a person, by their ID.
type personAccessSubject string

func (person personAccessSubject) actorID() string { return string(person) }

func (person personAccessSubject) authorizeWorkspace(
	app core.App, workspaceID string, requirement permissionRequirement,
) (workspaceAuthorization, error) {
	return authorizeWorkspace(app, string(person), workspaceID, requirement)
}

func (person personAccessSubject) authorizePage(
	app core.App, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	return authorizePage(app, string(person), pageID, requirement)
}

func (person personAccessSubject) authorizePageBody(
	app core.App, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	return authorizePageBody(app, string(person), pageID, requirement)
}

func (person personAccessSubject) readWorkspaceFile(app core.App, fileID string) (*core.Record, error) {
	record, _, err := authorizeWorkspaceFile(app, string(person), fileID, permissionWorkspaceRead)
	return record, err
}

// systemJobSubject is the system at work in one workspace.
type systemJobSubject struct {
	workspaceID string
	// ownerID is the workspace's permission owner: the authority it works
	// under and the name its work is recorded in.
	ownerID string
}

// systemJobSubjectFor is the system at work in a workspace the server read.
func systemJobSubjectFor(workspace *core.Record) systemJobSubject {
	return systemJobSubject{workspaceID: workspace.Id, ownerID: workspacePermissionOwnerID(workspace)}
}

func (system systemJobSubject) actorID() string { return system.ownerID }

func (system systemJobSubject) authorizeWorkspace(
	app core.App, workspaceID string, requirement permissionRequirement,
) (workspaceAuthorization, error) {
	if system.workspaceID == "" || system.ownerID == "" || workspaceID != system.workspaceID {
		return workspaceAuthorization{}, permissionDenied()
	}
	return authorizeWorkspace(app, system.ownerID, workspaceID, requirement)
}

func (system systemJobSubject) authorizePage(
	app core.App, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	return system.decidePage(withAccessMemo(app), pageID, pageAccessRoleForWorkspaceRequirement(requirement))
}

// authorizePageBody is the system's authorizePageBody: a template's body
// under its database's page, a row's under its row, still out of the trash.
func (system systemJobSubject) authorizePageBody(
	app core.App, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	app = withAccessMemo(app)
	minimum := pageAccessRoleForWorkspaceRequirement(requirement)
	if fenced, err := blockMovePageFenceActive(app, pageID); err != nil || fenced {
		return pageAuthorization{}, deniedUnless(err)
	}
	if requirement != permissionWorkspaceRead {
		if template, err := isDatabaseTemplatePageID(app, pageID); err != nil {
			return pageAuthorization{}, err
		} else if template {
			return authorizeDatabaseTemplateBodyBy(app, pageID, minimum, func(containerID string) (pageAuthorization, error) {
				return system.decidePage(app, containerID, minimum)
			})
		}
		if row, err := isDatabaseRowPageRoom(app, pageID); err != nil {
			return pageAuthorization{}, err
		} else if row {
			return system.decideRowBody(app, pageID, minimum)
		}
	}
	return system.decidePage(app, pageID, minimum)
}

// decidePage gives the system full access to a page of its workspace whose
// state lets anyone have it; a row's page only to read, through the page
// above it, as a row's page is written through its row (decideRowBody).
func (system systemJobSubject) decidePage(app core.App, pageID string, minimum pageAccessRole) (pageAuthorization, error) {
	if pageID == "" || minimum <= pageAccessRoleNone {
		return pageAuthorization{}, permissionDenied()
	}
	page, fences, err := loadAccessPage(app, pageID, minimum)
	if err != nil {
		return pageAuthorization{}, err
	}
	if page.GetString("workspace") != system.workspaceID {
		return pageAuthorization{}, permissionDenied()
	}
	if fences.Moving {
		if fenced, err := blockMovePageFenceActive(app, pageID); err != nil || fenced {
			return pageAuthorization{}, deniedUnless(err)
		}
	}
	if isDatabaseRowPageRecord(page) {
		if minimum > pageAccessRoleView {
			return pageAuthorization{}, permissionDenied()
		}
		aboveID, err := databaseRowPageAbove(app, page)
		if err != nil {
			return pageAuthorization{}, err
		}
		if _, err := system.decidePage(app, aboveID, pageAccessRoleView); err != nil {
			return pageAuthorization{}, err
		}
	} else if _, err := pageStateAdmits(app, page, minimum, fences); err != nil {
		return pageAuthorization{}, err
	}
	authority, err := system.authorizeWorkspace(app, system.workspaceID, permissionWorkspaceRead)
	if err != nil {
		return pageAuthorization{}, err
	}
	return pageAuthorization{Workspace: authority, Page: page, AccessRole: pageAccessRoleFullAccess}, nil
}

// readWorkspaceFile is the system's read of a file its workspace keeps.
func (system systemJobSubject) readWorkspaceFile(app core.App, fileID string) (*core.Record, error) {
	record, err := loadAccessWorkspaceFile(app, fileID)
	if err == nil && record.GetString("workspace") != system.workspaceID {
		return nil, permissionDenied()
	}
	if err != nil {
		return nil, err
	}
	if _, err := system.authorizeWorkspace(app, system.workspaceID, permissionWorkspaceRead); err != nil {
		return nil, err
	}
	return record, nil
}

// decideRowBody admits the system's change of a row page's body while the
// row and the page holding its database are out of the trash.
func (system systemJobSubject) decideRowBody(app core.App, rowID string, minimum pageAccessRole) (pageAuthorization, error) {
	row, err := loadDatabaseRowPageAuthority(app, rowID)
	if errors.Is(err, errDatabaseRowPageNotFound) || err == nil && (row.WorkspaceID != system.workspaceID || row.TrashedAt != 0) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	if trashed, err := pageInTrashPath(app, row.ContainerPageID); err != nil || trashed {
		return pageAuthorization{}, deniedUnless(err)
	}
	if err := workspaceMoveFence(app, row.WorkspaceID, minimum); err != nil {
		return pageAuthorization{}, err
	}
	authorization, err := system.decidePage(app, rowID, pageAccessRoleView)
	if err == nil && authorization.Workspace.OwnerID != row.OwnerID {
		return pageAuthorization{}, permissionDenied()
	}
	return authorization, err
}

// deniedUnless is a refusal, or err where reading failed.
func deniedUnless(err error) error {
	if err != nil {
		return err
	}
	return permissionDenied()
}

// databaseMutationSystemRequestHash is the request hash a deletion the
// system admits keeps: one no person's request makes
// (databaseMutationDeletionRequestHash hashes four parts, this five), so its
// turns are known as the system's and nothing a person asks becomes one.
func databaseMutationSystemRequestHash(pageID string, input databaseMutationInput) string {
	encoded, _ := json.Marshal([]string{"system", pageID, input.MutationID, input.TargetKind, input.TargetID})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// databaseMutationRequestHashBy is the request hash a deletion admitted by
// the subject keeps.
func databaseMutationRequestHashBy(subject pageAccessSubject, pageID string, input databaseMutationInput) string {
	if _, system := subject.(systemJobSubject); system {
		return databaseMutationSystemRequestHash(pageID, input)
	}
	return databaseMutationDeletionRequestHash(pageID, input)
}

// databaseMutationDeletionSubject is whom a deletion's turn asks: the system
// for a trashed database's retention (databaseContainerTrashRetentionJob,
// which only the trash makes: a person's deletion of its source is fenced
// while it waits) and for a deletion the system admitted; else the person
// who asked for it.
func databaseMutationDeletionSubject(
	app core.App, job storedDatabaseMutationDeletion, retention bool,
) (pageAccessSubject, error) {
	admittedBySystem := job.RequestHash == databaseMutationSystemRequestHash(job.PageID, databaseMutationInput{
		MutationID: job.MutationID, TargetKind: job.TargetKind, TargetID: job.TargetID,
	})
	if !retention && !admittedBySystem {
		return personAccessSubject(job.ActorID), nil
	}
	workspace, err := app.FindRecordById(workspacesCollection, job.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return systemJobSubjectFor(workspace), nil
}
