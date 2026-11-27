package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Why a property's rule preview shows a person what it shows, as Notion's
// "다음 사용자로 미리보기" says it under the level (its
// columnPropertyPermissionsModal.viewAs lines and the code choosing them,
// read 2026-11-18): someone who cannot open the database sees nothing of it;
// someone with full access to it has full access to every property; else an
// exception naming them decides, a person's before a group's at the same
// level, then the rule's level for everyone, then what the database gives.
// Where the database gives it, the preview says how it came to them (Via):
// the teamspace, a group's share, everyone in the workspace's share, or the
// database itself — their own page or a share to them. Under it come the
// person-property exceptions that would give more on the pages that name
// them or that they made (Conditions).
const (
	propertyAccessPreviewNoDatabaseAccess = "noDatabaseAccess"
	propertyAccessPreviewFullAccess       = "fullAccess"
	propertyAccessPreviewPersonException  = "personException"
	propertyAccessPreviewGroupException   = "groupException"
	propertyAccessPreviewDefaultAccess    = "defaultAccess"
	propertyAccessPreviewInherited        = "inherited"
)

// DatabasePropertyAccessPreview is what a property shows one person in its
// database as a whole, and why (Reason, one of the above; GroupName for a
// group's exception). Via says how the database gave the level where it did:
// "teamspace", "group" (with ViaGroupName), "workspace" or "database".
// Comment is a level the database gives only to comment.
type DatabasePropertyAccessPreview struct {
	Level        string
	Reason       string
	GroupName    string
	Via          string
	ViaGroupName string
	Comment      bool
	Conditions   []DatabasePropertyAccessCondition
}

// DatabasePropertyAccessCondition is a person property's exception that
// gives the person more on the pages it names them in (PropertyName), or on
// the pages they made (PageCreator).
type DatabasePropertyAccessCondition struct {
	Level        string `json:"level"`
	PropertyName string `json:"propertyName,omitempty"`
	PageCreator  bool   `json:"pageCreator,omitzero"`
}

// Inherited is whether the level comes from the database itself, not from the rule.
func (preview DatabasePropertyAccessPreview) Inherited() bool {
	return preview.Reason == propertyAccessPreviewFullAccess ||
		preview.Reason == propertyAccessPreviewInherited && preview.Level != propertyAccessViewProperty
}

// explainPropertyAccessPreview gives the level shown its reason; reader and
// rule are nil where the property has no rule.
func explainPropertyAccessPreview(app core.App, target databasePropertyAccessTarget, dataSourceID, personID string,
	reader *databasePropertyAccessReader, rule *databasePropertyAccessRule, role pageAccessRole, level string) (DatabasePropertyAccessPreview, error) {
	preview := DatabasePropertyAccessPreview{Level: level, Reason: propertyAccessPreviewInherited}
	switch {
	case role == pageAccessRoleNone:
		preview.Reason = propertyAccessPreviewNoDatabaseAccess
		return preview, nil
	case role >= pageAccessRoleFullAccess:
		preview.Reason = propertyAccessPreviewFullAccess
		return preview, preview.explainVia(app, target, personID)
	}
	var decider *storedDatabasePropertyAccessException
	if rule != nil {
		for index := range rule.Exceptions {
			exception := &rule.Exceptions[index]
			if exception.PrincipalType == propertyAccessPrincipalProperty || !reader.names(*exception, nil) {
				continue
			}
			rank, best := propertyAccessRank[exception.Level], -1
			if decider != nil {
				best = propertyAccessRank[decider.Level]
			}
			if rank > best || rank == best && exception.PrincipalType != propertyAccessPrincipalGroup {
				decider = exception
			}
		}
	}
	switch {
	case decider != nil && decider.PrincipalType == propertyAccessPrincipalGroup:
		preview.Reason = propertyAccessPreviewGroupException
		name, err := propertyAccessGroupName(app, decider.PrincipalID)
		if err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
		preview.GroupName = name
	case decider != nil:
		preview.Reason = propertyAccessPreviewPersonException
	case rule != nil && rule.DefaultLevel != propertyAccessInherit:
		preview.Reason = propertyAccessPreviewDefaultAccess
	default:
		preview.Comment = role == pageAccessRoleComment
		if err := preview.explainVia(app, target, personID); err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
	}
	if rule == nil {
		return preview, nil
	}
	conditions, err := propertyAccessPreviewConditions(app, dataSourceID, rule, decider)
	preview.Conditions = conditions
	return preview, err
}

// explainVia names how the database gave the person their access, from the
// access decision's source (page_access_source.go).
func (preview *DatabasePropertyAccessPreview) explainVia(app core.App, target databasePropertyAccessTarget, personID string) error {
	preview.Via = "database"
	authorization, err := authorizePageAccessRole(app, personID, target.PageID, pageAccessRoleView)
	if isPermissionDenied(err) {
		return nil
	}
	if err != nil {
		return err
	}
	switch authorization.Source.Kind {
	case pageAccessFromTeamspace:
		preview.Via = "teamspace"
	case pageAccessFromShares:
		source, err := pageShareSource(app, target.WorkspaceID, target.PageID, personID, authorization.AccessRole)
		if err != nil {
			return err
		}
		switch source.Kind {
		case pageAccessFromWorkspace:
			preview.Via = "workspace"
		case pageAccessFromGroup:
			preview.Via = "group"
			if preview.ViaGroupName, err = propertyAccessGroupName(app, source.GroupID); err != nil {
				return err
			}
		}
	}
	return nil
}

// propertyAccessPreviewConditions are the person-property exceptions above
// what decides the person's level, as Notion's preview lists them: none
// where an exception names them with no access or, with none naming them,
// the rule gives everyone none; else each one higher than the person's own
// exception (every one where none names them).
func propertyAccessPreviewConditions(app core.App, dataSourceID string, rule *databasePropertyAccessRule,
	decider *storedDatabasePropertyAccessException) ([]DatabasePropertyAccessCondition, error) {
	if decider != nil && decider.Level == propertyAccessNone || decider == nil && rule.DefaultLevel == propertyAccessNone {
		return nil, nil
	}
	conditions := []DatabasePropertyAccessCondition{}
	for _, exception := range rule.Exceptions {
		if exception.PrincipalType != propertyAccessPrincipalProperty ||
			decider != nil && propertyAccessRank[decider.Level] >= propertyAccessRank[exception.Level] {
			continue
		}
		condition := DatabasePropertyAccessCondition{Level: exception.Level, PageCreator: exception.PrincipalID == propertyAccessPageCreator}
		if !condition.PageCreator {
			condition.PropertyName = exception.PrincipalID
			err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:dataSourceId} AND [[id]] = {:id}`).
				Bind(dbx.Params{"dataSourceId": dataSourceID, "id": exception.PrincipalID}).Row(&condition.PropertyName)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

func propertyAccessGroupName(app core.App, groupID string) (string, error) {
	name := ""
	err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspace_permission_groups}} WHERE [[id]] = {:id} AND [[deleted_at]] IS NULL`).
		Bind(dbx.Params{"id": groupID}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}
