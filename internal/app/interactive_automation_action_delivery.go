package app

import (
	"net/mail"
	"regexp"
	"strings"
)

const (
	// People picked by name, as Notion's "up to 20 people"; a People
	// property, formula or the clicking person reaches everyone it names.
	databaseAutomationMaxNotificationRecipients  = 20
	databaseAutomationMaxEmailRunes              = 320
	databaseAutomationMaxSubjectRunes            = 200
	databaseAutomationMaxWebhookHeaderNameRunes  = 128
	databaseAutomationMaxWebhookHeaderValueRunes = 2048
)

var databaseAutomationWebhookHeaderName = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

func normalizeInteractiveAutomationDeliveryAction(action *interactiveAutomationAction) error {
	switch action.Type {
	case interactiveAutomationActionSendNotification:
		if len(action.RecipientIDs)+len(action.RecipientSources) < 1 {
			return automationRefused("recipientIds and recipientSources are empty: name whom to notify")
		}
		if len(action.RecipientIDs) > databaseAutomationMaxNotificationRecipients {
			return automationRefused("recipientIds names %d people, over %d", len(action.RecipientIDs), databaseAutomationMaxNotificationRecipients)
		}
		seen := map[string]struct{}{}
		for index := range action.RecipientIDs {
			action.RecipientIDs[index] = strings.TrimSpace(action.RecipientIDs[index])
			if !boundedAutomationString(action.RecipientIDs[index], interactiveAutomationMaxIdentifierRunes) {
				return automationRefused("recipientIds[%d] is 1–%d characters", index, interactiveAutomationMaxIdentifierRunes)
			}
			if _, duplicate := seen[action.RecipientIDs[index]]; duplicate {
				return automationRefused("recipientIds[%d] names someone a second time", index)
			}
			seen[action.RecipientIDs[index]] = struct{}{}
		}
		if err := normalizeInteractiveAutomationRecipientSources(action.RecipientSources); err != nil {
			return refusalAt(err, "recipientSources")
		}
		// Whom its editor's chip names, kept while nobody is picked yet: people
		// picked ("특정 사용자") or a person property ("사용자 속성").
		switch action.RecipientKind {
		case "", "people", "property":
		default:
			return automationRefused("recipientKind is people or property, whom the notification's editor names")
		}
		if !normalizeAutomationMessage(action) {
			return errAutomationMessage
		}
	case interactiveAutomationActionSendEmail:
		action.RecipientEmail = strings.ToLower(strings.TrimSpace(action.RecipientEmail))
		action.Subject = strings.TrimSpace(action.Subject)
		address, err := mail.ParseAddress(action.RecipientEmail)
		switch {
		case err != nil || address.Address != action.RecipientEmail || !boundedAutomationString(action.RecipientEmail, databaseAutomationMaxEmailRunes):
			return automationRefused("recipientEmail is one email address, at most %d characters", databaseAutomationMaxEmailRunes)
		case !boundedAutomationString(action.Subject, databaseAutomationMaxSubjectRunes):
			return automationRefused("subject is 1–%d characters", databaseAutomationMaxSubjectRunes)
		case !normalizeAutomationMessage(action):
			return errAutomationMessage
		}
	case interactiveAutomationActionSendWebhook:
		normalized, err := normalizeDatabaseAutomationPublicURL(action.URL)
		if err != nil {
			return automationRefused("url is a public https address the webhook is sent to, not a private or local one")
		}
		action.URL = normalized
		seenProperties := map[string]struct{}{}
		for index := range action.PropertyIDs {
			action.PropertyIDs[index] = strings.TrimSpace(action.PropertyIDs[index])
			if !boundedAutomationString(action.PropertyIDs[index], interactiveAutomationMaxIdentifierRunes) {
				return automationRefused("propertyIds[%d] is 1–%d characters", index, interactiveAutomationMaxIdentifierRunes)
			}
			if _, duplicate := seenProperties[action.PropertyIDs[index]]; duplicate {
				return automationRefused("propertyIds[%d] names a property a second time", index)
			}
			seenProperties[action.PropertyIDs[index]] = struct{}{}
		}
		seenHeaders := map[string]struct{}{}
		for index := range action.Headers {
			header := &action.Headers[index]
			header.Name = strings.TrimSpace(header.Name)
			header.Value = strings.TrimSpace(header.Value)
			name := strings.ToLower(header.Name)
			switch {
			case !boundedAutomationString(header.Name, databaseAutomationMaxWebhookHeaderNameRunes) || !databaseAutomationWebhookHeaderName.MatchString(header.Name):
				return automationRefused("headers[%d].name is a header name of up to %d letters, digits and !#$%%&'*+-.^_`|~", index, databaseAutomationMaxWebhookHeaderNameRunes)
			case !boundedAutomationString(header.Value, databaseAutomationMaxWebhookHeaderValueRunes) || strings.ContainsAny(header.Value, "\r\n"):
				return automationRefused("headers[%d].value is one line of 1–%d characters", index, databaseAutomationMaxWebhookHeaderValueRunes)
			case databaseAutomationWebhookHeaderReserved(name):
				return automationRefused("headers[%d].name %s is one the webhook sets itself", index, header.Name)
			}
			if _, duplicate := seenHeaders[name]; duplicate {
				return automationRefused("headers[%d].name %s is named a second time", index, header.Name)
			}
			seenHeaders[name] = struct{}{}
		}
	case interactiveAutomationActionSendSlack:
		action.ConnectionID = strings.TrimSpace(action.ConnectionID)
		action.ChannelID = strings.TrimSpace(action.ChannelID)
		switch {
		case !boundedAutomationString(action.ConnectionID, interactiveAutomationMaxIdentifierRunes):
			return automationRefused("connectionId names the workspace's Slack connection, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
		case !boundedAutomationString(action.ChannelID, interactiveAutomationMaxIdentifierRunes):
			return automationRefused("channelId names the Slack channel, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
		case !normalizeAutomationMessage(action):
			return errAutomationMessage
		}
	default:
		return automationRefused("type %q isn't a message an automation sends", action.Type)
	}
	return nil
}

func normalizeDatabaseAutomationPublicURL(raw string) (string, error) {
	normalized, err := normalizeOutboundPublicURL(raw, 0, false)
	if err != nil {
		return "", errInteractiveAutomationInvalid
	}
	return normalized, nil
}

func databaseAutomationWebhookHeaderReserved(name string) bool {
	switch name {
	case "connection", "content-length", "content-type", "host", "idempotency-key",
		"transfer-encoding", "x-leafen-automation-delivery":
		return true
	default:
		return false
	}
}
