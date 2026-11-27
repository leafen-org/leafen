package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	databaseAutomationSlackConnectionsEnvironment = "LEAFEN_AUTOMATION_SLACK_CONNECTIONS"
	databaseAutomationSlackPostMessageURL         = "https://slack.com/api/chat.postMessage"
	databaseAutomationDeliveryRequestTimeout      = 10 * time.Second
)

type databaseAutomationDeliveryRequest struct {
	ID          string
	Channel     string
	PayloadJSON string
	// Attempt counts this delivery's tries, this one included.
	Attempt int
}

type databaseAutomationDeliveryProviderResult struct {
	State      string
	ProviderID string
	ErrorCode  string
}

const (
	databaseAutomationDeliveryResultSucceeded = "succeeded"
	databaseAutomationDeliveryResultRetryable = "retryable"
	databaseAutomationDeliveryResultFailed    = "failed"
)

type databaseAutomationDeliveryProvider interface {
	SupportsIdempotency(channel string) bool
	Deliver(context.Context, databaseAutomationDeliveryRequest) databaseAutomationDeliveryProviderResult
}

type databaseAutomationEmailProvider interface {
	SupportsIdempotency() bool
	Send(context.Context, databaseAutomationEmailPayload, string) (string, error)
}

type nativeDatabaseAutomationDeliveryProvider struct {
	client *http.Client
	email  databaseAutomationEmailProvider
	getenv func(string) string
}

func newNativeDatabaseAutomationDeliveryProvider() *nativeDatabaseAutomationDeliveryProvider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(
			ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext,
		)
	}
	return &nativeDatabaseAutomationDeliveryProvider{
		client: &http.Client{
			Transport:     transport,
			Timeout:       databaseAutomationDeliveryRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		getenv: os.Getenv,
	}
}

func (p *nativeDatabaseAutomationDeliveryProvider) SupportsIdempotency(channel string) bool {
	switch channel {
	case databaseAutomationDeliveryWebhook, databaseAutomationDeliverySlack:
		return true
	case databaseAutomationDeliveryEmail:
		return p.email != nil && p.email.SupportsIdempotency()
	default:
		return false
	}
}

func (p *nativeDatabaseAutomationDeliveryProvider) Deliver(
	ctx context.Context, request databaseAutomationDeliveryRequest,
) databaseAutomationDeliveryProviderResult {
	switch request.Channel {
	case databaseAutomationDeliveryEmail:
		return p.deliverEmail(ctx, request)
	case databaseAutomationDeliveryWebhook:
		return p.deliverWebhook(ctx, request)
	case databaseAutomationDeliverySlack:
		return p.deliverSlack(ctx, request)
	default:
		return databaseAutomationDeliveryProviderResult{
			State: databaseAutomationDeliveryResultFailed, ErrorCode: "delivery_channel_invalid",
		}
	}
}

type databaseAutomationEmailPayload struct {
	RecipientEmail string `json:"recipientEmail"`
	Subject        string `json:"subject"`
	Message        string `json:"message"`
}

func (p *nativeDatabaseAutomationDeliveryProvider) deliverEmail(
	ctx context.Context, request databaseAutomationDeliveryRequest,
) databaseAutomationDeliveryProviderResult {
	payload := databaseAutomationEmailPayload{}
	if json.Unmarshal([]byte(request.PayloadJSON), &payload) != nil || p.email == nil ||
		!p.email.SupportsIdempotency() {
		return databaseAutomationDeliveryProviderResult{
			State: databaseAutomationDeliveryResultFailed, ErrorCode: "email_provider_unavailable",
		}
	}
	providerID, err := p.email.Send(ctx, payload, request.ID)
	if err != nil {
		return databaseAutomationDeliveryProviderResult{
			State: databaseAutomationDeliveryResultRetryable, ErrorCode: "email_provider_retryable",
		}
	}
	return databaseAutomationDeliveryProviderResult{
		State: databaseAutomationDeliveryResultSucceeded, ProviderID: boundedDatabaseAutomationProviderID(providerID),
	}
}

type databaseAutomationWebhookPayload struct {
	URL     string                               `json:"url"`
	Headers []interactiveAutomationWebhookHeader `json:"headers"`
	Body    json.RawMessage                      `json:"body"`
}

