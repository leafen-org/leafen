package app

import (
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A call its person said no to says so and what they were asked, so its
// agent can tell a person's answer from a refusal of its own (2026-11-16, an
// agent calling the operations that ask first: every one ended in "The
// person declined this call", and it couldn't tell whether anyone had been
// asked). The answer is still a refusal (403): nothing was done.

// mcpActionDeclinedEarlier is a request its person already said no to.
const mcpActionDeclinedEarlier = "Nothing was done: your person said no to this request (%s) when Leafen asked them. " +
	"Ask them before trying it again; calling without the confirmation asks them again."

func mcpActionDeclined(event *core.RequestEvent, call personAskCall, ask personAsk, operation, request string, body []byte) error {
	message := fmt.Sprintf(mcpActionDeclinedEarlier, call.PassedOn)
	return event.JSON(http.StatusForbidden, mcpActionApprovalAnswer{
		Error: errorBody{Code: errorCodeUserKeyApprovalRefused, Message: message, Reference: newDiagnosticReference()},
		Approval: mcpActionApprovalPending{ApprovalID: call.PassedOn, Operation: operation, Family: mcpActionFamily(operation),
			Request: request, Final: mcpActionFinal(operation, body), Ask: ask, Message: message},
	})
}
