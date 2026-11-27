package main

import (
	"slices"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// The organization's shared vaults as its owners and people admins see
// them (decision "문서 앱과 플랫폼 확장" 7·9절): each vault's name, its members
// — email, role and whether they wait to be confirmed — the Leafen groups it
// follows and how many items it holds. Never what the items are: they stay
// encrypted with keys only the members' devices hold.

var roleNames = map[int64]string{roleOwner: "owner", roleAdmin: "admin", roleUser: "user", roleManager: "manager", roleCustom: "custom"}

// oversees says whether the caller sees the organization's shared vaults:
// its owners, and members who manage its people.
func oversees(call leafen.ActionCall) bool {
	return call.Role == "owner" || slices.Contains(call.AdminRoles, "people")
}

// orgVaults lists the organization's shared vaults for its administrators.
//
//go:wasmexport orgVaults
func orgVaults() int32 {
	call, err := leafen.Action(nil)
	if err != nil || call.Person == "" || call.Tenancy == "" {
		return fail("open the Vault app in an organization")
	}
	if !oversees(call) {
		return fail("only the organization's owners and people admins see its shared vaults")
	}
	s, err := openStore(call.Tenancy)
	if err != nil {
		return fail(err.Error())
	}
	vaults := []any{}
	for row, err := range s.organizations.Rows(leafen.Query{}) {
		if err != nil {
			return fail(err.Error())
		}
		org := organizationOf(row)
		members, err := s.membersOf(org)
		if err != nil {
			return fail(err.Error())
		}
		listed := []any{}
		for _, m := range members {
			listed = append(listed, map[string]any{"email": m.Email, "role": roleNames[m.Role], "state": memberStateName(m.State)})
		}
		groups := []any{}
		for _, link := range org.links() {
			groups = append(groups, map[string]any{"id": link.ID, "name": link.Name})
		}
		items, err := s.ciphers.Count(leafen.Query{Where: []leafen.Where{{Column: "Organization", Operator: "equals", Value: org.row.ID}}})
		if err != nil {
			return fail(err.Error())
		}
		vaults = append(vaults, map[string]any{"id": org.id(), "name": org.Name, "created": org.Created, "members": listed,
			"groups": groups, "items": items})
	}
	_ = leafen.OutputJSON(map[string]any{"vaults": vaults})
	return 0
}
