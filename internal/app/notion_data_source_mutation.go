package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type notionDataSourceUpdateRequest struct {
	Title      json.RawMessage                `json:"title,omitempty"`
	Icon       json.RawMessage                `json:"icon,omitempty"`
	Properties map[string]json.RawMessage     `json:"properties,omitempty"`
	InTrash    *bool                          `json:"in_trash,omitempty"`
	Parent     *notionDataSourceParentRequest `json:"parent,omitempty"`
	// description is a database update's, which the source keeps, with its formatting.
	description      *string
	descriptionSpans []graphTextSpan
	// titleSpans is the requested title's formatting, none when it is plain.
	titleSpans []graphTextSpan
}

const (
	maxNotionDataSourceSchemaProperties = 500
	maxNotionDataSourceSchemaBytes      = 50 << 10
)

func serveNotionDataSourceUpdate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicID string,
) error {
	if ygo == nil {
		return notionRequestNotCompleted(event, nil)
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	if refused, err := refuseNotionDatabaseOnlyDataSourceKeys(event); refused || err != nil {
		return err
	}
	input := notionDataSourceUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	// The trash, and a body that changes nothing, take their turn first
	// (notion_data_source_trash_turn.go).
	if input.InTrash != nil || !input.changes() {
		return serveNotionDataSourceTrashTurn(event, ygo, workspace.Identity, identity.InternalID, publicID, input)
	}
	return serveNotionDataSourceChange(event, ygo, workspace.Identity, identity.InternalID, publicID, input)
}

// serveNotionDataSourceChange applies an update's title, icon, properties
// and parent to a data source out of the trash.
func serveNotionDataSourceChange(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, bearer notionOAuthBearerIdentity,
	dataSourceID, publicID string, input notionDataSourceUpdateRequest,
) error {
	authorization, err := authorizeNotionDataSourceParentCurrent(
		event.App, bearer, dataSourceID, permissionWorkspaceWrite,
	)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		if state, stateErr := loadNotionDataSourceTrashState(
			event.App, bearer.WorkspaceID, dataSourceID,
		); stateErr == nil && state.trashed() {
			// A data source in the trash isn't found for a change, as
			// Notion's (seen 2026-11-11); one taken out of it beside the
			// change comes out first (notion_data_source_trash_turn.go).
			return notionNamedObjectNotFound(event, "data_source", notionDashedUUID(strings.TrimSpace(publicID)))
		}
		if moving, moveErr := dataSourceMoveActive(event.App, dataSourceID); moveErr == nil && moving {
			return notionCompatibilityError(event, http.StatusConflict, "conflict_error",
				"The data source is still moving. Retry the request to see the result.")
		}
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	// Nor is one under a page in the trash (notion_trash_refusal.go).
	if _, trashed, err := loadPageTrashEffect(event.App, authorization.Container.PageID); err != nil {
		return notionDataSourceMutationError(event, err)
	} else if trashed {
		return notionNamedObjectNotFound(event, "data_source", notionDashedUUID(strings.TrimSpace(publicID)))
	}
	if input.Icon != nil {
		if _, err := notionPagePresentation(input.Icon, nil); err != nil {
			return notionPageValidationError(event, "The data source icon is invalid.")
		}
	}
	if input.Properties != nil {
		encoded, encodeErr := json.Marshal(input.Properties)
		if encodeErr != nil || len(encoded) > maxNotionDataSourceSchemaBytes {
			return notionPageValidationError(event, "The data source schema exceeds the supported size.")
		}
	}
	var target notionDataSourceMoveTarget
	if input.Parent != nil {
		if target, err = authorizeNotionDataSourceMoveTarget(event.App, bearer, *input.Parent); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	moves := input.Parent != nil && target.ContainerID != authorization.Container.ContainerID
	updates := input.Title != nil || input.Icon != nil || input.Properties != nil
	viewID := newDatabaseEntityID()
	requestedTitle := ""
	if input.Title != nil {
		requestedTitle, input.titleSpans, err = notionDataSourceName(
			input.Title, notionNameMentions(event.App, authorization.Identity, authorization.Page),
		)
		if err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	if !moves && !updates {
		return serveNotionDataSourceUpdated(event, authorization)
	}
	if moves && target.PageID != authorization.Container.PageID {
		if updates {
			return notionPageValidationError(event,
				"Change the data source's other fields in a separate request when moving it to another page.")
		}
		return serveNotionDataSourceCrossPageMove(event, authorization, target)
	}
	// A property removed through the API goes to Deleted properties, as in Notion; its cells go too.
	trashed := []string{}
	err = applyNotionDataSourceMutation(
		withPropertiesTrashed(event.Request.Context(), &trashed), ygo, authorization,
		"notion-public-data-source-update:"+newDiagnosticReference(),
		func(txApp core.App, current notionDatabaseRowPageAuthorization, doc *crdt.Doc) ([]graphOperation, error) {
			operations := []graphOperation{}
			trashed = trashed[:0]
			if moves {
				currentTarget, err := authorizeNotionDataSourceMoveTarget(txApp, current.Identity, *input.Parent)
				if err != nil || currentTarget != target {
					return nil, refusedUnlessStorage(errDatabaseRowPageConflict, err)
				}
				spec := dataSourceMoveSpec{TargetID: target.ContainerID, ViewID: viewID}
				if operations, err = planNotionDataSourceMove(txApp, doc, current, target, spec); err != nil {
					return nil, err
				}
			}
			if updates {
				updated, err := planNotionDataSourceUpdate(txApp, current, input, requestedTitle)
				if err != nil {
					return nil, err
				}
				// Renaming the source alone leaves a full-page database its own
				// name (full_page_database_own_name.go).
				if input.Title != nil && !moves {
					if updated, err = keepFullPageDatabaseOwnName(
						txApp, current.Container.OwnerID, current.Container.PageID, current.Container.DataSource,
						requestedTitle, updated,
					); err != nil {
						return nil, err
					}
				}
				for _, operation := range updated {
					if operation.Kind == graphOperationKindDeleteProperty {
						trashed = append(trashed, operation.PropertyDeletion.ID)
						continue
					}
					operations = append(operations, operation)
				}
			}
			// A property removed goes to Deleted properties below, the views
			// on it keeping its name (database_property_trash_views.go).
			if len(operations) == 0 && len(trashed) == 0 {
				return nil, errNotionDataSourceUnchanged
			}
			return operations, nil
		},
		func(txApp core.App) (bool, error) {
			for _, propertyID := range trashed {
				target, err := loadDatabasePropertyTrashTarget(txApp, authorization.Container.PageID, propertyID)
				if err != nil {
					return false, err
				}
				// The other of a dependency pair went with the first.
				if target.DeletedAt != 0 {
					continue
				}
				if err := trashDatabaseProperty(
					txApp, target, authorization.Identity.PrincipalID, time.Now().UTC(),
				); err != nil {
					return false, err
				}
			}
			return len(trashed) > 0, nil
		},
	)
	if err == nil && moves {
		err = followDataSourceMoveLinkersElsewhere(event.App, authorization, authorization.Container.ContainerID, target.ContainerID)
	}
	if err != nil && !errors.Is(err, errNotionDataSourceUnchanged) {
		return notionDataSourceMutationError(event, err)
	}
	return serveNotionDataSourceUpdated(event, authorization)
}

// notionDataSourceName reads a requested title as the name and formatting a
// source keeps; mentions checks and names its mentions as a block's are
// (notionNameMentions). None, or only spaces, leaves a source or a database
// unnamed, as Notion's does when one is made or renamed so (seen 2026-11-12
// and 2026-11-13: [], "" and "   " all read back []); any other keeps its spaces, tabs and line breaks
// where they are, its ends too (seen 2026-11-13: " DB 끝 ", "\n가\n" and
// "\t나\t" read back as sent, and a name typed in Notion's app as "앱DB 끝 ").
// A fault of the title itself is a request error; a mention Notion refuses
// for what it names is said alone.
func notionDataSourceName(raw json.RawMessage, mentions func([]graphTextSpan) error) (string, []graphTextSpan, error) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The data source title is invalid.")
	}
	spans, err := notionTextRequestGraphSpans(value)
	if alone, refused := notionMentionRefusedAlone(err); refused {
		return "", nil, alone
	}
	if err != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The data source title is invalid.")
	}
	// Its formatting, links and mentions stay with it, as Notion's name
	// keeps them (seen 2026-11-07); a mention is named before the name is
	// measured.
	if err := mentions(spans); err != nil {
		alone, _ := notionMentionRefusedAlone(err)
		return "", nil, alone
	}
	result := spelledName(spans)
	if strings.TrimSpace(result) == "" {
		return "", []graphTextSpan{}, nil
	}
	if utf8.RuneCountInString(result) > maxGraphTitleRunes {
		return "", nil, notionDatabaseRowPageRequestInvalid("The data source title is invalid.")
	}
	encoded, _ := json.Marshal(result)
	kept, err := normalizeGraphNameSpans(encoded, spans)
	if err != nil {
		return "", nil, notionDatabaseRowPageRequestInvalid("The data source title's formatting is invalid.")
	}
	if kept == nil {
		kept = []graphTextSpan{}
	}
	return result, kept, nil
}

