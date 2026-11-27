package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAPIBaseURLEnvironment = "LEAFEN_NOTION_API_BASE_URL"
const notionProviderRequestsPerSecond = 3

const (
	notionProviderMaximumConcurrentRequests = 3
	notionProviderAdmissionRetention        = 5 * time.Minute
	notionProviderOutageWait                = 30 * time.Second
)

type notionProviderAdmissionOwner struct {
	clock durableJobClock
	mu    sync.Mutex
	lanes map[string]*notionProviderAdmissionLane
	// The top-level scope reads going on, which another of the same joins.
	scopeWalks notionScopeWalks
}

type notionProviderAdmissionLane struct {
	mu         sync.Mutex
	concurrent chan struct{}
	schedule   chan struct{}
	next       time.Time
	retryAfter time.Time
	// When Notion last asked the connection to wait (askedToWaitSince).
	limitedAt  time.Time
	references int
	lastUsed   time.Time
}

type notionProviderRateLimitError struct {
	retryAt time.Time
}

func (err *notionProviderRateLimitError) Error() string {
	return errorCodeNotionImportProviderFailed
}

func newNotionProviderAdmissionOwner(clock durableJobClock) *notionProviderAdmissionOwner {
	return &notionProviderAdmissionOwner{clock: clock, lanes: map[string]*notionProviderAdmissionLane{}}
}

type notionProviderClient struct {
	baseURL      string
	token        string
	client       *http.Client
	admission    *notionProviderAdmissionOwner
	connectionID string
	// A read someone waits for (the import scope) asks again once Notion's
	// Retry-After has passed rather than handing the wait back.
	waitOutRateLimit bool
}

type notionProviderFileObject struct {
	Type string `json:"type"`
	Name string `json:"name"`
	File struct {
		URL        string `json:"url"`
		ExpiryTime string `json:"expiry_time"`
	} `json:"file"`
	External struct {
		URL string `json:"url"`
	} `json:"external"`
	Caption []notionProviderRichText `json:"caption"`
}

type notionProviderObject struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children"`
	IsArchived  bool   `json:"is_archived"`
	IsInline    *bool  `json:"is_inline,omitempty"`
	// What holds it; a data source names its database here.
	Parent      notionProviderParent `json:"parent"`
	CreatedTime string               `json:"created_time,omitempty"`
	// When Notion last changed it, for a migration run again.
	LastEditedTime string `json:"last_edited_time,omitempty"`
	// Who made it and who last changed it, which an imported row keeps.
	CreatedBy    *notionProviderUser               `json:"created_by,omitempty"`
	LastEditedBy *notionProviderUser               `json:"last_edited_by,omitempty"`
	Title        []notionProviderRichText          `json:"title"`
	Description  []notionProviderRichText          `json:"description"`
	Icon         json.RawMessage                   `json:"icon"`
	Cover        json.RawMessage                   `json:"cover"`
	Properties   map[string]notionProviderProperty `json:"properties"`
	Payload      json.RawMessage                   `json:"-"`
	// A block in a shape the import can't read, reported where it stood.
	Unreadable bool `json:"-"`
	Paragraph  struct {
		RichText []notionProviderRichText `json:"rich_text"`
	} `json:"paragraph"`
	Template struct {
		RichText []notionProviderRichText `json:"rich_text"`
	} `json:"template"`
	Table struct {
		TableWidth      int  `json:"table_width"`
		HasColumnHeader bool `json:"has_column_header"`
		HasRowHeader    bool `json:"has_row_header"`
	} `json:"table"`
	TableRow struct {
		Cells [][]notionProviderRichText `json:"cells"`
	} `json:"table_row"`
	ChildDatabase struct {
		Title string `json:"title"`
	} `json:"child_database"`
	ChildPage struct {
		Title string `json:"title"`
	} `json:"child_page"`
	File     notionProviderFileObject `json:"file"`
	PDF      notionProviderFileObject `json:"pdf"`
	Bookmark struct {
		URL     string                   `json:"url"`
		Caption []notionProviderRichText `json:"caption"`
	} `json:"bookmark"`
	LinkPreview struct {
		URL string `json:"url"`
	} `json:"link_preview"`
}

type notionProviderPage struct {
	Results    []notionProviderObject `json:"results"`
	HasMore    bool                   `json:"has_more"`
	NextCursor *string                `json:"next_cursor"`
}

