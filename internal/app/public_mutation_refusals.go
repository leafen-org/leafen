package app

import (
	"fmt"
	"unicode/utf8"
)

// What a refused change of a page's sharing says to fix (request_refusal.go):
// the field as the request names it and why, after "Check the public sharing
// request." — a share route's 400 is never that line alone. A refusal of
// what the change's fields hold names them in the answer's fields too
// (publicMutationRouteError), by the names the contract lists
// (publicMutationField…), so the share window says why under the field
// itself — the site's address, its Analytics ID, its link title — rather
// than "사이트 설정을 바꾸지 못했습니다." at the panel's end (2026-11-24).

// publicMutationRefused refuses a sharing change for the reason given.
func publicMutationRefused(format string, args ...any) error {
	return refuse(errPublicMutationInvalid, format, args...)
}

// publicMutationFieldRefused refuses a sharing change for what one of its
// fields holds: the field named, with why.
func publicMutationFieldRefused(field, format string, args ...any) error {
	return refuseFields(errPublicMutationInvalid, apiErrorField{Name: field, Problem: fmt.Sprintf(format, args...)})
}

// publicMutationFieldsRefused refuses a sharing change for what some of
// its fields hold together, each named with the one reason.
func publicMutationFieldsRefused(fields []string, reason string) error {
	named := make([]apiErrorField, 0, len(fields))
	for _, field := range fields {
		named = append(named, apiErrorField{Name: field, Problem: reason})
	}
	return &requestRefusal{kind: errPublicMutationInvalid, reason: reason, fields: named}
}

// publicMutationRoles is what a share's role may be, as a refusal names it.
const publicMutationRoles = "view, comment, edit or full_access"