func (p *nativeDatabaseAutomationDeliveryProvider) deliverWebhook(
	ctx context.Context, request databaseAutomationDeliveryRequest,
) databaseAutomationDeliveryProviderResult {
	payload := databaseAutomationWebhookPayload{}
	if json.Unmarshal([]byte(request.PayloadJSON), &payload) != nil || len(payload.Body) == 0 ||
		len(payload.Body) > databaseAutomationDeliveryPayloadBytes {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_payload_invalid"}
	}
	normalized, err := normalizeDatabaseAutomationPublicURL(payload.URL)
	if err != nil || normalized != payload.URL {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_target_invalid"}
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_target_invalid"}
	}
	body := databaseAutomationWebhookBodyAttempt(payload.Body, request.Attempt)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_request_invalid"}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Idempotency-Key", request.ID)
	httpRequest.Header.Set("X-Leafen-Automation-Delivery", request.ID)
	for _, header := range payload.Headers {
		httpRequest.Header.Set(header.Name, header.Value)
	}
	response, err := p.client.Do(httpRequest)
	if errors.Is(err, errOutboundUnsafeDestination) {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_target_invalid"}
	}
	if err != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "webhook_provider_retryable"}
	}
	defer response.Body.Close()
	if !databaseAutomationResponseWithinLimit(response.Body) {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_response_oversized"}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultSucceeded, ProviderID: request.ID}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "webhook_provider_retryable"}
	}
	return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_provider_rejected"}
}

type databaseAutomationSlackPayload struct {
	ConnectionID string `json:"connectionId"`
	ChannelID    string `json:"channelId"`
	Message      string `json:"message"`
}

func (p *nativeDatabaseAutomationDeliveryProvider) deliverSlack(
	ctx context.Context, request databaseAutomationDeliveryRequest,
) databaseAutomationDeliveryProviderResult {
	payload := databaseAutomationSlackPayload{}
	connections, valid := databaseAutomationSlackConnections(p.getenv(databaseAutomationSlackConnectionsEnvironment))
	token := connections[payload.ConnectionID]
	if json.Unmarshal([]byte(request.PayloadJSON), &payload) != nil || !valid {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "slack_configuration_invalid"}
	}
	token = connections[payload.ConnectionID]
	if token == "" {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "slack_connection_missing"}
	}
	body, _ := json.Marshal(map[string]any{
		"channel": payload.ChannelID, "text": payload.Message,
		"client_msg_id": databaseAutomationSlackClientMessageID(request.ID),
	})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, databaseAutomationSlackPostMessageURL, bytes.NewReader(body))
	if err != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "slack_request_invalid"}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "slack_provider_retryable"}
	}
	defer response.Body.Close()
	limited, ok := databaseAutomationReadResponse(response.Body)
	if !ok {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "slack_response_oversized"}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "slack_provider_retryable"}
	}
	var decoded struct {
		OK      bool   `json:"ok"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(limited, &decoded) != nil {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "slack_provider_retryable"}
	}
	if decoded.OK {
		return databaseAutomationDeliveryProviderResult{
			State:      databaseAutomationDeliveryResultSucceeded,
			ProviderID: boundedDatabaseAutomationProviderID(decoded.Channel + ":" + decoded.TS),
		}
	}
	if decoded.Error == "ratelimited" || decoded.Error == "internal_error" || decoded.Error == "fatal_error" {
		return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultRetryable, ErrorCode: "slack_provider_retryable"}
	}
	return databaseAutomationDeliveryProviderResult{State: databaseAutomationDeliveryResultFailed, ErrorCode: "slack_provider_rejected"}
}

func databaseAutomationSlackConnections(raw string) (map[string]string, bool) {
	if len(raw) == 0 || len(raw) > databaseAutomationDeliveryPayloadBytes {
		return map[string]string{}, false
	}
	values := map[string]string{}
	if json.Unmarshal([]byte(raw), &values) != nil || len(values) > 100 {
		return map[string]string{}, false
	}
	for id, token := range values {
		if strings.TrimSpace(id) != id || id == "" || len([]rune(id)) > interactiveAutomationMaxIdentifierRunes ||
			strings.TrimSpace(token) != token || token == "" {
			return map[string]string{}, false
		}
	}
	return values, true
}

func databaseAutomationSlackClientMessageID(id string) string {
	digest := sha256.Sum256([]byte(id))
	value := hex.EncodeToString(digest[:16])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32]
}

func databaseAutomationReadResponse(body io.Reader) ([]byte, bool) {
	value, err := io.ReadAll(io.LimitReader(body, databaseAutomationDeliveryResponseBytes+1))
	return value, err == nil && len(value) <= databaseAutomationDeliveryResponseBytes
}

func databaseAutomationResponseWithinLimit(body io.Reader) bool {
	_, ok := databaseAutomationReadResponse(body)
	return ok
}

func boundedDatabaseAutomationProviderID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > databaseAutomationMaxWebhookHeaderValueRunes {
		return ""
	}
	return value
}
