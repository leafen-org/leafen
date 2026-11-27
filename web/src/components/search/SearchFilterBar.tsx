import { withPersonDistinction } from "@/lib/peopleDirectory";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import {
  CalendarIcon, CheckIcon, ChevronDown, FileBlank, Plus, TeamspaceIcon, TitlePropertyIcon, UserIcon, VerifiedIcon, X, XCircleIcon,
} from "@/icons/leafen";
import type { WorkspaceSearchContentState } from "@/lib/pageArchive";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import type { Page, Session, Workspace } from "../../types";
import { pagePlace } from "@/lib/pagePath";
import { PlaceLine } from "@/components/PlaceLine";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SelfMark } from "@/components/people/SelfMark";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { SearchDateFilter } from "./SearchDateFilter";
import { FilterMenuList } from "../FilterMenuList";
import {
  type SearchFilters, type SearchPerson,
} from "./searchFilterModel";
import { foldNfcText } from "./searchModel";
import controls from "./SearchControls.module.css";
import styles from "./SearchDialog.module.css";
import menuStyles from "./SearchFilterMenu.module.css";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { usePageScopeChoices } from "./usePageScopeChoices";
import { fieldFocusOnOpen } from "@/lib/screenKind";

type FilterMenu = "createdBy" | "teamspace" | "in" | "date" | "contentState" | "add";

// Each filter's mark, as Notion's "+ 필터" and the teamspace menu draw them
// (seen 2026-10-07; the content state's a badge with a check, 2026-11-14).
const FILTER_MARKS: Record<FilterMenu | "titleOnly", ReactNode> = {
  titleOnly: <TitlePropertyIcon size="small" aria-hidden="true" />,
  createdBy: <UserIcon size="small" aria-hidden="true" />,
  in: <FileBlank size="small" aria-hidden="true" />,
  date: <CalendarIcon size="small" aria-hidden="true" />,
  teamspace: <TeamspaceIcon size="small" aria-hidden="true" />,
  contentState: <VerifiedIcon size="small" aria-hidden="true" />,
  add: <Plus size="small" aria-hidden="true" />,
};

// The same marks on the row's chips, as Notion's (seen 2026-11-14): 20px
// high, each as wide as Notion's own ("Aa" 16.6, the person 12.6, the page
// 11.8, the calendar 13.8, the teamspace 18, the badge 16), "+" 9.2×14.
const CHIP_MARKS: Record<FilterMenu | "titleOnly", ReactNode> = {
  titleOnly: <TitlePropertyIcon size="regular" trim glyphWidth={13.28} aria-hidden="true" />,
  createdBy: <UserIcon size="regular" trim glyphWidth={10.08} aria-hidden="true" />,
  in: <FileBlank size="regular" trim glyphWidth={9.44} aria-hidden="true" />,
  date: <CalendarIcon size="regular" trim glyphWidth={11.04} aria-hidden="true" />,
  teamspace: <TeamspaceIcon size="regular" trim glyphWidth={14.4} aria-hidden="true" />,
  contentState: <VerifiedIcon size="regular" trim glyphWidth={12.8} aria-hidden="true" />,
  add: <Plus size="tiny" trim aria-hidden="true" />,
};
type AddedFilter = "date" | "teamspace" | "contentState";

// The content states, in Notion's order (seen 2026-11-14): live pages, all,
// verified, archived.
const CONTENT_STATES: WorkspaceSearchContentState[] = ["active", "all", "verified", "archived"];

const MENU_RESULTS = 8;

/** How far under its chip a filter's choices open, as Notion's (seen 2026-11-14): a
    list of choices ("+ 필터", "콘텐츠 상태") 4px down, one with its own search
    ("생성자", "검색 범위:", "날짜") at the chip's foot. */
function menuGap(kind: FilterMenu) {
  return kind === "add" || kind === "contentState" ? 4 : 0;
}

/**
 * The search window's filters, as Notion's (seen 2026-09-30): "제목만",
 * "생성자", "검색 범위", and "+ 필터" for the date, the teamspace and, as
 * Notion's archived pages are, the content state; each added one stays on the row until "제거하기".
 * The content state chip keeps its name whatever is chosen, blue while it
 * is not the live pages, as Notion's (seen 2026-11-14).
 */
