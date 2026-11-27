//go:build !leafen_core && !ios && !android

package main

import "leafen.org/leafen/organization"

// The release attaches the organization module to the server the program
// runs, its own or a server's command line's; a build tagged leafen_core is
// the core alone.
func init() { organization.Attach() }
