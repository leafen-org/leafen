package app

import (
	"os"
	"strings"
)

// How the server was started, as the first setup's state tells a setup
// screen opened from outside (2026-11-27: a server from Railway's template
// showed a home server's guide — open it from the same network, read a
// NAS's or Docker's log — and folded away the code field, though the
// template had made the code and Railway lists it among the service's
// variables). Two facts, read here alone and sent with the state
// (administrator.go): whether the server was given a setup code
// (LEAFEN_SETUP_CODE, setup_code.go), and the hosting platform it knows it
// runs on. The screens, and an agent asked for the setup link, go by them:
// a server given a code shows its field first with where the code is, in the
// platform's words; one on a known platform says where its log is there;
// the same-network guide is left to the others, which may have a home or an
// office network around them.

// railwayEnvironment are variables Railway sets in every deployment of a
// service; a server started anywhere else has none of them.
var railwayEnvironment = []string{"RAILWAY_PROJECT_ID", "RAILWAY_SERVICE_ID", "RAILWAY_ENVIRONMENT_ID", "RAILWAY_DEPLOYMENT_ID"}

// setupHostingPlatform is the hosting platform the server knows it runs on,
// or "" for one it doesn't know (a home server, a machine of one's own, a
// platform it has no words for).
func setupHostingPlatform() string {
	for _, name := range railwayEnvironment {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return setupPlatformRailway
		}
	}
	return ""
}

// setupCodeGiven says whether the server was started with a setup code it
// takes (operatorSetupCode: one long enough).
func setupCodeGiven() bool {
	return operatorSetupCode() != ""
}
