import { PageIconGlyph, type PageIconSource } from "@/components/page/PageIcon";
import { pagesByIdOf } from "@/lib/pageIndex";
"use client";

import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpRightIcon, CheckIcon, ChevronDown, DatabaseCylinderIcon, DotsHorizontal } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { CellMenuPortal } from "@/components/database/PropertyCellMenu";
import { useProductPages } from "@/components/product/productPagesContext";
import { databaseSourcePlace } from "@/lib/pagePath";
import { PlaceLine } from "../PlaceLine";
import { usePageNames } from "@/lib/pageNames";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import type { Page } from "@/lib/types";
import { EXISTING_COUNTED } from "@/components/database/linkableSources";
import type { AutomationTargets } from "./automationTargetDatabases";
import styles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";
import menuStyles from "./automationTargetMenu.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { AutomationPhoneSheets } from "./AutomationActionMenus";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

// As Notion's (seen 2026-11-11 with over a hundred sources): five, or a
// search's first twenty, then "⋯ N개 더 보기" naming the twenty more a press
// shows, or the fewer left.
const SHOWN_AT_FIRST = 5, SEARCH_SHOWN_AT_FIRST = 20, SHOWN_MORE = 20;
const SEARCH_DELAY = 150;

type Option = { id: string; title: string; pageId?: string; sourceTitles?: DatabaseSourceTitle[]; databaseTitle?: string; icon?: PageIconSource };

/**
 * Where an action adds or edits pages, as Notion's: a search of the
 * workspace's databases, each under the path of the page holding it, the
 * first five shown, twenty more a press (seen 2026-10-10, 2026-11-11), from
 * the hundred read at once (EXISTING_COUNTED) or all a search finds.
 */
