package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The Slack channels an automation's "Slack 알림" step may post to, as
// Notion's step lists the connected Slack's channels to choose from. Here the
// server operator's Slack bot connections (LEAFEN_AUTOMATION_SLACK_CONNECTIONS)
// stand for Notion's per-workspace Slack connection: each lists the channels
// its bot can see, through Slack's conversations.list. With none, the list is
// empty and the step says Slack isn't connected on this server.

const (
	automationSlackChannelsPath     = "/workspaces/{workspaceId}/automation-slack-channels"
	automationSlackChannelPageLimit = 200
	// A page of Slack's channels is read up to this size: each channel comes
	// with its topic, purpose and more, from a few hundred bytes to a few kilobytes.
	automationSlackChannelPageBytes = 4 << 20
	// A bot's channels are read to Slack's last page, waiting as Slack's rate
	// limit asks; past this long in all the chooser keeps the channels read so
	// far and says more weren't read (incomplete).
	automationSlackChannelReadTime = 30 * time.Second
	automationSlackChannelTTL      = time.Minute
)

type automationSlackChannel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

type automationSlackConnection struct {
	ID       string                   `json:"id"`
	Channels []automationSlackChannel `json:"channels"`
	// Whether Slack stopped answering before its last page (its rate limit
	// past the read's time, or a page refused after the first): the channels
	// read so far stay to choose from, and the chooser says more weren't read.
	Incomplete bool `json:"incomplete,omitzero"`
	// Why the bot's channels couldn't be read ("slack_channels_unavailable"), its
	// channels then left empty; a channel saved before still reads as its id.
	Error string `json:"error,omitempty"`
}

type automationSlackChannelsResponse struct {
	Connections []automationSlackConnection `json:"connections"`
}

// A connection's channels read in the last minute, so that each step opened
// doesn't ask Slack again (its rate limit is a few calls a minute).
var automationSlackChannelCache = struct {
	sync.Mutex
	entries map[string]automationSlackChannelCacheEntry
}{entries: map[string]automationSlackChannelCacheEntry{}}

type automationSlackChannelCacheEntry struct {
	token    string
	at       time.Time
	channels []automationSlackChannel
}

var automationSlackChannelClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func readAutomationSlackChannels(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInteractiveAutomationInvalid, "Check the workspace.")
	}
	// Whoever may write an automation in the workspace may see where it can post.
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeInteractiveAutomationNotFound, "Workspace not found.")
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't read the workspace.", err)
	}
	connections, valid := databaseAutomationSlackConnections(os.Getenv(databaseAutomationSlackConnectionsEnvironment))
	response := automationSlackChannelsResponse{Connections: []automationSlackConnection{}}
	if valid {
		ids := make([]string, 0, len(connections))
		for id := range connections {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			channels, incomplete, err := automationSlackConnectionChannels(event.Request.Context(), id, connections[id])
			connection := automationSlackConnection{ID: id, Channels: channels, Incomplete: incomplete}
			if err != nil {
				connection.Channels, connection.Error = []automationSlackChannel{}, "slack_channels_unavailable"
			}
			response.Connections = append(response.Connections, connection)
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// automationSlackConnectionChannels is the channels a bot connection sees, by
// name, read to Slack's last page and again after a minute; incomplete when
// Slack stopped answering before it (those read stay, and aren't kept).
func automationSlackConnectionChannels(ctx context.Context, id, token string) ([]automationSlackChannel, bool, error) {
	automationSlackChannelCache.Lock()
	cached, found := automationSlackChannelCache.entries[id]
	automationSlackChannelCache.Unlock()
	if found && cached.token == token && time.Since(cached.at) < automationSlackChannelTTL {
		return cached.channels, false, nil
	}
	channels, cursor, incomplete := []automationSlackChannel{}, "", false
	deadline, seen := time.Now().Add(automationSlackChannelReadTime), map[string]bool{}
	for {
		page, wait, err := automationSlackChannelPage(ctx, token, cursor)
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// Slack's rate limit: read the same page again once Slack says, while the read has the time.
		if wait > 0 && time.Now().Add(wait).Before(deadline) {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if err != nil || wait > 0 {
			if cursor == "" {
				return nil, false, errAutomationSlackChannels
			}
			incomplete = true
			break
		}
		channels = append(channels, page.channels...)
		// Slack may give fewer channels than asked on a page with more after it.
		if cursor = page.next; cursor == "" {
			break
		}
		if seen[cursor] || !time.Now().Before(deadline) {
			incomplete = true
			break
		}
		seen[cursor] = true
	}
	sort.SliceStable(channels, func(left, right int) bool { return channels[left].Name < channels[right].Name })
	if !incomplete {
		automationSlackChannelCache.Lock()
		automationSlackChannelCache.entries[id] = automationSlackChannelCacheEntry{token: token, at: time.Now(), channels: channels}
		automationSlackChannelCache.Unlock()
	}
	return channels, incomplete, nil
}

type automationSlackChannelsPage struct {
	channels []automationSlackChannel
	next     string
}

// automationSlackChannelPage is one page of Slack's conversations.list from
// cursor, or how long Slack asks to wait before asking again (HTTP 429).
func automationSlackChannelPage(ctx context.Context, token, cursor string) (automationSlackChannelsPage, time.Duration, error) {
	query := url.Values{
		"types":            {"public_channel,private_channel"},
		"exclude_archived": {"true"},
		"limit":            {strconv.Itoa(automationSlackChannelPageLimit)},
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		slackAPIBaseURL+"/conversations.list?"+query.Encode(), nil)
	if err != nil {
		return automationSlackChannelsPage{}, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := automationSlackChannelClient.Do(request)
	if err != nil {
		return automationSlackChannelsPage{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After")))
		if err != nil || seconds < 1 {
			seconds = 1
		}
		return automationSlackChannelsPage{}, time.Duration(seconds) * time.Second, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, automationSlackChannelPageBytes+1))
	if err != nil || len(body) > automationSlackChannelPageBytes || response.StatusCode != http.StatusOK {
		return automationSlackChannelsPage{}, 0, errAutomationSlackChannels
	}
	var page struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Channels []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsPrivate bool   `json:"is_private"`
		} `json:"channels"`
		Metadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if json.Unmarshal(body, &page) != nil || !page.OK {
		return automationSlackChannelsPage{}, 0, errAutomationSlackChannels
	}
	read := automationSlackChannelsPage{channels: make([]automationSlackChannel, 0, len(page.Channels)), next: page.Metadata.NextCursor}
	for _, channel := range page.Channels {
		if channel.ID != "" {
			read.channels = append(read.channels, automationSlackChannel{ID: channel.ID, Name: channel.Name, Private: channel.IsPrivate})
		}
	}
	return read, 0, nil
}

type automationSlackChannelsError struct{}

func (automationSlackChannelsError) Error() string { return "slack channels unavailable" }

var errAutomationSlackChannels error = automationSlackChannelsError{}
