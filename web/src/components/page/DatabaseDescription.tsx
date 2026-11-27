import { type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ContentGraph, GraphBlock, GraphDataSource } from "@/contentGraph";
import { InfoFillIcon } from "@/icons/leafen";
import type { TextSpan } from "@/lib/types";
import headerStyles from "./PageHeader.module.css";
import styles from "./DatabaseDescription.module.css";
import { RichTextCellEditor } from "../database/RichTextCellEditor";
import { inlineDatabaseViews } from "../editor/InlineDatabaseBlock";
import { untitledDataSourceTitle } from "../editor/databaseTitles";
import { chosenDatabaseView, useDatabaseViewChoices } from "@/lib/databaseViewChoice";
import { databaseNamingSourceIds } from "@/lib/nativeDataSourceManagement";

export type DatabaseDescriptionChange = Pick<GraphDataSource, "description" | "descriptionSpans" | "descriptionHidden">;

/** A page's own database and how its description changes. */
export type DatabaseDescriptionSource = {
  source: GraphDataSource;
  generatedPageTitle?: string;
  /** A full-page linked database's, whose page shows the database's own name and icon. */
  linked?: boolean;
  onChange?: (change: DatabaseDescriptionChange) => void;
  /**
   * A full-page database named by its one source: its page's title is that
   * name, rich text as Notion's, written to the source (the server titles the
   * page after it) — read so by whoever opens it, a web link's visitor too.
   */
  named?: boolean;
  onNameChange?: (title: string, spans: TextSpan[]) => void | Promise<boolean>;
  /**
   * A full-page database its page's title names — of several sources of its
   * own, or linked: that name's formatting, kept on its block, and how the
   * name is written there (database_page_name_spans.go).
   */
  pageName?: { block: GraphBlock; spans?: TextSpan[]; onSpansChange?: (spans: TextSpan[]) => void | Promise<boolean> };
  /**
   * A full-page database whose own sources are all in the trash heads itself
   * with the open view's source alone (useTrashedSourcesHead).
   */
  trashedSources?: {
    block: GraphBlock;
    views: readonly { id: string; dataSourceId: string }[];
    sources: GraphDataSource[];
    rename?: (source: GraphDataSource, title: string, spans: TextSpan[]) => void;
  };
};

/** How a database page's source is written: its description, and its name. */
export type DatabasePageSourceWrites = {
  changeDescription(source: GraphDataSource, change: DatabaseDescriptionChange): void;
  changeTitle(source: GraphDataSource, title: string, spans?: GraphDataSource["titleSpans"]): void | Promise<boolean>;
  setPageNameSpans?(block: GraphBlock, spans: GraphDataSource["titleSpans"]): void | Promise<boolean>;
};

/** What a database page shows of its database's description: its button over the title and its field under it. */
export type DatabaseDescriptionParts = { control: ReactNode; field: ReactNode };

/**
 * The database a page shows as its own, whose description sits under its
 * title: a full-page database's, or an inline one opened alone.
 */