export function AutomationTargetDatabasePicker({ number, value, warnEmpty = false, invalidTip, databases, targets, search, onChange , thisPage}: {
  number: number;
  /** Whether no choice yet is shown as a warning: on an action saved so. */
  warnEmpty?: boolean;
  /**
   * What the chip says while a data source chosen is gone. A red card's head
   * tips its lack itself, over this chip too (AutomationActionCard's problemTip).
   */
  invalidTip?: string;
  value: string;
  databases: readonly Page[];
  targets: AutomationTargets;
  search?: (query: string, signal: AbortSignal, upTo?: number) => Promise<RelationDatabaseOption[]>;
  onChange: (databaseId: string) => void;
  /**
   * A button's edit may aim at its own row: above the data sources, as Notion's (seen 2026-11-04), "이 페이지";
   * an automation's archive at the page that set it off, labelled "페이지 실행" (seen 2026-11-15).
   */
  thisPage?: { chosen: boolean; onPick(): void; label?: string };
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("databaseView");
  const pages = useProductPages()?.pages;
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<RelationDatabaseOption[]>([]);
  const [searched, setSearched] = useState(false);
  const [shownCount, setShownCount] = useState(SHOWN_AT_FIRST);
  useEffect(() => {
    if (!open || !search) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      search(query.trim(), controller.signal, EXISTING_COUNTED).then(
        result => { if (!controller.signal.aborted) { setFound(result); setSearched(true); } },
        () => { if (!controller.signal.aborted) { setFound([]); setSearched(true); } },
      );
    }, SEARCH_DELAY);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [open, query, search]);
  const pagesById = pagesByIdOf(pages ?? []);
  const thisPageLabel = thisPage?.label ?? t("openTargetThisPage");
  const options = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const seen = new Set<string>();
    const local: Option[] = databases.filter(database => !needle || database.title.toLowerCase().includes(needle))
      // A database here shows its path too, as Notion's list does (seen 2026-11-03): the page holding it.
      .map(database => { const twin = found.find(item => item.id === database.id);
        return { id: database.id, title: database.title, pageId: twin?.pageId ?? (pagesById[database.id] ? database.id : database.parentId ?? undefined), sourceTitles: twin?.sourceTitles, databaseTitle: twin?.databaseTitle, icon: { icon: database.icon ?? "", iconType: database.iconType } }; });
    return [...local, ...found].filter(option => !seen.has(option.id) && Boolean(seen.add(option.id)));
  }, [databases, found, pagesById, query]);
  const local = databases.find(database => database.id === value);
  const target = targets[value];
  const unavailable = !thisPage?.chosen && Boolean(value) && !local && target === null;
  const label = thisPage?.chosen ? thisPageLabel : !value ? t("chooseDataSource")
    : local ? pageNames.title({ title: local.title, kind: "database" })
      : target ? pageNames.title({ title: target.title, kind: "database" })
        : unavailable ? t("automationTargetUnavailable") : "…";
  const invalid = (warnEmpty && !value) || unavailable;
  const close = () => { setOpen(false); setQuery(""); setShownCount(SHOWN_AT_FIRST); setSearched(false); };
  const shown = options.slice(0, shownCount);
  const phone = useContext(AutomationPhoneSheets);
  const typed = (words: string) => { setQuery(words); setShownCount(words.trim() ? SEARCH_SHOWN_AT_FIRST : SHOWN_AT_FIRST); };
  // A source's mark: 16px on the desktop's list, 24 on a phone's, as Notion's (seen 2026-11-16 on an A90 at 411).
  const markOf = (option: Option, onPhone = false) => "icon" in option && option.icon
    ? <PageIconGlyph page={option.icon} size={onPhone ? 24 : 16} box={onPhone ? 24 : undefined} fallback="database" />
    : <DatabaseCylinderIcon size={onPhone ? "large" : "small"} aria-hidden="true" />;
  return <>
    <button ref={button} type="button" className={`${styles.targetButton} ${chipStyles.chip}`} aria-label={t("targetDatabaseForAction", { number })}
      aria-haspopup="dialog" aria-expanded={open} aria-invalid={invalid} data-icon-tip={unavailable ? invalidTip : undefined}
      data-unset={!value && !thisPage?.chosen && !invalid ? true : undefined}
      onClick={() => open ? close() : setOpen(true)}>
      {invalid ? <WarningFillIcon size="tiny" aria-hidden="true" /> : null}{thisPage?.chosen ? <ArrowUpRightIcon size="small" aria-hidden="true" /> : null}<span>{label}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {/* On a phone a list rising from the screen's foot, its search first with no keyboard up, a row per source with
        its 24px mark and the page holding it in the third grey under its name, then "⋯ N개 더 보기" with its 22px ⋯
        (Notion's, seen 2026-11-15 and 2026-11-16 on an A90 at 411). */}
    {open && phone ? <PhoneSheetMenu label={t("targetDatabaseForAction", { number })} marks="pages" onClose={close}>
      <PhoneSheetSearch autoFocus={fieldFocusOnOpen()} aria-label={t("searchDataSources")} placeholder={t("searchDataSources")}
        value={query} onValueChange={typed} />
      {thisPage && (!query.trim() || thisPageLabel.toLowerCase().includes(query.trim().toLowerCase())) ? <PhoneSheetRow
        icon={<ArrowUpRightIcon size="wide" aria-hidden="true" />} label={thisPageLabel} more={false} value={thisPage.chosen ? <CheckIcon size="small" /> : undefined}
        onPress={() => { close(); thisPage.onPick(); }} data={{ "data-automation-target-this-page": "" }} /> : null}
      {shown.map(option => {
        const path = databaseSourcePlace(option.pageId, pagesById, option.sourceTitles, option.databaseTitle);
        return <PhoneSheetRow key={option.id} icon={markOf(option, true)} label={pageNames.title({ title: option.title, kind: "database" })} more={false}
          description={path?.length ? <span className={menuStyles.phonePath}><PlaceLine place={path} /></span> : undefined}
          value={!thisPage?.chosen && option.id === value ? <CheckIcon size="small" /> : undefined}
          onPress={() => { close(); onChange(option.id); }} data={{ "data-automation-target-option": option.id }} />;
      })}
      {!options.length && (searched || !search) ? <p className={sheetStyles.groupLabel}>{t("noDataSourcesFound")}</p> : null}
      {options.length > shownCount ? <PhoneSheetRow muted icon={<DotsHorizontal size="wide" />} more={false}
        label={t("showMoreDataSources", { count: Math.min(SHOWN_MORE, options.length - shownCount) })}
        onPress={() => setShownCount(count => count + SHOWN_MORE)} /> : null}
    </PhoneSheetMenu> : null}
    {/* 280 wide, centred on its chip and right on it, under it or over it where the room below is short (Notion's, seen 2026-11-15 at 1440). */}
    {open && !phone && <CellMenuPortal triggerRef={button} ariaLabel={t("targetDatabaseForAction", { number })}
      className={menuStyles.targetMenu} width={280} align="center" gap={0} onClose={close}>
      <input autoFocus={fieldFocusOnOpen()} className={menuStyles.targetSearch} aria-label={t("searchDataSources")} placeholder={t("searchDataSources")}
        value={query} onChange={event => typed(event.target.value)} />
      {thisPage && (!query.trim() || thisPageLabel.toLowerCase().includes(query.trim().toLowerCase())) ? <>
        <div className={menuStyles.targetSection}>{t("openTargetFromAutomation")}</div>
        <button type="button" role="option" aria-selected={thisPage.chosen} className={menuStyles.targetOption}
          onClick={() => { thisPage.onPick(); close(); }} data-automation-target-this-page>
          <span className={menuStyles.targetMark}><ArrowUpRightIcon size="small" aria-hidden="true" /></span><span><span className={menuStyles.targetTitle}>{thisPageLabel}</span></span>
          {thisPage.chosen ? <CheckIcon size="small" aria-hidden="true" /> : null}
        </button>
      </> : null}
      <div className={menuStyles.targetSection}>{t("dataSourcesSection")}</div>
      <div role="listbox" aria-label={t("dataSourcesSection")}>
        {shown.map(option => {
          const path = databaseSourcePlace(option.pageId, pagesById, option.sourceTitles, option.databaseTitle);
          return <button key={option.id} type="button" role="option" aria-selected={!thisPage?.chosen && option.id === value}
            className={menuStyles.targetOption} onClick={() => { onChange(option.id); close(); }}>
            {/* Each by its own icon, as Notion's database pickers (seen 2026-11-10). */}
            <span className={menuStyles.targetMark}>{markOf(option)}</span>
            <span>
              <span className={menuStyles.targetTitle}>{pageNames.title({ title: option.title, kind: "database" })}</span>
              {path?.length ? <span className={menuStyles.targetPath}><PlaceLine place={path} /></span> : null}
            </span>
          </button>;
        })}
        {!options.length && (searched || !search) ? <p className={menuStyles.targetEmpty}>{t("noDataSourcesFound")}</p> : null}
      </div>
      {options.length > shownCount ? <button type="button" className={menuStyles.targetMore}
        onClick={() => setShownCount(count => count + SHOWN_MORE)}>··· {t("showMoreDataSources", { count: Math.min(SHOWN_MORE, options.length - shownCount) })}</button> : null}
    </CellMenuPortal>}
  </>;
}
