package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type preparedFormSubmission struct {
	Link             storedFormLink
	Source           formSource
	Authorization    pageAuthorization
	Values           map[string]graphRowValue
	ReciprocalRows   []*graphDatabaseRow
	SemanticHash     string
	RuntimePrincipal string
	// Member is a member of the workspace answering with the form's owner's access.
	Member bool
}

func normalizeFormSubmitRequest(input formSubmitRequest) (formSubmitRequest, error) {
	input.Token = strings.TrimSpace(input.Token)
	input.Revision = strings.TrimSpace(input.Revision)
	input.RequestID = strings.TrimSpace(input.RequestID)
	switch {
	case input.Action != formActionSubmit:
		return formSubmitRequest{}, formInvalid("action is %s", formActionSubmit)
	case !validFormToken(input.Token):
		return formSubmitRequest{}, formInvalid("token is the form's own, as its link or definition gives it")
	case !sha256Pattern.MatchString(input.Revision):
		return formSubmitRequest{}, formInvalid("revision is the one the form's definition gives")
	case input.RequestID == "" || !utf8.ValidString(input.RequestID) || len([]byte(input.RequestID)) > maxFormRequestIDBytes:
		return formSubmitRequest{}, formInvalid("requestId is required, of up to %d bytes", maxFormRequestIDBytes)
	case input.Answers == nil:
		return formSubmitRequest{}, formInvalid("answers is an object of each question's propertyId to its answer")
	}
	return input, nil
}

func submitFormResponse(
	persistence *pocketBaseYgoPersistence,
	app core.App,
	principalID string,
	input formSubmitRequest,
) (formSubmitResponse, error) {
	input, err := normalizeFormSubmitRequest(input)
	if err != nil {
		return formSubmitResponse{}, err
	}
	link, err := loadStoredFormLinkByToken(app, input.Token)
	if err != nil {
		return formSubmitResponse{}, err
	}
	if link.Audience == formAudienceWorkspace && principalID == "" {
		return formSubmitResponse{}, errFormAuthenticationRequired
	}
	// A member's answer is written with the form's owner's access, as a web
	// one's, and reads as the respondent's: its page's author and history,
	// the database's edit and its notices. An anonymous one reads as no one's,
	// "익명" as Notion's (seen 2026-11-05), nor do its files name them.
	// Anyone else, a guest, writes it with their own page access.
	writer, respondent := principalID, principalID
	anonymous := link.Audience == formAudienceWorkspace && formLinkAnonymous(app, link)
	member := false
	if link.Audience == formAudienceWorkspace && principalID != "" {
		if member, err = formWorkspaceMember(app, principalID, link); err != nil {
			return formSubmitResponse{}, err
		}
	}
	if anonymous || member {
		writer = ""
	}
	if anonymous {
		respondent = ""
	}
	digest := sha256.Sum256([]byte(link.ID + "\x00" + input.RequestID))
	digestText := hex.EncodeToString(digest[:])
	rowID, err := formSubmissionRowID(app, link.ID, input.RequestID, digestText)
	if err != nil {
		return formSubmitResponse{}, err
	}
	mutationID := "form:" + link.ID + ":" + digestText

	// The row gets its ID numbers from the database's unique ID allocator,
	// as every added row does.
	prepared := preparedFormSubmission{}
	receipt, err := persistence.applyContentGraphMutation(
		writer,
		link.PageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, prepareErr := prepareCurrentFormSubmission(
					txApp, principalID, input, rowID,
				)
				if prepareErr != nil {
					return pageAuthorization{}, prepareErr
				}
				if current.Link.ID != link.ID || current.Link.PageID != link.PageID ||
					link.Audience == formAudienceWorkspace && (formSourceAnonymous(current.Source) != anonymous ||
						current.Member != member) {
					return pageAuthorization{}, errFormConflict
				}
				prepared = current
				return current.Authorization, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				if prepared.SemanticHash == "" {
					return "", errFormConflict
				}
				return prepared.SemanticHash, nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				// An answer comes in above the rows there are, the newest first,
				// as Notion's (seen 2026-11-05).
				position, positionErr := databaseRowPositionAtStart(
					txApp, authorization.Workspace.OwnerID, prepared.Link.PageID, prepared.Link.DataSourceID,
				)
				if positionErr != nil {
					return contentGraphMutationPreparation{}, positionErr
				}
				if markErr := markFormFilesUsed(txApp, prepared.Link, prepared.Source, rowID, prepared.Values); markErr != nil {
					return contentGraphMutationPreparation{}, markErr
				}
				operations := []graphOperation{{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: rowID, DataSourceID: prepared.Link.DataSourceID,
						Position: position, Values: prepared.Values,
					},
				}}
				for _, row := range prepared.ReciprocalRows {
					operations = append(operations, graphOperation{
						Kind: graphOperationKindUpsertRow, Row: row,
					})
				}
				// Written with another's access, it still keeps to what the
				// respondent may change.
				if writer == "" && principalID != "" {
					if err := refusePropertyAccessChanges(txApp, principalID, operations); err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if err := refuseUnlinkedRowChanges(txApp, principalID, operations); err != nil {
						return contentGraphMutationPreparation{}, err
					}
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			// Its steps name the answer's page's author and share that page
			// with its respondent, a read of the row taking both in: its
			// change says the rows it wrote (StepsKeepGraph).
			StepsKeepGraph: true,
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				// An anonymous response keeps its respondent out of the page's
				// author and history; a web form's has none to keep.
				if writer == "" {
					author := respondent
					if author == "" {
						author = leafenAnonymousAuthorID
					}
					if err := markPagesAuthoredBy(txApp, author, rowID); err != nil {
						return err
					}
				}
				// An anonymous answer shares its page with no one, as Notion holds
				// "제출물 사용 권한" at "사용 권한 없음" while it is on (seen 2026-11-05).
				return grantFormSubmitterAccess(txApp, prepared.Link, respondent, rowID)
			},
			FormResponse: true, FormRespondent: respondent,
		},
	)
	if err != nil {
		return formSubmitResponse{}, err
	}
	confirmation := prepared.Source.Form.Submit
	return formSubmitResponse{
		RowID: rowID, Replayed: receipt.Duplicate, Confirmation: &confirmation,
	}, nil
}

