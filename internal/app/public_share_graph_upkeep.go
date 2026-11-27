package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The upkeep settles the queued pages of the share graphs: a page whose
// parent is in a graph joins it, to be expanded with the pages under it, and
// any other page leaves it, queueing the pages under it in the graph after
// it. A database page sits under the page holding its database. Page changes
// arrive through requests, so the upkeep wakes after each write request and
// also looks for work now and then.

const (
	publicShareGraphUpkeepRunnerStoreKey = "leafenPublicShareGraphUpkeepRunner"
	publicShareGraphUpkeepWakeHandlerID  = "leafenPublicShareGraphUpkeepWake"
	publicShareGraphUpkeepBurst          = 64
	publicShareGraphUpkeepTurnDelay      = time.Millisecond
	publicShareGraphUpkeepFailureDelay   = time.Second
	publicShareGraphUpkeepIdlePoll       = 30 * time.Second
	publicShareGraphChildWindow          = 256
	errorCodePublicShareGraphUpkeep      = "LEAFEN_PUBLIC_SHARE_GRAPH_UPKEEP_FAILED"
)

type storedPublicShareGraphJob struct {
	LinkID string `db:"share_link_id"`
	PageID string `db:"page_id"`
}

type storedPublicShareGraphLink struct {
	WorkspaceID    string `db:"workspace_id"`
	OwnerID        string `db:"owner_id"`
	RootPageID     string `db:"root_page_id"`
	SourceRevision int    `db:"source_revision"`
	Enabled        bool   `db:"enabled"`
}

// advancePublicShareGraphUpkeep settles one queued page, or else expands one
// page that joined a graph, and reports whether it found work.
func advancePublicShareGraphUpkeep(ctx context.Context, app core.App) (bool, error) {
	worked := false
	err := app.RunInTransaction(func(txApp core.App) error {
		job := storedPublicShareGraphJob{}
		err := txApp.DB().NewQuery(`
			SELECT [[share_link_id]], [[page_id]] FROM {{leafen_public_share_graph_jobs}}
			INDEXED BY [[idx_leafen_public_share_graph_jobs_ready]]
			ORDER BY [[queued_at]], [[share_link_id]], [[page_id]] LIMIT 1
		`).WithContext(ctx).One(&job)
		if err == nil {
			worked = true
			return settlePublicShareGraphPage(ctx, txApp, job)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = txApp.DB().NewQuery(`
			SELECT [[member]].[[share_link_id]], [[member]].[[page_id]]
			FROM {{leafen_public_share_graph_memberships}} [[member]]
				INDEXED BY [[idx_leafen_public_share_graph_building]]
			JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]] = [[member]].[[share_link_id]]
				AND [[link]].[[source_revision]] = [[member]].[[source_revision]] AND [[link]].[[enabled]] = 1
			WHERE [[member]].[[state]] = 'building'
			LIMIT 1
		`).WithContext(ctx).One(&job)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		return expandPublicShareGraphPage(ctx, txApp, job)
	})
	return worked, err
}

func loadPublicShareGraphLink(ctx context.Context, app core.App, linkID string) (storedPublicShareGraphLink, bool, error) {
	link := storedPublicShareGraphLink{}
	err := app.DB().NewQuery(`
		SELECT [[link]].[[workspace_id]], [[workspace]].[[owner]] AS [[owner_id]], [[link]].[[root_page_id]],
			[[link]].[[source_revision]], [[link]].[[enabled]]
		FROM {{leafen_public_share_links}} [[link]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[link]].[[workspace_id]]
		WHERE [[link]].[[id]] = {:linkId}
	`).WithContext(ctx).Bind(dbx.Params{"linkId": linkID}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return link, false, nil
	}
	return link, err == nil, err
}

// settlePublicShareGraphPage puts a queued page in its graph when the page it
// sits under is there, and takes it out, with the pages under it following,
// when not.
func settlePublicShareGraphPage(ctx context.Context, app core.App, job storedPublicShareGraphJob) error {
	link, found, err := loadPublicShareGraphLink(ctx, app, job.LinkID)
	if err != nil {
		return err
	}
	params := dbx.Params{
		"linkId": job.LinkID, "pageId": job.PageID, "rootPageId": link.RootPageID,
		"workspaceId": link.WorkspaceID, "ownerId": link.OwnerID, "revision": link.SourceRevision,
		"now": time.Now().UTC().Format(time.RFC3339Nano), "queuedAt": time.Now().UnixMilli(),
	}
	if found && link.Enabled && job.PageID != link.RootPageID {
		belongs := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] = {:pageId} AND [[page]].[[workspace]] = {:workspaceId}
				AND [[page]].[[owner]] = {:ownerId} AND NOT ` + pageInTrashSQL("[[page]]") + `
				AND [[page]].[[notion_import_staging]] = 0
				AND NOT EXISTS (SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]] = [[page]].[[id]])
				AND EXISTS (
					SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[parent]]
					WHERE [[parent]].[[share_link_id]] = {:linkId} AND [[parent]].[[page_id]] = ` + pageAncestryParentSQL("[[page]]") + `
						AND [[parent]].[[source_revision]] = {:revision}
				)
		`).WithContext(ctx).Bind(params).Row(&belongs); err != nil {
			return err
		}
		if belongs > 0 {
			err = insertPublicShareGraphMember(ctx, app, params)
		} else {
			err = removePublicShareGraphMember(ctx, app, params)
		}
		if err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_public_share_graph_jobs}} WHERE [[share_link_id]] = {:linkId} AND [[page_id]] = {:pageId}
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func insertPublicShareGraphMember(ctx context.Context, app core.App, params dbx.Params) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_memberships}}
			([[share_link_id]], [[root_page_id]], [[page_id]], [[source_revision]], [[state]], [[updated_at]], [[position]])
		VALUES ({:linkId}, {:rootPageId}, {:pageId}, {:revision}, 'building', {:now}, {:pageId})
		ON CONFLICT ([[share_link_id]], [[page_id]]) DO NOTHING
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func removePublicShareGraphMember(ctx context.Context, app core.App, params dbx.Params) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_public_share_graph_memberships}}
		WHERE [[share_link_id]] = {:linkId} AND [[page_id]] = {:pageId}
	`).WithContext(ctx).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_jobs}} ([[share_link_id]], [[page_id]], [[queued_at]])
		SELECT [[member]].[[share_link_id]], [[member]].[[page_id]], {:queuedAt}
		FROM {{leafen_public_share_graph_memberships}} [[member]]
		WHERE [[member]].[[share_link_id]] = {:linkId}
			AND [[member]].[[page_id]] IN (` + publicShareGraphChildrenSQL("{:pageId}") + `)
		ON CONFLICT ([[share_link_id]], [[page_id]]) DO NOTHING
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

