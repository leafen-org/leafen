import { type MutableRefObject, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { BodyFocus } from "@/lib/pageTitleFocus";
import i18next from "i18next";
import { api } from "@/api";
import { userErrorText } from "@/lib/settingsErrors";
import { emptyContentGraph, type ContentGraph, type GraphDataSource } from "@/contentGraph";
import { SuggestingModeContext, type SuggestingMode } from "@/components/editor/suggestingMode";
import { useProductPages, type ProductPages } from "@/components/product/productPagesContext";
import { PageBodyDatabasePicker } from "@/components/product/PageBodyDatabasePicker";
import { usePageBodyEditing } from "@/components/product/usePageBodyEditing";
import { commentServiceFor, databaseRowCommentServiceFor } from "@/lib/commentService";
import { noteRowBodyEdited } from "@/lib/rowBodyEdits";
import { useShowPageLockedForMe } from "@/lib/pageLockedForMe";
import { holdPageBodyStats, pageBodyStats } from "@/lib/pageBodyStats";
import { useTellRowPageBody } from "@/lib/openPageBody";
import { StructuredEditor } from "@/StructuredEditor";
import type { Page, Session } from "@/types";
import type { TextSpan } from "@/lib/types";
import { PagePresentationHost } from "@/components/page/PageSlideshow";
import { inlineDatabaseFullPage, inlineDatabaseName, inlineDatabaseShownName, linkedDatabasePresentation, writtenInlineDatabaseName } from "@/lib/inlineDatabaseFullPage";
import { InlineDatabasePeekTitle } from "./InlineDatabasePeekTitle";
import { useInlineDatabaseHeadLocked } from "./databaseHeadLock";
import { SidePeekContext } from "./databaseRowOpeners";
import { templateBodyPlaceholder } from "./databaseViewShared";
import { WikiDatabaseView } from "@/components/WikiDatabaseView";
import { readDatabaseViewChoice, useDatabaseViewChoices } from "@/lib/databaseViewChoice";
import { wikiDatabaseLayout, wikiDatabaseViews } from "@/lib/wikiDatabaseLayout";
import { databasePageSource, type DatabaseDescriptionSource, type DatabasePageSourceWrites, wikiPageSource } from "@/components/page/DatabaseDescription";

/** A database shown alone in a database page's body: its name, as its page's title, and icon. */
export type OpenedRowPageDatabase = { title: string; titleSpans?: TextSpan[]; icon: string; iconType: NonNullable<GraphDataSource["iconType"]> };

const noPages: ProductPages = { pages: [], mentionPages: [], setData: () => {} };
const noStatus = () => {};

/**
 * A database page's body, edited as a page's is: its page content graph,
 * its page document and the page editor, under the row page's permission.
 * In suggesting mode its edits are suggested as the row's comments. A page
 * opened in a peek uses it too, with its comments the page's own.
 */
export function RowPageBodyEditor({
  session,
  rowId,
  workspaceId,
  readOnly,
  lockedForMe = false,
  databasesLockable = false,
  template = false,
  ownPage = false,
  tableOfContentsDisplay,
  suggesting = false,
  peek,
  whole = false,
  onSuggestingChange,
  onCanSuggestChange,
  onContentChange,
  onlyBlockId,
  fullPageDatabase = false,
  presentAs,
  pageStarter = true,
  focusRef,
  wiki,
  onDatabaseSource,
  onOpenedDatabase,
}: {
  session: Session;
  rowId: string;
  workspaceId: string;
  readOnly: boolean;
  /** The page is locked for the reader: its databases' rows' own pages read it (pageLockedForMe.ts). */
  lockedForMe?: boolean;
  /** Its lock alone holds the body for a reader who may edit it: its databases' own lock still unlocks. */
  databasesLockable?: boolean;
  /** A database template's body, whose @ menu offers "when duplicated" mentions. */
  template?: boolean;
  /** A page's own body rather than a database page's: its comments are the page's. */
  ownPage?: boolean;
  tableOfContentsDisplay?: string;
  suggesting?: boolean;
  /** Shown in a peek over its database, beside it or over its center (SuggestingMode `inPeek`, SidePeekContext). */
  peek?: "side" | "center";
  /**
   * The page opened whole, its database's page drawing it (StandaloneRowPage):
   * this body is the page's, which the product around it reads (lib/openPageBody.ts).
   */
  whole?: boolean;
  /** Turns the row page's suggesting on or off, where it is offered. */
  onSuggestingChange?: (active: boolean) => void;
  /** Hears whether the person may suggest edits to the body. */
  onCanSuggestChange?: (canSuggest: boolean) => void;
  /** Hears whether the body holds any block, once read, which the row's icon shows. */
  onContentChange?: (hasContent: boolean) => void;
  /** Shows only this block of the body, as an inline database opened alone does. */
  onlyBlockId?: string;
  /** Whether the empty body offers the page's starter; the template prompt stands in its place, as Notion's (seen 2026-11-01). */
  pageStarter?: boolean;
  /** A wiki's page that is a full-page database: its body is its database, titled by the page. */
  fullPageDatabase?: boolean;
  /** Presents the body as a database page opened over its database, titled as this page. */
  presentAs?: Page;
  /**
   * Takes the body's first line, adding one to an empty body, as a full
   * page's title's Enter does: a new page's body has no line to focus yet.
   */
  focusRef?: MutableRefObject<((selection: BodyFocus) => void) | null>;
  /**
   * A wiki whose pages are its database's, as its page shows it
   * (WikiDatabaseView): its Home, the body without its database, or one of
   * the database's views, as Notion's peek of a wiki (seen 2026-11-14).
   */
  wiki?: Page;
  /**
   * Hears the database the page heads as its own — a full-page database's,
   * a wiki's — as its body reads it, whose description its head offers
   * (PagePeek, as PageView's head from the open page's graph).
   */
  onDatabaseSource?: (source: DatabaseDescriptionSource | undefined) => void;
  /** Hears the name and icon of the database shown alone (onlyBlockId), which its page's path and window show; null until read. */
  onOpenedDatabase?: (database: OpenedRowPageDatabase | null) => void;
}) {
  const product = useProductPages() ?? noPages;
  const [graph, setGraph] = useState<ContentGraph>(emptyContentGraph);
  const [databasePickerOpen, setDatabasePickerOpen] = useState(false);
  const page = useMemo(() => ({ id: rowId, workspaceId }), [rowId, workspaceId]);
  const body = usePageBodyEditing({
    bootstrapRevision: 0, graph, setGraph, onStatus: product.onStatus ?? noStatus, page,
    pages: product.pages, mentionPages: product.mentionPages, organizationId: product.organizationId,
    refreshPages: product.refreshPages, session, setData: product.setData, setDatabasePickerOpen, workspaceId,
    mutationOutbox: product.mutationOutbox, inlineDatabases: product.inlineDatabases,
    templateMentions: template,
  });
  const onStatus = product.onStatus ?? noStatus;
  const lineless = Boolean(onlyBlockId) || fullPageDatabase;
  useLayoutEffect(() => {
    if (!focusRef) return;
    focusRef.current = lineless ? null : body.editorActions.focusFirstBody;
    return () => { focusRef.current = null; };
  });
  useEffect(() => onCanSuggestChange?.(body.graphCanSuggest), [body.graphCanSuggest, onCanSuggestChange]);
  // An edit of the body made here — not one the server or another tab sends —
  // shows the row last edited now, at most every two seconds (rowBodyEdits.ts).
  const textDocument = body.collaboration.document;
  useEffect(() => {
    if (!textDocument || ownPage || template) return;
    let told = 0;
    const hear = (_update: Uint8Array, _origin: unknown, _document: unknown, transaction: { local: boolean }) => {
      if (!transaction.local || Date.now() - told < 2000) return;
      told = Date.now();
      noteRowBodyEdited(rowId);
    };
    textDocument.on("update", hear);
    return () => textDocument.off("update", hear);
  }, [textDocument, ownPage, template, rowId]);
  // The body says its words to the page's "⋯" while it is open — a page's own, one opened whole, one in a
  // side or center peek, a wiki's page and a database's alike (pageBodyStats.ts).
  const statsSource = useRef({ graph, textDocument, pages: product.pages });
  statsSource.current = { graph, textDocument, pages: product.pages };
  useEffect(() => holdPageBodyStats(rowId, () => pageBodyStats(statsSource.current.graph, statsSource.current.textDocument, "",
    (id) => statsSource.current.pages.find((item) => item.id === id)?.title)), [rowId]);
  // A body being filled with its databases' copies has content coming: it
  // offers no templates meanwhile, as Notion's shows only its skeleton.
  const hasContent = body.graphFilling ? true : graph === emptyContentGraph ? null : graph.blocks.length > 0;
  useTellRowPageBody(whole ? rowId : null, { state: body.collaboration, canEdit: body.graphCanEdit, canSuggest: body.graphCanSuggest,
    pending: body.graphLoading || body.collaboration.status === "loading", hasContent: hasContent ?? undefined },
  () => ({ graph: statsSource.current.graph, document: statsSource.current.textDocument }));
  useEffect(() => {
    if (hasContent !== null) onContentChange?.(hasContent);
  }, [hasContent, onContentChange]);
  const suggestingMode = useMemo<SuggestingMode>(() => ({
    active: suggesting,
    onChange: onSuggestingChange,
    inPeek: Boolean(peek),
    async suggest(edit) {
      try {
        // A page's own body suggests as the page's comments, a database page's as its row's.
        await (ownPage ? commentServiceFor : databaseRowCommentServiceFor)(session, api).suggest(rowId, edit);
      } catch (error) {
        // A suggestion isn't kept to send later, so even offline it says it failed.
        onStatus(userErrorText(error, i18next.t("commentsPanel:errors.suggestFailed")), "error");
        throw error;
      }
    },
  }), [onStatus, onSuggestingChange, ownPage, peek, rowId, session, suggesting]);
  // An inline database opened alone is titled by its name; a linked one by
  // its own, or "〈source〉 보기" while it has none, as it is opened alone as a
  // page (inlineDatabasePageView) and as Notion's path and window name it
  // (seen 2026-11-21, a linked view in a row's body opened alone).
  const opened = onlyBlockId ? inlineDatabaseFullPage(graph, onlyBlockId) : null;
  const linked = opened?.block.content.linkedDatabaseSource === true ? linkedDatabasePresentation(opened.block) : null;
  const ownName = opened && !linked && !opened.viewBlock && opened.sources.length > 1 ? inlineDatabaseName(opened.block, opened.sources) : null;
  // Its own lock holds its name and icon, as on a page opened alone (databaseHeadLock.ts).
  const headLocked = useInlineDatabaseHeadLocked(opened && !linked && !opened.viewBlock ? { blockId: opened.block.id, locked: opened.block.content.locked === true } : null);
  // The name it goes by in the window, path and bar: one of several unnamed by its sources' names, a view block's "제목 없음" (inlineDatabaseShownName).
  const shownName = opened ? inlineDatabaseShownName(opened) : null;
  const openedName = shownName ? shownName.title : null;
  const openedSpans = shownName ? JSON.stringify(shownName.titleSpans) ?? "" : "";
  const openedIcon = opened ? (linked?.icon ? { icon: linked.icon, iconType: linked.iconType } : { icon: opened.titleSource.icon ?? "", iconType: opened.titleSource.iconType ?? "none" }) : null;
  useEffect(() => {
    onOpenedDatabase?.(openedName === null || !openedIcon ? null : {
      title: openedName, titleSpans: openedSpans ? JSON.parse(openedSpans) as TextSpan[] | undefined : undefined,
      icon: openedIcon.icon, iconType: openedIcon.iconType,
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps -- heard again as its name, spans or icon change.
  }, [onOpenedDatabase, openedName, openedSpans, openedIcon?.icon, openedIcon?.iconType]);
  // Its database's description and name are written through the adapter of
  // the moment, the source heard only when the body's graph changes.
  const adapter = useRef(body.databaseViewAdapter);
  adapter.current = body.databaseViewAdapter;
  const sourceWrites = useMemo<DatabasePageSourceWrites>(() => ({
    changeDescription: (source, change) => adapter.current.changeDescription(source, change),
    changeTitle: (source, title, spans) => adapter.current.changeTitle(source, title, spans),
    setPageNameSpans: (block, spans) => adapter.current.setPageNameSpans?.(block, spans),
  }), []);
  useEffect(() => {
    if (!onDatabaseSource) return;
    onDatabaseSource(fullPageDatabase ? databasePageSource(graph, "database", null, sourceWrites)
      : wiki ? wikiPageSource(graph, wiki, sourceWrites.changeDescription) : undefined);
  }, [fullPageDatabase, graph, onDatabaseSource, sourceWrites, wiki]);
  useShowPageLockedForMe(rowId, lockedForMe);
  const viewChoices = useDatabaseViewChoices();
  const wikiSourceId = wiki?.isWiki && wiki.wikiRootId === wiki.id ? wiki.wikiDataSourceId : undefined;
  const wikiLayout = useMemo(() => wikiDatabaseLayout(graph, wikiSourceId, (blockId) => readDatabaseViewChoice(session.user.id, blockId)),
    // A choice made here or in another view draws it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [graph, session.user.id, viewChoices, wikiSourceId]);
  // The body's editor showing a graph, or one block of it alone.
  const editor = (shown: ContentGraph, only: string | null) => (
    <StructuredEditor
      showPageOutline={tableOfContentsDisplay !== "off" && !only && !fullPageDatabase}
      fixedBody={Boolean(only) || fullPageDatabase}
      onlyBlockId={only}
      readOnly={readOnly || !body.graphCanEdit || suggesting}
      databasesWritable={suggesting && !readOnly && body.graphCanEdit}
      databasesLockable={databasesLockable && body.graphCanEdit && !suggesting}
      rowComments={!template && !ownPage}
      starterHint={template ? templateBodyPlaceholder() : undefined}
      pageStarter={pageStarter}
      pageId={rowId}
      workspaceId={workspaceId}
      session={session}
      collaborationDocument={body.collaboration.document}
      mentionSnapshot={body.mentionSnapshot}
      graph={shown}
      boardAdapter={body.boardAdapter}
      databaseViewAdapter={body.databaseViewAdapter}
      tableAdapter={body.tableAdapter}
      timelineAdapter={body.timelineAdapter}
      formService={body.formService}
      loading={body.graphLoading || body.collaboration.status === "loading"}
      // A body this device keeps no copy of, the server not there to read it, says so as a page's does.
      unavailable={body.graphUnavailable}
      pendingToolbarDataSourceIds={body.pendingToolbarDataSourceIds}
      {...body.editorBridge}
      onAddParagraph={body.editorActions.addParagraph}
      onAddDatabase={body.editorActions.addDatabase}
      // A page's own body offers Notion's starting points as the page view does (the starter row's form and "⋯").
      starter={ownPage && !template ? body.editorActions.pageStarter : undefined}
      // Every body's new or linked database asks for its source in its start
      // panel, a database page's and a template's too (2026-11-11: a linked
      // view made there stayed "New table", its "데이터 소스 연결" doing nothing).
      databaseStart={body.editorActions.databaseStart}
      onChangeParagraph={body.editorActions.changeParagraph}
      onAddProperty={body.addGraphProperty}
      onConfiguredView={body.applyConfiguredFormView}
    />
  );
  return (
    <SuggestingModeContext.Provider value={suggestingMode}><SidePeekContext.Provider value={peek === "side"}>
      {presentAs ? (
        <PagePresentationHost page={presentAs} graph={graph} collaborationDocument={body.collaboration.document} session={session} opened />
      ) : null}
      {opened && linked ? (linked.title ? (
        <InlineDatabasePeekTitle source={{ ...opened.titleSource, title: linked.title, titleSpans: linked.titleSpans as GraphDataSource["titleSpans"],
          icon: linked.icon, iconType: linked.iconType }} workspaceId={workspaceId} pageId={rowId} readOnly={readOnly || !body.graphCanEdit}
          onChange={(title, spans) => body.databaseViewAdapter.setLinkedPresentation(opened.block, { title, titleSpans: spans as GraphDataSource["titleSpans"] })}
          onIconChange={(icon) => body.databaseViewAdapter.setLinkedPresentation(opened.block, { icon })} />
      ) : null) : opened && ownName?.own ? (
        // One of several sources goes by its own name, which its title renames (inlineDatabaseName).
        <InlineDatabasePeekTitle source={{ ...opened.titleSource, title: ownName.title, titleSpans: ownName.titleSpans }} placeholder={ownName.placeholder}
          workspaceId={workspaceId} pageId={rowId} readOnly={readOnly || !body.graphCanEdit || headLocked}
          onChange={(title, spans) => body.databaseViewAdapter.setPageNameSpans?.(opened.block, writtenInlineDatabaseName(title, spans, opened.block))}
          onIconChange={(icon) => body.databaseViewAdapter.changeIcon(opened.titleSource, icon)} />
      ) : opened ? (
        <InlineDatabasePeekTitle source={opened.titleSource} workspaceId={workspaceId} pageId={rowId} readOnly={readOnly || !body.graphCanEdit || headLocked} nameless={opened.viewBlock}
          onChange={(title, spans) => body.databaseViewAdapter.changeTitle(opened.titleSource, title, spans)}
          onIconChange={(icon) => body.databaseViewAdapter.changeIcon(opened.titleSource, icon)} />
      ) : null}
      {wikiLayout && wiki ? (
        <WikiDatabaseView root={wiki} wiki={wikiDatabaseViews(wikiLayout, graph, readOnly || !body.graphCanEdit || suggesting,
          body.databaseViewAdapter, editor(wikiLayout.databaseGraph, wikiLayout.block.id))}>{editor(wikiLayout.homeGraph, null)}</WikiDatabaseView>
      ) : editor(graph, onlyBlockId ?? null)}
      {databasePickerOpen && (
        <PageBodyDatabasePicker actions={body.editorActions} onClose={() => setDatabasePickerOpen(false)} />
      )}
    </SidePeekContext.Provider></SuggestingModeContext.Provider>
  );
}