// errNotionDataSourceUnchanged is an update whose every part stays as it
// is (errNotionDataSourcePropertyLeft), answered with the source as it is.
var errNotionDataSourceUnchanged = errors.New("the data source update changes nothing")

func planNotionDataSourceUpdate(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	input notionDataSourceUpdateRequest,
	requestedTitle string,
) ([]graphOperation, error) {
	stored, err := loadStoredNotionDataSourceProperties(app, authorization.Container)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]storedNotionDataSourceProperty, len(stored)*2)
	// byPublicID finds a property a request names by its public ID, encoded
	// or not, after its name and Leafen's ID; the title's is "title", and a
	// property named so keeps its name.
	byPublicID := make(map[string]storedNotionDataSourceProperty, len(stored))
	names := make(map[string]string, len(stored))
	hiddenKeys := make(map[string]struct{})
	made := []string{}
	titleID := ""
	for _, property := range stored {
		names[property.Name] = property.ID
		if !notionDatabaseRowPagePropertyVisible(property.Type) {
			hiddenKeys[property.ID] = struct{}{}
			hiddenKeys[property.Name] = struct{}{}
			hiddenKeys[notionPropertyIDKey(property.PublicID)] = struct{}{}
			continue
		}
		if _, duplicate := byKey[property.ID]; duplicate {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		byKey[property.ID] = property
		if existing, duplicate := byKey[property.Name]; duplicate && existing.ID != property.ID {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		byKey[property.Name] = property
		byPublicID[notionPropertyIDKey(property.rowPageProperty().publicID())] = property
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) == graphValueTypeTitle {
			if titleID != "" {
				return nil, errNotionDatabaseRowPageProjectionInvalid
			}
			titleID = property.ID
		}
	}
	if titleID == "" {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	if input.Properties != nil && len(stored) > maxNotionDataSourceSchemaProperties {
		return nil, notionDatabaseRowPageRequestInvalid("the data source schema has too many properties")
	}
	operations := make([]graphOperation, 0, len(input.Properties)+1)
	if input.Title != nil || input.Icon != nil || input.description != nil {
		source := &graphDataSource{ID: authorization.Container.DataSource, Title: authorization.Container.Title,
			Description: input.description, DescriptionSpans: input.descriptionSpans}
		if input.Title != nil {
			source.Title, source.TitleSpans = requestedTitle, input.titleSpans
			if source.TitleSpans == nil {
				source.TitleSpans = []graphTextSpan{}
			}
			// The properties it plans read the title it is given
			// (notionRelationPairDefaultName).
			authorization.Container.Title = requestedTitle
		}
		if input.Icon != nil {
			var err error
			if source.Icon, source.IconType, err = notionDataSourceIcon(app, authorization.Identity, input.Icon); err != nil {
				return nil, err
			}
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: source})
	}
	keys := make([]string, 0, len(input.Properties))
	for key := range input.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	touched := map[string]struct{}{}
	// left is a property the request names that stays as it is
	// (errNotionDataSourcePropertyLeft).
	droppedID, madeID, left := false, false, false
	// final is the data source as the request leaves it, which the formulas
	// it writes read and a new property is placed among.
	final := make(map[string]databaseFormulaReferenceProperty, len(stored))
	for _, property := range stored {
		final[property.ID] = databaseFormulaReferenceProperty{
			ID: property.ID, Type: property.Type, Name: property.Name, Position: property.Position,
		}
	}
	for _, requestKey := range keys {
		raw := input.Properties[requestKey]
		key := strings.TrimSpace(requestKey)
		if key == "" {
			return nil, notionDatabaseRowPageRequestInvalid("database property key is empty")
		}
		if _, hidden := hiddenKeys[key]; hidden {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q does not exist", key)
		}
		current, exists := byKey[key]
		if !exists {
			if _, hidden := hiddenKeys[notionPropertyIDKey(key)]; hidden {
				return nil, notionDatabaseRowPageRequestInvalid("database property %q does not exist", key)
			}
			current, exists = byPublicID[notionPropertyIDKey(key)]
		}
		if exists {
			if _, duplicate := touched[current.ID]; duplicate {
				return nil, notionDatabaseRowPageRequestInvalid("database property %q was supplied more than once", key)
			}
			touched[current.ID] = struct{}{}
		}
		if string(raw) == "null" {
			if !exists {
				return nil, notionDatabaseRowPageRequestInvalid("database property %q does not exist", key)
			}
			if current.ID == titleID {
				return nil, notionDatabaseRowPageRequestInvalid("the title property cannot be removed")
			}
			delete(names, current.Name)
			delete(final, current.ID)
			droppedID = droppedID || current.Type == graphValueTypeUniqueID
			operations = append(operations, graphOperation{
				Kind:             graphOperationKindDeleteProperty,
				PropertyDeletion: &graphDatabaseDelete{ID: current.ID},
			})
			continue
		}
		next := ""
		if !exists {
			placed := make([]databaseFormulaReferenceProperty, 0, len(final))
			for _, property := range final {
				placed = append(placed, property)
			}
			var err error
			if next, err = notionNamedPropertyPosition(placed, titleID, notionRequestPropertyName(key, raw)); err != nil {
				return nil, err
			}
		}
		planned, err := planNotionDataSourceProperty(
			app, authorization, key, raw, current, exists, next, false,
		)
		if errors.Is(err, errNotionDataSourcePropertyLeft) {
			left = true
			continue
		}
		if err != nil {
			return nil, err
		}
		if exists {
			delete(names, current.Name)
		} else {
			made = append(made, planned.ID)
			madeID = madeID || planned.Type == graphValueTypeUniqueID
		}
		// A name another property has takes a number, as in Notion.
		if conflict, duplicate := names[planned.Name]; duplicate && conflict != planned.ID {
			taken := make(map[string]bool, len(names))
			for name := range names {
				taken[name] = true
			}
			planned.Name = distinctDatabasePropertyName(planned.Name, taken)
		}
		names[planned.Name] = planned.ID
		final[planned.ID] = databaseFormulaReferenceProperty{
			ID: planned.ID, Type: planned.Type, Name: planned.Name, Position: planned.Position,
		}
		operations = append(operations, mcpDataSourcePropertyOperation(
			authorization.Container.DataSource,
			storedMCPDataSourceProperty{
				ID: planned.ID, Name: planned.Name, Type: planned.Type,
				Position: planned.Position, Config: planned.Config,
			},
		))
	}
	// An ID property removed goes to Deleted properties, where it still is
	// the data source's one (database_unique_id_rules.go).
	if droppedID && madeID {
		return nil, notionDatabaseRowPageRequestInvalid(uniqueIDUnsavedMessage)
	}
	if len(operations) == 0 && left {
		return nil, nil
	}
	if len(operations) == 0 {
		return nil, notionDatabaseRowPageRequestInvalid("the data source update is empty")
	}
	written := notionFormulasWritten(operations, stored)
	if err := resolveNotionRollupRequests(app, authorization, authorization.Container.DataSource, operations, stored); err != nil {
		return nil, err
	}
	// A formula reads a property by its name, or by a name it had (database_formula_former_names.go).
	if err := checkNotionFormulaTypes(operations, notionFormulaUpdateScope(stored, operations, written),
		authorization.Container.DataSource, notionFormulaRelatedLoader(app)); err != nil {
		return nil, err
	}
	if err := storeNotionFormulaReferences(operations, func() (notionFormulaScope, error) {
		properties := make([]databaseFormulaReferenceProperty, 0, len(final))
		for _, property := range final {
			properties = append(properties, property)
		}
		stored := make([]storedNotionDataSourceProperty, 0, len(properties))
		sortDatabaseFormulaReferenceProperties(properties)
		for _, property := range properties {
			stored = append(stored, storedNotionDataSourceProperty{
				ID: property.ID, Type: property.Type, Name: property.Name, Position: property.Position,
			})
		}
		return notionDataSourceFormulaScope(app, authorization.Container, stored)
	}); err != nil {
		return nil, err
	}
	if err := notionFormulaCycleRefusal(notionFormulaUpdateScope(stored, operations, nil), written); err != nil {
		return nil, err
	}
	if len(names) > maxNotionDataSourceSchemaProperties {
		return nil, notionDatabaseRowPageRequestInvalid("the data source schema has too many properties")
	}
	hidden, err := databaseNewPropertiesHiddenInTables(app, authorization.Container.PageID, authorization.Container.DataSource, made)
	if err != nil {
		return nil, err
	}
	return append(operations, hidden...), nil
}