export function databasePageSource(
  graph: ContentGraph,
  pageKind: string | undefined,
  opened: { titleSource: GraphDataSource } | null,
  writes?: DatabasePageSourceWrites,
): DatabaseDescriptionSource | undefined {
  let generatedPageTitle: string | undefined;
  let linked = false, named = false;
  let source = opened?.titleSource;
  let block: GraphBlock | undefined;
  if (!source && pageKind === "database") {
    block = graph.blocks.find((candidate) => !candidate.parentId && candidate.type === "inline_database");
    generatedPageTitle = typeof block?.content.generatedPageTitle === "string" ? block.content.generatedPageTitle : undefined;
    linked = block?.content.linkedDatabaseSource === true;
    // One source of its own alone names it (full_page_database_name.go) —
    // one of several left with one by the trash, the rest sent there with
    // their views, that one, its title typed renaming it, as Notion's (seen
    // 2026-11-19); with more, those it links from other databases among them
    // (databaseNamingSourceIds), its name is its page's own, its source
    // keeping its name (seen 2026-11-25: "F3" typed left the source "F2").
    const left = block && !linked ? databaseNamingSourceIds(block) : [];
    const sourceId = left.length === 1 ? left[0] : String(block?.content.dataSourceId ?? "");
    source = graph.dataSources.find((candidate) => candidate.id === sourceId);
    const more = block?.content.additionalDataSourceIds;
    named = !linked && (left.length === 1 || left.length === 0 && !(Array.isArray(more) && more.some(Boolean)));
  }
  if (!source) return undefined;
  const own = source, top = block;
  const ownIds = top && !linked ? [String(top.content.dataSourceId ?? ""), ...(Array.isArray(top.content.additionalDataSourceIds)
    ? top.content.additionalDataSourceIds.map(String) : [])].filter(Boolean) : [];
  const trashedIds = Array.isArray(top?.content.trashedDataSourceIds) ? top.content.trashedDataSourceIds.map(String) : [];
  const trashedSources = top && ownIds.length > 1 && ownIds.every((id) => trashedIds.includes(id)) ? {
    block: top, views: inlineDatabaseViews(graph, top).views,
    sources: ownIds.flatMap((id) => graph.dataSources.filter((candidate) => candidate.id === id)),
    rename: writes ? (named: GraphDataSource, title: string, spans: TextSpan[]) => writes.changeTitle(named, title, spans as GraphDataSource["titleSpans"]) : undefined,
  } : undefined;
  const pageName = top && !named ? { block: top, spans: Array.isArray(top.content.pageTitleSpans) ? top.content.pageTitleSpans as TextSpan[] : undefined,
    onSpansChange: writes?.setPageNameSpans ? (spans: TextSpan[]) => writes.setPageNameSpans?.(top, spans as GraphDataSource["titleSpans"]) : undefined } : undefined;
  return { source: own, generatedPageTitle, ...(linked ? { linked } : {}), ...(named ? { named } : {}),
    onChange: writes ? (change) => writes.changeDescription(own, change) : undefined,
    onNameChange: writes && named ? (title, spans) => writes.changeTitle(own, title, spans as GraphDataSource["titleSpans"]) : undefined,
    ...(pageName ? { pageName } : {}), ...(trashedSources ? { trashedSources } : {}) };
}

/**
 * The head of a full-page database whose own sources are all in the trash:
 * the open view's source alone, its name written to that source, as Notion's
 * (seen 2026-11-12: "J one", then "J second" once its tab opens, the bar
 * keeping the database's name; an untitled one "새 데이터 소스" while the
 * database holds more, its only one "새 데이터베이스"). Undefined for any
 * other database.
 */
export function useTrashedSourcesHead(description: DatabaseDescriptionSource | undefined, userId: string | undefined) {
  useDatabaseViewChoices();
  const trashed = description?.trashedSources;
  if (!trashed) return undefined;
  const view = chosenDatabaseView(trashed.views, userId, trashed.block.id, trashed.block.content.viewId);
  const source = trashed.sources.find((candidate) => candidate.id === view?.dataSourceId) ?? trashed.sources[0];
  if (!source) return undefined;
  const rename = trashed.rename;
  return {
    title: source.title, spans: source.titleSpans as TextSpan[] | undefined, placeholder: untitledDataSourceTitle(trashed.sources.length),
    onChange: rename ? (title: string, spans: TextSpan[] = []) => rename(source, title, spans) : undefined,
  };
}

/**
 * A wiki's own database, whose description its head offers as a database
 * page's does — "설명 추가" in place of "댓글 추가", as Notion's wiki (seen
 * 2026-10-31).
 */
export function wikiPageSource(
  graph: ContentGraph,
  page: { id: string; isWiki?: boolean; wikiRootId?: string | null; wikiDataSourceId?: string | null },
  onChange?: (source: GraphDataSource, change: DatabaseDescriptionChange) => void,
): DatabaseDescriptionSource | undefined {
  if (!page.isWiki || page.wikiRootId !== page.id || !page.wikiDataSourceId) return undefined;
  const source = graph.dataSources.find((candidate) => candidate.id === page.wikiDataSourceId);
  if (!source) return undefined;
  return { source, onChange: onChange ? (change) => onChange(source, change) : undefined };
}

// Typing saves after this pause, and on leaving the field.
const SAVE_DELAY_MS = 500;

