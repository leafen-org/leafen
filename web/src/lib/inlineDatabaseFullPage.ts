import type { ReactNode } from "react";
import type { ContentGraph, GraphBlock, GraphDataSource } from "@/contentGraph";
import type { GraphDataSourceIconType } from "@/contentGraphWire.generated";
import type { DataSourceIcon, NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { inlineDatabaseAtAddress, rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { pageHref } from "@/lib/navigation";
import { linkedDatabaseName, untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";
import { databaseBlockMembers, databaseNamingSourceIds, databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { TextSpan } from "@/lib/types";
import { cellSpans, cellValueFromSpans } from "@/components/database/richTextCellModel";

/**
 * An inline database opened alone as a page, as Notion's "전체 페이지로
 * 열기" does (seen 2026-10-01): its own ID's address with its view, the
 * database's name as the page's title. An older link names its page with this
 * parameter.
 */
const INLINE_DATABASE_PAGE_PARAM = "db";

/** The inline database the address opens alone, if it does. */
export function inlineDatabaseOpenedAlone() {
  return new URLSearchParams(window.location.search).get(INLINE_DATABASE_PAGE_PARAM)
    ?? inlineDatabaseAtAddress(window.location.pathname);
}

export function inlineDatabaseFullPageHref(pageId: string, blockId: string, viewId?: string) {
  rememberInlineDatabaseAddress(blockId, pageId);
  return pageHref(blockId, new URLSearchParams(viewId ? { v: viewId } : {}));
}

/** A name's formatting kept in a block's content, while it spells the name; none once the name changed. */
export function blockNameSpans(spans: unknown, title: string): TextSpan[] | undefined {
  return title && Array.isArray(spans) ? cellSpans(spans as TextSpan[], title) : undefined;
}

/**
 * A name written to a block's content as typed, the spaces at its ends too
 * (as Notion keeps them, seen 2026-11-13: "앱DB 끝 "): with its formatting
 * when given (an empty list, the name plain), else with what
 * it had while it keeps the same words — as a data source's name keeps its
 * formatting (data_source_title_spans.go).
 */
export function writtenBlockName(title: string, spans: readonly TextSpan[] | undefined, before: { title: string; spans: unknown }) {
  const typed = spans?.length ? cellValueFromSpans(spans) : { value: title, spans: undefined };
  const kept = spans ? typed.spans : typed.value === before.title ? blockNameSpans(before.spans, before.title) : undefined;
  return { title: typed.value, spans: kept?.length ? kept : undefined };
}

/** A linked database's own name, with its formatting, and icon, apart from its source's; none where it has none. */
export function linkedDatabasePresentation(block: GraphBlock) {
  const content = block.content;
  const title = typeof content.linkedTitle === "string" ? content.linkedTitle : "";
  return {
    title,
    titleSpans: blockNameSpans(content.linkedTitleSpans, title),
    icon: typeof content.linkedIcon === "string" ? content.linkedIcon : "",
    iconType: (typeof content.linkedIconType === "string" ? content.linkedIconType : "none") as GraphDataSourceIconType,
  };
}

/**
 * The names a database of several sources is listed by while it has no name
 * of its own (untitledDatabaseSourcesName), given its naming sources in their
 * order: one it shows from another database marked linked, which names it
 * "〈원본〉 보기" there ("C 및 X 보기", seen 2026-11-25), as the server marks it
 * (database_source_titles.go).
 */
export function databaseNamingSourceTitles(block: GraphBlock, sources: readonly Pick<GraphDataSource, "id" | "title">[]): DatabaseSourceTitle[] {
  const linked = new Set(databaseBlockMembers(block).linked.map(({ dataSourceId }) => dataSourceId));
  return sources.map(({ id, title }): DatabaseSourceTitle => linked.has(id) ? { title, linked: true } : title);
}

/**
 * An inline database's name, as Notion's (seen 2026-11-11): one of one source
 * goes by that source's; one of two or more — its own and those it links
 * (databaseNamingSourceIds, seen 2026-11-25) — by a name of its own, which
 * its block keeps with its formatting (pageTitleSpans), its sources keeping
 * theirs — its sources' names listed ("A 및 B", one it links "X 보기") stand
 * in while it has none. The server names it so (inline_database_own_name.go).
 * Given the sources naming it in order, the first naming one of one.
 */
export function inlineDatabaseName(block: GraphBlock, sources: readonly (Pick<GraphDataSource, "id" | "title"> & Partial<Pick<GraphDataSource, "titleSpans">>)[]): {
  title: string;
  titleSpans?: GraphDataSource["titleSpans"];
  /** What stands in while one of several has no name. */
  placeholder?: string;
  /** Whether the name is its own, apart from its sources'. */
  own: boolean;
} {
  if (sources.length < 2) return { title: sources[0]?.title ?? "", titleSpans: sources[0]?.titleSpans, own: false };
  const kept = Array.isArray(block.content.pageTitleSpans) ? block.content.pageTitleSpans as TextSpan[] : [];
  const title = kept.map(({ text }) => text).join("");
  return {
    title,
    titleSpans: blockNameSpans(kept, title) as GraphDataSource["titleSpans"],
    placeholder: untitledDatabaseSourcesName(databaseNamingSourceTitles(block, sources)),
    own: true,
  };
}

/**
 * The name of a database of several written to its block: its words with
 * their formatting, as one plain piece when it has none, and nothing once
 * emptied (writtenBlockName).
 */
export function writtenInlineDatabaseName(title: string, spans: readonly TextSpan[] | undefined, block: GraphBlock): GraphDataSource["titleSpans"] {
  const before = Array.isArray(block.content.pageTitleSpans) ? block.content.pageTitleSpans as TextSpan[] : [];
  const named = writtenBlockName(title, spans, { title: before.map(({ text }) => text).join(""), spans: before });
  return (named.spans ?? (named.title ? [{ text: named.title }] : [])) as GraphDataSource["titleSpans"];
}

/**
 * The inline database a page's address opens alone, with the source naming
 * it, and whether it is a view block (databaseViewBlock).
 */
export function inlineDatabaseFullPage(
  graph: ContentGraph,
  blockId: string | null,
): { block: GraphBlock; titleSource: GraphDataSource; sources: GraphDataSource[]; viewBlock: boolean; titleLabel?: string } | null {
  if (!blockId) return null;
  const block = graph.blocks.find((candidate) => candidate.id === blockId && candidate.type === "inline_database");
  const naming = block ? databaseNamingSourceIds(block) : [];
  // One of several left with one by the trash goes by that one, its first
  // gone or not (databaseNamingSourceIds).
  const titleId = naming.length === 1 ? naming[0] : String(block?.content.dataSourceId ?? "");
  const titleSource = block ? graph.dataSources.find((candidate) => candidate.id === titleId) : undefined;
  const sources = naming.flatMap((id) => graph.dataSources.filter((candidate) => candidate.id === id));
  // Its title property's name, which names a row's new title in its updates.
  const titleLabel = titleSource ? graph.properties.find((property) => property.dataSourceId === titleSource.id && property.type === "title")?.name : undefined;
  return block && titleSource ? { block, titleSource, sources, viewBlock: databaseViewBlock(block, graph.views), titleLabel } : null;
}

/**
 * Whether a graph draws an inline database by itself: its block, the source
 * it is named by and a view of it (InlineDatabaseBlock). A page's copy kept
 * while the database was drawn without its view held the block alone, and
 * drew it "missing" until the page was read (2026-11-13).
 */
export function inlineDatabaseDrawable(graph: ContentGraph, blockId: string) {
  const block = graph.blocks.find(({ id }) => id === blockId);
  if (!block) return false;
  const sourceId = typeof block.content.dataSourceId === "string" ? block.content.dataSourceId : "";
  return (!sourceId || graph.dataSources.some(({ id }) => id === sourceId))
    && graph.views.some((view) => view.containerId ? view.containerId === blockId : view.dataSourceId === sourceId);
}

/**
 * The name an inline database opened alone goes by — its window, its path,
 * the labels of its bar's ☆, share and "⋯" — whether opened as a page, in a
 * side peek or in a row's page, as the server names it on the screen
 * (databaseNameFrom): a linked one by its own name, "〈원본〉 보기" while it
 * has none; a view block by its name, "제목 없음" while it has none; one of
 * several sources by the name it keeps, its sources' names listed while it
 * has none (inlineDatabaseName); any other by its source's, "" while that has
 * none, which its reader calls "새 데이터베이스". With the formatting that
 * spells it, when it has any.
 */
export function inlineDatabaseShownName(
  opened: { block: GraphBlock; titleSource: GraphDataSource; sources?: readonly GraphDataSource[]; viewBlock?: boolean },
): { title: string; titleSpans?: GraphDataSource["titleSpans"] } {
  const { block, titleSource: source } = opened;
  if (block.content.linkedDatabaseSource === true) {
    const own = linkedDatabasePresentation(block);
    return { title: linkedDatabaseName(own.title, source.title), titleSpans: own.title ? own.titleSpans as GraphDataSource["titleSpans"] : undefined };
  }
  if (opened.viewBlock === true) {
    return source.title.trim() ? { title: source.title.trim(), titleSpans: source.titleSpans } : { title: untitledPageDisplayTitle({ kind: "database", viewBlock: true }) };
  }
  const sources = opened.sources ?? [];
  const name = inlineDatabaseName(block, sources.length > 1 ? sources : [source]);
  return name.title.trim() ? { title: name.title.trim(), titleSpans: name.titleSpans } : { title: name.placeholder ?? "" };
}

/** What a page shows of one of its inline databases opened alone: its name, icon and cover, and how they change. */
export type InlineDatabasePage = {
  /** The database's block on the page. */
  blockId: string;
  /** The source it is named by, whose own updates it lists (none for a full page's head). */
  sourceId?: string;
  /** Its title property's name. */
  titleLabel?: string;
  title: string;
  /** The name's formatting, when it has any. */
  titleSpans?: GraphDataSource["titleSpans"];
  /** A linked database's: its large title shows only once it has a name of its own. */
  linked?: boolean;
  /**
   * A dashboard's or a link's still waiting for its source — a view block
   * with no database of its own (databaseViewBlock): its page shows no name,
   * only its icon over its views, whose own title row names it.
   */
  viewBlock?: boolean;
  /** Its own lock is on (its block's "locked"), holding its name, icon, cover and description's words (databaseHeadLock.ts). */
  locked?: boolean;
  /** What the window is called. */
  windowTitle: string;
  /** What its title shows while it has none: an untitled database with several sources is named by them. */
  placeholder?: string;
  icon: { icon: string; iconType: GraphDataSourceIconType; workspaceId: string };
  cover: string;
  coverPosition: number;
  href: string;
  /** The name typed, with its formatting when the field gives it. */
  onTitleChange: (title: string, spans?: GraphDataSource["titleSpans"]) => void;
  onIconChange?: (icon: DataSourceIcon) => void;
  onCoverChange?: (change: Pick<GraphDataSource, "cover" | "coverPosition">) => void;
  /** Its block alone is in the trash: the banner over it, the page reading as it was (TrashedDatabaseBanner). */
  trashBanner?: ReactNode;
};

export function inlineDatabasePageView(
  opened: { block: GraphBlock; titleSource: GraphDataSource; sources?: readonly GraphDataSource[]; viewBlock?: boolean; titleLabel?: string } | null,
  page: { id: string; workspaceId: string },
  adapter: Pick<NativeDatabaseViewProductAdapter, "changeTitle" | "changeIcon" | "changeCover" | "setLinkedPresentation" | "setPageNameSpans">,
): InlineDatabasePage | undefined {
  if (!opened) return undefined;
  const source = opened.titleSource;
  // A linked database opened alone is named and shown by its own name and
  // icon, its source's staying as they are (Notion's, seen 2026-10-15).
  if (opened.block.content.linkedDatabaseSource === true) {
    const block = opened.block, own = linkedDatabasePresentation(block);
    return {
      blockId: block.id,
      sourceId: source.id,
      titleLabel: opened.titleLabel,
      title: own.title,
      titleSpans: own.titleSpans as GraphDataSource["titleSpans"],
      linked: true,
      windowTitle: inlineDatabaseShownName(opened).title,
      icon: { icon: own.icon, iconType: own.iconType, workspaceId: page.workspaceId },
      cover: "",
      coverPosition: 50,
      href: inlineDatabaseFullPageHref(page.id, block.id),
      onTitleChange: (title, titleSpans) => adapter.setLinkedPresentation(block, { title, titleSpans }),
      onIconChange: (icon) => adapter.setLinkedPresentation(block, { icon }),
    };
  }
  // A view block — a dashboard, or a link still waiting for its source — is
  // named by its block's name, "제목 없음" while it has none, in its window
  // and path, and shows no name on its page (Notion's, seen 2026-11-09). One
  // of several sources goes by its own name, which its title renames, its
  // sources keeping theirs (inlineDatabaseName), and its own icon, theirs kept too.
  const sources = opened.sources ?? [];
  const viewBlock = opened.viewBlock === true;
  const block = opened.block;
  const name = viewBlock ? { title: source.title, titleSpans: source.titleSpans, own: false }
    : inlineDatabaseName(block, sources.length > 1 ? sources : [source]);
  const placeholder = viewBlock ? untitledPageDisplayTitle({ kind: "database", viewBlock }) : "placeholder" in name ? name.placeholder : undefined;
  return {
    blockId: block.id,
    sourceId: source.id,
    titleLabel: opened.titleLabel,
    title: name.title,
    titleSpans: name.titleSpans,
    ...(viewBlock ? { viewBlock } : block.content.locked === true ? { locked: true } : {}),
    windowTitle: inlineDatabaseShownName(opened).title,
    placeholder,
    icon: { ...(name.own ? databaseOwnIcon(block) : null) ?? { icon: source.icon ?? "", iconType: source.iconType ?? "none" }, workspaceId: page.workspaceId },
    cover: source.cover ?? "",
    coverPosition: source.coverPosition ?? 50,
    href: inlineDatabaseFullPageHref(page.id, block.id),
    onTitleChange: name.own
      ? (title, spans) => adapter.setPageNameSpans?.(block, writtenInlineDatabaseName(title, spans as TextSpan[] | undefined, block))
      : (title, spans) => adapter.changeTitle(source, title, spans),
    onIconChange: name.own ? (icon) => adapter.setLinkedPresentation(block, { icon }) : (icon) => adapter.changeIcon(source, icon),
    onCoverChange: (change) => adapter.changeCover(source, change),
  };
}

/**
 * The icon a database of several sources of its own keeps on its block, apart
 * from its sources', as Notion's (seen 2026-11-22: its sources' headings keep
 * theirs) — "none" once taken away — or null while it has never had one, its
 * first source's showing as before (database_own_icon.go).
 */
function databaseOwnIcon(block: GraphBlock): { icon: string; iconType: GraphDataSourceIconType } | null {
  const iconType = block.content.databaseIconType;
  if (typeof iconType !== "string" || !iconType) return null;
  const icon = typeof block.content.databaseIcon === "string" && iconType !== "none" ? block.content.databaseIcon : "";
  return { icon, iconType: (icon ? iconType : "none") as GraphDataSourceIconType };
}