export function SearchFilterBar({ session, workspace, pages, pagesById, teamspaces, filters, onChange, contentState, onContentState }: {
  session: Session;
  workspace: Workspace;
  pages: Page[];
  /** The pages by id as the search window reads them: none yet on its first frame (SearchDialog). */
  pagesById: Record<string, Page>;
  /** The teamspaces the reader joined, which the dialog reads once for this and the results' places. */
  teamspaces: readonly { id: string; name: string; icon?: string }[];
  filters: SearchFilters;
  onChange: (next: SearchFilters) => void;
  contentState: WorkspaceSearchContentState;
  onContentState: (next: WorkspaceSearchContentState) => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["searchDialog"]);
  // Where the pressed chip stands in the row: its start, and its foot.
  const [menu, setMenu] = useState<{ kind: FilterMenu; left: number; foot: number } | null>(null);
  const [menuQuery, setMenuQuery] = useState("");
  const [people, setPeople] = useState<SearchPerson[]>([]);
  const [added, setAdded] = useState<ReadonlySet<AddedFilter>>(() => new Set());
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  usePopoverAppear(Boolean(menu), menuRef);
  const layer = useOverlayLayer(Boolean(menu), { kind: "menu", ref: menuRef, inside: () => [rootRef.current],
    onEscape: () => setMenu(null), onOutsidePress: () => setMenu(null) });

  // People are searched in the workspace's organization as they are typed.
  useEffect(() => {
    if (menu?.kind !== "createdBy") return;
    const me: SearchPerson = { userId: session.user.id, label: t("searchDialog:filters.me") };
    // A guest doesn't look through the organization's people.
    if (!workspace.organizationId || workspace.guest) {
      setPeople([me]);
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      // People only, as Notion's "생성자" lists them, not the accounts apps act as.
      api.searchOrganizationPeople(session, {
        organizationId: workspace.organizationId!, query: menuQuery.trim(), limit: MENU_RESULTS, peopleOnly: true, workspaceId: workspace.id,
      }, { signal: controller.signal }).then((result) => {
        const found = result.people.flatMap((person) => person.userId
          ? [{ userId: person.userId, label: withPersonDistinction(person.userId, person.displayName || person.email || person.userId) }] : []);
        // The reader first, as Notion's (seen 2026-10-30).
        const mine = found.filter((person) => person.userId === me.userId);
        const others = found.filter((person) => person.userId !== me.userId);
        setPeople(mine.length || menuQuery.trim() ? [...mine, ...others] : [me, ...others]);
      }, () => setPeople([me]));
    }, 150);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [menu?.kind, menuQuery, session, t, workspace.guest, workspace.organizationId]);


  // A page chosen to search within, looked for in the list while the window's first frame has none read.
  const pageOf = (id: string) => pagesById[id] ?? pages.find((page) => page.id === id);
  const pageName = (id: string) => { const page = pageOf(id); return page ? pageNames.title(page) : id; };
  // Gone through as "검색 범위" opens, not with the window: the page on screen,
  // then the reader's visits, then the rest (usePageScopeChoices, the trash's too).
  const choosingPages = menu?.kind === "in";
  const pageChoices = usePageScopeChoices({ session, workspaceId: workspace.id, pages, query: menuQuery, open: choosingPages });
  usePathsAboveShown(menu?.kind === "in" ? pageChoices : [], pagesById);

  function open(kind: FilterMenu, button: HTMLElement) {
    const root = rootRef.current?.getBoundingClientRect();
    const rect = button.getBoundingClientRect();
    setMenuQuery("");
    setMenu(menu?.kind === kind ? null : { kind, left: root ? rect.left - root.left : 0, foot: root ? rect.bottom - root.top : 0 });
  }

  // As Notion's (seen 2026-10-07): "제목만", "생성자" and "검색 범위:" stand in
  // the row; date and teamspace join it from "+ 필터".
  const dateShown = added.has("date") || filters.date.preset !== "any";
  const teamspaceShown = teamspaces.length > 0 && (added.has("teamspace") || filters.teamspaces.length > 0);
  const contentStateShown = added.has("contentState") || contentState !== "active";
  const addable: AddedFilter[] = [
    ...(dateShown ? [] : ["date" as const]),
    ...(teamspaces.length === 0 || teamspaceShown ? [] : ["teamspace" as const]),
    ...(contentStateShown ? [] : ["contentState" as const]),
  ];

  // An added filter joins the row with its choices open where "+ 필터" was.
  function add(kind: AddedFilter) {
    setAdded((current) => new Set([...current, kind]));
    setMenu((current) => ({ kind, left: current?.left ?? 0, foot: current?.foot ?? 0 }));
  }

  // "제거하기" takes a filter off the row, its choices with it, as Notion's
  // teamspace and content state ones.
  function remove(kind: "teamspace" | "contentState") {
    setAdded((current) => new Set([...current].filter((item) => item !== kind)));
    setMenu(null);
    if (kind === "contentState") onContentState("active");
    else onChange({ ...filters, teamspaces: [] });
  }

  // Each chip leads with its mark, and one that opens choices ends with a
  // 8.6×14 ⌄, as Notion's (seen 2026-11-14).
  function chip(kind: FilterMenu | "titleOnly", label: ReactNode, active: boolean, onClick: (button: HTMLElement) => void) {
    const mark = CHIP_MARKS[kind];
    return (
      <button type="button" className={`${styles.filterOption} ${kind === "add" ? controls.addFilter : ""}`}
        data-active={active ? "true" : undefined}
        data-search-filter={kind} aria-pressed={kind === "titleOnly" ? active : undefined}
        aria-haspopup={kind === "titleOnly" ? undefined : "menu"} aria-expanded={kind === "titleOnly" ? undefined : menu?.kind === kind}
        onClick={(event) => onClick(event.currentTarget)}>
        {mark}
        <span>{label}</span>
        {kind === "titleOnly" || kind === "add" ? null : <ChevronDown size="tiny" trim glyphWidth={9.83} aria-hidden="true" />}
      </button>
    );
  }

  // What is chosen reads in the pill by its names, joined, as Notion's (seen
  // 2026-11-10: "검색 범위:: W215 link hub, W215 …", cut by its width).
  const counted = (label: string, names: string[]) => names.length === 0 ? label : `${label}: ${names.join(", ")}`;
  // A range reads by its days, as the calendar picked them.
  const dateLabel = filters.date.preset === "any" ? t("searchDialog:filters.date")
    : filters.date.preset === "custom" ? `${t("searchDialog:filters.date")}: ${[filters.date.from, filters.date.to]
      .filter((day, index, days): day is string => Boolean(day) && days.indexOf(day) === index).join(" → ")}`
      : `${t("searchDialog:filters.date")}: ${t(`searchDialog:date.${filters.date.preset}`)}`;

  // Each menu's choices; an opened menu takes the keys at its search, else
  // at its chosen or first choice.
  type Choice = { key: string; label: ReactNode; text: string; checked?: boolean; role: "menuitem" | "menuitemcheckbox" | "menuitemradio";
    data?: Record<string, string>; run(): void };
  const radio = (key: string, label: string, checked: boolean, run: () => void): Choice =>
    ({ key, label, text: label, checked, role: "menuitemradio", run });
  let choices: Choice[] = [];
  if (menu?.kind === "createdBy") {
    // As Notion's (seen 2026-10-30): each person with their picture, the
    // reader with the lists' own mark (SelfMark); one chosen goes into the
    // search above as a token and leaves the list.
    choices = people.filter((person) => !filters.createdBy.some((chosen) => chosen.userId === person.userId)).map((person) => ({
      key: person.userId, text: person.label, role: "menuitem", data: { "data-search-filter-choice": "createdBy" },
      label: <span className={menuStyles.filterMenuMarked}><PersonAvatar userId={person.userId} name={person.label} size={20} />
        <span>{person.label}{person.userId === session.user.id && person.label !== t("searchDialog:filters.me")
          ? <SelfMark /> : null}</span></span>,
      run: () => onChange({ ...filters, createdBy: [...filters.createdBy, person] }),
    }));
  } else if (menu?.kind === "teamspace") {
    // As Notion's teamspace filter (seen 2026-10-14): each with its mark, the
    // chosen ones in the search above and out of the list (as its search's
    // pages and the trash's menus, seen 2026-11-10), and "제거하기" under a line.
    // A teamspace's mark is its own icon, small in a 20px place, its name 8px
    // after it, as Notion's (seen 2026-11-14).
    const folded = foldNfcText(menuQuery.trim());
    choices = [
      ...teamspaces.filter((teamspace) => !filters.teamspaces.includes(teamspace.id) && (!folded || foldNfcText(teamspace.name).includes(folded)))
        .map((teamspace) => ({
          key: teamspace.id, text: teamspace.name, role: "menuitem" as const,
          run: () => onChange({ ...filters, teamspaces: [...filters.teamspaces, teamspace.id] }),
          label: <span className={menuStyles.filterMenuMarked}>
            <span className={menuStyles.filterMenuMark}><span className={menuStyles.filterMenuTeamspace}>
              {teamspaceGlyph({ icon: teamspace.icon ?? "", name: teamspace.name })}</span></span><span>{teamspace.name}</span></span>,
          data: { "data-search-filter-choice": "teamspace" },
        })),
      { key: "remove", label: t("searchDialog:filters.remove"), text: t("searchDialog:filters.remove"), role: "menuitem",
        data: { "data-search-filter-remove": "teamspace", "data-divided": "true" }, run: () => remove("teamspace") },
    ];
  } else if (menu?.kind === "in") {
    // Each page with its icon and where it sits under its title, as Notion's
    // (seen 2026-10-12); one chosen goes into the search above as a token and
    // leaves the list (seen 2026-11-10).
    choices = pageChoices.filter((page) => !filters.inPages.includes(page.id)).map((page) => {
      const location = pagePlace(page, pagesById);
      return { key: page.id, text: pageNames.title(page), role: "menuitem" as const,
        run: () => onChange({ ...filters, inPages: [...filters.inPages, page.id] }),
        label: <span className={menuStyles.filterMenuMarked}><PageIconGlyph page={page} size={20} />
          <span className={menuStyles.filterMenuPage}><span><PageTitleText page={page} /></span>{location.length ? <small><PlaceLine place={location} /></small> : null}</span></span> };
    });
  } else if (menu?.kind === "contentState") {
    // As Notion's (seen 2026-11-14): 260px, the chosen one with ✓, "제거하기" under a line.
    choices = [
      ...CONTENT_STATES.map((state) => ({ ...radio(state, t(`searchDialog:contentState.${state}`), contentState === state,
        () => { onContentState(state); setMenu(null); }), data: { "data-search-content-state": state } })),
      { key: "remove", label: t("searchDialog:filters.remove"), text: t("searchDialog:filters.remove"), role: "menuitem",
        data: { "data-search-filter-remove": "contentState", "data-divided": "true" }, run: () => remove("contentState") },
    ];
  } else if (menu?.kind === "add") {
    choices = addable.map((kind) => {
      const text = kind === "date" ? t("searchDialog:filters.date") : kind === "teamspace" ? t("searchDialog:filters.teamspace")
        : t("searchDialog:contentState.label");
      return { key: kind, label: <>{FILTER_MARKS[kind]}{text}</>, text, role: "menuitem", data: { "data-search-add-filter": kind },
        run: () => add(kind) };
    });
  }
  const searchRef = useRef<HTMLInputElement>(null);
  const search = menu?.kind === "createdBy" ? t("searchDialog:filters.searchPeople")
    : menu?.kind === "in" ? t("searchDialog:filters.searchPages")
      : menu?.kind === "teamspace" ? t("searchDialog:filters.searchTeamspaces") : null;
  // The teamspaces, people or pages chosen, which the search of their menu holds.
  const tokens = menu?.kind === "teamspace"
    ? filters.teamspaces.map((id) => ({ id, name: teamspaces.find((teamspace) => teamspace.id === id)?.name ?? id }))
    : menu?.kind === "createdBy" ? filters.createdBy.map((person) => ({ id: person.userId, name: person.label }))
      : menu?.kind === "in" ? filters.inPages.map((id) => ({ id, name: pageName(id) })) : [];
  const unchoose = (ids: string[]) => onChange(menu?.kind === "teamspace"
    ? { ...filters, teamspaces: filters.teamspaces.filter((id) => !ids.includes(id)) }
    : menu?.kind === "createdBy" ? { ...filters, createdBy: filters.createdBy.filter((person) => !ids.includes(person.userId)) }
      : { ...filters, inPages: filters.inPages.filter((id) => !ids.includes(id)) });
  const picker = useNativeMenu({ items: choices, open: Boolean(menu), menuRef,
    label: !menu ? "" : menu.kind === "contentState" ? t("searchDialog:contentState.label")
      : menu.kind === "add" ? t("searchDialog:filters.add") : t(`searchDialog:filters.${menu.kind}`),
    keyOf: (choice) => choice.key, textOf: (choice) => choice.text, onAction: (choice) => choice.run(),
    // The content state's keys start at its first row whatever is chosen, as Notion's (seen 2026-11-14);
    // "검색 범위" opens lighting its first row, the page on screen (Notion's, seen 2026-11-16).
    firstKey: menu?.kind === "contentState" || menu?.kind === "in" ? choices[0]?.key : choices.find((choice) => choice.checked)?.key,
    focusOnOpen: search === null, highlightOnOpen: menu?.kind === "in" || undefined,
    focusOwnerRef: search === null ? undefined : searchRef });

  function menuBody() {
    return <>
      {search !== null && (menu?.kind === "teamspace" || menu?.kind === "createdBy" || menu?.kind === "in" ? (
        <div className={menuStyles.filterMenuTokens} data-search-filter-tokens={menu.kind}>
          {tokens.map((token) => (
            <span key={token.id} className={menuStyles.filterMenuToken}>
              {menu.kind === "createdBy" ? <PersonAvatar userId={token.id} name={token.name} size={16} />
                : menu.kind === "in" ? (pageOf(token.id) ? <PageIconGlyph page={pageOf(token.id)!} size={16} /> : null)
                  : FILTER_MARKS[menu.kind]}<span>{token.name}</span>
              <button type="button" aria-label={t("searchDialog:filters.unchoose", { name: token.name })}
                onClick={() => unchoose([token.id])}><X size="micro" aria-hidden="true" /></button>
            </span>
          ))}
          <input ref={searchRef} value={menuQuery} autoFocus={fieldFocusOnOpen()} aria-label={search} placeholder={tokens.length ? "" : search}
            {...picker.ownerProps} onKeyDown={picker.ownerProps.onKeyDown} onChange={(event) => setMenuQuery(event.target.value)} />
          {tokens.length > 0 && <button type="button" className={menuStyles.filterMenuClear} aria-label={t("searchDialog:filters.clearChosen")}
            onClick={() => unchoose(tokens.map((token) => token.id))}><XCircleIcon size="small" aria-hidden="true" /></button>}
        </div>
      ) : <input ref={searchRef} className={menuStyles.filterMenuInput} value={menuQuery} autoFocus={fieldFocusOnOpen()}
        aria-label={search} placeholder={search} {...picker.ownerProps} onKeyDown={picker.ownerProps.onKeyDown}
        onChange={(event) => setMenuQuery(event.target.value)} />)}
      <FilterMenuList>{choices.map((choice) => (
        <NativeMenuItem key={choice.key} state={picker.state} itemKey={choice.key} role={choice.role}
          aria-checked={choice.role === "menuitem" ? undefined : Boolean(choice.checked)} className={menuStyles.filterMenuItem}
          data-active={choice.checked ? "true" : undefined} {...choice.data}>
          {({ labelProps }) => <>
            {choice.role === "menuitem" ? null
              : <span className={menuStyles.filterMenuCheck} aria-hidden="true">{choice.checked ? <CheckIcon size="small" /> : null}</span>}
            <span {...labelProps} className={menuStyles.filterMenuLabel}>{choice.label}</span>
          </>}
        </NativeMenuItem>
      ))}
      {menu?.kind === "in" && !choices.length && menuQuery.trim()
        ? <div className={menuStyles.filterMenuEmpty} role="status">{t("searchDialog:filters.noPages")}</div> : null}
      {menu?.kind === "date" ? <SearchDateFilter date={filters.date} onChange={(date) => onChange({ ...filters, date })} /> : null}
      </FilterMenuList>
    </>;
  }

  return (
    <div ref={rootRef} className={menuStyles.filterBar}>
      <div className={styles.filters} role="group" aria-label={t("searchDialog:filters.label")}
        onKeyDown={layer.onKeyDown}>
        {chip("titleOnly", t("searchDialog:filters.titleOnly"), filters.titleOnly,
          () => onChange({ ...filters, titleOnly: !filters.titleOnly }))}
        {chip("createdBy", counted(t("searchDialog:filters.createdBy"), filters.createdBy.map((person) => person.label)),
          filters.createdBy.length > 0, (button) => open("createdBy", button))}
        {chip("in", filters.inPages.length === 0 ? t("searchDialog:filters.inEmpty") : counted(t("searchDialog:filters.in"),
          filters.inPages.map(pageName)),
        filters.inPages.length > 0, (button) => open("in", button))}
        {dateShown && chip("date", dateLabel, filters.date.preset !== "any", (button) => open("date", button))}
        {teamspaceShown && chip("teamspace", counted(t("searchDialog:filters.teamspace"),
          filters.teamspaces.map((id) => teamspaces.find((teamspace) => teamspace.id === id)?.name ?? id)),
        filters.teamspaces.length > 0, (button) => open("teamspace", button))}
        {contentStateShown && chip("contentState", t("searchDialog:contentState.label"), contentState !== "active",
          (button) => open("contentState", button))}
        {addable.length > 0 && chip("add", t("searchDialog:filters.add"), false, (button) => open("add", button))}
      </div>
      {menu && <div {...picker.menuProps} ref={menuRef} className={menuStyles.filterMenu} data-search-filter-menu={menu.kind} style={{ left: menu.left, top: menu.foot + menuGap(menu.kind), zIndex: layer.zIndex }}
        onKeyDown={(event) => { picker.menuProps.onKeyDown(event); layer.onKeyDown(event); }}>
        {menuBody()}
      </div>}
    </div>
  );
}
