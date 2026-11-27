import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, XMarkBoldSmallIcon } from "@/icons/leafen";
import { useAutomationRelationPages } from "@/components/automation/automationRelationRows";
import { SearchClearButton } from "@/components/PageActionSearch";
import { pageHref } from "@/lib/navigation";
import { pageNamesNow } from "@/lib/pageNames";
import type { DbProperty, Page } from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";
import { PhoneChoiceList } from "./DatabasePhoneFilterValues";
import { pageIconSource } from "./DatabaseFilterValueInput";
import { FILTER_PAGE_CHIP_ICON } from "./filterThisPage";
import { relationPagesFound, useRelationRowsInOrder } from "./relationPageOrder";
import { triggerChosen } from "./automationTriggerConditions";
import { type PickerWindowLine, pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import styles from "./AutomationRelationCondition.module.css";

/**
 * A relation condition's pages: its database's, as the automation's view
 * reads them (AutomationRelationRowsContext), in the relation picker's
 * order (useRelationRowsInOrder) — Notion's lists them so (seen 2026-11-17
 * in Chrome at 1440 and on a Galaxy A90 at 411: a page just picked moved up
 * the next time). None where the property is no relation.
 */
export function useTriggerRelationPages(property: DbProperty | undefined): readonly Page[] {
  const relation = property?.type === "relation";
  const rows = useAutomationRelationPages(property?.config?.relationDatabaseId ?? property?.databaseId, relation);
  return useRelationRowsInOrder(rows, rows?.[0]?.workspaceId, relation);
}

/** A page's name where it is named: its title, or "제목 없음". */
export function relationPageName(page: Page) {
  return pageNamesNow.title(page);
}

/**
 * A page a relation condition names on its card, as Notion's (seen
 * 2026-11-17 in Chrome at 1440 and on a Galaxy A90 at 411): its 18px icon
 * in a 20px box, its name 500 4px after it.
 */
export function RelationConditionPage({ page, name }: { page: Page | undefined; name: string }) {
  return (
    <span className={styles.page} data-relation-condition-page={page?.id}>
      <span className={styles.pageIcon} aria-hidden="true">
        <PageIconGlyph page={page ? pageIconSource(page) : { icon: "", iconType: "none", kind: "page" }} size={18} />
      </span>
      <span className={styles.pageName}>{name}</span>
    </span>
  );
}

/**
 * A relation condition's pages on a phone's screen: Notion's relation
 * filter's list (PhoneChoiceList — seen 2026-11-17 on a Galaxy A90 at 411:
 * a field holding the chosen pages' chips, "하나 이상의 페이지 검색" while
 * none, the database's pages under it, the chosen ones "✓"), its chips and
 * row icons as its condition screen draws them (`condition`), the field
 * nested a level in (28px and a line over the screen's own). Its "ⓧ" takes
 * the chosen pages and the words back and leaves the search taking the keys,
 * the screen open (Notion's, seen 2026-11-17 on a Galaxy A90 at 411 twice).
 */
export function PhoneRelationConditionPages({ property, value, onChange }: {
  property: DbProperty;
  value: unknown;
  onChange(value: unknown): void;
}) {
  const { t } = useTranslation("databaseView");
  const pages = useTriggerRelationPages(property).map((page) => ({
    id: page.id, label: relationPageName(page), icon: <PageIconGlyph page={pageIconSource(page)} size={22} />,
    chipIcon: <PageIconGlyph page={pageIconSource(page)} size={18} />,
  }));
  // Notion holds the field a level further in than the screen's other lists: 28px and a line more above it.
  return <>
    <div className={styles.phoneLevel} aria-hidden="true" />
    <PhoneChoiceList label={t("automationTriggerOperatorFor", { name: property.name })} placeholder={t("automationTriggerSearchPages")}
      choices={pages} value={value} pages condition onChange={(next) => onChange(next ?? [])} onClear={() => onChange([])} clearStays />
  </>;
}

/**
 * A relation condition's pages in a desktop's window, as Notion's (seen
 * 2026-11-17 in Chrome at 1440): a field 16px in holding the chosen pages'
 * chips (a 14.4px icon, the name 12/500, "×"), "하나 이상의 페이지 검색"
 * while none and an "ⓧ" once one is chosen or a word typed; 12px under it
 * the pages, found where their words start (relationPagesFound), in 28px
 * rows 4px in — a 20px icon 8px in, the name 8px after it, "✓" 8px from the
 * end on a chosen one.
 */
export function RelationConditionPicks({ property, value, inputRef, onChange }: {
  property: DbProperty;
  value: unknown;
  inputRef: React.RefObject<HTMLInputElement | null>;
  onChange(value: unknown): void;
}) {
  const { t } = useTranslation("databaseView");
  const [query, setQuery] = useState("");
  const pages = useTriggerRelationPages(property);
  const chosen = (triggerChosen(value) ?? []).filter((id): id is string => typeof id === "string");
  const named = useRef(new Map<string, Page>());
  for (const page of pages) named.current.set(page.id, page);
  const toggle = (id: string) => onChange(chosen.includes(id) ? chosen.filter((item) => item !== id) : [...chosen, id]);
  // Found again only as the pages or the words change.
  const shown = useMemo(() => relationPagesFound([...pages], query, relationPageName), [pages, query]);
  // Past 100 pages only those around what the window shows are drawn, as the
  // relation cell's picker draws them (usePickerWindow): each 28 high, in the
  // condition's window that scrolls them with its search (2026-11-22: 5,000
  // pages drew in 557 ms).
  const rowsRef = useRef<HTMLDivElement>(null);
  const scrollRef = useMemo(() => ({ get current() { return rowsRef.current?.closest<HTMLElement>('[role="dialog"]') ?? rowsRef.current; } }), []);
  const drawn = usePickerWindow({ scrollRef, open: true, count: shown.length, keyAt: (index) => shown[index].id, sizeAt: () => 28, focused: -1 });
  const drawRow = (line: PickerWindowLine) => {
    const page = shown[line.index], picked = chosen.includes(page.id);
    const row = (
      <button key={page.id} type="button" role="option" aria-selected={picked} className={styles.row}
        aria-setsize={drawn.windowed ? shown.length : undefined} aria-posinset={drawn.windowed ? line.index + 1 : undefined}
        data-automation-condition-item={page.id} onClick={() => toggle(page.id)}>
        <span className={styles.rowIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={18} /></span>
        <span className={styles.rowName}>{relationPageName(page)}</span>
        {picked ? <CheckIcon size="small" className={styles.check} aria-hidden="true" /> : null}
      </button>
    );
    return line.top === undefined ? row : <div key={page.id} {...pickerWindowLine(line, drawn.measure)}>{row}</div>;
  };
  return (
    <>
      <div className={styles.searchRow}>
        <div className={styles.search}>
          <div className={styles.picks}>
            {chosen.map((id) => {
              const page = named.current.get(id);
              return (
                <span key={id} className={styles.chip}>
                  <a className={styles.chipLink} href={pageHref(id)}>
                    <span className={styles.chipIcon} aria-hidden="true">
                      <PageIconGlyph page={page ? pageIconSource(page) : { icon: "", iconType: "none", kind: "page" }} size={FILTER_PAGE_CHIP_ICON} />
                    </span>
                    <span className={styles.chipName}>{page ? relationPageName(page) : t("untitled")}</span>
                  </a>
                  <button type="button" aria-label={t("sheetRemoveChoice", { name: page ? relationPageName(page) : t("untitled") })} onClick={() => toggle(id)}>
                    <XMarkBoldSmallIcon size="small" aria-hidden="true" />
                  </button>
                </span>
              );
            })}
            <input ref={inputRef} value={query} placeholder={chosen.length ? "" : t("automationTriggerSearchPages")}
              aria-label={t("automationTriggerOperatorFor", { name: property.name })} onChange={(event) => setQuery(event.currentTarget.value)} />
          </div>
          <SearchClearButton value={chosen.length > 0 || query !== ""} small label={t("sheetClearValue")}
            onClear={() => { setQuery(""); onChange([]); }} />
        </div>
      </div>
      <div ref={rowsRef} className={styles.rows} role="listbox" aria-multiselectable="true" aria-label={property.name}>
        {drawn.windowed
          ? <div ref={drawn.linesRef} role="presentation" style={pickerWindowBox(drawn.height)}>{drawn.lines.map(drawRow)}</div>
          : drawn.lines.map(drawRow)}
      </div>
    </>
  );
}
