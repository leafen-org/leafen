import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { UsableApp, UsableAppFileOpener } from "@/appPlatformWire.generated";
import { fileOpenersFor } from "@/components/apps/AppFileOpener";
import { openAppFile } from "@/components/apps/appFileOpening";
import { useUsableAppList, withAppInstalledAgain } from "@/components/apps/useUsableApps";
import { NotionSelect } from "@/components/database/NotionSelect";
import { TopBar } from "@/components/page/TopBar";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useProductPages } from "@/components/product/productPagesContext";
import { useSidebarLayout as useShellLayout } from "@/components/shell/AppShell";
import { FileText, FilterIcon, SheetRowIcon, SlidesRowIcon, SortIcon } from "@/icons/leafen";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import { libraryAgoLabel } from "@/lib/relativeTime";
import { newId } from "@/lib/ids";
import { libraryHref, pushRoute, type LibraryViewId } from "@/lib/router";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import type { Page, Session, Workspace } from "@/types";
import type { WorkspaceDocument, WorkspaceDocumentKind } from "@/workspaceMembershipWire.generated";
import { LIBRARY_DOCUMENT_KINDS, useLibraryDocumentsChoice, useWorkspaceDocuments, type LibraryDocumentKindChoice } from "./libraryDocumentsData";
import { LibraryDocumentsPhoneChoice } from "./LibraryDocumentsPhoneChoice";
import { LibraryNewDocumentMenu, libraryNewDocumentChoices, type LibraryNewDocumentChoice } from "./LibraryNewDocumentMenu";
import { LibrarySearch } from "./LibrarySearch";
import { LibraryViewTabs, type LibraryViewListActions } from "./LibraryViewTabs";
import documentStyles from "./LibraryDocuments.module.css";
import panelStyles from "./LibraryPanel.module.css";
import styles from "./LibraryView.module.css";

const NAME_WIDTH = 420;
const COLUMN_WIDTH = 200;
/** The lines' run past the last column, as Notion's library table. */
const TABLE_END = 24;
const COLUMNS = ["page", "creator", "edited"] as const;
/** How long typing rests before the search reads. */
const SEARCH_REST_MS = 250;

/** A document's mark by its kind, colored as the kind's own apps color it. */
function DocumentKindIcon({ kind }: { kind: WorkspaceDocumentKind }) {
  const Icon = kind === "xlsx" ? SheetRowIcon : kind === "pptx" ? SlidesRowIcon : FileText;
  return <span className={documentStyles.kind} data-kind={kind} aria-hidden="true"><Icon size="regular" /></span>;
}

/**
 * The library's "문서" (document apps decision of 2026-10-11, 12):
 * the docx, xlsx, pptx, PDF, HWP and HWPX files on the pages the reader can
 * see, as a Google Docs list in the library's table — each file's kind and
 * name over the start of its words, its page, who put it there and when it
 * last changed — latest change or name first, one kind at a time, found by
 * name or words from "검색" — on a phone "검색", then "형식" and "정렬" as
 * the row's marks, as Notion's phone row is icons, their choices on a sheet. A file opens straight in the app that opens its
 * kind, editing as the reader may edit its page; without one, its page
 * opens. Reads to the end with "더 보기". "새 문서" starts one of the kinds
 * the reader's apps start in their own "문서" page or one they choose, and
 * opens it in its app — not on a phone, which only reads documents.
 */
