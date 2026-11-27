package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Shared vaults: Bitwarden's organizations. Someone makes one from the web
// vault, which makes the organization's key on their device and sends it
// encrypted to their own public key; each member after them gets the key
// from an owner or administrator, who encrypts it to that member's public
// key when confirming them ("confirm" in the web vault) — the server never
// holds it in the clear. Collections group the shared items, and a member
// sees the items of the collections they are given (owners and
// administrators see all). Members come from the people of this Leafen
// organization who keep a vault here: invited by hand in the web vault, or
// following the Leafen groups the shared vault is linked to (groups.go).

// Bitwarden's membership states and roles, as numbers on the wire.
const (
	memberRevoked   = -1
	memberInvited   = 0
	memberAccepted  = 1
	memberConfirmed = 2

	roleOwner   = 0
	roleAdmin   = 1
	roleUser    = 2
	roleManager = 3
	roleCustom  = 4
)

var memberStates = map[string]int64{"revoked": memberRevoked, "invited": memberInvited, "accepted": memberAccepted, "confirmed": memberConfirmed}

func memberStateName(state int64) string {
	for name, value := range memberStates {
		if value == state {
			return name
		}
	}
	return "accepted"
}

// organization is a shared vault as kept.
type organization struct {
	row                                  leafen.Row
	Name, Billing, PublicKey, PrivateKey string
	Groups                               []string
	Created, Revision                    int64
}

func organizationOf(row leafen.Row) organization {
	org := organization{row: row, Name: row.Text("Name"), Billing: row.Text("Billing email"), PublicKey: row.Text("Public key"),
		PrivateKey: row.Text("Private key"), Created: number(row, "Created"), Revision: number(row, "Revision")}
	_ = json.Unmarshal([]byte(row.Text("Groups")), &org.Groups)
	return org
}

func (org organization) id() string { return bitwardenID(org.row.ID) }

// collectionAccess is what a member may do with one collection's items.
type collectionAccess struct {
	ID            string `json:"id"`
	ReadOnly      bool   `json:"readOnly"`
	HidePasswords bool   `json:"hidePasswords"`
	Manage        bool   `json:"manage"`
}

// member is one person's place in a shared vault.
type member struct {
	row                          leafen.Row
	Organization, Account, Email string
	Person, Key, Source          string
	State, Role                  int64
	AccessAll                    bool
	Collections                  []collectionAccess
	Created, Revision            int64
	// The linked groups the member is in, by their Bitwarden ids, and the
	// shared vault's links, which give those groups collections.
	Groups []string
	links  []groupLink
}

func memberOf(row leafen.Row) member {
	m := member{row: row, Organization: row.Text("Organization"), Email: row.Text("Email"), Person: row.Text("Person"),
		Key: row.Text("Key"), Source: row.Text("Source"), Role: number(row, "Role"), AccessAll: row.Checkbox("Access all"),
		Created: number(row, "Created"), Revision: number(row, "Revision")}
	if accounts := row.IDs("Account"); len(accounts) == 1 {
		m.Account = accounts[0]
	}
	m.State = memberStates[row.Option("Status")]
	_ = json.Unmarshal([]byte(row.Text("Collections")), &m.Collections)
	_ = json.Unmarshal([]byte(row.Text("Groups")), &m.Groups)
	return m
}

// in sets the shared vault a member is read with, whose links give their
// groups collections.
func (m member) in(org organization) member {
	m.links = org.links()
	return m
}

func (m member) groups() []string { return m.Groups }

func (m member) id() string { return bitwardenID(m.row.ID) }

// manages says whether the member runs the shared vault: its owners and
// administrators, once confirmed.
func (m member) manages() bool {
	return m.State == memberConfirmed && (m.Role == roleOwner || m.Role == roleAdmin)
}

// seesAll says whether the member sees every item of the shared vault.
func (m member) seesAll() bool { return m.manages() || (m.State == memberConfirmed && m.AccessAll) }