func notionDataSourceMutationError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return notionPageNotFound(event)
	}
	if answered, refused := notionPropertyAccessError(event, err); refused {
		return answered
	}
	if answered, missing := notionUserMissingAnswer(event, err); missing {
		return answered
	}
	requestError := &notionDatabaseRowPageRequestError{}
	if errors.As(err, &requestError) {
		return notionPageValidationError(event, requestError.message)
	}
	if tool := (notionToolRefusal{}); errors.As(err, &tool) {
		return notionCompatibilityErrorWithData(event, http.StatusBadRequest, notionErrorValidation, tool.message, tool.data(), nil)
	}
	if errors.Is(err, errDatabasePropertyInUse) {
		return notionPageValidationError(event,
			"A title property, a property a view groups or dates by, or a dependency property cannot be removed.")
	}
	graphError := &graphRequestError{}
	if errors.As(err, &graphError) {
		switch graphError.status {
		case http.StatusBadRequest:
			return notionPageValidationError(event, refusalMessage(graphError.message, graphError.reason))
		case http.StatusConflict:
			return notionGraphConflictError(event, graphError)
		}
	}
	if errors.Is(err, errDatabaseRowPageConflict) {
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The data source changed before the request could apply.",
		)
	}
	if errors.Is(err, errNotionDatabaseRowPageProjectionInvalid) {
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The database schema is not available in its current state.",
		)
	}
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return notionCompatibilityError(
			event, http.StatusForbidden, notionErrorRestrictedResource,
			"The organization's sharing security settings don't allow publishing this form to the web.",
		)
	}
	return notionRequestNotCompleted(event, err)
}