export function LibraryDocuments({ session, workspace, pages, views, list, canCreatePage, onCreatePage }: {
  session: Session;
  workspace: Workspace;
  /** The workspace's pages the reader has, for where a new document goes. */
  pages: readonly Page[];
  views: readonly LibraryViewId[];
  list: LibraryViewListActions;
  canCreatePage: boolean;
  onCreatePage: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["library", "common"]);
  const [choice, changeChoice] = useLibraryDocumentsChoice(session.user.id, workspace.id);
  const [searching, setSearching] = useState(false);
  const [typed, setTyped] = useState("");
  const [query, setQuery] = useState("");
  const [narrow, setNarrow] = useState(false);
  const { phone } = useShellLayout(true);
  const [refresh, setRefresh] = useState(0);
  const listed = useWorkspaceDocuments(session, workspace.id, query, choice, refresh);
  const usable = useUsableAppList(session);
  const organizationId = useProductPages()?.organizationId;
  // The window belongs to the product, so it stays while the person moves elsewhere; the list reads again once it closes.
  const openWith = ({ app, opener, document }: { app: UsableApp; opener: UsableAppFileOpener; document: WorkspaceDocument }) => openAppFile({
    session, app, opener, onClosed: () => setRefresh((count) => count + 1),
    file: {
      workspaceId: workspace.id, pageId: document.pageId, blockId: document.blockId, fileId: document.fileId,
      name: document.name, contentType: document.contentType, size: document.size, canEdit: document.canEdit, version: document.version,
    },
  });
  const [creating, setCreating] = useState<"busy" | "failed" | "pageConflict" | null>(null);
  const newChoices = libraryNewDocumentChoices(usable.apps, organizationId);

  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(typed.trim().toLocaleLowerCase()), SEARCH_REST_MS);
    return () => window.clearTimeout(timer);
  }, [typed]);

  function closeSearch() {
    setTyped("");
    setSearching(false);
  }
  // A row's page opens full in its database, as the library opens it.
  function openPage(document: WorkspaceDocument) {
    pushRoute(document.containerPageId
      ? pageHref(document.containerPageId, new URLSearchParams({ p: document.pageId, pm: "f" })) : pageHref(document.pageId));
  }
  // The new document opens in the app that started it, and joins the list.
  function createDocument({ app, opener, made }: LibraryNewDocumentChoice, pageId: string) {
    setCreating("busy");
    const kind = made.type.slice(1);
    // The app as the list reads now, if it was installed again since (withAppInstalledAgain).
    withAppInstalledAgain(session, app, (now) => api.createWorkspaceDocument(session, workspace.id, {
      requestId: newId(), installId: now.installId, opener: opener.key, type: made.type,
      name: `${t(`library:documentList.untitled.${kind}`, { defaultValue: t("library:documentList.untitled.docx") })}${made.type}`,
      ...(pageId ? { pageId } : { homeTitle: t("library:documentList.homeTitle") }),
    }).then(({ document }) => ({ document, now }))).then(({ document, now }) => {
      setCreating(null);
      setRefresh((count) => count + 1);
      openWith({ app: now, opener, document });
    }, (error: unknown) => setCreating(error instanceof LeafenApiError && error.status === 409 ? "pageConflict" : "failed"));
  }
  // A document a database page's file property holds opens with its page.
  function openDocument(document: WorkspaceDocument) {
    const found = document.propertyId ? undefined : fileOpenersFor(usable.apps, document.name, document.contentType, organizationId)[0];
    if (found) openWith({ ...found, document });
    else openPage(document);
  }

  const kindOptions = [{ value: "", label: t("library:documentList.kinds.all") },
    ...Object.keys(LIBRARY_DOCUMENT_KINDS).map((kind) => ({ value: kind, label: t(`library:documentList.kinds.${kind}`) }))];
  const sortOptions = (["recent", "name"] as const).map((sort) => ({ value: sort, label: t(`library:documentList.sorts.${sort}`) }));
  const search = narrow ? null : (
    <LibrarySearch open={searching} value={typed} placeholder={t("library:documentList.searchPlaceholder")} onOpen={() => setSearching(true)}
      onChange={setTyped} onClose={closeSearch} />
  );
  const chooseKind = (kind: string) => changeChoice({ ...choice, kind: kind as LibraryDocumentKindChoice });
  const chooseSort = (sort: string) => changeChoice({ ...choice, sort: sort === "name" ? "name" : "recent" });
  // On a phone each is a mark whose choices rise on a sheet (LibraryDocumentsPhoneChoice).
  const kindChoice = phone ? (
    <LibraryDocumentsPhoneChoice label={t("library:documentList.kind")} icon={<FilterIcon size="small" aria-hidden="true" />}
      chosen={choice.kind !== ""} value={choice.kind} options={kindOptions} onChange={chooseKind} />
  ) : (
    <NotionSelect ariaLabel={t("library:documentList.kind")} className={documentStyles.choice} buttonClassName={documentStyles.choiceButton}
      value={choice.kind} options={kindOptions} onChange={chooseKind} />
  );
  const sortChoice = phone ? (
    <LibraryDocumentsPhoneChoice label={t("library:documentList.sort")} icon={<SortIcon size="small" aria-hidden="true" />}
      chosen={choice.sort === "name"} value={choice.sort} options={sortOptions} onChange={chooseSort} />
  ) : (
    <NotionSelect ariaLabel={t("library:documentList.sort")} className={documentStyles.choice} buttonClassName={documentStyles.choiceButton}
      value={choice.sort} options={sortOptions} onChange={chooseSort} />
  );
  const actions = (
    <div className={panelStyles.tabActions} data-library-tab-actions>
      {/* A phone only reads documents (document apps decision, 2026-11-14), so it starts none: a new one opened to an empty page read only. */}
      {phone ? null : <LibraryNewDocumentMenu choices={newChoices} pages={pages} session={session} workspaceId={workspace.id}
        busy={creating === "busy"} icon={(kind) => <DocumentKindIcon kind={kind} />} onCreate={createDocument} />}
      {phone ? <>{search}{kindChoice}{sortChoice}</> : <>{kindChoice}{sortChoice}{search}</>}
    </div>
  );

  const filtered = query !== "" || choice.kind !== "";
  return (
    <>
      {/* The bar names nothing; the page heads itself, as Notion's library (seen 2026-10-31). */}
      <TopBar title={t("library:title")} crumbs={<></>} />
      <div className={styles.wrap} data-library-view="documents">
        <header className={styles.header}>
          <h1 className={styles.title}>{t("library:title")}</h1>
          {canCreatePage ? (
            <button type="button" className={styles.newPage} data-library-new-page onClick={onCreatePage}>{t("library:newPage")}</button>
          ) : null}
        </header>
        <LibraryViewTabs views={views} current="documents" onSelect={(next) => pushRoute(libraryHref(next))} actions={actions}
          list={list} onNarrow={setNarrow} />
        <div className={panelStyles.body}>
          <div className={styles.tableScroll}>
            <div className={styles.table} role="table" aria-label={t("library:documentList.table")}
              style={{ width: NAME_WIDTH + COLUMNS.length * COLUMN_WIDTH + TABLE_END }}>
              <div className={styles.headRow} role="row">
                <span className={`${styles.nameColumn} ${documentStyles.head}`} role="columnheader">{t("library:documentList.columns.name")}</span>
                {COLUMNS.map((column) => (
                  <span key={column} className={`${styles.column} ${documentStyles.head}`} role="columnheader">
                    {t(`library:documentList.columns.${column}`)}
                  </span>
                ))}
              </div>
              {listed.documents.map((document) => {
                const creator = listed.actors.get(document.createdBy);
                return (
                  <div key={document.blockId} className={`${styles.row} ${documentStyles.row}`} role="row" data-library-document={document.blockId}>
                    <span className={styles.nameColumn} role="cell">
                      <button type="button" className={`${styles.pageButton} ${documentStyles.open}`}
                        aria-label={t("library:documentList.open", { name: document.name })} onClick={() => openDocument(document)}>
                        <DocumentKindIcon kind={document.kind} />
                        <span className={documentStyles.names}>
                          <span className={styles.pageTitle}>{document.name}</span>
                          {document.preview ? <span className={documentStyles.preview} data-library-document-preview>{document.preview}</span> : null}
                          {document.partial ? (
                            <span className={documentStyles.partial} data-library-document-partial={document.partial}>
                              {t(`library:documentList.partial.${document.partial}`)}
                            </span>
                          ) : null}
                        </span>
                      </button>
                    </span>
                    <span className={styles.column} role="cell" data-library-cell="page">
                      <button type="button" className={styles.sourceButton} onClick={() => openPage(document)}>
                        <PageIconGlyph page={{ icon: document.pageIcon, iconType: document.pageIconType as Page["iconType"] }} size={16} />
                        <span className={styles.cellText}>{pageNames.title({ title: document.pageTitle })}</span>
                      </button>
                    </span>
                    <span className={styles.column} role="cell" data-library-cell="creator">
                      {creator?.name ? (
                        <>
                          <PersonAvatar className={styles.avatar} userId={creator.userId} name={creator.name} size={20} />
                          <span className={styles.cellText}>{creator.name}</span>
                        </>
                      ) : null}
                    </span>
                    <span className={styles.column} role="cell" data-library-cell="edited">{libraryAgoLabel(document.updatedAt)}</span>
                  </div>
                );
              })}
            </div>
            {!listed.loading && !listed.failed && listed.documents.length === 0 ? (
              <p className={styles.empty} data-library-documents-empty>{t(filtered ? "library:documentList.noMatch" : "library:documentList.empty")}</p>
            ) : null}
            {listed.nextCursor && !listed.loading ? (
              <button type="button" className={styles.showMore} data-library-documents-more onClick={listed.loadMore}>{t("library:showMore")}</button>
            ) : null}
            {listed.failed ? <p className={styles.empty} role="status">{t("library:documentList.loadFailed")}</p> : null}
            {creating === "failed" || creating === "pageConflict" ? (
              <p className={styles.empty} role="status" data-library-new-document-error>
                {t(creating === "pageConflict" ? "library:documentList.pageConflict" : "library:documentList.createFailed")}
              </p>
            ) : null}
          </div>
        </div>
      </div>
    </>
  );
}
