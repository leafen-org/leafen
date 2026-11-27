package app

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A view update names the view as its writer last saw it and the view it
// wants. The server keeps what others changed meanwhile: each field and
// each config key the writer changed takes the writer's value, the rest
// keep their stored value, and a key both changed differently is a
// conflict. A replayed update finds its own values and changes nothing.
// A dashboard's layout merged out of shape is a conflict too.

var errGraphViewConflict = graphConflictError(errorCodeGraphViewConflict, "The view settings changed elsewhere. Try again.")

// graphViewMergeMissing stands for a field or key a side does not have.
type graphViewMergeMissing struct{}

func resolveGraphViewUpdates(app core.App, pageID string, operations []graphOperation) ([]graphOperation, error) {
	resolved := operations
	copied := false
	for index, operation := range operations {
		if operation.Kind != graphOperationKindUpdateView {
			continue
		}
		update := operation.ViewUpdate
		if update == nil || update.View.ID == "" || update.Base.ID != update.View.ID {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the database view.")
		}
		current, exists, err := loadDatabaseViewRecord(app, pageID, update.View.ID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errGraphViewConflict
		}
		merged, err := mergeGraphView(update.Base, update.View, current)
		if err != nil {
			return nil, err
		}
		if !copied {
			resolved, copied = append([]graphOperation(nil), operations...), true
		}
		resolved[index] = graphOperation{Kind: graphOperationKindUpsertView, View: &merged}
	}
	return resolved, nil
}

func mergeGraphView(base, next graphDatabaseView, current databaseViewAuthority) (graphDatabaseView, error) {
	baseConfig, err := databaseJSONObject(base.Config)
	if err != nil {
		return graphDatabaseView{}, err
	}
	nextConfig, err := databaseJSONObject(next.Config)
	if err != nil {
		return graphDatabaseView{}, err
	}
	merged := graphDatabaseView{ID: next.ID}
	fields := []struct {
		target              *string
		base, next, current string
	}{
		{&merged.DataSourceID, base.DataSourceID, next.DataSourceID, current.DataSourceID},
		{&merged.ContainerID, base.ContainerID, next.ContainerID, current.ContainerID},
		{&merged.Type, base.Type, next.Type, current.Type},
		{&merged.Name, base.Name, next.Name, current.Name},
		{&merged.Position, base.Position, next.Position, current.Position},
	}
	for _, field := range fields {
		value, err := mergeGraphViewValue(field.base, field.next, field.current)
		if err != nil {
			return graphDatabaseView{}, err
		}
		*field.target = value.(string)
	}
	config := map[string]any{}
	keys := map[string]struct{}{}
	for _, values := range []map[string]any{baseConfig, nextConfig, current.Config} {
		for key := range values {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		value, err := mergeGraphViewValue(graphViewMergeValue(baseConfig, key), graphViewMergeValue(nextConfig, key),
			graphViewMergeValue(current.Config, key))
		if err != nil {
			return graphDatabaseView{}, err
		}
		if _, missing := value.(graphViewMergeMissing); !missing {
			config[key] = value
		}
	}
	if merged.Config, err = json.Marshal(config); err != nil {
		return graphDatabaseView{}, err
	}
	// A dashboard's rows and their sizes are one layout: the writer's rows
	// kept with sizes someone gave other rows meanwhile is a conflict, not
	// a layout anyone asked for.
	if validateDashboardLayoutSizes(parseDashboardViewConfig(next.Config)) == nil &&
		validateDashboardLayoutSizes(parseDashboardViewConfig(merged.Config)) != nil {
		return graphDatabaseView{}, errGraphViewConflict
	}
	return merged, nil
}

func graphViewMergeValue(values map[string]any, key string) any {
	if value, found := values[key]; found {
		return value
	}
	return graphViewMergeMissing{}
}

func mergeGraphViewValue(base, next, current any) (any, error) {
	switch {
	case graphViewValuesEqual(base, next):
		return current, nil
	case graphViewValuesEqual(current, base), graphViewValuesEqual(current, next):
		return next, nil
	default:
		return nil, errGraphViewConflict
	}
}

func graphViewValuesEqual(left, right any) bool {
	_, leftMissing := left.(graphViewMergeMissing)
	_, rightMissing := right.(graphViewMergeMissing)
	if leftMissing || rightMissing {
		return leftMissing == rightMissing
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func isGraphViewConflict(err error) bool {
	return errors.Is(err, errGraphViewConflict)
}