// notionDataSourceIcon resolves a strict icon object into the native
// presentation pair, reusing the page icon parser and reference checks.
func notionDataSourceIcon(
	app core.App, identity notionOAuthBearerIdentity, raw json.RawMessage,
) (*string, *string, error) {
	presentation, err := notionPagePresentation(raw, nil)
	if err != nil {
		return nil, nil, notionDatabaseRowPageRequestInvalid("the data source icon is invalid")
	}
	if err := validateNotionPagePresentationReferences(
		app, identity.PrincipalID, identity.WorkspaceID, &presentation,
	); err != nil {
		return nil, nil, err
	}
	return presentation.Icon, presentation.IconType, nil
}

// refuseNotionDatabaseOnlyDataSourceKeys answers a data source update that
// names a database's own keys as Notion's does (seen 2026-11-10): a
// description of null fails the body's shape, and a description or is_inline
// at all is pointed to the database update, whatever else the body holds.
func refuseNotionDatabaseOnlyDataSourceKeys(event *core.RequestEvent) (bool, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(event.Response, event.Request.Body, notionPublicAPIMaxReadBytes))
	event.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return false, nil
	}
	keys := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &keys) != nil {
		return false, nil
	}
	if value, found := keys["description"]; found && strings.TrimSpace(string(value)) == "null" {
		return true, notionPageValidationError(event,
			notionBodyFault("body.description", "an array or `undefined`", "null"))
	}
	for _, key := range []string{"description", "is_inline"} {
		if value, found := keys[key]; found && strings.TrimSpace(string(value)) != "null" {
			return true, notionPageValidationError(event,
				"A data source takes no `"+key+"`: set it through the database's update instead.")
		}
	}
	return false, nil
}
