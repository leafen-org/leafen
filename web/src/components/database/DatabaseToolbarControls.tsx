import { useContext, type RefObject } from "react";
import { flushSync } from "react-dom";
import { useDatabaseInTrash } from "./useDatabaseInTrash";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { useTranslation } from "react-i18next";
import {
  DoubleChevronLeft,
  DoubleChevronRight,
  FilterIcon,
  ExpandDiagonalIcon,
  LightningIcon,
  Search,
  SlidersIcon,
  SortIcon,
} from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { useRouter } from "@/lib/router";
import { TopBarLock } from "../page/TopBarLock";
import { useDatabaseFullPageLink } from "./databaseFullPageLink";
import { useDatabaseLock } from "./databaseStructureLock";
import { databaseViewLabels } from "./databaseViewLabels";
import { DatabaseSearchableContext } from "./databaseNewRowControl";
import inertStyles from "./DatabaseToolbarInert.module.css";
import phoneSearchStyles from "./DatabaseToolbarPhoneSearch.module.css";
import type { DatabaseToolbarMenu } from "./databaseToolbarModel";
import styles from "./databaseSheet";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

// The toolbar's buttons, as Notion's (seen 2026-09-30): filter, sort,
// automations and search, then "Settings", which opens the view's layout,
// properties, grouping, conditional color and data source settings in a
// panel. Each icon names itself in a tip above it — "»", the filter, sort,
// automations, search, the full page, settings and "«" alike (Notion's,
// seen 2026-11-15 at 1440); "새로 만들기" and its "⌄" have none.
type ControlsProps = {
  filterCount: number;
  /**
   * Whether a filter rows go by is set: one still waiting for its value
   * leaves the icon grey (its orange dot kept while unsaved), as Notion's
   * (seen 2026-11-17 at 1440, an empty 선택 and 이름 grey, "가" blue; an
   * advanced filter's rule, empty or not, blue). Without it, any filter.
   */
  filtersApplied?: boolean;
  openMenu: DatabaseToolbarMenu | null;
  /** Whether the viewer can't edit the view: they see its filters, sorts and search alone. */
  readOnly: boolean;
  /** Whether filters and sorts the viewer can't save apply for them alone. */
  ownQuery: boolean;
  /** Whether the viewer's own filters and sorts apply now, not yet the view's: a phone's "필터 및 정렬" carries an orange dot. */
  unsavedQuery?: boolean;
  /** On a computer, whether the viewer's own filters, or sorts, differ from the view's: an orange dot on "필터" or "정렬" (Notion's, seen 2026-11-05). */
  unsavedFilters?: boolean;
  unsavedSorts?: boolean;
  /** Whether the viewer can edit the database though its lock keeps its views: its settings and automations stay. */
  settingsWritable?: boolean;
  /**
   * A signed-in viewer who can only comment or read: the settings open to be
   * looked at, the rows they can't change dimmed, as Notion's (seen 2026-10-11).
   */
  readerSettings?: boolean;
  search: string;
  searchOpen: boolean;
  searchRef: RefObject<HTMLInputElement | null>;
  sortCount: number;
  onSearchChange(value: string): void;
  onSearchOpenChange(open: boolean): void;
  onToggleMenu(menu: DatabaseToolbarMenu, trigger: HTMLElement, modality: "pointer" | "keyboard"): void;
  /**
   * The row is too narrow for the filter, sort, automations and search: the
   * full page and settings alone, as Notion's (seen 2026-10-30, a 307px row
   * beside an open sidebar kept "⤢ ⚙ 새로 만들기").
   */
  narrow?: boolean;
  /** Narrower still, settings alone: the full page goes too (Notion's, seen 2026-11-03 under 320px). */
  cramped?: boolean;
  /**
   * Whether the viewer folded the tools away, as Notion's "»" ("최소화하기",
   * seen 2026-10-30): pointing at a row with its tools shows "»" before them,
   * which leaves "«" ("펼치기", while pointed at) and settings — New too goes.
   * Without it the row has no "»".
   */
  minimized?: boolean;
  onMinimizedChange?(minimized: boolean): void;
  /**
   * Where a view's own controls go (a date view's "날짜 없음(N)", a map's
   * "위치 없음(N개)"): after "»" and before the filter, put away with the
   * tools while they are folded, as Notion's (calendar and timeline, seen
   * 2026-11-15 at 1440: "»" 770, "날짜 없음(3)" 798, the filter 881; folded,
   * only "«" and settings), and while the row is too narrow for them (a
   * 399px timeline row keeping "⤢ ⚙ 새로 만들기", seen 2026-11-19 at 606 in
   * Korean and Arabic).
   */
  viewControlsSlotRef?(element: HTMLElement | null): void;
  /**
   * A form view keeps its settings alone, as Notion's; on a phone, where its
   * builder edits nothing, its search and "필터 및 정렬" instead, no settings
   * (Notion's, seen 2026-11-15 twice in Android Chrome at 411).
   */
  form?: boolean;
  /** The tools the kind of view keeps, all unless said otherwise. */
  viewTools?: { filter?: boolean; sort?: boolean; automations?: boolean; search?: boolean; settings?: boolean; openFullPage?: boolean };
  /**
   * A phone's row, as Notion's (seen 2026-10-17 and 2026-10-20 at 390px):
   * search, one "필터 및 정렬", then settings, however narrow the row.
   */
  phone?: boolean;
  /** A touch screen's, a phone's or a tablet's: its tools are 20px icons in 32px buttons (Notion's iPad, seen 2026-11-14 at 834). */
  touch?: boolean;
  /** The database has automations to show (People's are its own, PeopleDatabaseView). */
  automations?: boolean;
  /** It has one or more, on or off: "⚡" is blue, as Notion's (seen 2026-11-08). */
  automated?: boolean;
  /** The database's page and source, to read whether it shows from the trash: "⚡" is dim there, saying why. */
  trashCheck?: Parameters<typeof useDatabaseInTrash>[0];
};