func notionProviderContinuationCursor(current string, page notionProviderPage) (string, error) {
	if !page.HasMore {
		return "", nil
	}
	if page.NextCursor == nil || *page.NextCursor == "" || *page.NextCursor == current {
		return "", errors.New(errorCodeNotionImportProviderFailed)
	}
	return *page.NextCursor, nil
}

func notionProviderPageResults(page notionProviderPage) ([]notionProviderObject, error) {
	if len(page.Results) > notionImportProviderPageItems {
		return nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	return page.Results, nil
}

func newNotionProviderClient(token string) notionProviderClient {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv(notionAPIBaseURLEnvironment)), "/")
	if baseURL == "" {
		baseURL = "https://api.notion.com"
	}
	return notionProviderClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (r *durableJobRunner) notionProviderClient(connectionID, token string) notionProviderClient {
	client := notionSourceClient(r.app, connectionID, token)
	client.admission = r.notionProviderAdmission
	client.connectionID = connectionID
	return client
}

func (owner *notionProviderAdmissionOwner) acquire(
	ctx context.Context,
	connectionID string,
) (*notionProviderAdmissionLane, func(), error) {
	lane := owner.retainLane(connectionID)
	select {
	case lane.concurrent <- struct{}{}:
	case <-ctx.Done():
		owner.releaseReference(lane)
		return nil, nil, ctx.Err()
	}
	select {
	case lane.schedule <- struct{}{}:
	case <-ctx.Done():
		<-lane.concurrent
		owner.releaseReference(lane)
		return nil, nil, ctx.Err()
	}
	releaseSchedule := func() { <-lane.schedule }
	interval := time.Second / notionProviderRequestsPerSecond
	for {
		now := owner.clock.now()
		lane.mu.Lock()
		admitAt := now
		if lane.next.After(admitAt) {
			admitAt = lane.next
		}
		if lane.retryAfter.After(admitAt) {
			admitAt = lane.retryAfter
		}
		lane.mu.Unlock()

		if wait := admitAt.Sub(owner.clock.now()); wait > 0 {
			timer := owner.clock.newTimer(wait)
			select {
			case <-ctx.Done():
				timer.stop()
				releaseSchedule()
				<-lane.concurrent
				owner.releaseReference(lane)
				return nil, nil, ctx.Err()
			case <-timer.channelValue():
			}
			timer.stop()
		}

		lane.mu.Lock()
		if err := ctx.Err(); err != nil {
			lane.mu.Unlock()
			releaseSchedule()
			<-lane.concurrent
			owner.releaseReference(lane)
			return nil, nil, err
		}
		now = owner.clock.now()
		if lane.next.After(now) || lane.retryAfter.After(now) {
			lane.mu.Unlock()
			continue
		}
		lane.next = now.Add(interval)
		lane.mu.Unlock()
		releaseSchedule()
		return lane, func() {
			<-lane.concurrent
			owner.releaseReference(lane)
		}, nil
	}
}

func (owner *notionProviderAdmissionOwner) retainLane(connectionID string) *notionProviderAdmissionLane {
	now := owner.clock.now()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	for key, lane := range owner.lanes {
		lane.mu.Lock()
		expired := lane.references == 0 && !lane.next.After(now) && !lane.retryAfter.After(now) &&
			now.Sub(lane.lastUsed) >= notionProviderAdmissionRetention
		lane.mu.Unlock()
		if expired {
			delete(owner.lanes, key)
		}
	}
	lane := owner.lanes[connectionID]
	if lane == nil {
		lane = &notionProviderAdmissionLane{
			concurrent: make(chan struct{}, notionProviderMaximumConcurrentRequests),
			schedule:   make(chan struct{}, 1),
			lastUsed:   now,
		}
		owner.lanes[connectionID] = lane
	}
	lane.mu.Lock()
	lane.references++
	lane.lastUsed = now
	lane.mu.Unlock()
	return lane
}

func (owner *notionProviderAdmissionOwner) releaseReference(lane *notionProviderAdmissionLane) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	lane.mu.Lock()
	lane.references--
	lane.lastUsed = owner.clock.now()
	lane.mu.Unlock()
}

func (owner *notionProviderAdmissionOwner) noteRetryAfter(
	lane *notionProviderAdmissionLane,
	retryAt time.Time,
) {
	lane.mu.Lock()
	defer lane.mu.Unlock()
	lane.limitedAt = owner.clock.now()
	if !retryAt.After(lane.retryAfter) {
		return
	}
	lane.retryAfter = retryAt
	if retryAt.After(lane.next) {
		lane.next = retryAt
	}
}