func prepareCurrentFormSubmission(
	app core.App,
	principalID string,
	input formSubmitRequest,
	rowID string,
) (preparedFormSubmission, error) {
	prepared := preparedFormSubmission{}
	link, err := loadStoredFormLinkByToken(app, input.Token)
	if err != nil {
		return prepared, err
	}
	runtimePrincipal := principalID
	if link.Audience == formAudienceWorkspace {
		if principalID == "" {
			return prepared, errFormAuthenticationRequired
		}
		// A member's answer is written with the form's owner's access, as a web
		// one's, whether or not the member can open the database.
		member, memberErr := formWorkspaceMember(app, principalID, link)
		if memberErr != nil {
			return prepared, memberErr
		}
		if member {
			if err = formPageOpen(app, link); err == nil {
				runtimePrincipal, prepared.Member = link.Owner, true
			}
		}
		if err == nil {
			prepared.Authorization, err = authorizePageBody(
				app, runtimePrincipal, link.PageID, permissionWorkspaceWrite,
			)
		}
	} else if link.Audience == formAudienceWeb {
		if err = authorizeFormDefinition(app, principalID, link); err == nil {
			runtimePrincipal = link.Owner
			prepared.Authorization, err = authorizePageBody(
				app, runtimePrincipal, link.PageID, permissionWorkspaceWrite,
			)
		}
	} else {
		return prepared, errFormNotFound
	}
	if isPermissionDenied(err) {
		return prepared, errFormNotFound
	}
	if err != nil {
		return prepared, err
	}
	if prepared.Authorization.Workspace.OwnerID != link.Owner ||
		prepared.Authorization.Workspace.WorkspaceID != link.WorkspaceID {
		return prepared, errFormNotFound
	}
	source, err := loadFormSource(
		app, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil,
	)
	if err != nil {
		if errors.Is(err, errFormInvalid) {
			return prepared, errFormConflict
		}
		return prepared, err
	}
	if source.ViewConfig["leafenFormAudience"] != link.Audience ||
		source.ViewConfig["type"] != graphViewTypeForm {
		return prepared, errFormNotFound
	}
	revision, err := formDefinitionRevision(link, source)
	if err != nil {
		return prepared, err
	}
	if revision != input.Revision {
		return prepared, errFormConflict
	}
	values, err := normalizeFormSubmissionAnswers(source, input.Answers)
	if err != nil {
		return prepared, err
	}
	if err := resolveFormFileAnswers(app, link, source, rowID, values); err != nil {
		return prepared, err
	}
	reciprocalRows, err := prepareFormSubmissionReferences(
		app, runtimePrincipal, link, source, rowID, values,
	)
	if err != nil {
		return prepared, err
	}
	semanticHash, err := formSubmissionSemanticHash(
		link, revision, principalID, values,
	)
	if err != nil {
		return prepared, err
	}
	prepared.Link = link
	prepared.Source = source
	prepared.Values = values
	prepared.ReciprocalRows = reciprocalRows
	prepared.SemanticHash = semanticHash
	prepared.RuntimePrincipal = runtimePrincipal
	return prepared, nil
}

func formSubmissionSemanticHash(
	link storedFormLink,
	revision, principalID string,
	values map[string]graphRowValue,
) (string, error) {
	payload := struct {
		LinkID      string                   `json:"linkId"`
		Audience    string                   `json:"audience"`
		Revision    string                   `json:"revision"`
		PrincipalID string                   `json:"principalId"`
		Values      map[string]graphRowValue `json:"values"`
	}{
		LinkID: link.ID, Audience: link.Audience, Revision: revision,
		PrincipalID: principalID, Values: values,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// formSourceAnonymous says a form takes its answers anonymously.
func formSourceAnonymous(source formSource) bool {
	return source.Form.Anonymous != nil && *source.Form.Anonymous
}

// formLinkAnonymous reads whether a form takes anonymous answers before its
// answer is written; the write checks it again against what it then reads.
func formLinkAnonymous(app core.App, link storedFormLink) bool {
	source, err := loadFormSource(
		app, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil,
	)
	return err == nil && formSourceAnonymous(source)
}

// formSubmissionRowID is the row an answer makes, the same for the same
// answer sent again: a row is a page, so its ID is a page's (derivedPageID).
// An answer whose row went in before rows took page IDs is the row named
// "form-" and its digest, which an answer sent again finds as it did.
func formSubmissionRowID(app core.App, linkID, requestID, digest string) (string, error) {
	older := "form-" + digest
	var found int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": older}).Row(&found); err != nil {
		return "", err
	}
	if found > 0 {
		return older, nil
	}
	return derivedPageID("form", linkID, requestID), nil
}