// publicShareGraphChildrenSQL lists the pages under a page in the published
// page graph: its sub-pages, the pages of wikis that sit under it and the
// pages of the other databases it holds. A database's pages sit on the page
// holding their data source and a wiki page's sub-pages in its own data
// source, so each list is looked up by those keys and reads only the pages
// under the page; sub-pages and wikis are read by their page's index whatever
// order the statement asks for or however few wikis the server has
// (2026-11-08: by the data source alone, unlinking a page's sharing read
// every database page of the server).
func publicShareGraphChildrenSQL(parent string) string {
	return `SELECT [[id]] FROM {{leafen_pages}} INDEXED BY [[idx_leafen_pages_parent_source_lookup]]
			WHERE [[parent_id]] = ` + parent + ` AND [[data_source_id]] = ''
		UNION ALL
		SELECT [[wiki_child]].[[id]] FROM {{leafen_data_sources}} [[wiki_child_source]]
			INDEXED BY [[idx_leafen_data_sources_page_id_lookup]]
		JOIN {{leafen_database_rows}} [[wiki_child]] ON [[wiki_child]].[[owner]] = [[wiki_child_source]].[[owner]]
			AND [[wiki_child]].[[page_id]] = [[wiki_child_source]].[[page_id]]
			AND [[wiki_child]].[[data_source_id]] = [[wiki_child_source]].[[id]] AND [[wiki_child]].[[subitem_parent_id]] = ''
		WHERE [[wiki_child_source]].[[page_id]] = ` + parent + ` AND [[wiki_child_source]].[[wiki]] = 1
		UNION ALL
		SELECT [[wiki_child]].[[id]] FROM {{leafen_database_rows}} [[wiki_parent]]
		CROSS JOIN {{leafen_data_sources}} [[wiki_child_source]] ON [[wiki_child_source]].[[id]] = [[wiki_parent]].[[data_source_id]]
			AND [[wiki_child_source]].[[wiki]] = 1
		JOIN {{leafen_database_rows}} [[wiki_child]] INDEXED BY [[idx_leafen_database_rows_subitem_order]]
			ON [[wiki_child]].[[owner]] = [[wiki_parent]].[[owner]] AND [[wiki_child]].[[page_id]] = [[wiki_parent]].[[page_id]]
			AND [[wiki_child]].[[data_source_id]] = [[wiki_parent]].[[data_source_id]]
			AND [[wiki_child]].[[subitem_parent_id]] = [[wiki_parent]].[[id]]
		WHERE [[wiki_parent]].[[id]] = ` + parent + `
		UNION ALL
		SELECT [[database_child]].[[id]] FROM {{leafen_data_sources}} [[database_child_source]]
		JOIN {{leafen_database_rows}} [[database_child]] ON [[database_child]].[[owner]] = [[database_child_source]].[[owner]]
			AND [[database_child]].[[page_id]] = [[database_child_source]].[[page_id]]
			AND [[database_child]].[[data_source_id]] = [[database_child_source]].[[id]]
		WHERE [[database_child_source]].[[page_id]] = ` + parent + ` AND [[database_child_source]].[[wiki]] = 0`
}

