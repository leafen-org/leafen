package app

import (
	"fmt"
	"strings"
)

// A tenancy key refused is told the keys the server has, from the list the
// check that refused it reads (2026-11-26, the wider scorecard: strict mode
// turned on with tenancyKey "server" was told server was the key of the
// workspaces outside any organization — the server had none, so the check
// took no such key — and was given no organization's key; the agent gave up).

// appTenancyKeysNamed caps the keys a refusal names; GET /admin/apps lists them all.
const appTenancyKeysNamed = 10

// appTenancyRefusal is why key isn't one of tenancies, at the field that
// names it, or nil.
func appTenancyRefusal(field, key string, tenancies []appTenancyWire) error {
	for _, tenancy := range tenancies {
		if key != "" && tenancy.Key == key {
			return nil
		}
	}
	if key == "" {
		return refuse(errAppInputInvalid, "%s is empty: it names the tenancy, %s", field, appTenancyKeysSaid(tenancies))
	}
	return refuse(errAppInputInvalid, "%s is %s, not %q", field, appTenancyKeysSaid(tenancies), key)
}

// appTenancyKeysSaid names the keys a tenancy is taken by, each with whose it is.
func appTenancyKeysSaid(tenancies []appTenancyWire) string {
	if len(tenancies) == 0 {
		return "an organization's key (org: and its ID), and the server has none"
	}
	said := []string{}
	for index, tenancy := range tenancies {
		if index == appTenancyKeysNamed {
			said = append(said, fmt.Sprintf("%d more as GET /admin/apps lists them under tenancies", len(tenancies)-index))
			break
		}
		whose := tenancy.Name
		if tenancy.Key == appTenancyServer {
			whose = "the workspaces outside any organization"
		}
		said = append(said, fmt.Sprintf("%s (%s)", tenancy.Key, whose))
	}
	if len(said) == 1 {
		return said[0]
	}
	return "one of " + strings.Join(said, ", ")
}