func (client notionProviderClient) validate(ctx context.Context) error {
	_, err := client.connectionName(ctx)
	return err
}

// connectionName asks Notion who the token is (GET /v1/users/me) and gives
// what Notion calls the connection: the workspace it reaches, else the
// integration's own name, else "".
func (client notionProviderClient) connectionName(ctx context.Context) (string, error) {
	var me struct {
		Name string `json:"name"`
		Bot  struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if err := client.request(ctx, http.MethodGet, "/v1/users/me", nil, &me); err != nil {
		return "", err
	}
	name := strings.TrimSpace(me.Bot.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(me.Name)
	}
	if runes := []rune(name); len(runes) > 255 {
		name = string(runes[:255])
	}
	return name, nil
}

func (client notionProviderClient) page(ctx context.Context, id string) (notionProviderObject, error) {
	var page notionProviderObject
	err := client.request(ctx, http.MethodGet, "/v1/pages/"+url.PathEscape(id), nil, &page)
	return page, err
}

func (client notionProviderClient) block(ctx context.Context, id string) (notionProviderObject, error) {
	var block notionProviderObject
	err := client.request(ctx, http.MethodGet, "/v1/blocks/"+url.PathEscape(id), nil, &block)
	return block, err
}

func (client notionProviderClient) children(ctx context.Context, id string, cursor string) (notionProviderPage, error) {
	query := url.Values{"page_size": []string{fmt.Sprint(notionImportProviderPageItems)}}
	if cursor != "" {
		query.Set("start_cursor", cursor)
	}
	var page notionProviderPage
	err := client.request(ctx, http.MethodGet, "/v1/blocks/"+url.PathEscape(id)+"/children?"+query.Encode(), nil, &page)
	return page, err
}

func (client notionProviderClient) database(ctx context.Context, id string) (notionProviderObject, error) {
	var database notionProviderObject
	err := client.request(ctx, http.MethodGet, "/v1/databases/"+url.PathEscape(id), nil, &database)
	return database, err
}

func (client notionProviderClient) databaseRows(ctx context.Context, id string, cursor string) (notionProviderPage, error) {
	payload := map[string]any{"page_size": notionImportProviderPageItems}
	if cursor != "" {
		payload["start_cursor"] = cursor
	}
	var page notionProviderPage
	err := client.request(ctx, http.MethodPost, "/v1/databases/"+url.PathEscape(id)+"/query", payload, &page)
	return page, err
}

func (client notionProviderClient) request(ctx context.Context, method string, path string, payload any, target any) error {
	return client.requestVersion(ctx, "2022-06-28", method, path, payload, target)
}

// A failure that may pass is tried again after these waits, each try going
// through the connection's pace again. Notion asking to wait with a
// Retry-After goes back to the caller, which waits without holding a worker.
var notionProviderRetryWaits = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

func (client notionProviderClient) requestVersion(
	ctx context.Context, version, method, path string, payload any, target any,
) error {
	var encoded []byte
	if payload != nil {
		var err error
		if encoded, err = json.Marshal(payload); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		err := client.requestOnce(ctx, version, method, path, encoded, target)
		var limited *notionProviderRateLimitError
		if errors.As(err, &limited) && client.waitOutRateLimit {
			// The connection's pace holds the next try until then.
			reflect.ValueOf(target).Elem().SetZero()
			attempt--
			continue
		}
		if err == nil || attempt == len(notionProviderRetryWaits) || errors.As(err, &limited) || !notionProviderMayPass(err) {
			return err
		}
		timer := time.NewTimer(notionProviderRetryWaits[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		reflect.ValueOf(target).Elem().SetZero()
	}
}

func (client notionProviderClient) requestOnce(
	ctx context.Context, version, method, path string, encoded []byte, target any,
) error {
	var body io.Reader
	if encoded != nil {
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Notion-Version", version)
	if encoded != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	var admissionLane *notionProviderAdmissionLane
	var releaseAdmission func()
	if client.paced() {
		admissionLane, releaseAdmission, err = client.admission.acquire(ctx, client.connectionID)
		if err != nil {
			return err
		}
		defer releaseAdmission()
	}
	response, err := client.client.Do(request)
	if err != nil {
		return notionProviderTimedOut(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if response.StatusCode == http.StatusTooManyRequests && client.admission != nil && admissionLane != nil {
			if retryAt, ok := notionProviderRetryAt(client.admission.clock.now(), response.Header.Get("Retry-After")); ok {
				client.admission.noteRetryAfter(admissionLane, retryAt)
				return &notionProviderRateLimitError{retryAt: retryAt}
			}
		}
		return &notionProviderStatusError{status: response.StatusCode}
	}
	decoder := json.NewDecoder(composedReader(io.LimitReader(response.Body, 16<<20))) // stored composed (composed_text.go)
	if err := decoder.Decode(target); err != nil {
		return notionProviderTimedOut(ctx, err)
	}
	return ctx.Err()
}

func notionProviderRetryAt(now time.Time, raw string) (time.Time, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	maximumSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
	if err != nil || seconds < 0 || seconds > maximumSeconds {
		return time.Time{}, false
	}
	return now.Add(time.Duration(seconds) * time.Second), true
}

func (r *durableJobRunner) scheduleNotionProviderRateLimit(
	jobID, reference string,
	cause error,
) (bool, error) {
	// Notion asking to wait names when and costs the job nothing, however
	// often it asks; another failure that may pass is tried again a little
	// later, within the job's attempts.
	retryAt := r.clock.now().Add(notionProviderOutageWait)
	spent := 1
	var rateLimit *notionProviderRateLimitError
	if errors.As(cause, &rateLimit) {
		retryAt, spent = rateLimit.retryAt, 0
	} else if !notionProviderMayPass(cause) {
		return false, nil
	}
	handled := false
	err := r.app.RunInTransaction(func(txApp core.App) error {
		job, err := loadStoredJob(txApp, jobID, "")
		if err != nil || job.Kind != notionImportJobKind || job.Status != durableJobStatusQueued {
			return err
		}
		if spent > 0 && job.MaxAttempts > 0 && job.Attempt+1 >= job.MaxAttempts {
			return nil
		}
		handled = true
		now := r.clock.now().UnixMilli()
		notBefore := retryAt.UnixMilli()
		if notBefore < now {
			notBefore = now
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[error_code]] = {:errorCode}, [[diagnostic_reference]] = {:reference},
				[[attempt]] = [[attempt]] + {:spent}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[kind]] = {:kind} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "kind": notionImportJobKind,
			"errorCode": errorCodeNotionImportProviderFailed, "reference": reference,
			"spent": spent, "notBefore": notBefore, "updated": now,
		}).Execute()
		return err
	})
	return handled, err
}

// settleNotionImportTurn forgets the failures before a turn that went
// through: a job's attempts are for failures in a row, not for the whole of a
// run of hours.
func (r *durableJobRunner) settleNotionImportTurn(jobID string) error {
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[attempt]] = 0, [[error_code]] = '', [[diagnostic_reference]] = ''
		WHERE [[id]] = {:id} AND [[kind]] = {:kind} AND [[status]] = 'queued' AND ([[attempt]] > 0 OR [[error_code]] != '')
	`).Bind(dbx.Params{"id": jobID, "kind": notionImportJobKind}).Execute()
	return err
}

// notionObjectTitle is a page's title or a database's or data source's name
// as Notion keeps it: its spaces, tabs and line breaks where they are, its
// ends too (seen 2026-11-13 through Notion's API and app). A name of spaces
// alone leaves a database or data source unnamed, as Notion's does; a page's
// title stays as written.
func notionObjectTitle(object notionProviderObject) string {
	title := ""
	if len(object.Title) > 0 {
		title = notionProviderPlainTitle(object.Title)
	} else {
		for _, property := range object.Properties {
			parts, err := notionProviderRichTextParts(property.Title)
			if err == nil && len(parts) > 0 {
				title = notionProviderPlainTitle(parts)
				break
			}
		}
	}
	if object.Object != "page" && strings.TrimSpace(title) == "" {
		return ""
	}
	return title
}

// notionProviderPlainTitle is a title's whole text: a bold word or a mention
// in it makes it several parts.
func notionProviderPlainTitle(parts []notionProviderRichText) string {
	title := strings.Builder{}
	for _, part := range parts {
		title.WriteString(part.PlainText)
	}
	return title.String()
}

func notionProviderRichTextParts(raw json.RawMessage) ([]notionProviderRichText, error) {
	if len(raw) == 0 || raw[0] != '[' {
		return nil, nil
	}
	var parts []notionProviderRichText
	err := json.Unmarshal(raw, &parts)
	return parts, err
}

func notionParagraphText(object notionProviderObject) string {
	parts := make([]string, 0, len(object.Paragraph.RichText))
	for _, part := range object.Paragraph.RichText {
		parts = append(parts, part.PlainText)
	}
	return strings.Join(parts, "")
}