/**
 * A database's description, as Notion's (seen 2026-10-01): "설명 추가" over
 * the title opens an empty field under it ("설명을 추가하세요", 14px) and
 * becomes "설명 숨기기"; hiding keeps the text, and "설명 표시" shows it
 * again. A reader who can't change the database sees a shown description.
 * Its lock holds its words, not whether they show: locked, it offers "설명
 * 숨기기" or "설명 표시" alone, no "설명 추가" (Notion's, seen 2026-11-18 in
 * Chrome, two tabs, full page and inline opened alone; databaseHeadLock.ts).
 */
export function useDatabaseDescription({ source, readOnly, locked = false, onChange }: {
  source: GraphDataSource | null;
  readOnly: boolean;
  /** Its database's lock holds it for this reader. */
  locked?: boolean;
  onChange?: (change: DatabaseDescriptionChange) => void;
}): DatabaseDescriptionParts {
  const { t } = useTranslation("pageHeader");
  const [adding, setAdding] = useState(false);
  const sourceId = source?.id ?? "";
  const text = source?.description ?? "";
  const hidden = source?.descriptionHidden === true;
  const shown = Boolean(source) && !hidden && (text !== "" || adding && !locked);
  const toggles = !readOnly && Boolean(onChange) && (!locked || text !== ""), editable = toggles && !locked;

  useEffect(() => setAdding(false), [sourceId]);
  const saveField = useRef<() => void>(() => {});

  function toggle() {
    if (shown) {
      saveField.current();
      setAdding(false);
      onChange?.({ descriptionHidden: true });
      return;
    }
    if (hidden) onChange?.({ descriptionHidden: false });
    if (!text) setAdding(true);
  }

  const label = shown ? t("hideDescription") : text ? t("showDescription") : t("addDescription");
  const control = toggles && source ? (
    <button type="button" className={headerStyles.ctrlBtn} data-page-header-control
      data-database-description-control={shown ? "hide" : text ? "show" : "add"} onClick={toggle}>
      {/* A filled circle with an "i", as Notion's "설명 추가" over a database's name (seen 2026-11-07). */}
      <InfoFillIcon size="tiny" aria-hidden="true" />
      <span>{label}</span>
    </button>
  ) : null;
  const field = shown ? <DescriptionField key={sourceId} text={text} spans={source?.descriptionSpans as TextSpan[] | undefined} editable={editable} autoFocus={adding}
    label={t("databaseDescription")} placeholder={t("descriptionPlaceholder")}
    onChange={onChange} saveField={saveField} /> : null;
  return { control, field };
}

/**
 * The description's field: its text with formatting, as Notion's takes
 * (seen 2026-10-30: ⌘B bolds it; the selection bar, links and @ mentions
 * are the rich text cell's). Typing saves after a pause and on leaving it;
 * plain text takes the formatting away.
 */
function DescriptionField(props: { text: string; spans: readonly TextSpan[] | undefined; editable: boolean; autoFocus: boolean;
  label: string; placeholder: string; onChange?: (change: DatabaseDescriptionChange) => void; saveField: { current: () => void } }) {
  const latest = useRef(props); latest.current = props;
  const pending = useRef<{ value: string; spans?: TextSpan[] } | null>(null), timer = useRef<number | null>(null);
  function save() {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
    const next = pending.current;
    pending.current = null;
    if (!next || !latest.current.editable) return;
    latest.current.onChange?.({ description: next.value,
      descriptionSpans: next.spans?.length ? next.spans as DatabaseDescriptionChange["descriptionSpans"] : undefined });
  }
  props.saveField.current = save;
  useEffect(() => () => save(), []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!props.editable) {
    return <RichTextCellEditor spans={props.spans} value={props.text} multiline readOnly ariaLabel={props.label} className={styles.field}
      placeholder={props.placeholder} dataAttributes={{ "data-database-description": "" }} commitOn="blur" onChange={() => {}} />;
  }
  return (
    <RichTextCellEditor spans={props.spans} value={props.text} multiline textCell pastedMarks autoFocus={props.autoFocus} ariaLabel={props.label}
      className={styles.field} placeholder={props.placeholder} dataAttributes={{ "data-database-description": "" }} commitOn="input"
      onChange={(change) => {
        pending.current = change;
        if (timer.current !== null) window.clearTimeout(timer.current);
        timer.current = window.setTimeout(save, SAVE_DELAY_MS);
      }}
      onFinish={save} />
  );
}
