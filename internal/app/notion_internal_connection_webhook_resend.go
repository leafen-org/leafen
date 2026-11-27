package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// internalConnectionWebhookResendResponse is a subscription after its token
// was sent again: whether its address took the token and, when it didn't,
// the status it answered.
type internalConnectionWebhookResendResponse struct {
	internalConnectionWebhookResponse
	TokenDelivered bool `json:"tokenDelivered"`
	TokenStatus    int  `json:"tokenStatus"`
}

// resendInternalConnectionWebhookToken sends a waiting subscription its
// token again, as Notion's "토큰 재전송", and answers how that went, as
// Notion says it at once (seen 2026-11-21: an address that never answers
// refused with status 502) — never that a token went where none came. The
// send is kept among its deliveries, so one the address didn't take is a
// failed event (Notion's count went from one to two). A subscription already
// verified is sent nothing.
func resendInternalConnectionWebhookToken(event *core.RequestEvent) error {
	_, stored, err := managedInternalConnectionWebhook(event)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-resend")
	}
	if stored.VerifyState != internalConnectionWebhookPending {
		return event.JSON(http.StatusOK, internalConnectionWebhookResendResponse{
			internalConnectionWebhookResponse: internalConnectionWebhookOf(event.App, stored),
		})
	}
	token, err := decryptAdministratorSetting(event.App, stored.SecretCipher)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-resend-secret")
	}
	status := sendNotionWebhookVerificationNow(event.Request.Context(), event.App, stored, token)
	if err := recordNotionWebhookVerificationResent(event.App, stored, status, time.Now().UTC()); err != nil {
		return internalConnectionWebhookError(event, err, "webhook-resend-record")
	}
	return event.JSON(http.StatusOK, internalConnectionWebhookResendResponse{
		internalConnectionWebhookResponse: internalConnectionWebhookOf(event.App, stored),
		TokenStatus:                       status, TokenDelivered: status >= 200 && status < 300,
	})
}

// eventWebhookDeliveryRetries reports whether a delivery its address didn't
// take is tried again later (eventWebhookRetryWaits). A Notion
// subscription's verification token is sent once, as Notion's: an address
// that never answers kept its one failure and that failure's time for 49 s
// (2026-11-21 18:44) and again for 13 min (19:21–19:34), where Leafen had
// sent it again after 10 s, 1 min and 5 min. Only "토큰 재전송"
// (resendInternalConnectionWebhookToken) sends it again.
func eventWebhookDeliveryRetries(eventType string) bool {
	return eventType != notionWebhookVerify
}

// sendNotionWebhookVerificationNow posts a subscription its verification
// token as the queued first one goes (queueNotionWebhookVerification,
// eventWebhookRunner.deliver) and answers the status its address gave:
// 502, a gateway's "nothing answered", when it couldn't be reached or may
// not be (a private address), as Notion says of one.
func sendNotionWebhookVerificationNow(ctx context.Context, app core.App, subscription storedEventWebhookSubscription, token string) int {
	body, _ := json.Marshal(map[string]string{"verification_token": token})
	target, err := normalizeEventWebhookURL(subscription.URL)
	if err != nil {
		return http.StatusBadGateway
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadGateway
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "notion-api")
	request.Header.Set(notionWebhookSignature, notionWebhookSignatureValue(token, body))
	runner, running := app.Store().Get(eventWebhookRunnerStoreKey).(*eventWebhookRunner)
	if !running {
		runner = newEventWebhookRunner(app)
	}
	response, err := runner.client.Do(request)
	if err != nil {
		return http.StatusBadGateway
	}
	defer response.Body.Close()
	databaseAutomationResponseWithinLimit(response.Body)
	return response.StatusCode
}
