package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	wikiVerificationEmailEndpointEnvironment = "LEAFEN_WIKI_VERIFICATION_EMAIL_ENDPOINT"
	wikiVerificationEmailClaimLimit          = 8
	wikiVerificationEmailConcurrency         = 4
	wikiVerificationEmailMaxAttempts         = 5
	wikiVerificationEmailRequestTimeout      = 10 * time.Second

	wikiVerificationEmailReady     = "ready"
	wikiVerificationEmailClaimed   = "claimed"
	wikiVerificationEmailRetryable = "retryable"
	wikiVerificationEmailSent      = "sent"
	wikiVerificationEmailSkipped   = "skipped"
	wikiVerificationEmailFailed    = "failed"
)

type wikiVerificationEmailPayload struct {
	RecipientEmail string `json:"recipientEmail"`
	PageID         string `json:"pageId"`
	PageTitle      string `json:"pageTitle"`
}

type wikiVerificationEmailResult struct {
	State      string
	ProviderID string
	ErrorCode  string
}

type wikiVerificationEmailProvider interface {
	Deliver(context.Context, wikiVerificationEmailPayload, string) wikiVerificationEmailResult
}

type nativeWikiVerificationEmailProvider struct {
	endpoint string
	client   *http.Client
}

func newNativeWikiVerificationEmailProvider() wikiVerificationEmailProvider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(
			ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext,
		)
	}
	return &nativeWikiVerificationEmailProvider{
		endpoint: os.Getenv(wikiVerificationEmailEndpointEnvironment),
		client: &http.Client{
			Transport: transport,
			Timeout:   wikiVerificationEmailRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (provider *nativeWikiVerificationEmailProvider) Deliver(
	ctx context.Context,
	payload wikiVerificationEmailPayload,
	deliveryID string,
) wikiVerificationEmailResult {
	if provider == nil || provider.endpoint == "" {
		return wikiVerificationEmailResult{State: wikiVerificationEmailSkipped}
	}
	normalized, err := normalizeDatabaseAutomationPublicURL(provider.endpoint)
	if err != nil || normalized != provider.endpoint {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "endpoint_invalid"}
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "endpoint_invalid"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "payload_invalid"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "request_invalid"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", deliveryID)
	request.Header.Set("X-Leafen-Wiki-Verification-Delivery", deliveryID)
	response, err := provider.client.Do(request)
	if errors.Is(err, errOutboundUnsafeDestination) {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "endpoint_invalid"}
	}
	if err != nil {
		return wikiVerificationEmailResult{State: wikiVerificationEmailRetryable, ErrorCode: "provider_retryable"}
	}
	defer response.Body.Close()
	if !databaseAutomationResponseWithinLimit(response.Body) {
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "response_oversized"}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return wikiVerificationEmailResult{State: wikiVerificationEmailSent, ProviderID: deliveryID}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return wikiVerificationEmailResult{State: wikiVerificationEmailRetryable, ErrorCode: "provider_retryable"}
	}
	return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "provider_rejected"}
}

type storedWikiVerificationEmailDelivery struct {
	ID             string `db:"id"`
	PageID         string `db:"page_id"`
	DataSourceID   string `db:"data_source_id"`
	Generation     int    `db:"generation"`
	WorkspaceID    string `db:"workspace_id"`
	UserID         string `db:"user_id"`
	RecipientEmail string `db:"recipient_email"`
	PageTitle      string `db:"page_title"`
	Status         string `db:"status"`
	Attempts       int    `db:"attempts"`
	NextAttemptAt  int64  `db:"next_attempt_at"`
	LeaseToken     string `db:"lease_token"`
}

type wikiVerificationQueueKey struct {
	PageID       string
	DataSourceID string
	Generation   int
}

