//go:build !leafen_core

package main

import "leafen.org/leafen/organization"

// The server alone attaches the organization module as the desktop app does
// (desktop/organization.go): its screens come with the web the image serves,
// so their routes must too; a build tagged leafen_core is the core alone.
func init() { organization.Attach() }
