package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func registerDatabaseStarterRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+databaseStarterSourceViewsPath, readDatabaseStarterSourceViews)
	event.Router.POST(apiRoot+databaseStarterApplyPath, ygo.applyDatabaseStarter).Bind(apis.BodyLimit(databaseStarterRequestBytes))
	event.Router.GET(apiRoot+databaseStarterUsedPath, readDatabaseStarterUsed)
}

func databaseStarterInvalid() error {
	return graphInputError(errorCodeDatabaseStarterInvalid, "Check the database template choices.")
}

func (p *pocketBaseYgoPersistence) applyDatabaseStarter(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseStarterApplyInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseStarterError(event, databaseStarterRefused("%s", bodyRefusalReason("database template choice", err)), "starter-input")
	}
	if err := databaseStarterInputRefusal(pageID, input); err != nil {
		return writeDatabaseStarterError(event, err, "starter-input")
	}
	catalog, err := readDatabaseStarterCatalog()
	if err != nil {
		return writeGraphError(event, err, "starter-catalog", errorCodeDatabaseStarterFailed, "Couldn't create the database.")
	}
	var template *databaseStarterTemplate
	for index := range catalog.Templates {
		if catalog.Templates[index].Key == input.TemplateID {
			template = &catalog.Templates[index]
			break
		}
	}
	if err := databaseStarterChoiceRefusal(catalog, template, input); err != nil {
		return writeDatabaseStarterError(event, err, "starter-choices")
	}
	sort.Strings(input.Features)
	// View order comes from the exported template, not the submitted order.
	sort.Strings(input.Views)
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(append([]byte("database-starter-v1\x00"), encoded...))
	hash := hex.EncodeToString(digest[:])
	apply := p.applyDatabaseStarterTemplate
	if input.ReplaceBlockID != "" {
		apply = p.applyDatabaseStarterInPlace
	}
	response, stage, err := apply(event.Request.Context(), user.Id, pageID, *template, input, hash, databaseStarterLanguage(catalog, input.Language))
	if err != nil {
		return writeDatabaseStarterError(event, err, stage)
	}
	return event.JSON(http.StatusCreated, response)
}

// applyDatabaseStarterTemplate turns an empty page into the template's
// database for the person: the add window's starters and the scoped "Memory"
// database agents make. The stage names where it failed.
func (p *pocketBaseYgoPersistence) applyDatabaseStarterTemplate(
	ctx context.Context, userID, pageID string, starter databaseStarterTemplate,
	input databaseStarterApplyInput, hash string, labels map[string]string,
) (databaseStarterApplyResponse, string, error) {
	template := &starter
	prepared, err := prepareDatabaseStarter(*template, pageID, input, labels, time.Now().In(personLocation(p.app, userID)))
	if err != nil {
		return databaseStarterApplyResponse{}, "starter-prepare", err
	}
	if !input.TaskDatabase {
		if err := markDatabaseStarterBand(&prepared, template.Key); err != nil {
			return databaseStarterApplyResponse{}, "starter-prepare", err
		}
	}
	kind, title := "database", template.Name
	if translated, ok := labels[title]; ok {
		title = translated
	}
	mutation := graphMutationInput{MutationID: input.MutationID, Operations: prepared.Operations, BasePageCursor: &input.BasePageCursor, BaseGraphRevision: &input.BaseGraphRevision, PageKind: &kind, PageTitle: &title, AsHeld: input.AsHeld}
	// Started on a page of the person's, the template's database takes the
	// page's place under the template's name and icon, the page as it was
	// going to the trash (database_start_page_trash.go), as Notion's does
	// (seen 2026-11-18: "작업 트래커" over a titled page's title). On a page
	// made for it — the add window's — a name given meanwhile is kept.
	if input.ReplacesPage {
		mutation.ReplacesPage = &input.ReplacesPage
	}
	receipt, err := p.applyContentGraphMutationContext(ctx, userID, pageID, mutation, contentGraphMutationHooks{
		EnforcePageLock: true, PersonChange: true, SuppressNotifications: true, SuppressAutomationEvents: true,
		PrepareInput: func(app core.App, auth pageAuthorization, doc *crdt.Doc, in graphMutationInput) (graphMutationInput, error) {
			if current := auth.Page.GetString("title"); current != "" && !input.ReplacesPage {
				in.PageTitle = &current
			}
			return in, nil
		},
		BeforeApplyOperations: func(_ core.App, auth pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
			// The made source's icon is the new database page's default icon —
			// the template's, or the empty task database's own (prepareDatabaseStarterTaskDatabase).
			// The existing page metadata commit below saves it with title/kind.
			if icon, iconType := databaseStarterMadeIcon(prepared); (auth.Page.GetString("icon") == "" || input.ReplacesPage) && icon != nil {
				auth.Page.Set("icon", *icon)
				if iconType != nil {
					auth.Page.Set("icon_type", *iconType)
				}
			}
			return nil
		},
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) { return hash, nil },
		Prepare: func(app core.App, auth pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
			if auth.Page.GetString("kind") == "database" {
				return contentGraphMutationPreparation{}, databaseStarterRefused("the page is a database already: a template goes on an empty page, or names its inline database with replaceBlockId")
			}
			blocks := []struct {
				ID       string `db:"id"`
				Type     string `db:"type"`
				ParentID string `db:"parent_id"`
			}{}
			if err := app.DB().NewQuery(`SELECT [[id]],[[type]],[[parent_id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} ORDER BY [[position]],[[id]] LIMIT 3`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
				return contentGraphMutationPreparation{}, err
			}
			if len(blocks) > 1 {
				return contentGraphMutationPreparation{}, databaseStarterPageNotEmpty()
			}
			operations := []graphOperation{}
			for _, block := range blocks {
				if block.ParentID != "" || block.Type != graphBlockTypeParagraph || doc.GetText(ygoBlockTextRootPrefix+block.ID).Len() > 0 {
					return contentGraphMutationPreparation{}, databaseStarterPageNotEmpty()
				}
				operations = append(operations, graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: block.ID}})
			}
			if input.TaskDatabase {
				// Named by the person, the database and its source are one name.
				for _, operation := range prepared.Operations {
					if operation.Kind == graphOperationKindUpsertDataSource {
						operation.DataSource.Title = auth.Page.GetString("title")
					}
				}
			}
			return contentGraphMutationPreparation{Operations: append(operations, prepared.Operations...)}, nil
		},
		AfterApply: func(app core.App, auth pageAuthorization, _ *crdt.Doc) error {
			return finishDatabaseStarter(app, auth, userID, pageID, prepared)
		},
	})
	if err != nil {
		return databaseStarterApplyResponse{}, "starter-apply", err
	}
	return databaseStarterApplyResponse{Receipt: receipt, DataSourceID: prepared.DataSourceID, ViewID: prepared.ViewID}, "", nil
}