function MenuButton({
  active,
  applied,
  unsaved,
  icon,
  label,
  tip = label,
  menu,
  openMenu,
  onToggleMenu,
  refused,
}: {
  active?: boolean;
  /** On a phone, filters or sorts apply: the icon blue, with an orange dot while they are the viewer's own (Notion's, seen 2026-10-24). */
  applied?: boolean;
  unsaved?: boolean;
  icon: React.ReactNode;
  label: string;
  /** Its name in the tip above it, without the count its label carries: "필터", as Notion's (seen 2026-11-15). */
  tip?: string;
  menu: DatabaseToolbarMenu;
  openMenu: DatabaseToolbarMenu | null;
  onToggleMenu: ControlsProps["onToggleMenu"];
  /**
   * Why it can't open, said on it: dim to 0.4 with the default pointer, its
   * press doing nothing, as Notion's "⚡" on a database in the trash (seen
   * 2026-11-15: aria-disabled, opacity 0.4, its label and tip saying a
   * database in the trash takes no automation).
   */
  refused?: string;
}) {
  return (
    <button
      type="button"
      className={`${styles.toolbarBtn} ${styles.iconToolbarBtn}`}
      data-active={!refused && (active || openMenu === menu) ? "true" : undefined}
      data-applied={applied ? "true" : undefined}
      data-database-toolbar-menu={menu}
      aria-label={refused ?? label}
      aria-haspopup={refused ? undefined : "dialog"}
      aria-expanded={refused ? undefined : openMenu === menu}
      aria-disabled={refused ? "true" : undefined}
      style={refused ? { opacity: 0.4, cursor: "default", background: "transparent", color: "var(--text-secondary)" } : undefined}
      data-icon-tip={refused ?? tip}
      data-tip-side="top"
      onClick={(event) => refused ? undefined : onToggleMenu(
        menu,
        event.currentTarget,
        event.detail === 0 ? "keyboard" : "pointer",
      )}
    >
      {icon}
      {unsaved ? <span className={styles.toolbarUnsaved} aria-hidden="true" data-unsaved /> : null}
      <span className={styles.toolbarLabel}>{label}</span>
    </button>
  );
}