func claimWikiVerificationEmailDeliveries(
	ctx context.Context,
	app core.App,
	now time.Time,
) ([]storedWikiVerificationEmailDelivery, bool, error) {
	claimed := []storedWikiVerificationEmailDelivery{}
	hasMore := false
	err := app.RunInTransaction(func(txApp core.App) error {
		released, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_wiki_verification_email_deliveries}}
			SET [[status]]='retryable',[[lease_token]]='',[[lease_until]]=0,
				[[next_attempt_at]]=MIN([[next_attempt_at]],{:now}),[[updated_at]]={:now}
			WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_wiki_verification_email_deliveries}}
				INDEXED BY [[idx_leafen_wiki_verification_email_claimed]]
				WHERE [[status]]='claimed' AND [[lease_until]] <= {:now}
				ORDER BY [[lease_until]],[[id]] LIMIT {:limit}
			)
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now.UnixMilli(), "limit": wikiVerificationEmailClaimLimit,
		}).Execute()
		if err != nil {
			return err
		}
		releasedCount, err := released.RowsAffected()
		if err != nil {
			return err
		}
		hasMore = releasedCount == wikiVerificationEmailClaimLimit
		candidates := []storedWikiVerificationEmailDelivery{}
		for _, status := range []string{wikiVerificationEmailReady, wikiVerificationEmailRetryable} {
			statusCandidates := []storedWikiVerificationEmailDelivery{}
			if err := txApp.DB().NewQuery(`
				SELECT [[id]],[[page_id]],[[data_source_id]],[[generation]],[[workspace_id]],[[user_id]],
					[[recipient_email]],[[page_title]],[[status]],[[attempts]],[[next_attempt_at]],
					'' AS [[lease_token]]
				FROM {{leafen_wiki_verification_email_deliveries}}
				INDEXED BY [[idx_leafen_wiki_verification_email_ready]]
				WHERE [[status]]={:status} AND [[next_attempt_at]] <= {:now}
				ORDER BY [[next_attempt_at]],[[id]] LIMIT {:limit}
			`).WithContext(ctx).Bind(dbx.Params{
				"status": status, "now": now.UnixMilli(),
				"limit": wikiVerificationEmailClaimLimit + 1,
			}).All(&statusCandidates); err != nil {
				return err
			}
			candidates = append(candidates, statusCandidates...)
		}
		sort.Slice(candidates, func(left, right int) bool {
			if candidates[left].NextAttemptAt != candidates[right].NextAttemptAt {
				return candidates[left].NextAttemptAt < candidates[right].NextAttemptAt
			}
			return candidates[left].ID < candidates[right].ID
		})
		candidateMore := len(candidates) > wikiVerificationEmailClaimLimit
		hasMore = hasMore || candidateMore
		if candidateMore {
			candidates = candidates[:wikiVerificationEmailClaimLimit]
		}
		for _, candidate := range candidates {
			token := core.GenerateDefaultRandomId()
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_wiki_verification_email_deliveries}}
				SET [[status]]='claimed',[[lease_token]]={:token},[[lease_until]]={:leaseUntil},
					[[updated_at]]={:now}
				WHERE [[id]]={:id} AND [[status]]={:status}
					AND [[attempts]]={:attempts} AND [[next_attempt_at]] <= {:now}
			`).WithContext(ctx).Bind(dbx.Params{
				"token": token, "leaseUntil": now.Add(wikiVerificationLeaseDuration).UnixMilli(),
				"now": now.UnixMilli(), "id": candidate.ID, "status": candidate.Status,
				"attempts": candidate.Attempts,
			}).Execute()
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows == 1 {
				candidate.LeaseToken = token
				claimed = append(claimed, candidate)
			}
		}
		return nil
	})
	return claimed, hasMore, err
}

// holdWikiVerificationEmailsToInactive keeps back the emails to an account
// that may not act — being deleted, deleted or suspended — as its other
// emails are (sendNotificationEmailBatch): they settle as skipped, and the
// rest go. The address each was queued with is never used for them.
func holdWikiVerificationEmailsToInactive(
	app core.App,
	deliveries []storedWikiVerificationEmailDelivery,
) ([]storedWikiVerificationEmailDelivery, map[string]wikiVerificationEmailResult, error) {
	sending := make([]storedWikiVerificationEmailDelivery, 0, len(deliveries))
	results := map[string]wikiVerificationEmailResult{}
	mayAct := map[string]bool{}
	for _, delivery := range deliveries {
		active, known := mayAct[delivery.UserID]
		if !known {
			var err error
			if active, err = accountMayAct(app, delivery.UserID); err != nil {
				// None goes this turn; each is tried again later.
				for _, each := range deliveries {
					results[each.ID] = wikiVerificationEmailResult{State: wikiVerificationEmailRetryable, ErrorCode: "recipient_unreadable"}
				}
				return nil, results, err
			}
			mayAct[delivery.UserID] = active
		}
		if active {
			sending = append(sending, delivery)
		} else {
			results[delivery.ID] = wikiVerificationEmailResult{State: wikiVerificationEmailSkipped, ErrorCode: "recipient_inactive"}
		}
	}
	return sending, results, nil
}

func deliverWikiVerificationEmails(
	ctx context.Context,
	deliveries []storedWikiVerificationEmailDelivery,
	provider wikiVerificationEmailProvider,
) map[string]wikiVerificationEmailResult {
	results := map[string]wikiVerificationEmailResult{}
	var mu sync.Mutex
	var wait sync.WaitGroup
	semaphore := make(chan struct{}, wikiVerificationEmailConcurrency)
	for _, delivery := range deliveries {
		delivery := delivery
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			result := wikiVerificationEmailResult{State: wikiVerificationEmailSkipped}
			if provider != nil {
				requestCtx, cancel := context.WithTimeout(ctx, wikiVerificationEmailRequestTimeout)
				result = provider.Deliver(requestCtx, wikiVerificationEmailPayload{
					RecipientEmail: delivery.RecipientEmail,
					PageID:         delivery.PageID,
					PageTitle:      delivery.PageTitle,
				}, delivery.ID)
				cancel()
			}
			mu.Lock()
			results[delivery.ID] = normalizeWikiVerificationEmailResult(result)
			mu.Unlock()
		}()
	}
	wait.Wait()
	return results
}

func settleWikiVerificationEmailDeliveries(
	ctx context.Context,
	app core.App,
	deliveries []storedWikiVerificationEmailDelivery,
	results map[string]wikiVerificationEmailResult,
	now time.Time,
) (wikiVerificationTurn, error) {
	turn := wikiVerificationTurn{}
	err := app.RunInTransaction(func(txApp core.App) error {
		queueKeys := map[wikiVerificationQueueKey]storedWikiVerificationEmailDelivery{}
		for _, delivery := range deliveries {
			result := results[delivery.ID]
			status := result.State
			attempts := delivery.Attempts + 1
			nextAttemptAt := int64(0)
			sentAt := int64(0)
			if status == wikiVerificationEmailRetryable && attempts < wikiVerificationEmailMaxAttempts {
				nextAttemptAt = now.Add(wikiVerificationEmailRetryDelay(attempts)).UnixMilli()
			} else if status == wikiVerificationEmailRetryable {
				status = wikiVerificationEmailFailed
				result.ErrorCode = "retry_exhausted"
			} else if status == wikiVerificationEmailSent {
				sentAt = now.UnixMilli()
			}
			settled, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_wiki_verification_email_deliveries}}
				SET [[status]]={:status},[[attempts]]={:attempts},[[next_attempt_at]]={:nextAttemptAt},
					[[lease_token]]='',[[lease_until]]=0,[[error_code]]={:errorCode},
					[[provider_id]]={:providerId},[[sent_at]]={:sentAt},[[updated_at]]={:now}
				WHERE [[id]]={:id} AND [[status]]='claimed' AND [[lease_token]]={:token}
			`).WithContext(ctx).Bind(dbx.Params{
				"status": status, "attempts": attempts, "nextAttemptAt": nextAttemptAt,
				"errorCode": result.ErrorCode, "providerId": result.ProviderID,
				"sentAt": sentAt, "now": now.UnixMilli(), "id": delivery.ID,
				"token": delivery.LeaseToken,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := settled.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 1 {
				switch status {
				case wikiVerificationEmailRetryable:
					turn.Retried++
				case wikiVerificationEmailSent:
					turn.Emails++
				case wikiVerificationEmailFailed:
					turn.Failed++
				}
				queueKeys[wikiVerificationQueueKey{
					PageID: delivery.PageID, DataSourceID: delivery.DataSourceID, Generation: delivery.Generation,
				}] = delivery
			}
		}
		for _, delivery := range queueKeys {
			if err := settleWikiVerificationQueueDelivery(ctx, txApp, delivery, now); err != nil {
				return err
			}
		}
		return nil
	})
	return turn, err
}

