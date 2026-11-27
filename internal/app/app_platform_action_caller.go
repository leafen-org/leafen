package app

import "github.com/pocketbase/pocketbase/core"

// What an action learns of who called it in its organization: their role
// there and the admin roles they hold, as Leafen's own settings know them,
// so an app can show an organization's administrators what it keeps for
// them (decision "문서 앱과 플랫폼 확장" 7·9절: a shared vault's members, not
// its contents) — and no one else.

// withAppActionCaller adds the caller's organization role and admin roles
// to an action's input; a call outside an organization, or by someone no
// longer its member, carries neither.
func withAppActionCaller(app core.App, input map[string]any, personID, tenancyKey string) map[string]any {
	organizationID := appTenancyOrganization(tenancyKey)
	if personID == "" || organizationID == "" {
		return input
	}
	authorization, err := authorizeOrganization(app, personID, organizationID, permissionOrganizationActive)
	if err != nil {
		return input
	}
	input["role"], input["adminRoles"] = authorization.Role, authorization.AdminRoles
	return input
}
