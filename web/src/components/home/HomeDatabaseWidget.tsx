import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { RowPageBodyEditor } from "@/components/database/RowPageBodyEditor";
import { DatabaseNamesAddressContext } from "@/components/database/databaseFullPageLink";
import { RowsOpenInContext } from "@/components/database/databaseRowOpeners";
import { useProductPages } from "@/components/product/productPagesContext";
import { usePageActionAccess } from "@/lib/pageActionAccessContext";
import { DotsHorizontal, TableIcon as TableViewIcon } from "@/icons/leafen";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { usePageNames } from "@/lib/pageNames";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import { BuiltInIconGlyph, PageIconGlyph, type PageIconSource } from "@/components/page/PageIcon";
import { PlaceLine } from "@/components/PlaceLine";
import { EXISTING_FIRST_SHOWN, EXISTING_MORE_SHOWN, EXISTING_SEARCH_FIRST_SHOWN } from "@/components/database/linkableSources";
import { inlineDatabasePlaceholderTitle, untitledDatabaseSourcesName } from "@/components/editor/databaseTitles";
import type { Page, Session } from "@/types";
import { pagesByIdOf, trashedPageIdsOf } from "@/lib/pageIndex";
import { HomeSectionHead } from "./HomeRecents";
import styles from "./HomeView.module.css";
import widgets from "./HomeWidgets.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

const PINNED_KEY = "leafen:home-database";
const noPages: readonly Page[] = [];
// The sample's activities, each with its small mark: the built-in icons sun,
// wheat, book-closed, food-and-drink and run, as Notion's (seen 2026-11-16).
const SAMPLE_ROWS: readonly { key: string; icon: string }[] = [
  { key: "sampleRowOne", icon: "sun" }, { key: "sampleRowTwo", icon: "wheat" }, { key: "sampleRowThree", icon: "book-closed" },
  { key: "sampleRowFour", icon: "food-and-drink" }, { key: "sampleRowFive", icon: "run" },
];
type Pinned = { pageId: string; blockId?: string };

function readPinned(stored: string | null): Pinned | null {
  if (!stored) return null;
  try {
    const value = JSON.parse(stored) as Pinned;
    return typeof value?.pageId === "string" ? value : null;
  } catch { return { pageId: stored }; }
}

/**
 * Home's database view, as Notion's (seen 2026-11-05): until one is pinned, a
 * card that says what it is for beside a sample table, with "데이터베이스 선택"
 * listing the workspace's databases; then that database, written here.
 */
