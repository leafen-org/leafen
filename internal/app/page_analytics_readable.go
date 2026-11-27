package app

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A visit is sent in a batch with others, sometimes a while after it was
// made (the browser's queue, lib/pageAnalyticsQueue.ts). A page of the batch
// that can't be read any more — deleted for good, its workspace moved to
// another server, its share or link taken away — costs the batch none of the
// rest: its views are left out and the others count, as a row gone since it
// was viewed is (row_page_analytics.go). Only a page an operation holds for
// a while and leaves readable — its copy, its blocks being moved, its wiki
// change, its workspace on the way out, its move to another workspace — has the whole batch go again once
// that's done (page_busy.go), so that visit counts too.

// pageAnalyticsBusyPasses is whether a page held for this reason is read
// again once the operation is done.
func pageAnalyticsBusyPasses(reason string) bool {
	switch reason {
	case pageBusyCopying, pageBusyCopyingDatabases, pageBusyMoving, pageBusyWiki, pageBusyWorkspaceSending, pageBusyWorkspaceMoving:
		return true
	}
	return false
}

// pageAnalyticsObservationPage is the page a view is read through: a row
// page itself, a page or a database opened alone through the page holding it.
func pageAnalyticsObservationPage(observation pageAnalyticsObservation) string {
	if observation.RowID != "" {
		return observation.RowID
	}
	return observation.PageID
}

// recordPageAnalyticsMemberBatch records a member's batch, or, when one of
// its pages can't be read, the part of it that can.
func recordPageAnalyticsMemberBatch(
	app core.App,
	principalID string,
	input pageAnalyticsMemberRecordRequest,
) (pageAnalyticsRecordResponse, pageAnalyticsRecordStatus, error) {
	response, status, err := applyPageAnalyticsMemberBatch(app, principalID, input)
	if status != pageAnalyticsRecordNotFound || err != nil {
		return response, status, err
	}
	readable := map[string]bool{}
	kept := make([]pageAnalyticsObservation, 0, len(input.Observations))
	for _, observation := range input.Observations {
		pageID := pageAnalyticsObservationPage(observation)
		ok, asked := readable[pageID]
		if !asked {
			_, err := authorizePageSet(app, principalID, input.WorkspaceID, []string{pageID}, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return pageAnalyticsRecordResponse{}, pageAnalyticsRecordOK, err
			}
			if err != nil {
				if busy := pageBusyForMember(app, principalID, pageID); pageAnalyticsBusyPasses(busy) {
					return pageAnalyticsRecordResponse{}, pageAnalyticsRecordNotFound, pageBusyError(busy)
				}
			}
			ok = err == nil
			readable[pageID] = ok
		}
		if ok {
			kept = append(kept, observation)
		}
	}
	// A batch none of whose pages can be read stays not found: the browser
	// lets it go (lib/pageAnalyticsBrowserRuntime.ts).
	if len(kept) == 0 || len(kept) == len(input.Observations) {
		return response, status, nil
	}
	input.Observations = kept
	return applyPageAnalyticsMemberBatch(app, principalID, input)
}

// recordPageAnalyticsAnonymousBatch records a visitor's batch, or, when one
// of its pages isn't public any more, the part of it that still is.
func recordPageAnalyticsAnonymousBatch(
	app core.App,
	input pageAnalyticsPublicInput,
	authorize publicAnalyticsAuthorizer,
) (pageAnalyticsRecordResponse, pageAnalyticsRecordStatus, error) {
	response, status, err := applyPageAnalyticsAnonymousBatch(app, input, authorize)
	if status != pageAnalyticsRecordNotFound || err != nil {
		return response, status, err
	}
	now := time.Now().UTC()
	visible := map[string]bool{}
	kept := make([]pageAnalyticsObservation, 0, len(input.Observations))
	for _, observation := range input.Observations {
		ok, asked := visible[observation.PageID]
		if !asked {
			_, err := authorize(app, []string{observation.PageID}, now)
			if err != nil && !errors.Is(err, errPublicVisibilityNotFound) {
				return pageAnalyticsRecordResponse{}, pageAnalyticsRecordOK, err
			}
			ok = err == nil
			visible[observation.PageID] = ok
		}
		if ok {
			kept = append(kept, observation)
		}
	}
	if len(kept) == 0 || len(kept) == len(input.Observations) {
		return response, status, nil // not found, as above
	}
	input.Observations = kept
	return applyPageAnalyticsAnonymousBatch(app, input, authorize)
}