func validatePublicMutationInput(input publicMutationInput) error {
	switch {
	case input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > publicMutationMaxRequestIDRunes:
		return publicMutationFieldRefused(publicMutationFieldRequestID,
			"requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice",
			publicMutationMaxRequestIDRunes)
	case input.ExpectedRevision < 0:
		return publicMutationFieldRefused(publicMutationFieldExpectedRevision,
			"expectedRevision is the sharing settings' revision as GET reads it, 0 or more")
	case len(input.NavigationPageIDs) > publicMutationMaxNavigationPages:
		return publicMutationFieldRefused(publicMutationFieldNavigationPageIDs,
			"navigationPageIds names at most %d pages", publicMutationMaxNavigationPages)
	}
	for _, field := range []struct {
		name, value string
		most        int
	}{
		{publicMutationFieldTitle, input.Title, publicMutationMaxTextRunes},
		{publicMutationFieldDescription, input.Description, publicMutationMaxTextRunes},
		{publicMutationFieldInviteMessage, input.InviteMessage, publicMutationMaxTextRunes},
		{publicMutationFieldPrincipalID, input.PrincipalID, 255}, {publicMutationFieldPermissionID, input.PermissionID, 255},
	} {
		if count := utf8.RuneCountInString(field.value); count > field.most {
			return publicMutationFieldRefused(field.name, "%s is %d characters, over %d", field.name, count, field.most)
		}
	}
	for index, value := range input.NavigationPageIDs {
		if count := utf8.RuneCountInString(value); count > 255 {
			return publicMutationFieldRefused(publicMutationFieldNavigationPageIDs,
				"navigationPageIds[%d] is %d characters, over 255", index, count)
		}
	}
	switch input.Action {
	case publicMutationActionSetWebSharing:
		switch {
		case input.Enabled == nil:
			return publicMutationFieldRefused(publicMutationFieldEnabled,
				"enabled is true or false: whether anyone with the link opens the page")
		case !validPublicMutationExpiry(input.ExpiresAt):
			return publicMutationFieldRefused(publicMutationFieldExpiresAt, "expiresAt is left out, or a time as RFC 3339 writes it")
		case input.Role != "" && !pageLinkRoles[input.Role]:
			return publicMutationFieldRefused(publicMutationFieldRole,
				"role is left out, or view, comment or edit for anyone with the link")
		}
	case publicMutationActionRotateShareToken, publicMutationActionUnpublishSite:
		return nil
	case publicMutationActionPublishSite:
		switch {
		case len(input.Slug) > 60 || !publicSiteSlugPattern.MatchString(input.Slug):
			return publicMutationFieldRefused(publicMutationFieldSlug,
				"slug is the site's address, up to 60 lowercase letters, digits and single hyphens")
		case input.Theme != "system" && input.Theme != "light" && input.Theme != "dark":
			return publicMutationFieldRefused(publicMutationFieldTheme, "theme is system, light or dark")
		case input.ShowBreadcrumbs == nil || input.ShowSearch == nil || input.ShowBranding == nil:
			return publicMutationFieldsRefused(publicSiteSwitchesLeftOut(input),
				"showBreadcrumbs, showSearch and showBranding are each true or false")
		case !validPublicSiteFileID(input.PreviewImageFileID):
			return publicMutationFieldRefused(publicMutationFieldPreviewImageFileID,
				"previewImageFileId is left out, or names a file of the workspace by its id")
		case !validPublicSiteFileID(input.FaviconFileID):
			return publicMutationFieldRefused(publicMutationFieldFaviconFileID,
				"faviconFileId is left out, or names a file of the workspace by its id")
		case !validGoogleAnalyticsID(input.GoogleAnalyticsID):
			return publicMutationFieldRefused(publicMutationFieldGoogleAnalyticsID,
				"googleAnalyticsId is left out, or a measurement ID (G-…) or Google tag ID (GT-…)")
		}
	case publicMutationActionGrantPermission:
		switch {
		case !validPublicMutationPrincipal(input.PrincipalType):
			return publicMutationFieldRefused(publicMutationFieldPrincipalType,
				"principalType is user, email, group, integration or workspace, not %q", input.PrincipalType)
		case input.PrincipalID == "":
			return publicMutationFieldRefused(publicMutationFieldPrincipalID, "principalId is empty: it names whom the page is shared with")
		case !validPublicMutationRole(input.Role):
			return publicMutationFieldRefused(publicMutationFieldRole, "role is %s", publicMutationRoles)
		case input.PrincipalType != publicMutationPrincipalWorkspace && input.HideFromSearch != nil:
			return publicMutationFieldRefused(publicMutationFieldHideFromSearch, "hideFromSearch goes only with a share to the workspace")
		case input.PrincipalType == publicMutationPrincipalWorkspace && (input.InviteMessage != "" || input.NotifyOnAccess != nil && *input.NotifyOnAccess):
			return publicMutationFieldsRefused(publicMutationPersonFieldsSent(input),
				"inviteMessage and notifyOnAccess go with a share to a person, not to the workspace")
		}
		// Words that are no address would reach no one yet read as an invite.
		if input.PrincipalType == publicMutationPrincipalEmail {
			if _, ok := normalizeWorkspaceMemberEmail(input.PrincipalID); !ok {
				return publicMutationFieldRefused(publicMutationFieldPrincipalID, "principalId isn't an email address")
			}
		}
	case publicMutationActionUpdatePermission:
		switch {
		case input.PermissionID == "":
			return publicMutationFieldRefused(publicMutationFieldPermissionID, "permissionId is empty: it names the share to change")
		case !validPublicMutationRole(input.Role):
			return publicMutationFieldRefused(publicMutationFieldRole, "role is %s", publicMutationRoles)
		}
	case publicMutationActionRemovePermission:
		if input.PermissionID == "" {
			return publicMutationFieldRefused(publicMutationFieldPermissionID, "permissionId is empty: it names the share to remove")
		}
	default:
		return publicMutationFieldRefused(publicMutationFieldAction, "action is %s, %s, %s, %s, %s, %s or %s, not %q",
			publicMutationActionSetWebSharing, publicMutationActionRotateShareToken, publicMutationActionPublishSite,
			publicMutationActionUnpublishSite, publicMutationActionGrantPermission, publicMutationActionUpdatePermission,
			publicMutationActionRemovePermission, input.Action)
	}
	return nil
}

// publicSiteSwitchesLeftOut are the header's switches a site's publish left
// out.
func publicSiteSwitchesLeftOut(input publicMutationInput) []string {
	fields := []string{}
	for _, field := range []struct {
		name string
		set  *bool
	}{
		{publicMutationFieldShowBreadcrumbs, input.ShowBreadcrumbs}, {publicMutationFieldShowSearch, input.ShowSearch},
		{publicMutationFieldShowBranding, input.ShowBranding},
	} {
		if field.set == nil {
			fields = append(fields, field.name)
		}
	}
	return fields
}

// publicMutationPersonFieldsSent are what a share to the workspace sent that
// goes only with a share to a person.
func publicMutationPersonFieldsSent(input publicMutationInput) []string {
	fields := []string{}
	if input.InviteMessage != "" {
		fields = append(fields, publicMutationFieldInviteMessage)
	}
	if input.NotifyOnAccess != nil && *input.NotifyOnAccess {
		fields = append(fields, publicMutationFieldNotifyOnAccess)
	}
	return fields
}

// errPublicMutationNoSuchShare is a share the change names that the page
// doesn't have.
var errPublicMutationNoSuchShare = publicMutationRefused("permissionId names no share of this page; GET its sharing settings for their ids")

// publicMutationRefusalMessage is a refused sharing change's words.
func publicMutationRefusalMessage(err error) string {
	return refusalMessage("Check the public sharing request.", refusalReason(err))
}
