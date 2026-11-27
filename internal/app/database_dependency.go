package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type databaseDependencyRequestError struct {
	status  int
	code    string
	message string
}

func (e *databaseDependencyRequestError) Error() string { return e.code }

type databaseDependencyCursor struct {
	Version          int    `json:"v"`
	Kind             string `json:"kind"`
	DataSourceID     string `json:"dataSourceId"`
	RowID            string `json:"rowId,omitempty"`
	Direction        string `json:"direction,omitempty"`
	Revision         int    `json:"revision"`
	VisibleHash      string `json:"visibleHash,omitempty"`
	RelatedRowID     string `json:"relatedRowId,omitempty"`
	PredecessorRowID string `json:"predecessorRowId,omitempty"`
	SuccessorRowID   string `json:"successorRowId,omitempty"`
	EdgeID           string `json:"edgeId"`
}

func registerDatabaseDependencyRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+databaseDependencyEdgesPath, handleDatabaseDependencyEdges)
	event.Router.POST(apiRoot+databaseDependencyGraphPath, handleDatabaseDependencyGraph)
}

func handleDatabaseDependencyEdges(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseDependencyEdgeRequest{}
	if event.BindBody(&input) != nil {
		return writeDatabaseDependencyError(event, dependencyInputError())
	}
	input.DataSourceID = strings.TrimSpace(input.DataSourceID)
	input.RowID = strings.TrimSpace(input.RowID)
	if input.Limit == 0 {
		input.Limit = maxDatabaseDependencyReadItems
	}
	if pageID == "" || input.DataSourceID == "" || input.RowID == "" ||
		len(input.DataSourceID) > maxGraphIdentifierLength || len(input.RowID) > maxGraphIdentifierLength ||
		(input.Direction != databaseDependencyDirectionPredecessors &&
			input.Direction != databaseDependencyDirectionSuccessors) ||
		input.ExpectedRevision < 1 || input.Limit < 1 || input.Limit > maxDatabaseDependencyReadItems {
		return writeDatabaseDependencyError(event, dependencyInputError())
	}

	response := databaseDependencyEdgeResponse{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			return dependencyNotFoundError()
		}
		if err != nil {
			return err
		}
		binding, err := loadDatabaseDependencyBinding(
			txApp, authorization.Workspace.OwnerID, pageID, input.DataSourceID,
		)
		if err != nil {
			return err
		}
		if binding.Revision != input.ExpectedRevision {
			return dependencyStaleError()
		}
		if err := requireDatabaseDependencyRows(
			txApp, authorization.Workspace.OwnerID, pageID, input.DataSourceID, []string{input.RowID},
		); err != nil {
			return err
		}
		cursor, err := decodeDatabaseDependencyCursor(input.Cursor, databaseDependencyCursor{
			Version: 1, Kind: "databaseDependencyEdges", DataSourceID: input.DataSourceID,
			RowID: input.RowID, Direction: input.Direction, Revision: binding.Revision,
		})
		if err != nil {
			return err
		}
		edges, hasMore, err := loadDatabaseDependencyEdgeWindow(
			txApp, authorization.Workspace.OwnerID, pageID, binding, input, cursor,
		)
		if err != nil {
			return err
		}
		response = databaseDependencyEdgeResponse{
			PageID: pageID, DataSourceID: input.DataSourceID, RowID: input.RowID,
			Direction: input.Direction, DependencyRevision: binding.Revision,
			RelatedRowIDs: make([]string, len(edges)), HasMore: hasMore,
		}
		if input.Direction == databaseDependencyDirectionPredecessors {
			response.PropertyID = binding.PredecessorPropertyID
		} else {
			response.PropertyID = binding.SuccessorPropertyID
		}
		for index := range edges {
			response.RelatedRowIDs[index] = edges[index].RelatedRowID
		}
		if hasMore {
			last := edges[len(edges)-1]
			next, err := encodeDatabaseDependencyCursor(databaseDependencyCursor{
				Version: 1, Kind: "databaseDependencyEdges", DataSourceID: input.DataSourceID,
				RowID: input.RowID, Direction: input.Direction, Revision: binding.Revision,
				RelatedRowID: last.RelatedRowID, EdgeID: last.ID,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return confirmDatabaseDependencyBinding(txApp, binding)
	})
	if err != nil {
		return writeDatabaseDependencyError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func handleDatabaseDependencyGraph(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseDependencyGraphRequest{}
	if event.BindBody(&input) != nil {
		return writeDatabaseDependencyError(event, dependencyInputError())
	}
	input.DataSourceID = strings.TrimSpace(input.DataSourceID)
	if input.Limit == 0 {
		input.Limit = maxDatabaseDependencyReadItems
	}
	visibleIDs, visibleHash, err := canonicalDatabaseDependencyVisibleRows(input.VisibleRowIDs)
	if err != nil || pageID == "" || input.DataSourceID == "" ||
		len(input.DataSourceID) > maxGraphIdentifierLength || input.ExpectedRevision < 1 ||
		input.Limit < 1 || input.Limit > maxDatabaseDependencyReadItems {
		return writeDatabaseDependencyError(event, dependencyInputError())
	}
	input.VisibleRowIDs = visibleIDs

	response := databaseDependencyGraphResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			return dependencyNotFoundError()
		}
		if err != nil {
			return err
		}
		binding, err := loadDatabaseDependencyBinding(
			txApp, authorization.Workspace.OwnerID, pageID, input.DataSourceID,
		)
		if err != nil {
			return err
		}
		if binding.Revision != input.ExpectedRevision {
			return dependencyStaleError()
		}
		if err := requireDatabaseDependencyRows(
			txApp, authorization.Workspace.OwnerID, pageID, input.DataSourceID, visibleIDs,
		); err != nil {
			return err
		}
		cursor, err := decodeDatabaseDependencyCursor(input.Cursor, databaseDependencyCursor{
			Version: 1, Kind: "databaseDependencyGraph", DataSourceID: input.DataSourceID,
			Revision: binding.Revision, VisibleHash: visibleHash,
		})
		if err != nil {
			return err
		}
		edges, hasMore, err := loadVisibleDatabaseDependencyGraph(
			txApp, authorization.Workspace.OwnerID, pageID, binding,
			visibleIDs, input.Limit, cursor,
		)
		if err != nil {
			return err
		}
		response = databaseDependencyGraphResponse{
			PageID: pageID, DataSourceID: input.DataSourceID,
			DependencyRevision: binding.Revision, VisibleRowIDs: visibleIDs,
			Edges: make([]databaseDependencyGraphEdge, len(edges)), HasMore: hasMore,
		}
		for index := range edges {
			// Each link with its ID, by which a timeline takes it away
			// (deleteDependencyEdge).
			response.Edges[index] = databaseDependencyGraphEdge{
				ID:               edges[index].ID,
				PredecessorRowID: edges[index].PredecessorRowID,
				SuccessorRowID:   edges[index].SuccessorRowID,
			}
		}
		if hasMore {
			last := edges[len(edges)-1]
			next, err := encodeDatabaseDependencyCursor(databaseDependencyCursor{
				Version: 1, Kind: "databaseDependencyGraph", DataSourceID: input.DataSourceID,
				Revision: binding.Revision, VisibleHash: visibleHash,
				PredecessorRowID: last.PredecessorRowID,
				SuccessorRowID:   last.SuccessorRowID, EdgeID: last.ID,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return confirmDatabaseDependencyBinding(txApp, binding)
	})
	if err != nil {
		return writeDatabaseDependencyError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func canonicalDatabaseDependencyVisibleRows(values []string) ([]string, string, error) {
	if len(values) < 1 || len(values) > maxDatabaseDependencyVisibleRows {
		return nil, "", dependencyInputError()
	}
	result := make([]string, len(values))
	seen := map[string]struct{}{}
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxGraphIdentifierLength {
			return nil, "", dependencyInputError()
		}
		if _, exists := seen[value]; exists {
			return nil, "", dependencyInputError()
		}
		seen[value] = struct{}{}
		result[index] = value
	}
	sort.Strings(result)
	digest := sha256.Sum256([]byte(strings.Join(result, "\x00")))
	return result, hex.EncodeToString(digest[:]), nil
}

func encodeDatabaseDependencyCursor(cursor databaseDependencyCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	if len(value) > maxDatabaseQueryCursorBytes {
		return "", dependencyInputError()
	}
	return value, nil
}

func decodeDatabaseDependencyCursor(raw *string, expected databaseDependencyCursor) (databaseDependencyCursor, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return expected, nil
	}
	value := strings.TrimSpace(*raw)
	if len(value) > maxDatabaseQueryCursorBytes {
		return databaseDependencyCursor{}, dependencyInputError()
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	cursor := databaseDependencyCursor{}
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 ||
		cursor.Kind != expected.Kind || cursor.DataSourceID != expected.DataSourceID ||
		cursor.RowID != expected.RowID || cursor.Direction != expected.Direction ||
		cursor.Revision != expected.Revision || cursor.VisibleHash != expected.VisibleHash ||
		cursor.EdgeID == "" {
		return databaseDependencyCursor{}, dependencyInputError()
	}
	return cursor, nil
}

func dependencyInputError() error {
	return &databaseDependencyRequestError{
		status: http.StatusBadRequest, code: errorCodeDatabaseDependencyInvalid,
		message: "Check the database dependency request.",
	}
}

func dependencyNotFoundError() error {
	return &databaseDependencyRequestError{
		status: http.StatusNotFound, code: errorCodeDatabaseDependencyNotFound,
		message: "Database item not found.",
	}
}

func dependencyStaleError() error {
	return &databaseDependencyRequestError{
		status: http.StatusConflict, code: errorCodeDatabaseDependencyStale,
		message: "The dependency configuration changed. Restart the read.",
	}
}

func writeDatabaseDependencyError(event *core.RequestEvent, err error) error {
	var requestError *databaseDependencyRequestError
	if errors.As(err, &requestError) {
		return apiError(event, requestError.status, requestError.code, requestError.message)
	}
	return apiErrorFrom(
		event, http.StatusInternalServerError, errorCodeDatabaseDependencyFailed,
		"Couldn't read the database dependencies.", err,
	)
}
