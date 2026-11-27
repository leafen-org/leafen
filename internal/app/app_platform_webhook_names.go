package app

import "fmt"

// An app's incoming webhooks are named by keys its code uses ("payment");
// people read them by the names the manifest's webhook name gives in each
// language ("결제 알림", "Payment notices"): the review, the consent window
// and the app's settings window name a webhook so in the reader's language,
// and one left unnamed by its key (2026-11-16: the permission line read
// "외부 서비스가 보내는 호출 받기(payment)").

// appWebhookProblems checks a webhook's way of checking calls and its
// names: language tags, each name 1 to appNameMaxRunes characters.
func appWebhookProblems(webhook appManifestWebhook, add func(string, ...any)) {
	if webhook.Verify != "token" && webhook.Verify != "hmac-sha256" {
		add("webhook %q verify is token or hmac-sha256", webhook.Key)
	}
	appTextProblems(webhook.Name, fmt.Sprintf("webhook %q name", webhook.Key), appNameMaxRunes, false, add)
}

// appWebhooksApproved is what an approval holds of an app's webhooks: each
// one's key and what runs it, not what it is called, so naming a webhook
// keeps an app's approval. Webhooks with no names are held as they are.
func appWebhooksApproved(webhooks []appManifestWebhook) []appManifestWebhook {
	named := false
	for _, webhook := range webhooks {
		named = named || len(webhook.Name) > 0
	}
	if !named {
		return webhooks
	}
	approved := make([]appManifestWebhook, len(webhooks))
	for index, webhook := range webhooks {
		webhook.Name = nil
		approved[index] = webhook
	}
	return approved
}