// publicShareGraphLiveChildrenSQL lists, by id after {:afterId}, up to
// {:limit} live pages under the page {:parentId} in the published page graph
// of the workspace {:workspaceId} owned by {:ownerId}. A page whose sharing
// is unlinked from its parent's leaves the graphs above it, with the pages
// under it, as Notion's unlinked page keeps its own general access. The pages
// under the page lead the join (2026-11-08: led by the workspace's pages in id
// order, each window read every page of the workspace after {:afterId}).
func publicShareGraphLiveChildrenSQL() string {
	return `SELECT [[child]].[[id]] FROM (` + publicShareGraphChildrenSQL("{:parentId}") + `) [[child]]
		CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[child]].[[id]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[notion_import_staging]] = 0 AND NOT ` + pageInTrashSQL("[[page]]") + ` AND [[child]].[[id]] > {:afterId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]] = [[child]].[[id]])
		ORDER BY [[child]].[[id]] LIMIT {:limit}`
}

// expandPublicShareGraphPage puts the live pages under a page that joined a
// graph in it, its sub-pages and database pages, to be expanded in turn, and
// marks the page complete.
func expandPublicShareGraphPage(ctx context.Context, app core.App, job storedPublicShareGraphJob) error {
	link, found, err := loadPublicShareGraphLink(ctx, app, job.LinkID)
	if err != nil || !found {
		return err
	}
	params := dbx.Params{
		"linkId": job.LinkID, "parentId": job.PageID, "rootPageId": link.RootPageID,
		"workspaceId": link.WorkspaceID, "ownerId": link.OwnerID, "revision": link.SourceRevision,
		"now": time.Now().UTC().Format(time.RFC3339Nano), "limit": publicShareGraphChildWindow,
	}
	cursor := ""
	for {
		params["afterId"] = cursor
		children := []publicMutationRoutingPage{}
		if err := app.DB().NewQuery(publicShareGraphLiveChildrenSQL()).WithContext(ctx).Bind(params).All(&children); err != nil {
			return err
		}
		for _, child := range children {
			params["pageId"] = child.ID
			if err := insertPublicShareGraphMember(ctx, app, params); err != nil {
				return err
			}
		}
		if len(children) < publicShareGraphChildWindow {
			break
		}
		cursor = children[len(children)-1].ID
	}
	params["pageId"] = job.PageID
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_public_share_graph_memberships}} SET [[state]] = 'complete', [[updated_at]] = {:now}
		WHERE [[share_link_id]] = {:linkId} AND [[page_id]] = {:pageId} AND [[source_revision]] = {:revision}
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

type publicShareGraphUpkeepRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newPublicShareGraphUpkeepRunner(app core.App) *publicShareGraphUpkeepRunner {
	return &publicShareGraphUpkeepRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *publicShareGraphUpkeepRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel
	runner.done = make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *publicShareGraphUpkeepRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodePublicShareGraphUpkeep, ctx.Err())
	}
}

func (runner *publicShareGraphUpkeepRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *publicShareGraphUpkeepRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		hasMore, err := runner.advanceBurst(ctx)
		wait := publicShareGraphUpkeepIdlePoll
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodePublicShareGraphUpkeep, "public.share-graph.upkeep", err)
			wait = publicShareGraphUpkeepFailureDelay
		} else if hasMore {
			wait = publicShareGraphUpkeepTurnDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// advanceBurst settles queued graph pages first, then the effects of
// sharing changes (advancePublicMutationEffect), which start new graphs.
func (runner *publicShareGraphUpkeepRunner) advanceBurst(ctx context.Context) (bool, error) {
	for turn := 0; turn < publicShareGraphUpkeepBurst; turn++ {
		worked, err := advancePublicShareGraphUpkeep(ctx, runner.app)
		if err != nil {
			return false, err
		}
		if !worked {
			worked, err = advancePublicMutationEffect(ctx, runner.app)
		}
		if err == nil && !worked {
			worked, err = advancePageShareAccessNotice(ctx, runner.app)
		}
		if err != nil || !worked {
			return false, err
		}
	}
	return true, nil
}

func registerPublicShareGraphUpkeepWake(event *core.ServeEvent, runner *publicShareGraphUpkeepRunner) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: publicShareGraphUpkeepWakeHandlerID,
		Func: func(request *core.RequestEvent) error {
			err := request.Next()
			if request.Request.Method != http.MethodGet && request.Request.Method != http.MethodHead &&
				request.Request.Method != http.MethodOptions {
				runner.notify()
			}
			return err
		},
	})
}