export function HomeDatabaseWidget({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const { t } = useTranslation("homeView");
  const place = useMemo(() => ({ userId: session.user.id, part: workspaceId }), [session.user.id, workspaceId]);
  const [pinned, setPinned] = useState(() => readPinned(personValue(PINNED_KEY, place)));
  const [picking, setPicking] = useState(false);
  const pin = (next: Pinned) => { storePersonValue(PINNED_KEY, JSON.stringify(next), place); setPinned(next); setPicking(false); };
  const unpin = () => { storePersonValue(PINNED_KEY, "", place); setPinned(null); };
  if (pinned) return <PinnedDatabase session={session} workspaceId={workspaceId} pinned={pinned} onUnpin={unpin} />;
  return (
    <section className={styles.homeSection} aria-labelledby="home-database-title" data-home-database data-home-widget>
      <HomeSectionHead id="home-database-title" widget="database" icon={<TableViewIcon size="small" aria-hidden="true" />}>{t("databaseWidget")}</HomeSectionHead>
      <div className={widgets.card}>
        <div className={widgets.text}>
          <span className={widgets.mark} aria-hidden="true"><TableViewIcon size="empty" /></span>
          <p>{t("databaseWidgetHint")}</p>
          <span className={widgets.startWrap}>
            <button type="button" className={widgets.start} aria-expanded={picking} onClick={() => setPicking((open) => !open)}>{t("databaseWidgetPick")}</button>
            {picking ? <DatabasePicker onPick={pin} onClose={() => setPicking(false)} /> : null}
          </span>
        </div>
        <div className={widgets.databaseSample} aria-hidden="true">
          <div className={widgets.databaseSampleRow} data-head><span>{t("sampleActivity")}</span><span>{t("sampleStatus")}</span></div>
          {SAMPLE_ROWS.map(({ key, icon }, index) => <div key={key} className={widgets.databaseSampleRow}>
            <span className={widgets.sampleIcon}><BuiltInIconGlyph icon={{ name: icon, color: "gray" }} size={16} color="currentColor" /></span>
            <span className={widgets.sampleName}>{t(key)}</span>
            <span>{t(index === 0 ? "sampleDone" : index === 1 ? "sampleDoing" : "sampleTodo")}</span>
          </div>)}
        </div>
      </div>
    </section>
  );
}

/**
 * The workspace's databases to pin, as Notion's home widget lists them (seen
 * 2026-11-14 at 1440, dark): beside the button, centred on it, a search
 * "검색하기…", then each database by its icon (a 20px cylinder without one),
 * its name and, under it, where it sits — the pages above a full-page one,
 * the page holding an inline one (PlaceLine, foundDatabaseSourcePlace) —
 * five, then "N개 더 보기" for twenty more a press.
 */
function DatabasePicker({ onPick, onClose }: { onPick(pinned: Pinned): void; onClose(): void }) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["homeView", "databaseView"]);
  const product = useProductPages();
  const [query, setQuery] = useState("");
  const [count, setCount] = useState(0);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const away = (event: PointerEvent) => { if (!box.current?.contains(event.target as Node)) onClose(); };
    window.addEventListener("pointerdown", away, true);
    return () => window.removeEventListener("pointerdown", away, true);
  }, [onClose]);
  // The list's own index (lib/pageIndex.ts), not one made as the chooser opens; a database in the trash, or under a page that is, is none to choose.
  const listPages = product?.pages ?? noPages;
  const pagesById = pagesByIdOf(listPages);
  const candidates = useMemo(() => {
    const trashed = trashedPageIdsOf(listPages);
    return [
    ...listPages.filter((page) => page.kind === "database" && !trashed.has(page.id)).map((page) => {
      const name = pageNames.title(page);
      return { key: page.id, name, icon: page as PageIconSource, pinned: { pageId: page.id } as Pinned,
        place: foundDatabaseSourcePlace({ pageId: page.id, pageTitle: name, title: name, pageKind: "database" }, pagesById) };
    }),
    ...(product?.inlineDatabases ?? []).filter((database) => pagesById[database.pageId] && !trashed.has(database.pageId)).map((database) => {
      // One of several sources unnamed goes by its sources' names (inlineDatabaseName);
      // one unnamed is "새 데이터베이스", as Notion's home widget lists it (seen 2026-11-14).
      const name = database.title || (database.sourceTitles && database.sourceTitles.length > 1
        ? untitledDatabaseSourcesName(database.sourceTitles) : inlineDatabasePlaceholderTitle());
      const holder = pagesById[database.pageId];
      return { key: database.containerId, name, icon: { kind: "database", icon: database.icon, iconType: database.iconType } as PageIconSource,
        pinned: { pageId: database.pageId, blockId: database.containerId } as Pinned,
        place: foundDatabaseSourcePlace({ pageId: holder.id, pageTitle: pageNames.title(holder), title: name, sourceTitles: database.sourceTitles,
          databaseTitle: database.title, pageKind: holder.kind, viewBlock: database.viewBlock }, pagesById) };
    }),
    ];
  }, [listPages, pagesById, product?.inlineDatabases, pageNames]);
  const needle = query.trim().toLocaleLowerCase();
  const found = needle ? candidates.filter(({ name }) => name.toLocaleLowerCase().includes(needle)) : candidates;
  const shown = found.slice(0, (needle ? EXISTING_SEARCH_FIRST_SHOWN : EXISTING_FIRST_SHOWN) + count);
  const search = (value: string) => { setQuery(value); setCount(0); };
  return (
    <div ref={box} className={widgets.databasePicker} role="dialog" aria-label={t("databaseWidgetPick")}
      onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); onClose(); } }}>
      <span className={widgets.databasePickerSearch}>
        <input autoFocus={fieldFocusOnOpen()} value={query} placeholder={t("databaseWidgetSearch")} aria-label={t("databaseWidgetSearch")}
          onChange={(event) => search(event.currentTarget.value)} />
      </span>
      <div className={widgets.databasePickerList} role="listbox" aria-label={t("databaseWidgetPick")}>
        {shown.map(({ key, name, icon, pinned, place }) => <button key={key} type="button" role="option" aria-selected={false}
          className={widgets.databasePickerRow} onClick={() => onPick(pinned)}>
          <span className={widgets.databasePickerIcon} aria-hidden="true"><PageIconGlyph page={icon} size={18} database="cylinder" /></span>
          <span className={widgets.databasePickerText}>
            <span>{name}</span>
            {place.length ? <small><PlaceLine place={place} /></small> : null}
          </span>
        </button>)}
        {shown.length < found.length ? <button type="button" className={widgets.databasePickerRow} data-more
          onClick={() => setCount((more) => more + EXISTING_MORE_SHOWN)}>
          <span className={widgets.databasePickerIcon} aria-hidden="true"><DotsHorizontal size="regular" /></span>
          <span className={widgets.databasePickerText}><span>{t("databaseView:showMoreDataSources", { count: found.length - shown.length })}</span></span>
        </button> : null}
        {found.length ? null : <p>{t("databaseWidgetNone")}</p>}
      </div>
    </div>
  );
}

/**
 * The pinned database as it is anywhere, written here by who may; its rows
 * open in the middle over the home whatever its views say, as Notion's
 * widget's (RowsOpenInContext), and the home's address names none of its
 * views (DatabaseNamesAddressContext).
 */
function PinnedDatabase({ session, workspaceId, pinned, onUnpin }: { session: Session; workspaceId: string; pinned: Pinned; onUnpin(): void }) {
  const { t } = useTranslation("homeView");
  const page = useProductPages()?.pages.find((candidate) => candidate.id === pinned.pageId);
  const access = usePageActionAccess(pinned.pageId, true);
  if (!page) return null;
  return (
    <section className={styles.homeSection} data-home-database>
      <RowsOpenInContext.Provider value="center"><DatabaseNamesAddressContext.Provider value={false}>
        <RowPageBodyEditor key={`${pinned.pageId}:${pinned.blockId ?? ""}`} session={session} rowId={pinned.pageId} workspaceId={workspaceId}
          readOnly={!access.canEdit} ownPage {...(pinned.blockId ? { onlyBlockId: pinned.blockId } : { fullPageDatabase: true, presentAs: page })} />
      </DatabaseNamesAddressContext.Provider></RowsOpenInContext.Provider>
      <button type="button" className={widgets.start} onClick={onUnpin}>{t("databaseWidgetChange")}</button>
    </section>
  );
}
