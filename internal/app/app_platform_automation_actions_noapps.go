//go:build leafen_noapps

package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A server built without apps takes no app actions in automations.

func normalizeInteractiveAutomationAppAction(*interactiveAutomationAction) error {
	return errInteractiveAutomationUnsupported
}

func queueAutomationAppActions(_ core.App, _ databaseAutomationEvaluationCandidate, _ []interactiveAutomationAction, _ string, _ time.Time) error {
	return nil
}

func automationAppActionAllowed(core.App, pageAuthorization, interactiveAutomationAction) bool {
	return false
}