// finishDatabaseStarter writes what the graph change doesn't hold: the
// template its data source came from and what it calls its untitled pages,
// the example rows' page bodies and icons, the database's row templates and
// its page layout.
func finishDatabaseStarter(app core.App, auth pageAuthorization, userID, pageID string, prepared preparedDatabaseStarter) error {
	if err := recordDatabaseStarterOrigin(app, prepared.DataSourceID, prepared.Origin); err != nil {
		return err
	}
	if err := recordDataSourceUntitledName(app, prepared.DataSourceID, prepared.UntitledName); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	for rowID, blocks := range prepared.Bodies {
		if err := writeNewRowPageBody(app, rowID, userID, blocks, now); err != nil {
			return err
		}
	}
	for _, row := range prepared.RowIcons {
		if err := updateDatabaseRowPagePresentation(
			app, row.RowID, auth.Workspace.WorkspaceID, pageID, userID, row.Icon, row.IconType,
		); err != nil {
			return err
		}
	}
	authority := databaseToolbarConfigAuthority{OwnerID: auth.Workspace.OwnerID, WorkspaceID: auth.Workspace.WorkspaceID, PageID: pageID, DataSourceID: prepared.DataSourceID}
	state := databaseToolbarMutationState{ActorID: userID}
	for _, operation := range prepared.Templates {
		if err := createDatabaseToolbarTemplate(app, authority, operation, &state); err != nil {
			return err
		}
	}
	if len(prepared.PageLayout) > 0 {
		layout, err := parseDatabasePageLayout(app, prepared.PageLayout, pageID, prepared.DataSourceID, auth.Workspace.WorkspaceID)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(layout)
		if err != nil {
			return err
		}
		if err := storeDatabasePageLayout(app, prepared.DataSourceID, auth.Workspace.WorkspaceID, encoded, 1, userID, now); err != nil {
			return err
		}
	}
	if len(prepared.TaskRoles) > 0 {
		revision := 0
		return configureDatabaseToolbarTaskFeatures(app, authority, databaseToolbarConfigMutationOperation{
			ExpectedRevision: &revision,
			TaskFeatures:     &databaseToolbarTaskFeatureWire{Subitems: json.RawMessage(`{}`), Dependencies: json.RawMessage(`{}`), Tasks: prepared.TaskRoles},
		}, &state)
	}
	return nil
}

// markDatabaseStarterBand gives the page a template made its "시작하기" band, as
// Notion's (seen 2026-11-06, task tracker): "샘플 페이지 삭제하기" for the
// example rows it made, "투어" where Leafen knows the template's tour, and
// "맞춤 설정", until someone closes it.
func markDatabaseStarterBand(prepared *preparedDatabaseStarter, key string) error {
	samples := []string{}
	for _, operation := range prepared.Operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			samples = append(samples, operation.Row.ID)
		}
	}
	band := map[string]any{"samples": samples}
	if key == "tasks" {
		band["tour"] = "tasks"
	}
	for _, operation := range prepared.Operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(operation.Block.Content, &content) != nil {
			return databaseStarterInvalid()
		}
		content["starterBand"] = band
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		operation.Block.Content = encoded
		return nil
	}
	return nil
}

// databaseStarterMadeIcon is the icon of the one source a template made, or none.
func databaseStarterMadeIcon(prepared preparedDatabaseStarter) (*string, *string) {
	var made *graphDataSource
	for _, operation := range prepared.Operations {
		if operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil {
			if made != nil {
				return nil, nil
			}
			made = operation.DataSource
		}
	}
	if made == nil {
		return nil, nil
	}
	return made.Icon, made.IconType
}
