"use client";
import { useEffect, useMemo, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ArrowUpRightIcon, LinkIcon } from "@/icons/leafen";
import { CellMenuPortal } from "@/components/database/PropertyCellMenu";
import { ViewTypeIcon } from "@/components/database/databaseViewShared";
import type { RowPageMentionCandidate } from "@/components/editor/blockPickerModel";
import { useLibraryPages, usePageVisitsNow } from "@/components/library/useLibraryData";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { usePageNames } from "@/lib/pageNames";
import type { Place } from "@/lib/pagePath";
import { usePagePathsAbove, usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { AutomationAction } from "@/lib/types";
import type { Page, Session } from "@/types";
import { PlaceLine } from "../PlaceLine";
import {
  OPEN_TARGETS_AT_FIRST, OPEN_TARGETS_MORE, openTargetPlace, openTargetsShown, orderOpenTargetChoices, searchedRowChoice, visitedRowChoice, visitTimes,
  type OpenTargetChoice,
} from "./automationOpenTargetChoices";
import menuStyles from "./automationTargetMenu.module.css";

/** As many database rows as a search asks for: what twenty shown and twenty more may hold. */
const ROWS_SEARCHED = OPEN_TARGETS_MORE * 2;

/**
 * The open list of "페이지, 양식 또는 URL 열기" (AutomationOpenTargetPicker),
 * drawn while it is open: a field to search or paste a link into, a link
 * pasted offered first, "이 페이지" where the action has one, the pages under
 * "페이지" and, for the words typed, the forms under "폼". The pages are the
 * reader's, the databases their bodies hold and, as Notion's (seen
 * 2026-11-24 in two tabs), database rows — those a search finds, those
 * visited lately with nothing typed — in one order (orderOpenTargetChoices),
 * five shown with nothing typed and twenty for a search, then twenty more a
 * press of "N개 더 보기".
 */
export function AutomationOpenTargetList({
  session, workspaceId, triggerRef, number, label, listed, pagesById, query, onQuery, url, thisPage, forms, action, id, onChoose, onClose,
}: {
  session: Session;
  workspaceId: string;
  triggerRef: RefObject<HTMLButtonElement | null>;
  number: number;
  label: string;
  /** The reader's pages and the databases they hold, out of the trash, in their list's order. */
  listed: readonly Page[];
  pagesById: Readonly<Record<string, Page>>;
  query: string;
  onQuery(query: string): void;
  url: string;
  /** "이 페이지", where the action's database has one and the search finds it. */
  thisPage: string;
  forms: ReadonlyArray<{ viewId: string; name: string; databaseId: string; where: Place }>;
  action: AutomationAction;
  /** The ID the action chosen keeps. */
  id: string;
  /** A choice made, with the row's page where a row is chosen. */
  onChoose(next: AutomationAction, rowPage?: Page): void;
  onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const pageNames = usePageNames();
  const needle = query.trim().toLowerCase();
  const [more, setMore] = useState({ query: needle, presses: 0 });
  const presses = more.query === needle ? more.presses : 0;
  const visits = usePageVisitsNow(session, workspaceId);
  const visitedAt = useMemo(() => visitTimes(visits), [visits]);
  // The rows a search finds, as the "@" menu finds them, read a moment after the typing stops.
  const [searched, setSearched] = useState<{ query: string; rows: RowPageMentionCandidate[] }>({ query: "", rows: [] });
  useEffect(() => {
    if (!needle) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api.searchRowPageMentions(session, workspaceId, query.trim(), { signal: controller.signal, limit: ROWS_SEARCHED })
        .then((result) => { if (!controller.signal.aborted) setSearched({ query: needle, rows: result.items }); }, () => undefined);
    }, 150);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [needle, query, session, workspaceId]);
  const listedIds = useMemo(() => new Set(listed.map((page) => page.id)), [listed]);
  // With nothing typed, the rows among the pages last visited that could be shown, read for their names.
  const visitedOthers = useMemo(() => needle ? [] : [...new Set(visits.map((visit) => visit.pageId))]
    .slice(0, OPEN_TARGETS_AT_FIRST.empty + presses * OPEN_TARGETS_MORE)
    .filter((id) => !pagesById[id] && !listedIds.has(id)), [listedIds, needle, pagesById, presses, visits]);
  const { pages: visitedRead } = useLibraryPages(session, workspaceId, visitedOthers);
  const choices = useMemo(() => {
    const title = (page: Page) => pageNames.title(page);
    const rows = needle
      ? (searched.query === needle ? searched.rows : []).filter((row) => !pagesById[row.rowId] && !listedIds.has(row.rowId))
        .map((row) => searchedRowChoice(row, workspaceId, title))
      : visitedOthers.map((id) => visitedRead.get(id)).map((detail) => detail ? visitedRowChoice(detail, workspaceId, title) : null);
    const all: OpenTargetChoice[] = [...listed.map((page) => ({ page, title: title(page) })), ...rows.filter((row): row is OpenTargetChoice => Boolean(row))];
    return orderOpenTargetChoices(all, needle, visitedAt);
  }, [listed, listedIds, needle, pageNames, pagesById, searched, visitedAt, visitedOthers, visitedRead, workspaceId]);
  const { shown: count, more: moreCount } = openTargetsShown(choices.length, Boolean(needle), presses);
  const shown = choices.slice(0, count);
  usePathsAboveShown(shown.filter((choice) => !choice.row).map((choice) => choice.page), pagesById);
  // A row's path names its database's source, as the server reads it (databasePagePlace).
  usePagePathsAbove(shown.filter((choice) => choice.row).map((choice) => ({ id: choice.page.id, workspaceId })));
  return <CellMenuPortal triggerRef={triggerRef} ariaLabel={label} className={menuStyles.targetMenu} width={280} rise onClose={onClose}>
    {/* Its search says a link may be pasted on any button, a database's too, as Notion's (seen 2026-11-24 in two tabs). */}
    <input autoFocus={fieldFocusOnOpen()} className={menuStyles.targetSearch} aria-label={t("searchOrPasteLink")}
      placeholder={t("searchOrPasteLink")} value={query} onChange={(event) => onQuery(event.target.value)} />
    <div role="listbox" aria-label={t("openTargetForAction", { number })}>
      {/* A typed address under "링크", as Notion's (seen 2026-11-02). */}
      {url ? <div className={menuStyles.targetSection}>{t("openTargetLinks")}</div> : null}
      {url ? <button type="button" role="option" aria-selected={action.type === "open_url" && action.url === url} className={menuStyles.targetOption}
        data-open-target-url onClick={() => onChoose({ id, type: "open_url", url })}>
        <span className={menuStyles.targetMark}><LinkIcon size="small" aria-hidden="true" /></span><span><span className={menuStyles.targetTitle}>{url}</span></span>
      </button> : null}
      {/* What the automation itself has, first, as Notion's "이 자동화에서 검색 › ↗ 이 페이지" (seen 2026-11-02). */}
      {thisPage ? <div className={menuStyles.targetSection}>{t("openTargetFromAutomation")}</div> : null}
      {thisPage ? <button type="button" role="option" aria-selected={action.type === "open_page" && action.target === "trigger_page"}
        className={menuStyles.targetOption} data-open-target-this-page onClick={() => onChoose({ id, type: "open_page", pageId: "", target: "trigger_page" })}>
        <span className={menuStyles.targetMark}><ArrowUpRightIcon size="small" aria-hidden="true" /></span><span><span className={menuStyles.targetTitle}>{thisPage}</span></span>
      </button> : null}
      {shown.length ? <div className={menuStyles.targetSection}>{t("openTargetPages")}</div> : null}
      {shown.map((choice) => {
        const path = openTargetPlace(choice, pagesById);
        return <button key={choice.page.id} type="button" role="option" aria-selected={action.type === "open_page" && action.pageId === choice.page.id}
          className={menuStyles.targetOption} data-open-target-page={choice.page.id} {...(choice.row ? { "data-open-target-row": "" } : {})}
          onClick={() => onChoose({ id, type: "open_page", pageId: choice.page.id }, choice.row ? choice.page : undefined)}>
          <span className={menuStyles.targetMark}><PageIconGlyph page={choice.page} size={16} /></span>
          <span>
            <span className={menuStyles.targetTitle}><PageTitleText page={choice.page} /></span>
            {path.length ? <span className={menuStyles.targetPath}><PlaceLine place={path} /></span> : null}
          </span>
        </button>;
      })}
      {moreCount > 0 ? <button type="button" className={menuStyles.targetMore} data-open-target-more
        onClick={() => setMore({ query: needle, presses: presses + 1 })}>··· {t("showMoreDataSources", { count: moreCount })}</button> : null}
      {forms.length ? <div className={menuStyles.targetSection}>{t("openTargetForms")}</div> : null}
      {forms.map((form) => <button key={form.viewId} type="button" role="option"
        aria-selected={action.type === "open_form" && action.viewId === form.viewId} className={menuStyles.targetOption} data-open-target-form={form.viewId}
        onClick={() => onChoose({ id, type: "open_form", databaseId: form.databaseId, viewId: form.viewId })}>
        <span className={menuStyles.targetMark}><ViewTypeIcon type="form" size="small" /></span>
        <span>
          <span className={menuStyles.targetTitle}>{form.name}</span>
          {form.where.length ? <span className={menuStyles.targetPath}><PlaceLine place={form.where} /></span> : null}
        </span>
      </button>)}
      {!url && !thisPage && !shown.length && !forms.length ? <p className={menuStyles.targetEmpty}>{t("openTargetNone")}</p> : null}
    </div>
  </CellMenuPortal>;
}
