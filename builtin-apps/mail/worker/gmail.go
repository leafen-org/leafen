package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// gmailBox is the person's Gmail through the Gmail API, with the Google
// sign-in Leafen keeps and fills ({{oauth:google}}). Each listed message is
// a call of its own, so Gmail lists fewer.

const (
	gmailBase   = "https://gmail.googleapis.com/gmail/v1/users/me"
	gmailListed = 20
)

// relay reaches an approved address through Leafen (a test answers it).
var relay = leafen.Relay

type gmailBox struct{}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailMessage struct {
	ID           string   `json:"id"`
	LabelIDs     []string `json:"labelIds"`
	InternalDate string   `json:"internalDate"`
	SizeEstimate int      `json:"sizeEstimate"`
	Raw          string   `json:"raw"`
	Payload      struct {
		MimeType string        `json:"mimeType"`
		Headers  []gmailHeader `json:"headers"`
	} `json:"payload"`
}

func (gmailBox) close() {}

func gmailCall(method, path string, body any, target any) error {
	request := leafen.RelayRequest{Method: method, URL: gmailBase + path,
		Headers: map[string]string{"Authorization": "Bearer {{oauth:google}}", "Accept": "application/json"}}
	if body != nil {
		encoded, _ := json.Marshal(body)
		request.Body, request.Headers["Content-Type"] = string(encoded), "application/json"
	}
	answer, err := relay(request)
	switch {
	case err != nil && strings.Contains(err.Error(), "log in to"):
		return errCredentials
	case err != nil && strings.Contains(err.Error(), "OAUTH"):
		return errLogin
	case err != nil:
		return fmt.Errorf("%w: %v", errUnreachable, err)
	case answer.Status == 401 || answer.Status == 403:
		return errLogin
	case answer.Status == 404 || answer.Status == 400:
		return errInput
	case answer.Status >= 300:
		return fmt.Errorf("gmail answered %d", answer.Status)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal([]byte(answer.Body), target)
}

func hasLabel(labels []string, wanted string) bool {
	for _, label := range labels {
		if label == wanted {
			return true
		}
	}
	return false
}

// summary of a message Gmail gave with format=metadata.
func (message gmailMessage) summary() summary {
	header := mail.Header{}
	for _, field := range message.Payload.Headers {
		header[field.Name] = append(header[field.Name], field.Value)
	}
	arrived, _ := strconv.ParseInt(message.InternalDate, 10, 64)
	return summary{
		ID: message.ID, order: arrived, From: first(people(header, "From")), Subject: headerText(header.Get("Subject")),
		Date: dateOf(header, time.UnixMilli(arrived)), Seen: !hasLabel(message.LabelIDs, "UNREAD"),
		Attachment: message.Payload.MimeType == "multipart/mixed", Size: message.SizeEstimate,
	}
}

func gmailMetadata(id string) (gmailMessage, error) {
	found := gmailMessage{}
	query := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "Subject", "Date"}}
	err := gmailCall("GET", "/messages/"+url.PathEscape(id)+"?"+query.Encode(), nil, &found)
	return found, err
}

// newest lists the inbox's newest messages, one call for the list and one
// for each message.
func gmailNewest(count int, query string) ([]summary, int, error) {
	listing := struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		ResultSizeEstimate int `json:"resultSizeEstimate"`
	}{}
	values := url.Values{"labelIds": {"INBOX"}, "maxResults": {strconv.Itoa(count)}}
	if query != "" {
		values.Set("q", query)
	}
	if err := gmailCall("GET", "/messages?"+values.Encode(), nil, &listing); err != nil {
		return nil, 0, err
	}
	out := []summary{}
	for _, listed := range listing.Messages {
		message, err := gmailMetadata(listed.ID)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, message.summary())
	}
	return out, max(listing.ResultSizeEstimate, len(out)), nil
}

func (gmailBox) list(count int) ([]summary, int, seen, error) {
	messages, total, err := gmailNewest(min(count, gmailListed), "")
	mark := seen{Validity: -1}
	for _, message := range messages {
		mark.Next = max(mark.Next, message.order)
	}
	return messages, total, mark, err
}

func (gmailBox) read(id string) (opened, bool, error) {
	message := gmailMessage{}
	if err := gmailCall("GET", "/messages/"+url.PathEscape(id)+"?format=raw", nil, &message); err != nil {
		return opened{}, false, err
	}
	raw, err := base64.URLEncoding.DecodeString(message.Raw)
	if err != nil {
		if raw, err = base64.RawURLEncoding.DecodeString(message.Raw); err != nil {
			return opened{}, false, errServer
		}
	}
	if hasLabel(message.LabelIDs, "UNREAD") {
		if err := gmailCall("POST", "/messages/"+url.PathEscape(id)+"/modify", map[string]any{"removeLabelIds": []string{"UNREAD"}}, nil); err != nil {
			leafen.Log("mail: not marked read: " + err.Error())
		}
	}
	return openMessage(raw), false, nil
}

func (gmailBox) send(message outgoing, now time.Time) error {
	to, cc, err := message.check()
	if err != nil {
		return err
	}
	// Gmail sends from the signed-in account.
	headers, body := message.compose("", to, cc, now)
	return gmailCall("POST", "/messages/send", map[string]string{"raw": base64.URLEncoding.EncodeToString([]byte(headers + body))}, nil)
}

func (gmailBox) since(mark seen) ([]summary, seen, error) {
	unread, _, err := gmailNewest(10, "is:unread")
	if err != nil {
		return nil, mark, err
	}
	current := seen{Validity: -1, Next: mark.Next}
	fresh := []summary{}
	for _, message := range unread {
		current.Next = max(current.Next, message.order)
		if mark.Validity == -1 && message.order > mark.Next {
			fresh = append(fresh, message)
		}
	}
	return fresh, current, nil
}