// access is what the member may do with a collection's items: given to
// them, or to a linked group they are in — the most of either.
func (m member) access(collection string) (collectionAccess, bool) {
	found, given := collectionAccess{ID: collection, ReadOnly: true, HidePasswords: true}, false
	take := func(access collectionAccess) {
		if strings.EqualFold(access.ID, collection) {
			given = true
			found.ReadOnly = found.ReadOnly && access.ReadOnly
			found.HidePasswords = found.HidePasswords && access.HidePasswords
			found.Manage = found.Manage || access.Manage
		}
	}
	for _, access := range m.Collections {
		take(access)
	}
	for _, link := range m.links {
		if slices.Contains(m.Groups, link.ID) {
			for _, access := range link.Collections {
				take(access)
			}
		}
	}
	if !given {
		return collectionAccess{}, false
	}
	return found, true
}

// organizationByID reads a shared vault by its Bitwarden id.
func (s *store) organizationByID(id string) (organization, error) {
	rowID, ok := rowOf(id)
	if !ok {
		return organization{}, errMissing
	}
	row, err := s.organizations.Row(rowID)
	if errors.Is(err, leafen.ErrNoRow) || (err == nil && row.Option("Status") != "active") {
		return organization{}, errMissing
	}
	return organizationOf(row), err
}

// memberships are an account's places in shared vaults.
func (s *store) memberships(acct account) ([]member, error) {
	rows, err := owned(s.members, acct.row.ID)
	if err != nil {
		return nil, err
	}
	found := []member{}
	for _, row := range rows {
		found = append(found, memberOf(row))
	}
	return found, nil
}

// membersOf are a shared vault's members.
func (s *store) membersOf(org organization) ([]member, error) {
	found := []member{}
	for row, err := range s.members.Rows(leafen.Query{Where: []leafen.Where{{Column: "Organization", Operator: "equals", Value: org.row.ID}}}) {
		if err != nil {
			return nil, err
		}
		found = append(found, memberOf(row).in(org))
	}
	return found, nil
}

