package main

import (
	"errors"
	"fmt"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// A shared vault's members. Leafen's vault sends no email, so an
// invitation is taken at once: anyone in this Leafen organization with a
// vault here can be added, and waits ("accepted", in Bitwarden's words)
// until an owner or administrator confirms them from the web vault — the
// one step only a device holding the vault's key can take. Taking someone
// out can't take back what their devices kept, so the others are told to
// change those passwords.

const membersAtOnce = 100

// memberAnswer is a member as the web vault lists them.
func memberAnswer(m member, name string) map[string]any {
	collections := []any{}
	for _, given := range m.Collections {
		collections = append(collections, map[string]any{"id": given.ID, "readOnly": given.ReadOnly, "hidePasswords": given.HidePasswords, "manage": given.Manage})
	}
	return map[string]any{
		"id": m.id(), "userId": bitwardenID(m.Account), "type": m.Role, "status": m.State, "accessAll": m.AccessAll, "externalId": nil,
		"accessSecretsManager": false, "permissions": permissionsOf(m.Role == roleOwner || m.Role == roleAdmin), "resetPasswordEnrolled": false,
		"usesKeyConnector": false, "hasMasterPassword": true, "name": name, "email": m.Email, "avatarColor": nil, "twoFactorEnabled": false,
		"ssoBound": false, "collections": collections, "groups": []any{}, "managedByOrganization": false, "claimedByOrganization": false,
		"object": "organizationUserUserDetails",
	}
}

// names are the accounts' names by their rows, read in one call.
func (s *store) names() (map[string]string, error) {
	names := map[string]string{}
	for row, err := range s.accounts.Rows(leafen.Query{Where: []leafen.Where{{Column: "Status", Operator: "equals", Value: statusActive}}}) {
		if err != nil {
			return nil, err
		}
		names[row.ID] = row.Text("Name")
	}
	return names, nil
}

func (c *call) memberRoute(acct account, org organization, m member, rest string) (leafen.EndpointAnswer, error) {
	members, err := c.store.membersOf(org)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	id, tail, _ := strings.Cut(rest, "/")
	if c.method == "GET" && (id == "" || id == "mini-details") {
		names, err := c.store.names()
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		listed := []any{}
		for _, other := range members {
			if !m.manages() && other.State != memberConfirmed {
				continue
			}
			listed = append(listed, memberAnswer(other, names[other.Account]))
		}
		return list(listed), nil
	}
	if !m.manages() {
		return problem(403, "Only the shared vault's owners and administrators manage its members."), nil
	}
	switch c.method + " " + id {
	case "POST invite":
		return c.invite(org, members)
	case "POST public-keys":
		return c.publicKeys(members)
	case "POST confirm":
		return c.confirmMany(org, members)
	case "POST reinvite":
		return list([]any{}), nil
	case "DELETE ", "POST remove", "POST delete":
		return c.removeMany(org, m, members)
	case "PUT revoke", "PUT restore":
		return c.revokeMany(org, m, members, id == "revoke")
	}
	index := -1
	for at, other := range members {
		if strings.EqualFold(other.id(), id) {
			index = at
		}
	}
	if index < 0 {
		return problem(404, "That member isn't in the shared vault."), nil
	}
	target := members[index]
	switch c.method + " " + tail {
	case "GET ":
		names, err := c.store.names()
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return reply(200, memberAnswer(target, names[target.Account])), nil
	case "GET groups":
		return reply(200, []any{}), nil
	case "PUT ", "POST ":
		return c.changeMember(org, m, target, members)
	case "POST confirm":
		input := struct {
			Key string `json:"key"`
		}{}
		if !c.decode(&input) {
			return problem(400, "Your Bitwarden app sent no key."), nil
		}
		if refusal, err := c.store.confirm(org, target, input.Key); refusal != "" || err != nil {
			return problem(400, refusal), err
		}
		return reply(200, nil), nil
	case "POST reinvite", "POST accept":
		return reply(200, nil), nil
	case "DELETE ", "POST delete", "POST remove":
		if refusal := lastOwner(m, members, []member{target}); refusal != "" {
			return problem(400, refusal), nil
		}
		return reply(200, nil), c.store.removeMembers(org, []member{target})
	case "PUT revoke", "PUT restore":
		if refusal := lastOwner(m, members, []member{target}); refusal != "" && tail == "revoke" {
			return problem(400, refusal), nil
		}
		return reply(200, nil), c.store.revoke(org, []member{target}, tail == "revoke")
	}
	return problem(404, "Not found."), nil
}

// lastOwner refuses a change that would leave the shared vault with no
// owner, or an administrator changing an owner.
func lastOwner(by member, members, changed []member) string {
	owners := 0
	for _, other := range members {
		if other.Role == roleOwner && other.State == memberConfirmed {
			owners++
		}
	}
	for _, target := range changed {
		if target.Role == roleOwner && by.Role != roleOwner {
			return "Only an owner changes another owner."
		}
		if target.Role == roleOwner && target.State == memberConfirmed {
			owners--
		}
	}
	if owners < 1 {
		return "The shared vault needs an owner: make someone else an owner first."
	}
	return ""
}

// roleOf is a role the web vault sends, of those Leafen's vault keeps.
func roleOf(role int64) (int64, bool) {
	switch role {
	case roleOwner, roleAdmin, roleUser:
		return role, true
	case roleManager, roleCustom:
		return roleUser, true
	}
	return 0, false
}

// collectionsGiven checks the collections a change gives against the
// shared vault's.
func (s *store) collectionsGiven(org organization, given []collectionAccess) ([]collectionAccess, bool, error) {
	if len(given) == 0 {
		return []collectionAccess{}, true, nil
	}
	known, err := orgRows(s.collections, org.row.ID)
	if err != nil {
		return nil, false, err
	}
	kept := []collectionAccess{}
	for _, access := range given {
		found := false
		for _, row := range known {
			if strings.EqualFold(bitwardenID(row.ID), access.ID) {
				found = true
				access.ID = bitwardenID(row.ID)
			}
		}
		if !found {
			return nil, false, nil
		}
		kept = append(kept, access)
	}
	return kept, true, nil
}

// invite adds people with a vault here, by their vault's email.
func (c *call) invite(org organization, members []member) (leafen.EndpointAnswer, error) {
	input := struct {
		Emails      []string           `json:"emails"`
		Type        int64              `json:"type"`
		AccessAll   bool               `json:"accessAll"`
		Collections []collectionAccess `json:"collections"`
	}{}
	if !c.decode(&input) || len(input.Emails) == 0 || len(input.Emails) > 20 {
		return problem(400, "Invite 1 to 20 people at a time."), nil
	}
	role, ok := roleOf(input.Type)
	if !ok {
		return problem(400, "Choose a role."), nil
	}
	given, ok, err := c.store.collectionsGiven(org, input.Collections)
	if err != nil || !ok {
		return problem(400, "Choose the shared vault's collections."), err
	}
	invited := []account{}
	for _, email := range input.Emails {
		acct, err := c.store.accountByEmail(email)
		if err != nil && !errors.Is(err, errMissing) {
			return leafen.EndpointAnswer{}, err
		}
		if err != nil || acct.Status != statusActive {
			return problem(400, fmt.Sprintf("%s has no vault here: they make theirs in Leafen's Vault app first, then you add them.", normalEmail(email))), nil
		}
		for _, other := range members {
			if other.Account == acct.row.ID {
				return problem(400, fmt.Sprintf("%s is in the shared vault already.", acct.Email)), nil
			}
		}
		invited = append(invited, acct)
	}
	for _, acct := range invited {
		if _, err := c.store.addMember(org.row.ID, acct, role, memberAccepted, "", "invite", input.AccessAll, given); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	c.store.orgChanged(org.row.ID)
	c.store.askToConfirm(org, members, len(invited))
	return reply(200, nil), nil
}

// publicKeys hands the web vault the public keys of members it confirms.
func (c *call) publicKeys(members []member) (leafen.EndpointAnswer, error) {
	input := struct {
		IDs []string `json:"ids"`
	}{}
	if !c.decode(&input) || len(input.IDs) > membersAtOnce {
		return problem(400, "Choose up to 100 members."), nil
	}
	keys := []any{}
	for _, id := range input.IDs {
		for _, other := range members {
			if !strings.EqualFold(other.id(), id) {
				continue
			}
			acct, err := c.store.accountByRow(other.Account)
			if errors.Is(err, errMissing) {
				continue
			}
			if err != nil {
				return leafen.EndpointAnswer{}, err
			}
			keys = append(keys, map[string]any{"id": other.id(), "userId": acct.id(), "key": acct.PublicKey, "object": "organizationUserPublicKeyResponseModel"})
		}
	}
	return list(keys), nil
}

// confirm gives a waiting member the shared vault's key, encrypted to
// their public key by the confirming device.
func (s *store) confirm(org organization, target member, key string) (string, error) {
	if target.State != memberAccepted {
		return "Only someone waiting to be confirmed can be.", nil
	}
	// An RSA-encrypted string (types 3 to 6), as the confirming device made it.
	if len(key) < 3 || key[1] != '.' || key[0] < '3' || key[0] > '6' || tooLong(key) {
		return "Your Bitwarden app sent no key.", nil
	}
	if err := s.members.Update(target.row.ID, map[string]any{"Status": "confirmed", "Key": key, "Revision": float64(nowMs())}); err != nil {
		return "", err
	}
	s.orgChanged(org.row.ID)
	return "", nil
}

func (c *call) confirmMany(org organization, members []member) (leafen.EndpointAnswer, error) {
	input := struct {
		Keys []struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"keys"`
	}{}
	if !c.decode(&input) || len(input.Keys) > membersAtOnce {
		return problem(400, "Choose up to 100 members."), nil
	}
	results := []any{}
	for _, given := range input.Keys {
		refusal := "That member isn't in the shared vault."
		for _, other := range members {
			if strings.EqualFold(other.id(), given.ID) {
				var err error
				if refusal, err = c.store.confirm(org, other, given.Key); err != nil {
					return leafen.EndpointAnswer{}, err
				}
			}
		}
		results = append(results, map[string]any{"id": given.ID, "error": refusal, "object": "OrganizationBulkConfirmResponseModel"})
	}
	return list(results), nil
}

// chosen are the members a bulk change names.
func (c *call) chosen(members []member) ([]member, bool) {
	input := struct {
		IDs []string `json:"ids"`
	}{}
	if !c.decode(&input) || len(input.IDs) == 0 || len(input.IDs) > membersAtOnce {
		return nil, false
	}
	found := []member{}
	for _, id := range input.IDs {
		for _, other := range members {
			if strings.EqualFold(other.id(), id) {
				found = append(found, other)
			}
		}
	}
	return found, true
}

func (c *call) removeMany(org organization, by member, members []member) (leafen.EndpointAnswer, error) {
	targets, ok := c.chosen(members)
	if !ok {
		return problem(400, "Choose up to 100 members."), nil
	}
	if refusal := lastOwner(by, members, targets); refusal != "" {
		return problem(400, refusal), nil
	}
	if err := c.store.removeMembers(org, targets); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return bulkDone(targets), nil
}

func (c *call) revokeMany(org organization, by member, members []member, revoke bool) (leafen.EndpointAnswer, error) {
	targets, ok := c.chosen(members)
	if !ok {
		return problem(400, "Choose up to 100 members."), nil
	}
	if refusal := lastOwner(by, members, targets); refusal != "" && revoke {
		return problem(400, refusal), nil
	}
	if err := c.store.revoke(org, targets, revoke); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return bulkDone(targets), nil
}

func bulkDone(targets []member) leafen.EndpointAnswer {
	results := []any{}
	for _, target := range targets {
		results = append(results, map[string]any{"id": target.id(), "error": "", "object": "OrganizationUserBulkResponseModel"})
	}
	return list(results)
}

// changeMember changes a member's role and collections.
func (c *call) changeMember(org organization, by, target member, members []member) (leafen.EndpointAnswer, error) {
	input := struct {
		Type        int64              `json:"type"`
		AccessAll   bool               `json:"accessAll"`
		Collections []collectionAccess `json:"collections"`
	}{}
	if !c.decode(&input) {
		return problem(400, "The change couldn't be read."), nil
	}
	role, ok := roleOf(input.Type)
	if !ok {
		return problem(400, "Choose a role."), nil
	}
	if (role == roleOwner || target.Role == roleOwner) && by.Role != roleOwner {
		return problem(400, "Only an owner makes or changes an owner."), nil
	}
	if target.Role == roleOwner && role != roleOwner {
		if refusal := lastOwner(by, members, []member{target}); refusal != "" {
			return problem(400, refusal), nil
		}
	}
	given, ok, err := c.store.collectionsGiven(org, input.Collections)
	if err != nil || !ok {
		return problem(400, "Choose the shared vault's collections."), err
	}
	encoded, _ := jsonText(given)
	if err := c.store.members.Update(target.row.ID, map[string]any{"Role": float64(role), "Access all": input.AccessAll, "Collections": encoded,
		"Revision": float64(nowMs())}); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	c.store.orgChanged(org.row.ID)
	return reply(200, nil), nil
}

// userKey hands out an account's public key, which a device encrypts a
// shared vault's key to.
func (c *call) userKey(id string) (leafen.EndpointAnswer, error) {
	rowID, ok := rowOf(id)
	if !ok {
		return problem(404, "Not found."), nil
	}
	acct, err := c.store.accountByRow(rowID)
	if errors.Is(err, errMissing) || (err == nil && acct.Status != statusActive) {
		return problem(404, "Not found."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, map[string]any{"userId": acct.id(), "publicKey": acct.PublicKey, "object": "userKey"}), nil
}
