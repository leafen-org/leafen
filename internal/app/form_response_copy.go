package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// "이메일로 사본 받기" after a form is sent, as Notion's submit screen offers
// (seen 2026-10-06 and 2026-10-08): a signed-in respondent gets their answers
// at their account's address, when the server can send email. The response
// is the one their submit request made — its request id names it, as the
// submit did — so no one is mailed another's answers; each response is
// mailed once, and a respondent's copies share the form submit window.

// formResponseCopyAvailable says whether a form's submit screen offers a copy.
func formResponseCopyAvailable(app core.App, principalID string) bool {
	if principalID == "" {
		return false
	}
	settings, err := loadAdministratorSettings(app)
	return err == nil && notificationEmailConfigured(settings)
}

func sendFormResponseCopy(ctx context.Context, app core.App, principalID string, input formResponseCopyRequest) (formResponseCopyResponse, error) {
	token, err := normalizeFormIdentifier(input.Token)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" || len(requestID) > maxFormRequestIDBytes {
		return formResponseCopyResponse{}, errFormInvalid
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	if !notificationEmailConfigured(settings) {
		return formResponseCopyResponse{}, errFormForbidden
	}
	link, err := loadStoredFormLinkByToken(app, token)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	digest := sha256.Sum256([]byte(link.ID + "\x00" + requestID))
	rowID := "form-" + hex.EncodeToString(digest[:])
	var held int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:row} AND [[data_source_id]] = {:source}
	`).Bind(dbx.Params{"row": rowID, "source": link.DataSourceID}).Row(&held); err != nil {
		return formResponseCopyResponse{}, err
	}
	if held == 0 {
		return formResponseCopyResponse{}, errFormNotFound
	}
	user, err := app.FindRecordById("users", principalID)
	if err != nil {
		return formResponseCopyResponse{}, refusedUnlessStorage(errFormForbidden, err)
	}
	recipient := strings.TrimSpace(user.GetString("email"))
	if recipient == "" {
		return formResponseCopyResponse{}, errFormForbidden
	}
	admitted, again, err := admitFormResponseCopy(app, principalID, rowID)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	if again {
		return formResponseCopyResponse{Sent: true}, nil
	}
	if !admitted {
		return formResponseCopyResponse{}, errFormRateLimit
	}
	source, err := loadFormSource(app, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	language, err := serverTextLanguageIn(ctx, app, principalID)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	if strings.TrimSpace(source.Form.Title) == "" {
		// A form without its own title goes by its database's name.
		_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:source}`).
			Bind(dbx.Params{"source": link.DataSourceID}).Row(&source.Form.Title)
	}
	subject, body := formResponseCopyContent(app, source, rowID, language)
	password, err := decryptAdministratorSetting(app, settings.SMTPPasswordCipher)
	if err != nil {
		return formResponseCopyResponse{}, err
	}
	if outcome, err := sendConfiguredSMTP(settings, password, recipient, buildAuthEmail(settings, recipient, subject, body)); err != nil {
		logNotificationEmailFailure(app, "form.response_copy", err)
		// A copy the server surely did not hand over may be asked for again.
		if outcome == smtpDeliverySettledFailure {
			_, _ = app.DB().NewQuery(`
				DELETE FROM {{leafen_form_submission_admission_logical_ids}}
				WHERE [[key_digest]] = {:keyDigest} AND [[logical_digest]] = {:logicalDigest}
			`).Bind(dbx.Params{
				"keyDigest":     formSubmissionAdmissionDigest(formActionResponseCopy, principalID),
				"logicalDigest": formSubmissionAdmissionDigest(rowID),
			}).Execute()
		}
		return formResponseCopyResponse{}, errorCode(errorCodeEmailDeliveryFailed)
	}
	return formResponseCopyResponse{Sent: true}, nil
}

