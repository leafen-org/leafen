package main

import (
	"errors"
	"slices"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// What Leafen's Vault app shows of a person's shared vaults — their names
// (Bitwarden keeps them in the clear), the person's role, who waits to be
// confirmed and the Leafen groups each follows — and the linking of groups
// by its owners and administrators. The items stay where only Bitwarden's
// apps open them.

func sharedOf(s *store, acct account) (map[string]any, error) {
	_, places, err := s.viewOf(acct)
	if err != nil {
		return nil, err
	}
	vaults := []any{}
	manages := false
	for _, m := range places {
		org, err := s.organizationByID(bitwardenID(m.Organization))
		if errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		vault := map[string]any{"id": org.id(), "name": org.Name, "role": m.Role, "state": memberStateName(m.State), "manages": m.manages()}
		links := []any{}
		for _, link := range org.links() {
			links = append(links, map[string]any{"id": link.ID, "name": link.Name})
		}
		vault["groups"] = links
		if m.manages() {
			manages = true
			members, err := s.membersOf(org)
			if err != nil {
				return nil, err
			}
			waiting := 0
			for _, other := range members {
				if other.State == memberAccepted {
					waiting++
				}
			}
			vault["members"], vault["waiting"] = len(members), waiting
		}
		vaults = append(vaults, vault)
	}
	answer := map[string]any{"vaults": vaults}
	if manages {
		groups, err := s.leafenGroups()
		if err != nil {
			return nil, err
		}
		listed := []any{}
		for _, group := range groups {
			listed = append(listed, map[string]any{"id": group.ID, "name": group.Name})
		}
		answer["groups"] = listed
	}
	return answer, nil
}

// sharedCall finds the person's vault for a shared vault action.
func sharedCall(input any) (*store, account, error) {
	_, s, acct, found, err := personCall(input)
	if err != nil {
		return nil, account{}, err
	}
	if !found || acct.Status != statusActive {
		return nil, account{}, errors.New("make your vault from your Bitwarden app first")
	}
	return s, acct, nil
}

// shared lists the person's shared vaults.
//
//go:wasmexport shared
func shared() int32 {
	s, acct, err := sharedCall(nil)
	if err != nil {
		return fail(err.Error())
	}
	answer, err := sharedOf(s, acct)
	if err != nil {
		return fail(err.Error())
	}
	_ = leafen.OutputJSON(answer)
	return 0
}

// managed finds a shared vault the person runs.
func managed(s *store, acct account, id string) (organization, error) {
	org, err := s.organizationByID(id)
	if errors.Is(err, errMissing) {
		return org, errors.New("that shared vault isn't here")
	}
	if err != nil {
		return org, err
	}
	m, found, err := s.membership(acct, org)
	if err != nil {
		return org, err
	}
	if !found || !m.manages() {
		return org, errors.New("only the shared vault's owners and administrators link groups")
	}
	return org, nil
}

// linkGroup links a Leafen group to a shared vault the person runs.
//
//go:wasmexport link
func linkGroup() int32 {
	input := struct {
		Vault string `json:"vault"`
		Group string `json:"group"`
	}{}
	s, acct, err := sharedCall(&input)
	if err != nil {
		return fail(err.Error())
	}
	org, err := managed(s, acct, input.Vault)
	if err != nil {
		return fail(err.Error())
	}
	if _, refusal, err := s.link(org, input.Group); refusal != "" || err != nil {
		if err == nil {
			err = errors.New(strings.ToLower(refusal[:1]) + strings.TrimSuffix(refusal[1:], "."))
		}
		return fail(err.Error())
	}
	return followed(s, acct, org)
}

// unlinkGroup stops a shared vault following a group: who came by it alone
// leaves.
//
//go:wasmexport unlink
func unlinkGroup() int32 {
	input := struct {
		Vault string `json:"vault"`
		Link  string `json:"link"`
	}{}
	s, acct, err := sharedCall(&input)
	if err != nil {
		return fail(err.Error())
	}
	org, err := managed(s, acct, input.Vault)
	if err != nil {
		return fail(err.Error())
	}
	links := org.links()
	at := slices.IndexFunc(links, func(link groupLink) bool { return strings.EqualFold(link.ID, input.Link) })
	if at < 0 {
		return fail("that group isn't linked to the shared vault")
	}
	if err := s.saveLinks(org, slices.Delete(links, at, at+1)); err != nil {
		return fail(err.Error())
	}
	return followed(s, acct, org)
}

// followed follows the shared vault's groups now and answers with the
// person's shared vaults.
func followed(s *store, acct account, org organization) int32 {
	fresh, err := s.organizationByID(org.id())
	if err != nil {
		return fail(err.Error())
	}
	if err := s.follow(fresh, 40); err != nil {
		leafen.Log("shared vault not followed: " + err.Error())
	}
	answer, err := sharedOf(s, acct)
	if err != nil {
		return fail(err.Error())
	}
	_ = leafen.OutputJSON(answer)
	return 0
}