func settleWikiVerificationQueueDelivery(
	ctx context.Context,
	app core.App,
	delivery storedWikiVerificationEmailDelivery,
	now time.Time,
) error {
	var pending, failed int
	if err := app.DB().NewQuery(`
		SELECT
			SUM(CASE WHEN [[status]] IN ('ready','claimed','retryable') THEN 1 ELSE 0 END),
			SUM(CASE WHEN [[status]]='failed' THEN 1 ELSE 0 END)
		FROM {{leafen_wiki_verification_email_deliveries}}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId} AND [[generation]]={:generation}
	`).WithContext(ctx).Bind(dbx.Params{
		"pageId": delivery.PageID, "dataSourceId": delivery.DataSourceID, "generation": delivery.Generation,
	}).Row(&pending, &failed); err != nil {
		return err
	}
	status := wikiVerificationQueueCompleted
	errorCode := ""
	completedAt := now.UnixMilli()
	if failed > 0 {
		status = wikiVerificationQueueFailed
		errorCode = "email_delivery_failed"
	} else if pending > 0 {
		status = wikiVerificationQueueDelivering
		completedAt = 0
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_verification_queue}}
		SET [[status]]={:status},[[error_code]]={:errorCode},
			[[completed_at]]={:completedAt},[[updated_at]]={:now}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId} AND [[generation]]={:generation}
			AND [[status]]='delivering'
	`).WithContext(ctx).Bind(dbx.Params{
		"status": status, "errorCode": errorCode, "completedAt": completedAt,
		"now": now.UnixMilli(), "pageId": delivery.PageID, "dataSourceId": delivery.DataSourceID,
		"generation": delivery.Generation,
	}).Execute()
	return err
}

func normalizeWikiVerificationEmailResult(result wikiVerificationEmailResult) wikiVerificationEmailResult {
	switch result.State {
	case wikiVerificationEmailSent, wikiVerificationEmailRetryable,
		wikiVerificationEmailSkipped, wikiVerificationEmailFailed:
	default:
		return wikiVerificationEmailResult{State: wikiVerificationEmailFailed, ErrorCode: "provider_result_invalid"}
	}
	result.ProviderID = boundedDatabaseAutomationProviderID(result.ProviderID)
	result.ErrorCode = databaseAutomationDeliveryErrorCode.FindString(result.ErrorCode)
	if result.State != wikiVerificationEmailSent && result.State != wikiVerificationEmailSkipped && result.ErrorCode == "" {
		result.ErrorCode = "provider_failure"
	}
	return result
}

func wikiVerificationEmailRetryDelay(attempt int) time.Duration {
	delay := time.Minute
	for current := 1; current < attempt && delay < time.Hour; current++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