// formResponseCopyContent is the copy's subject and its lines: the form's
// title, then each shown question and the answer given, as it reads.
func formResponseCopyContent(app core.App, source formSource, rowID, language string) (string, string) {
	title := strings.TrimSpace(source.Form.Title)
	korean := language == accountLanguageKo
	if title == "" {
		title = map[bool]string{true: "양식", false: "Form"}[korean]
	}
	subject := map[bool]string{true: title + " 응답 사본", false: "Your response to " + title}[korean]
	properties := make(map[string]storedFormProperty, len(source.Properties))
	for _, property := range source.Properties {
		properties[property.ID] = property
	}
	lines := []string{title, ""}
	for _, question := range source.Form.Questions {
		property, known := properties[question.PropertyID]
		if question.Hidden || !known {
			continue
		}
		label := strings.TrimSpace(question.Label)
		if label == "" {
			label = property.Name
		}
		answer := formResponseCopyAnswer(app, rowID, property)
		// A question its condition left out was not asked.
		if answer == "" && question.ConditionGroupID != "" {
			continue
		}
		if answer == "" {
			answer = "—"
		}
		lines = append(lines, label, answer, "")
	}
	return subject, strings.Join(lines, "\r\n")
}

// formResponseCopyAnswer reads an answer as its person or page names, or as
// the row's value reads.
func formResponseCopyAnswer(app core.App, rowID string, property storedFormProperty) string {
	if property.Type != graphValueTypePerson && property.Type != graphValueTypeRelation {
		return appEventRowValueText(app, rowID, property.ID, property.Type, property.Config)
	}
	var raw string
	if err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}`).
		Bind(dbx.Params{"row": rowID, "property": property.ID}).Row(&raw); err != nil {
		return ""
	}
	ids := []string{}
	if json.Unmarshal([]byte(raw), &ids) != nil {
		var single string
		if json.Unmarshal([]byte(raw), &single) != nil || single == "" {
			return ""
		}
		ids = []string{single}
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := ""
		if property.Type == graphValueTypePerson {
			_ = app.DB().NewQuery(`SELECT COALESCE(NULLIF(trim([[name]]), ''), [[email]]) FROM {{users}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": id}).Row(&name)
		} else {
			_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).Row(&name)
		}
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

// admitFormResponseCopy keeps a respondent's copies within the form submit
// window's limit and says when this response was already mailed.
func admitFormResponseCopy(app core.App, principalID, rowID string) (admitted, again bool, err error) {
	keyDigest := formSubmissionAdmissionDigest(formActionResponseCopy, principalID)
	logicalDigest := formSubmissionAdmissionDigest(rowID)
	now := time.Now().UTC().UnixMilli()
	resetAt := now + int64(formSubmissionAdmissionWindowSeconds)*1000
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := cleanupExpiredFormSubmissionAdmission(txApp, now); err != nil {
			return err
		}
		window := storedFormSubmissionAdmissionWindow{}
		err := txApp.DB().NewQuery(`
			SELECT [[key_digest]], [[reset_at]] FROM {{leafen_form_submission_admission_windows}}
			WHERE [[key_digest]] = {:keyDigest}
		`).Bind(dbx.Params{"keyDigest": keyDigest}).One(&window)
		if errors.Is(err, sql.ErrNoRows) {
			return createFormSubmissionAdmissionWindow(txApp, keyDigest, logicalDigest, resetAt, now, &admitted)
		}
		if err != nil {
			return err
		}
		summary := storedFormSubmissionAdmissionSummary{}
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) AS [[logical_count]], COALESCE(MAX([[logical_digest]] = {:logicalDigest}), 0) AS [[duplicate]]
			FROM {{leafen_form_submission_admission_logical_ids}} WHERE [[key_digest]] = {:keyDigest}
		`).Bind(dbx.Params{"keyDigest": keyDigest, "logicalDigest": logicalDigest}).One(&summary); err != nil {
			return err
		}
		if summary.Duplicate == 1 {
			again = true
			return nil
		}
		if summary.Count >= maxFormSubmissionAdmission {
			return nil
		}
		if err := insertFormSubmissionAdmissionLogicalID(txApp, keyDigest, logicalDigest, now); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	return admitted, again, err
}