export function DatabaseToolbarControls({
  filterCount,
  filtersApplied = filterCount > 0,
  openMenu,
  readOnly,
  ownQuery,
  unsavedQuery = false,
  unsavedFilters = false,
  unsavedSorts = false,
  settingsWritable,
  readerSettings = false,
  search,
  searchOpen,
  searchRef,
  sortCount,
  onSearchChange,
  onSearchOpenChange,
  onToggleMenu,
  narrow = false,
  cramped = false,
  form = false,
  viewTools = {},
  phone = false,
  touch = false,
  automations = true,
  automated = false,
  trashCheck = {},
  minimized = false,
  onMinimizedChange,
  viewControlsSlotRef,
}: ControlsProps) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("common");
  const automationsRefused = useDatabaseInTrash(trashCheck) ? t("databaseView:automationsInTrash") : undefined;
  const router = useRouter();
  // Notion's inline database opens alone as a page from here (seen 2026-10-01).
  const fullPageHref = useDatabaseFullPageLink();
  // A locked inline database shows "잠금" before its buttons (seen 2026-10-01),
  // and keeps its automations and settings, whose screens then change only
  // what the lock allows.
  const lock = useDatabaseLock();
  // A database page's layout tab keeps filter, sort, search and new; its settings are the layout builder's.
  const tabMode = usePageLayoutTabMode(), pageTab = tabMode === "page";
  const settingsOpen = !pageTab && (!readOnly || Boolean(settingsWritable));
  const formOnly = !pageTab && form && !phone;
  // Where "»" folds them is DatabaseToolbar's to decide (onMinimizedChange): a row page's data tab folds as an inline database does.
  const minimizable = Boolean(onMinimizedChange) && !phone && !form;
  const folded = minimizable && minimized;
  const settingsOnly = (!pageTab && narrow && !phone) || formOnly || folded;
  // A narrow row page's data tab keeps "»" and "새로 만들기 ⌄" alone: its filter, sort and search go as an inline
  // database's do, but it has no full page or settings to keep, and "»" still shows in its tools' share (Notion's,
  // seen 2026-11-24 twice in Chrome at 640 and 684, a 433 and a 477 row).
  const toolsAway = settingsOnly || (pageTab && narrow && !phone);
  const queryable = !toolsAway && (!readOnly || ownQuery);
  const searchable = useContext(DatabaseSearchableContext);
  // A touch screen's tools draw 20px icons, as Notion's phone's three (seen
  // 2026-10-24 at 375px) and its iPad's row (seen 2026-11-14 at 834).
  const toolIcon = phone || touch ? "regular" : "small";
  // A phone's search, open, is the row's alone after the view's picker, its
  // name kept: "필터 및 정렬", settings and "+ ⌄" put away until it closes
  // (Notion's, seen 2026-11-12 in Android Chrome at 411 and 2026-11-15 at
  // 402pt in iOS Safari).
  const phoneSearching = phone && searchOpen;
  return (
    <>
      <div className={styles.dbToolbarGroup}>
        {lock?.locked && !lock.page && !lock.topBar && (
          <TopBarLock unlockedForMe={lock.unlockedForMe} onLockAgain={lock.relock}
            onUnlockForMe={lock.unlockForMe} onUnlockForEveryone={lock.unlockForEveryone} />
        )}
        {minimizable && !settingsOnly ? (
          <button type="button" className={`${styles.toolbarBtn} ${styles.iconToolbarBtn} ${styles.toolbarMinimize}`}
            aria-label={labels.minimizeToolbar} title={labels.minimizeToolbar} data-tip-side="top" data-toolbar-fold onClick={() => onMinimizedChange?.(true)}>
            <DoubleChevronRight size="small" aria-hidden="true" />
          </button>
        ) : null}
        <div ref={viewControlsSlotRef} className={styles.dbToolbarGroup} hidden={toolsAway || undefined}
          style={toolsAway ? { display: "none" } : undefined} data-view-controls-slot />
        {queryable && !phone && (
          <>
            {viewTools.filter !== false && <MenuButton applied={filtersApplied} unsaved={unsavedFilters} icon={<FilterIcon size={toolIcon} />}
              label={filterCount ? `${labels.filter} ${filterCount}` : labels.filter} tip={labels.filter}
              menu="filter" openMenu={openMenu} onToggleMenu={onToggleMenu} />}
            {viewTools.sort !== false && <MenuButton applied={sortCount > 0} unsaved={unsavedSorts} icon={<SortIcon size={toolIcon} />}
              label={sortCount ? `${labels.sort} ${sortCount}` : labels.sort} tip={labels.sort}
              menu="sort" openMenu={openMenu} onToggleMenu={onToggleMenu} />}
          </>
        )}
        {/* A reader sees the automations too, to read them (Notion's, seen 2026-10-12). */}
        {/* A form's builder keeps them too, before "전체 페이지로 열기" (Notion's, seen 2026-11-03). */}
        {(!settingsOnly || (formOnly && !folded)) && !phone && !tabMode && automations && viewTools.automations !== false && (settingsOpen || readerSettings) && (
          <MenuButton applied={automated} icon={<LightningIcon size={toolIcon} />} label={labels.automations}
            menu="automations" openMenu={openMenu} onToggleMenu={onToggleMenu} refused={automationsRefused} />
        )}
      </div>
      <div className={styles.dbToolbarSpacer} />
      <div className={styles.dbToolbarGroup}>
        {/* Notion's search (seen 2026-11-02): the magnifier stays as the box's
            head and puts the box away, the words still searching; the box is
            bare, 146px of 14px words, and a filled "x" clears and closes it
            once there are words. */}
        {(toolsAway && !search) || viewTools.search === false ? null : (
          <span className={`${styles.dbSearch} ${phoneSearching ? phoneSearchStyles.search : ""}`}>
            {/* Its field is drawn and focused within the press, so a phone's keyboard comes up with it, as Notion's
                (seen 2026-11-12 in Android Chrome at 411, after the press only: the keyboard up and a typed word in
                the field): iOS Safari raises the keyboard only for a focus inside the gesture, not one a frame later. */}
            <button type="button" className={`${styles.toolbarBtn} ${styles.iconToolbarBtn} ${inertStyles.inert}`} disabled={!searchable}
              aria-label={t("actions.search")} aria-expanded={searchOpen} data-icon-tip={t("actions.search")} data-tip-side="top" onClick={() => {
                if (searchOpen) { onSearchOpenChange(false); return; }
                flushSync(() => onSearchOpenChange(true));
                searchRef.current?.focus({ preventScroll: true });
              }}>
              <Search size={toolIcon} /><span className={styles.toolbarLabel}>{t("actions.search")}</span>
            </button>
            {searchOpen ? (
              <span className={styles.dbSearchBox}>
                <input
                  ref={searchRef}
                  value={search}
                  aria-label={labels.searchDatabaseRows}
                  placeholder={labels.searchPlaceholder}
                  onChange={(event) => onSearchChange(event.currentTarget.value)}
                  // Escape clears the words and puts the box away, every row
                  // back and the focus on the page, as Notion's (seen
                  // 2026-11-15 at 1440 after the key only: with "zzz" or
                  // none, the box gone, a letter typed after it reaching
                  // nothing, the focus on BODY; the 2026-11-12 audit too).
                  onKeyDown={(event) => {
                    if (!fieldEscape(event)) return;
                    event.preventDefault();
                    event.stopPropagation();
                    onSearchChange("");
                    onSearchOpenChange(false);
                  }}
                />
                {/* Notion's shared "입력 값 지우기" (seen 2026-11-17 at 1440): it
                    empties the search and puts the box away, as Escape does. */}
                <SearchClearButton value={search} onClear={() => {
                  onSearchChange("");
                  onSearchOpenChange(false);
                }} />
              </span>
            ) : null}
          </span>
        )}
        {/* Named for what its sheet holds: "필터" where the view doesn't sort, as Notion's dashboard (seen twice
            2026-11-17 in Android Chrome at 411), "필터 및 정렬" where it does. */}
        {queryable && phone && !phoneSearching && viewTools.filter !== false ? (
          <MenuButton applied={filtersApplied || sortCount > 0} unsaved={unsavedQuery} icon={<FilterIcon size={toolIcon} />}
            label={viewTools.sort === false ? labels.filter : labels.filterAndSort}
            menu="filter" openMenu={openMenu} onToggleMenu={onToggleMenu} />
        ) : null}
        {fullPageHref && !folded && !phone && !cramped && viewTools.openFullPage !== false ? (
          <a className={`${styles.toolbarBtn} ${styles.iconToolbarBtn}`} href={fullPageHref}
            aria-label={labels.openAsFullPage} title={labels.openAsFullPage} data-tip-side="top" data-database-open-full-page
            onClick={(event) => {
              if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
              event.preventDefault();
              router.push(fullPageHref);
            }}>
            <ExpandDiagonalIcon size={toolIcon} aria-hidden="true" />
          </a>
        ) : null}
        {folded ? (
          <button type="button" className={`${styles.toolbarBtn} ${styles.iconToolbarBtn} ${styles.toolbarMinimize}`}
            aria-label={labels.expandToolbar} title={labels.expandToolbar} data-tip-side="top" data-toolbar-unfold onClick={() => onMinimizedChange?.(false)}>
            <DoubleChevronLeft size="small" aria-hidden="true" />
          </button>
        ) : null}
        {!pageTab && !phoneSearching && !(phone && form) && viewTools.settings !== false && (settingsOpen || readerSettings) && (
          // A layout builder's tab opens them from its pill (PageLayoutCanvas), the button unseen.
          <button type="button" className={`${styles.toolbarBtn} ${styles.iconToolbarBtn}`} hidden={tabMode === "builder" || undefined}
            style={tabMode === "builder" ? { display: "none" } : undefined}
            aria-label={labels.settings} aria-haspopup="dialog" aria-expanded={openMenu === "view"} data-icon-tip={labels.settings} data-tip-side="top"
            // Shaded while its panel shows, its "새 보기" too (Notion's, seen 2026-11-15: the ⚙ grey under both).
            data-active={openMenu === "view" || openMenu === "newView" ? "true" : undefined}
            data-database-settings-trigger onClick={(event) => onToggleMenu(
              "view", event.currentTarget, event.detail === 0 ? "keyboard" : "pointer",
            )}>
            <SlidersIcon size={toolIcon} /><span className={styles.toolbarLabel}>{labels.settings}</span>
          </button>
        )}
      </div>
    </>
  );
}