// orgRows are a table's rows of one shared vault.
func orgRows(table *leafen.Table, org string) ([]leafen.Row, error) {
	rows := []leafen.Row{}
	for row, err := range table.Rows(leafen.Query{Where: []leafen.Where{{Column: "Organization", Operator: "equals", Value: org}}}) {
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// membership is the account's place in a shared vault, if any.
func (s *store) membership(acct account, org organization) (member, bool, error) {
	places, err := s.memberships(acct)
	if err != nil {
		return member{}, false, err
	}
	for _, place := range places {
		if place.Organization == org.row.ID {
			return place, true, nil
		}
	}
	return member{}, false, nil
}

// orgChanged notes a change to a shared vault, which every member's
// devices sync.
func (s *store) orgChanged(org string) {
	_, _ = leafen.KV(leafen.KVRequest{Command: "SET", Key: "revision:org:" + org, Value: strconv.FormatInt(nowMs(), 10), TTL: 3600, Tenancy: s.tenancy})
}

// orgRevision is a shared vault's newest change; when the temporary store
// forgot it, the vault's own row (moved on with every removal) tells,
// along with its items.
func (s *store) orgRevision(org string) (int64, error) {
	kept, err := leafen.KV(leafen.KVRequest{Command: "GET", Key: "revision:org:" + org, Tenancy: s.tenancy})
	if err == nil && countOf(kept) > 0 {
		return int64(countOf(kept)), nil
	}
	row, err := s.organizations.Row(org)
	if err != nil {
		return 0, err
	}
	newest := number(row, "Revision")
	for _, table := range []*leafen.Table{s.ciphers, s.collections} {
		rows, err := orgRows(table, org)
		if err != nil {
			return 0, err
		}
		for _, row := range rows {
			newest = max(newest, number(row, "Revision"))
		}
	}
	_, _ = leafen.KV(leafen.KVRequest{Command: "SET", Key: "revision:org:" + org, Value: strconv.FormatInt(newest, 10), TTL: 3600, Tenancy: s.tenancy})
	return newest, nil
}

// orgRemoved notes a removal in a shared vault, in its row too.
func (s *store) orgRemoved(org string) error {
	s.orgChanged(org)
	return s.organizations.Update(org, map[string]any{"Revision": float64(nowMs())})
}

// orgRoute answers what the web vault asks of a shared vault.
func (c *call) orgRoute(acct account, id, rest string) (leafen.EndpointAnswer, error) {
	if id == "" {
		if c.method == "POST" {
			return c.makeOrganization(acct)
		}
		return problem(404, "Not found."), nil
	}
	org, err := c.store.organizationByID(id)
	if errors.Is(err, errMissing) {
		return problem(404, "That shared vault isn't here."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	m, found, err := c.store.membership(acct, org)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if !found || m.State == memberRevoked {
		return problem(404, "That shared vault isn't here."), nil
	}
	m = m.in(org)
	head, tail, _ := strings.Cut(rest, "/")
	switch head {
	case "users":
		return c.memberRoute(acct, org, m, tail)
	case "collections":
		return c.collectionRoute(org, m, tail)
	case "leave":
		if c.method == "POST" {
			return c.leave(org, m)
		}
	case "keys":
		return c.orgKeys(org, m)
	case "groups":
		return c.groupRoute(org, m, tail)
	case "billing":
		// Leafen's shared vaults have no billing; the web vault asks how
		// many seats are taken.
		if c.method == "GET" && strings.HasSuffix(tail, "metadata") {
			members, err := c.store.membersOf(org)
			if err != nil {
				return leafen.EndpointAnswer{}, err
			}
			return reply(200, map[string]any{"isOnSecretsManagerStandalone": false, "organizationOccupiedSeats": len(members),
				"object": "organizationBillingMetadata"}), nil
		}
	case "policies", "events":
		if c.method == "GET" {
			return list([]any{}), nil
		}
	case "":
		return c.orgSelf(acct, org, m)
	}
	return problem(404, "Not found."), nil
}

// orgSelf reads, renames or deletes a shared vault.
func (c *call) orgSelf(acct account, org organization, m member) (leafen.EndpointAnswer, error) {
	switch c.method {
	case "GET":
		return reply(200, organizationAnswer(org)), nil
	case "PUT", "POST":
		if !m.manages() {
			return problem(403, "Only the shared vault's owners and administrators change it."), nil
		}
		input := struct {
			Name         string `json:"name"`
			BillingEmail string `json:"billingEmail"`
		}{}
		if !c.decode(&input) || strings.TrimSpace(input.Name) == "" || tooLong(input.Name, input.BillingEmail) {
			return problem(400, "Give the shared vault a name."), nil
		}
		if err := c.store.organizations.Update(org.row.ID, map[string]any{"Name": input.Name, "Billing email": input.BillingEmail,
			"Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		c.store.orgChanged(org.row.ID)
		org.Name, org.Billing = input.Name, input.BillingEmail
		return reply(200, organizationAnswer(org)), nil
	case "DELETE":
		if m.State != memberConfirmed || m.Role != roleOwner {
			return problem(403, "Only the shared vault's owners delete it."), nil
		}
		input := struct {
			Hash string `json:"masterPasswordHash"`
		}{}
		if !c.decode(&input) || !acct.passwordIs(input.Hash) {
			return problem(400, "Invalid password."), nil
		}
		budget := 150
		if err := closeOrganization(c.store, org, &budget); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return reply(200, nil), nil
	}
	return problem(404, "Not found."), nil
}

// makeOrganization makes a shared vault, its first collection and its
// owner: the person making it, whose device made the keys.
func (c *call) makeOrganization(acct account) (leafen.EndpointAnswer, error) {
	input := struct {
		Name           string   `json:"name"`
		BillingEmail   string   `json:"billingEmail"`
		Key            string   `json:"key"`
		Keys           *keyPair `json:"keys"`
		CollectionName string   `json:"collectionName"`
	}{}
	if !c.decode(&input) || strings.TrimSpace(input.Name) == "" || input.Key == "" {
		return problem(400, "Give the shared vault a name; your Bitwarden app makes its key."), nil
	}
	keys := keyPair{}
	if input.Keys != nil {
		keys = *input.Keys
	}
	if tooLong(input.Name, input.BillingEmail, input.Key, input.CollectionName, keys.PublicKey, keys.EncryptedPrivateKey) {
		return problem(400, "The shared vault's details are too long."), nil
	}
	if acct.PublicKey == "" {
		return problem(400, "Your account has no keys yet; log in again from your Bitwarden app."), nil
	}
	now := float64(nowMs())
	orgRow, err := c.store.organizations.Add(map[string]any{"Name": input.Name, "Billing email": input.BillingEmail, "Public key": keys.PublicKey,
		"Private key": keys.EncryptedPrivateKey, "Groups": "[]", "Status": "active", "Created": now, "Revision": now})
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	given := []collectionAccess{}
	if input.CollectionName != "" {
		collectionRow, err := c.store.collections.Add(map[string]any{"Name": input.CollectionName, "Organization": orgRow, "Revision": now})
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		given = append(given, collectionAccess{ID: bitwardenID(collectionRow), Manage: true})
	}
	if _, err := c.store.addMember(orgRow, acct, roleOwner, memberConfirmed, input.Key, "invite", false, given); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	org, err := c.store.organizationByID(bitwardenID(orgRow))
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	c.store.changed(acct)
	return reply(200, organizationAnswer(org)), nil
}

// addMember gives an account a place in a shared vault.
func (s *store) addMember(org string, acct account, role, state int64, key, source string, accessAll bool, given []collectionAccess) (string, error) {
	encoded, _ := json.Marshal(given)
	now := float64(nowMs())
	return s.members.Add(map[string]any{"Email": acct.Email, "Organization": org, "Account": []string{acct.row.ID}, "Person": acct.Person,
		"Status": memberStateName(state), "Role": float64(role), "Key": key, "Source": source, "Access all": accessAll,
		"Collections": string(encoded), "Created": now, "Revision": now})
}

// orgKeys hands out the shared vault's key pair (its private key
// encrypted with the vault's key), or takes it once for a vault made
// without one.
func (c *call) orgKeys(org organization, m member) (leafen.EndpointAnswer, error) {
	answer := func() leafen.EndpointAnswer {
		return reply(200, map[string]any{"publicKey": org.PublicKey, "privateKey": org.PrivateKey, "object": "organizationKeys"})
	}
	switch c.method {
	case "GET":
		return answer(), nil
	case "POST":
		input := keyPair{}
		if !m.manages() || !c.decode(&input) || input.PublicKey == "" || input.EncryptedPrivateKey == "" || tooLong(input.PublicKey, input.EncryptedPrivateKey) {
			return problem(400, "Give the keys."), nil
		}
		if org.PublicKey != "" {
			return problem(400, "The shared vault has its keys."), nil
		}
		if err := c.store.organizations.Update(org.row.ID, map[string]any{"Public key": input.PublicKey, "Private key": input.EncryptedPrivateKey,
			"Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		org.PublicKey, org.PrivateKey = input.PublicKey, input.EncryptedPrivateKey
		c.store.orgChanged(org.row.ID)
		return answer(), nil
	}
	return problem(404, "Not found."), nil
}

// leave takes the account out of a shared vault; its last owner can't
// leave it, only delete it.
func (c *call) leave(org organization, m member) (leafen.EndpointAnswer, error) {
	if m.Role == roleOwner && m.State == memberConfirmed {
		members, err := c.store.membersOf(org)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if !slices.ContainsFunc(members, func(other member) bool {
			return other.row.ID != m.row.ID && other.Role == roleOwner && other.State == memberConfirmed
		}) {
			return problem(400, "You're its only owner: make someone else an owner first, or delete the shared vault."), nil
		}
	}
	if err := c.store.removeMembers(org, []member{m}); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, nil), nil
}

// closeOrganization deletes a shared vault: its items, collections and
// memberships wiped and trashed as far as budget allows, the nightly tidy
// going on with the rest; its row goes last.
func closeOrganization(s *store, org organization, budget *int) error {
	if *budget < 20 {
		return nil
	}
	*budget -= 10
	if err := s.organizations.Update(org.row.ID, map[string]any{"Status": "closed", "Public key": "", "Private key": "",
		"Revision": float64(nowMs())}); err != nil {
		return err
	}
	s.orgChanged(org.row.ID)
	return clearOrganization(s, org.row.ID, budget)
}

// clearOrganization wipes and trashes a closed shared vault's rows.
func clearOrganization(s *store, org string, budget *int) error {
	wiped := []struct {
		table  *leafen.Table
		values map[string]any
	}{{s.ciphers, map[string]any{"Data": "", "Kind": ""}}, {s.collections, map[string]any{"Name": ""}}, {s.members, map[string]any{"Key": ""}}}
	for _, part := range wiped {
		rows, err := orgRows(part.table, org)
		if err != nil {
			return err
		}
		if err := writeRows(part.table, rows, part.values); err != nil {
			return err
		}
		if done, err := trashRows(s.tenancy, idsOf(rows), budget); err != nil || !done {
			return err
		}
	}
	_, err := trashRows(s.tenancy, []string{org}, budget)
	return err
}
