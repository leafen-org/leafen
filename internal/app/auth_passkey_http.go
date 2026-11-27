package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerPasskeyRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/auth/passkeys", readPasskeyCapability)
	event.Router.POST(apiRoot+"/auth/passkeys/begin", beginPasskeyLogin).Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+"/auth/passkeys/finish", finishPasskeyLogin).Bind(apis.BodyLimit(passkeyMaxBodyBytes))
	event.Router.POST(apiRoot+"/auth/passkeys/app/exchange", exchangePasskeyApp).Bind(apis.BodyLimit(32768))
	event.Router.GET(apiRoot+"/auth/passkeys/app/manage-return", passkeyManageReturn)
	event.Router.GET(apiRoot+"/account/passkeys", readAccountPasskeys)
	event.Router.POST(apiRoot+"/account/passkeys/begin", beginPasskeyRegistration).Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+"/account/passkeys/finish", finishPasskeyRegistration).Bind(apis.BodyLimit(passkeyMaxBodyBytes))
	event.Router.DELETE(apiRoot+"/account/passkeys", removeAccountPasskey).Bind(apis.BodyLimit(4096))
}

func passkeyManageReturn(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	appReturn, valid := parseAuthAppReturn(query.Get(authAppReturnParam), query.Get(authAppChallengeParam))
	if !valid || appReturn.Address == "" {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	destination, err := authAppReturnDestination(appReturn.Address, "managed", "1")
	if err != nil {
		return passkeyAPIError(event, err)
	}
	return event.JSON(http.StatusOK, passkeyLoginResponse{ReturnURL: destination})
}

func readPasskeyCapability(event *core.RequestEvent) error {
	_, capability, _ := configuredPasskeyRP()
	return event.JSON(http.StatusOK, capability)
}

type passkeyAppReceipt struct {
	Kind         string `json:"kind"`
	UserID       string `json:"userId"`
	SessionID    string `json:"sessionId"`
	TokenKeyHash string `json:"tokenKeyHash"`
	Challenge    string `json:"challenge"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func passkeyAppCompletion(app core.App, ticket passkeyLoginTicket, session sessionResponse, tokenKeyHash string) (passkeyLoginResponse, error) {
	code, err := sealPasskeyValue(app, passkeyAppReceipt{
		Kind: "passkey-app:v1", UserID: session.User.ID, SessionID: session.SessionID,
		TokenKeyHash: tokenKeyHash, Challenge: ticket.AppReturn.Challenge,
		ExpiresAt: ticket.Session.Expires.UnixMilli(),
	})
	if err != nil {
		return passkeyLoginResponse{}, err
	}
	destination, err := authAppReturnDestination(ticket.AppReturn.Address, "passkeyCompletion", code)
	return passkeyLoginResponse{ReturnURL: destination}, err
}

func exchangePasskeyApp(event *core.RequestEvent) error {
	input := authProviderExchangeInput{}
	if err := event.BindBody(&input); err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	receipt := passkeyAppReceipt{}
	if err := openPasskeyValue(event.App, input.Code, &receipt); err != nil {
		return passkeyAPIError(event, err)
	}
	if receipt.Kind != "passkey-app:v1" || receipt.ExpiresAt <= time.Now().UTC().UnixMilli() || !authAppVerifierMatches(receipt.Challenge, input.Verifier) {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	response := sessionResponse{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(tx core.App) error {
		user, err := tx.FindRecordById("users", receipt.UserID)
		if err != nil {
			return err
		}
		if receipt.TokenKeyHash != hashAuthMFAValue(user.TokenKey()) {
			return errPasskeyInvalid
		}
		if err := authorizePasskeySignIn(tx, user); err != nil {
			return err
		}
		var remaining int64
		if err := tx.DB().NewQuery(`SELECT [[expires_at]] FROM {{leafen_auth_sessions}}
			WHERE [[id]] = {:id} AND [[owner_id]] = {:user} AND [[revoked_at]] = 0 AND [[request_id]] GLOB 'passkey-login:*'`).
			Bind(map[string]any{"id": receipt.SessionID, "user": receipt.UserID}).Row(&remaining); err != nil {
			return refusedUnlessStorage(errPasskeyInvalid, err)
		}
		remaining -= time.Now().UTC().UnixMilli()
		if remaining <= 0 {
			return errPasskeyInvalid
		}
		token, err := newBrowserSessionToken(user, receipt.SessionID, time.Duration(remaining)*time.Millisecond)
		if err != nil {
			return err
		}
		response = browserSessionResponse(user, receipt.SessionID, token)
		return nil
	})
	if err != nil {
		return passkeyAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func passkeyAPIError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errAuthSessionInvalid):
		return authSessionInvalidAPIError(event)
	case errors.Is(err, errPasskeyUnavailable):
		return apiError(event, http.StatusServiceUnavailable, errorCodePasskeyUnavailable, "Ask your administrator to configure a secure public address for passkeys.")
	case errors.Is(err, errPasskeyInvalid):
		return apiError(event, http.StatusBadRequest, errorCodePasskeyInvalid, "Start the passkey operation again.")
	case errors.Is(err, errPasskeyConflict):
		return apiError(event, http.StatusConflict, errorCodePasskeyConflict, "Refresh the passkey settings and try again.")
	case errors.Is(err, errPasskeyLimit):
		return apiError(event, http.StatusConflict, errorCodePasskeyLimit, "Remove a passkey before adding another.")
	case errors.Is(err, errSAMLRequired):
		return apiError(event, http.StatusForbidden, errorCodeSAMLRequired, "This organization requires SAML login.")
	default:
		if storage, ok := authStorageAPIError(event, err, "account.passkeys", "passkey-authority"); ok {
			return storage
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePasskeyFailed, "Couldn't complete the passkey operation.", "account.passkeys", "passkey-authority", errorCodePasskeyFailed, err)
	}
}
