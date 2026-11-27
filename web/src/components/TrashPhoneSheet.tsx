import { useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { DatabaseCylinderIcon, FileBlank, RestoreIcon, Search, TableIcon, TeamspaceIcon, Trash, UserIcon, X, XCircleIcon } from "@/icons/leafen";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { pagePlace, type Place } from "@/lib/pagePath";
import { PlaceLine } from "./PlaceLine";
import { trashNoResultsText } from "./TrashNoMatches";
import type { Page, Session, Workspace } from "@/types";
import { useTrashFilterChoices, type TrashFilterChoice, type TrashFilterMenu } from "./TrashFilters";
import type { TrashFilterState } from "./trashFilterModel";
import { unansweredReadText } from "@/lib/unansweredRead";
import styles from "./TrashPhoneSheet.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";
import { SelfMark } from "@/components/people/SelfMark";

/** A line of the trash: a trashed page (opened on a press) or a deleted database. */
export type TrashEntry = {
  key: string;
  title: string;
  page?: Page;
  databaseId?: string;
  /** A data source kept in its database, rather than a database. */
  source?: boolean;
  place?: Place;
  restore(): void;
  /** Opens a database line's database alone, in a new tab when asked (TrashView's openTrashedDatabase). */
  open?: (newTab: boolean) => void;
  deleteForever?: () => void;
  /**
   * Why the reader may not delete it forever, said on its dim delete as the
   * trashed page's band says it: the workspace's owner alone may,
   * or an active legal hold keeps it.
   */
  deleteRefused?: string;
};

type Props = {
  query: string;
  onQuery(query: string): void;
  filters: TrashFilterState;
  onFilters(filters: TrashFilterState): void;
  entries: TrashEntry[];
  busy: boolean;
  listRef: RefObject<HTMLDivElement | null>;
  pages: Page[];
  session: Session;
  workspace: Workspace;
  onOpen(page: Page): void;
  onClose(): void;
};

/** A row's text: its name, and where it was under it. */
function RowText({ icon, title, place }: { icon: ReactNode; title: string; place?: Place }) {
  return (
    <>
      <span className={styles.icon} aria-hidden="true">{icon}</span>
      <span className={styles.text}>
        <span className={styles.title}>{title}</span>
        {place?.length ? <PlaceLine place={place} className={styles.place} /> : null}
      </span>
    </>
  );
}

/**
 * A phone's trash, as Notion's (seen 2026-11-10 on an iPhone's Safari): a
 * whole screen on the soft page ground under a head of "휴지통" and a blue
 * "완료" (its "?" opens Notion's help, which Leafen leaves out); a white
 * search row across it, its three filters as white rows across it parted by
 * bands of the ground, each opening a screen of its own; then the trashed
 * pages, scrolling under them, each with its place and its restore and
 * delete buttons. It doesn't take the keys as it opens.
 */
export function TrashPhoneSheet({ query, onQuery, filters, onFilters, entries, busy, listRef, pages, session, workspace, onOpen, onClose, unanswered = false, wide = false }: Props & {
  /** Nothing to list because the server wasn't there to ask: said as such (unansweredRead.ts). */
  unanswered?: boolean;
  /**
   * A touch screen wider than a phone's, as Notion's iPad (seen 2026-11-13 at
   * 1210 and 834 in iPad Safari): a sheet in the screen's middle under its
   * grip, its search a white round field and its pages a round group on the
   * sheet's ground, with no filter rows.
   */
  wide?: boolean;
}) {
  const { t } = useTranslation(["trashView", "common"]);
  const [filter, setFilter] = useState<TrashFilterMenu | null>(null);
  const [filterQuery, setFilterQuery] = useState("");
  const picks = useTrashFilterChoices(filter, filterQuery, { filters, onChange: onFilters, pages, session, workspace });
  const pagesById = pagesByIdOf(pages);

  const filterRow = (kind: TrashFilterMenu, label: string, active: boolean) => {
    const Icon = kind === "editors" ? UserIcon : kind === "in" ? FileBlank : TeamspaceIcon;
    return (
      <button type="button" className={styles.filterRow} data-trash-filter={kind} data-active={active ? "true" : undefined}
        onClick={() => { setFilterQuery(""); setFilter(kind); }}>
        <span className={styles.filterIcon} aria-hidden="true"><Icon size="regular" /></span>
        <span>{label}</span>
      </button>
    );
  };

  return (
    <PhoneSheet size={wide ? "full" : "screen"} title={t("trashView:trash")} onClose={onClose}
      action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-trash-popover": "", "data-trash-phone": "" }}>
      <div className={styles.screen} data-wide={wide ? "true" : undefined}>
        <label className={styles.search}>
          <Search size="small" aria-hidden="true" />
          <input value={query} aria-label={t("trashView:searchTrash")} placeholder={t("trashView:searchTrash")}
            onChange={(event) => onQuery(event.target.value)} />
          {query ? (
            <button type="button" className={styles.clear} aria-label={t("trashView:phone.searchClear")} onClick={() => onQuery("")}>
              <XCircleIcon size="page" aria-hidden="true" />
            </button>
          ) : null}
        </label>
        {/* Notion's rows say nothing of what each filter holds (seen 2026-11-10). */}
        {wide ? null : <div className={styles.filters} role="group" aria-label={t("trashView:filters.label")}>
          {filterRow("editors", t("trashView:filters.editedBy"), filters.editors.length > 0)}
          {filterRow("in", t("trashView:filters.in"), filters.inPages.length > 0)}
          {filterRow("teamspace", t("trashView:filters.teamspace"), filters.teamspaces.length > 0)}
        </div>}
        <div ref={listRef} className={styles.list} tabIndex={-1}>
          <div className={styles.rows} role="list" aria-label={t("trashView:deletedPages")}>
            {entries.length === 0 ? (
              // Notion's words when a search finds nothing, and when the filters leave nothing (TrashNoMatches).
              <p className={styles.note} role="status">{!query.trim() && unanswered ? unansweredReadText()
                : trashNoResultsText(t, Boolean(query.trim()))}</p>
            ) : entries.map((entry) => {
              const page = entry.page;
              const text = <RowText title={entry.title} place={entry.place}
                icon={page ? <PageIconGlyph page={page} size={25} /> : entry.source ? <DatabaseCylinderIcon size="regular" /> : <TableIcon size="regular" />} />;
              return (
                <div key={entry.key} className={styles.row} role="listitem" data-placed={entry.place?.length ? "true" : undefined}
                  data-trash-database={entry.databaseId}>
                  {page ? (
                    <button type="button" className={styles.page} aria-label={t("trashView:aria.openFromTrash", { title: entry.title })}
                      onClick={() => onOpen(page)}>{text}</button>
                  ) : entry.open ? (
                    <button type="button" className={styles.page} aria-label={t("trashView:aria.openFromTrash", { title: entry.title })}
                      onClick={() => entry.open?.(false)}>{text}</button>
                  ) : <span className={styles.page}>{text}</span>}
                  <span className={styles.actions}>
                    <button type="button" className={styles.action} disabled={busy} data-trash-restore
                      aria-label={t("trashView:aria.restorePage", { title: entry.title })} onClick={entry.restore}>
                      <RestoreIcon size="tiny" aria-hidden="true" />
                    </button>
                    {entry.deleteForever ? (
                      <button type="button" className={styles.action} disabled={busy} data-trash-delete
                        aria-label={t("trashView:aria.deletePageForever", { title: entry.title })} onClick={entry.deleteForever}>
                        <Trash size="tiny" aria-hidden="true" />
                      </button>
                    ) : entry.deleteRefused ? (
                      <button type="button" className={styles.action} aria-disabled="true" data-trash-delete
                        aria-label={t("trashView:aria.deletePageForever", { title: entry.title })}
                        aria-description={entry.deleteRefused}>
                        <Trash size="tiny" aria-hidden="true" />
                      </button>
                    ) : null}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
      {filter ? <TrashPhoneFilterScreen kind={filter} choices={picks.choices} chosen={picks.chosen} onClear={picks.clear}
        query={filterQuery} onQuery={setFilterQuery} session={session} placeOf={(page) => pagePlace(page, pagesById)}
        onClose={() => setFilter(null)} /> : null}
    </PhoneSheet>
  );
}

/**
 * A trash filter's own screen, as Notion's phone opens each (seen
 * 2026-11-10): its name and "완료" at its head, a white field holding what is
 * chosen as chips with "×" (and "ⓧ" clearing them all) that searches the
 * rest, then the rest in rows; a row picked goes into the field. The reader
 * is named, "(나)" after the name in the rows.
 */
function TrashPhoneFilterScreen({ kind, choices, chosen, onClear, query, onQuery, session, placeOf, onClose }: {
  kind: TrashFilterMenu;
  choices: TrashFilterChoice[];
  chosen: TrashFilterChoice[];
  onClear(): void;
  query: string;
  onQuery(query: string): void;
  session: Session;
  /** Where a page sits, under its name in its row. */
  placeOf(page: Page): Place | undefined;
  onClose(): void;
}) {
  const { t } = useTranslation(["trashView", "common"]);
  const title = t(kind === "editors" ? "trashView:filters.editedBy" : kind === "in" ? "trashView:filters.in" : "trashView:filters.teamspace");
  const search = t(kind === "editors" ? "trashView:filters.searchPeople" : kind === "in" ? "trashView:phone.searchPages" : "trashView:phone.searchTeamspaces");
  const reader = session.user.name.trim() || session.user.email;
  const name = (choice: TrashFilterChoice) => (choice.userId === session.user.id ? reader : choice.label);
  const rest = choices.filter((choice) => !choice.checked);
  // Taking one out, or all, leaves the keys in the field, as Notion's.
  const fieldRef = useRef<HTMLInputElement>(null);
  const refocus = (run: () => void) => () => { run(); fieldRef.current?.focus(); };

  const chip = (choice: TrashFilterChoice) => (
    <span key={choice.key} className={styles.chip} data-kind={kind} data-trash-filter-chip={choice.key}>
      {choice.userId ? <PersonAvatar userId={choice.userId} name={name(choice)} size={24} />
        : choice.page ? <span className={styles.chipIcon} aria-hidden="true"><PageIconGlyph page={choice.page} size={21} /></span> : null}
      <span className={styles.chipLabel}>{name(choice)}</span>
      <button type="button" aria-label={t("trashView:phone.removeChoice", { name: name(choice) })} onClick={refocus(choice.toggle)}>
        <X size="micro" aria-hidden="true" />
      </button>
    </span>
  );

  const row = (choice: TrashFilterChoice) => { const place = choice.page ? placeOf(choice.page) : undefined; return choice.userId ? (
    <button key={choice.key} type="button" role="option" aria-selected={false} className={styles.person}
      data-trash-filter-choice={choice.key} onClick={choice.toggle}>
      <PersonAvatar userId={choice.userId} name={name(choice)} size={28} />
      <span className={styles.personName}>
        {name(choice)}
        {choice.userId === session.user.id ? <SelfMark className={styles.mark} text={t("trashView:phone.meMark")} /> : null}
      </span>
    </button>
  ) : (
    <button key={choice.key} type="button" role="option" aria-selected={false} className={styles.choiceRow}
      data-placed={place?.length ? "true" : undefined} data-trash-filter-choice={choice.key} onClick={choice.toggle}>
      <RowText title={choice.label} place={place}
        icon={choice.page ? <PageIconGlyph page={choice.page} size={25} /> : <TeamspaceIcon size="regular" />} />
    </button>
  ); };

  return (
    <PhoneSheet size="screen" title={title} onClose={onClose} action={{ label: t("common:actions.done"), onPress: onClose }}
      data={{ "data-trash-filter-sheet": kind }}>
      <div className={styles.screen}>
        <div className={styles.pickTop}>
          <div className={styles.field}>
            {chosen.map(chip)}
            <input ref={fieldRef} value={query} aria-label={search} placeholder={chosen.length ? "" : search}
              onChange={(event) => onQuery(event.target.value)} />
            {chosen.length ? (
              <button type="button" className={styles.fieldClear} aria-label={t("common:actions.clearAll")} onClick={refocus(onClear)}>
                <XCircleIcon size="regular" aria-hidden="true" />
              </button>
            ) : null}
          </div>
        </div>
        <div className={styles.list}>
          <div className={styles.rows} role="listbox" aria-multiselectable="true" aria-label={title}>
            {rest.length ? rest.map(row) : <p className={styles.note} role="status">{t("trashView:phone.noResults")}</p>}
          </div>
        </div>
      </div>
    </PhoneSheet>
  );
}
