package main

import (
	"errors"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// The vault itself: the items (Bitwarden's ciphers) and folders a person's
// apps keep, encrypted, and the sync that hands them back.

// itemMaxBytes is the most one item keeps; Bitwarden's own limits keep a
// real one far smaller.
const itemMaxBytes = 64 << 10

// itemParts are the parts of an item its app encrypts and the server keeps
// as they come.
var itemParts = []string{"name", "notes", "fields", "passwordHistory", "key", "reprompt", "login", "secureNote", "card", "identity", "sshKey"}

// itemKinds are Bitwarden's item types by number and the part each keeps.
var itemKinds = map[int64]string{1: "login", 2: "secureNote", 3: "card", 4: "identity", 5: "sshKey"}

func (c *call) sync(acct account) (leafen.EndpointAnswer, error) {
	folders, err := owned(c.store.folders, acct.row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	v, places, err := c.store.viewOf(acct)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	items, err := c.store.visibleItems(acct, v)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	collections, err := c.store.myCollections(places)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	orgs, err := c.store.profileOrganizations(places)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	answer := map[string]any{"profile": profileOf(acct, orgs), "folders": folderList(folders), "ciphers": items,
		"collections": collections, "policies": []any{}, "sends": []any{}, "domains": nil, "object": "sync"}
	if c.query.Get("excludeDomains") != "true" {
		answer["domains"] = map[string]any{"equivalentDomains": []any{}, "globalEquivalentDomains": []any{}, "object": "domains"}
	}
	return reply(200, answer), nil
}

// profileOrganizations are the shared vaults an account has a place in,
// revoked ones too (their apps show them as such).
func (s *store) profileOrganizations(places []member) ([]any, error) {
	orgs := []any{}
	for _, m := range places {
		org, err := s.organizationByID(bitwardenID(m.Organization))
		if errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, profileOrganization(org, m))
	}
	return orgs, nil
}

func profileOf(acct account, orgs []any) map[string]any {
	return map[string]any{
		"_status": 0, "id": acct.id(), "name": acct.Name, "email": acct.Email, "emailVerified": true, "premium": true,
		"premiumFromOrganization": false, "culture": "en-US", "twoFactorEnabled": false, "key": acct.Key,
		"privateKey": acct.PrivateKey, "accountKeys": accountKeys(acct), "securityStamp": acct.Stamp, "organizations": orgs, "providers": []any{},
		"providerOrganizations": []any{}, "forcePasswordReset": false, "avatarColor": nil, "usesKeyConnector": false,
		"creationDate": stamp(acct.Created), "object": "profile",
	}
}

func (c *call) accountRoute(acct account, part, rest string) (leafen.EndpointAnswer, error) {
	switch c.method + " " + part {
	case "GET profile":
		return c.profile(acct)
	case "PUT profile", "POST profile":
		input := struct {
			Name string `json:"name"`
		}{}
		if !c.decode(&input) || tooLong(input.Name) {
			return problem(400, "Give a name."), nil
		}
		if err := c.store.accounts.Update(acct.row.ID, map[string]any{"Name": input.Name, "Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		acct.Name = input.Name
		return c.profile(acct)
	case "GET revision-date":
		revision, err := c.store.revision(acct)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return reply(200, revision), nil
	case "POST keys":
		input := keyPair{}
		if !c.decode(&input) || input.PublicKey == "" || input.EncryptedPrivateKey == "" || tooLong(input.PublicKey, input.EncryptedPrivateKey) {
			return problem(400, "Give the keys."), nil
		}
		if acct.PrivateKey != "" {
			return problem(400, "The account has its keys."), nil
		}
		if err := c.store.accounts.Update(acct.row.ID, map[string]any{"Public key": input.PublicKey, "Private key": input.EncryptedPrivateKey,
			"Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		acct.PublicKey, acct.PrivateKey = input.PublicKey, input.EncryptedPrivateKey
		return c.profile(acct)
	case "POST key-management":
		// Newer apps note the id of their user key; Leafen's vault keeps
		// the key as sent, so there is nothing to change.
		if rest == "user-key-id" {
			return reply(200, nil), nil
		}
	case "POST verify-password":
		input := struct {
			Hash string `json:"masterPasswordHash"`
		}{}
		if !c.decode(&input) || !acct.passwordIs(input.Hash) {
			return problem(400, "Invalid password."), nil
		}
		return reply(200, map[string]any{"object": "masterPasswordPolicy"}), nil
	case "POST password", "POST kdf":
		return c.changePassword(acct, part == "kdf")
	case "DELETE ", "POST delete":
		input := struct {
			Hash string `json:"masterPasswordHash"`
		}{}
		if !c.decode(&input) || !acct.passwordIs(input.Hash) {
			return problem(400, "Invalid password."), nil
		}
		budget := 150
		if err := closeAccount(c.store, acct, &budget); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return reply(200, nil), nil
	}
	return problem(404, "Not found."), nil
}

// profile answers with the account's profile and its shared vaults.
func (c *call) profile(acct account) (leafen.EndpointAnswer, error) {
	_, places, err := c.store.viewOf(acct)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	orgs, err := c.store.profileOrganizations(places)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, profileOf(acct, orgs)), nil
}

// changePassword takes a new master password (or new key settings): the
// app re-encrypts the vault's key with it and every other device signs in
// again.
func (c *call) changePassword(acct account, kdfChange bool) (leafen.EndpointAnswer, error) {
	input := struct {
		Hash           string `json:"masterPasswordHash"`
		NewHash        string `json:"newMasterPasswordHash"`
		Hint           string `json:"masterPasswordHint"`
		Key            string `json:"key"`
		Kdf            int64  `json:"kdf"`
		KdfIterations  int64  `json:"kdfIterations"`
		KdfMemory      *int64 `json:"kdfMemory"`
		KdfParallelism *int64 `json:"kdfParallelism"`
	}{}
	if !c.decode(&input) || !acct.passwordIs(input.Hash) {
		return problem(400, "Invalid password."), nil
	}
	if input.NewHash == "" || input.Key == "" || tooLong(input.NewHash, input.Key, input.Hint) {
		return problem(400, "Give the new master password."), nil
	}
	salt := randomToken(16)
	values := map[string]any{"Hash": serverHash(input.NewHash, salt), "Salt": salt, "Key": input.Key, "Stamp": randomToken(18),
		"Revision": float64(nowMs())}
	if kdfChange {
		kdf := kdfSettings{Kdf: input.Kdf, Iterations: input.KdfIterations}
		if input.KdfMemory != nil {
			kdf.Memory = *input.KdfMemory
		}
		if input.KdfParallelism != nil {
			kdf.Parallelism = *input.KdfParallelism
		}
		if !kdf.valid() {
			return problem(400, "Choose PBKDF2 with 100,000 or more iterations, or Argon2id."), nil
		}
		values["Kdf"], values["Iterations"], values["Memory"], values["Parallelism"] = float64(kdf.Kdf), float64(kdf.Iterations), float64(kdf.Memory), float64(kdf.Parallelism)
	} else {
		values["Hint"] = input.Hint
	}
	if err := c.store.accounts.Update(acct.row.ID, values); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, nil), nil
}

func (c *call) deviceRoute(acct account, part, rest string) (leafen.EndpointAnswer, error) {
	switch {
	case c.method == "GET" && part == "":
		devices, err := owned(c.store.devices, acct.row.ID)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		listed := []any{}
		for _, device := range devices {
			listed = append(listed, map[string]any{"id": bitwardenID(device.ID), "name": device.Text("Name"), "type": number(device, "Type"),
				"identifier": device.Text("Identifier"), "creationDate": stamp(number(device, "Created")), "isTrusted": false, "object": "device"})
		}
		return list(listed), nil
	case c.method == "PUT" && part == "identifier":
		// Push notices: Leafen's vault sends none, so a device's push token
		// has nowhere to go.
		return reply(200, nil), nil
	}
	return problem(404, "Not found."), nil
}
