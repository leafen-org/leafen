import { withPersonDistinction } from "@/lib/peopleDirectory";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ChevronDown, FileBlank, TeamspaceIcon, UserIcon, X } from "@/icons/leafen";
import { SearchClearButton } from "./PageActionSearch";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { usePageNames } from "@/lib/pageNames";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Page, Session, Workspace } from "@/types";
import { useSidebarTeamspaces } from "./sidebar/useSidebarTeamspaces";
import type { TrashFilterState, TrashPerson } from "./trashFilterModel";
import styles from "./TrashView.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { usePageScopeChoices } from "./search/usePageScopeChoices";
import { pagePlace } from "@/lib/pagePath";
import { pagesByIdOf } from "@/lib/pageIndex";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { PlaceLine } from "./PlaceLine";
import { FilterMenuList } from "./FilterMenuList";
import scopeStyles from "./TrashScopeMenu.module.css";
import { SelfMark } from "@/components/people/SelfMark";

export type TrashFilterMenu = "editors" | "in" | "teamspace";

const MENU_RESULTS = 8;

function fold(value: string) {
  return value.normalize("NFC").toLowerCase();
}

/** A filter's choice, checked as the filter has it; it toggles. A person's or a page's says which. */
export type TrashFilterChoice = { key: string; label: string; checked: boolean; toggle(): void; userId?: string; page?: Page };

type TrashFilterSource = {
  filters: TrashFilterState;
  onChange: (next: TrashFilterState) => void;
  pages: Page[];
  session: Session;
  workspace: Workspace;
};

/**
 * The choices of the trash's open filter, for its desktop menu and its phone
 * sheet alike: the people searched as they are typed (the reader first while
 * nothing is), the pages to look in, or the joined teamspaces.
 */
export function useTrashFilterChoices(kind: TrashFilterMenu | null, menuQuery: string,
  { filters, onChange, pages, session, workspace }: TrashFilterSource) {
  const pageNames = usePageNames();
  const { t } = useTranslation("trashView");
  const [people, setPeople] = useState<TrashPerson[]>([]);
  const teamspaces = useSidebarTeamspaces({ session, workspace }).joinedTeamspaces;
  const me = useMemo<TrashPerson>(() => ({ userId: session.user.id, label: t("filters.me") }), [session.user.id, t]);

  // The workspace's people are searched as they are typed, not its organization's other workspaces'.
  useEffect(() => {
    if (kind !== "editors") return;
    if (!workspace.organizationId || workspace.guest) {
      setPeople([me]);
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api.searchOrganizationPeople(session, {
        organizationId: workspace.organizationId!, query: menuQuery.trim(), limit: MENU_RESULTS, workspaceId: workspace.id,
      }, { signal: controller.signal }).then((result) => {
        const found = result.people.flatMap((person) => person.userId && person.userId !== me.userId
          ? [{ userId: person.userId, label: withPersonDistinction(person.userId, person.displayName || person.email || person.userId) }] : []);
        setPeople(menuQuery.trim() ? found : [me, ...found]);
      }, () => setPeople([me]));
    }, 150);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [kind, me, menuQuery, session, workspace.guest, workspace.organizationId]);

  // The pages to look in, as the search window's "검색 범위:" lists them (usePageScopeChoices).
  const pageChoices = usePageScopeChoices({ session, workspaceId: workspace.id, pages, query: menuQuery, open: kind === "in" });

  function toggle<T>(values: T[], value: T, same: (left: T, right: T) => boolean) {
    return values.some((item) => same(item, value)) ? values.filter((item) => !same(item, value)) : [...values, value];
  }

  // Each menu's choices, checked as the filter has them; a choice toggles
  // and leaves its menu open.
  const person = (one: TrashPerson): TrashFilterChoice => ({ key: one.userId, label: one.label, userId: one.userId,
    checked: filters.editors.some((chosen) => chosen.userId === one.userId),
    toggle: () => onChange({ ...filters, editors: toggle(filters.editors, one, (left, right) => left.userId === right.userId) }) });
  const page = (one: Page): TrashFilterChoice => ({ key: one.id, label: pageNames.title(one), page: one, checked: filters.inPages.includes(one.id),
    toggle: () => onChange({ ...filters, inPages: toggle(filters.inPages, one.id, (left, right) => left === right) }) });
  const teamspace = (one: { id: string; name: string }): TrashFilterChoice => ({ key: one.id, label: one.name,
    checked: filters.teamspaces.includes(one.id),
    toggle: () => onChange({ ...filters, teamspaces: toggle(filters.teamspaces, one.id, (left, right) => left === right) }) });
  const foldedQuery = fold(menuQuery.trim());
  const choices: TrashFilterChoice[] = kind === "editors"
    ? [...filters.editors, ...people.filter((one) => !filters.editors.some((chosen) => chosen.userId === one.userId))].map(person)
    : kind === "in" ? pageChoices.map(page)
      : kind === "teamspace" ? teamspaces.filter((one) => !foldedQuery || fold(one.name).includes(foldedQuery)).map(teamspace)
        : [];
  // What the open filter has chosen, whatever is searched: a phone's field holds them.
  const chosen: TrashFilterChoice[] = kind === "editors" ? filters.editors.map(person)
    : kind === "in" ? filters.inPages.flatMap((id) => pages.filter((one) => one.id === id).slice(0, 1)).map(page)
      : kind === "teamspace" ? teamspaces.filter((one) => filters.teamspaces.includes(one.id)).map(teamspace)
        : [];
  const clear = () => onChange(kind === "editors" ? { ...filters, editors: [] } : kind === "in" ? { ...filters, inPages: [] }
    : kind === "teamspace" ? { ...filters, teamspaces: [] } : filters);
  return { choices, chosen, clear };
}

/**
 * The trash's filters, as Notion's trash shows them (seen 2026-09-30): the
 * last editor, on for the reader at first, where to look, and the teamspace.
 */
export function TrashFilterBar(source: TrashFilterSource) {
  const { filters } = source;
  const { t } = useTranslation(["trashView", "common"]);
  const [menu, setMenu] = useState<{ kind: TrashFilterMenu; button: HTMLElement } | null>(null);
  const [menuQuery, setMenuQuery] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const { choices, chosen, clear } = useTrashFilterChoices(menu?.kind ?? null, menuQuery, source);
  // What is chosen stands in the search field; the list holds the rest, as
  // Notion's desktop trash (seen 2026-11-10) and its phone sheet do.
  const rest = choices.filter((choice) => !choice.checked);
  const reader = source.session.user.name.trim() || source.session.user.email;
  const nameOf = (choice: TrashFilterChoice) => (choice.userId === source.session.user.id ? reader : choice.label);

  // The menu opens right under its pill, its top on the pill's foot and
  // lined up with its left edge (Notion's three, seen twice 2026-11-17 at
  // 1440), and reaches past the trash's right edge where it is wider than
  // the room left there, as Notion's (seen 2026-11-10: the teamspaces' hung out of the trash);
  // "검색 범위:" no taller than 40% of the window, as Notion's (its 40vh,
  // seen 2026-11-16), the room below it written over its class's cap.
  const place = usePopoverPlacement(menuRef, menu ? (size, viewport) => placeAround(
    popoverRectOf(menu.button) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { gap: 0, ...(menu.kind === "in" ? { cap: viewport.height * 0.4 } : {}) },
  ) : null, [menu], undefined, { kind: "menu", inside: () => [rootRef.current],
    onEscape: () => setMenu(null), onOutsidePress: () => setMenu(null) });

  function open(kind: TrashFilterMenu, button: HTMLElement) {
    setMenuQuery("");
    setMenu(menu?.kind === kind ? null : { kind, button });
  }

  const searchRef = useRef<HTMLInputElement>(null);
  // Every menu is searched, the teamspaces' too, as Notion's (seen 2026-11-10).
  const searched = Boolean(menu);
  // "검색 범위:" opens lighting its first row, the page on screen, as Notion's (seen 2026-11-16).
  const scope = menu?.kind === "in";
  const picker = useNativeMenu({ items: rest, open: Boolean(menu), menuRef,
    label: t(menu?.kind === "editors" ? "filters.editedBy" : menu?.kind === "in" ? "filters.in" : "filters.teamspace"),
    keyOf: choice => choice.key, textOf: choice => choice.label, onAction: choice => choice.toggle(),
    focusOnOpen: !searched, focusOwnerRef: searched ? searchRef : undefined,
    firstKey: scope ? rest[0]?.key : undefined, highlightOnOpen: scope || undefined });
  const pagesById = useMemo<Readonly<Record<string, Page>>>(() => (scope ? pagesByIdOf(source.pages) : {}), [scope, source.pages]);
  usePathsAboveShown(scope ? rest.flatMap((choice) => (choice.page ? [choice.page] : [])) : [], pagesById);

  function menuBody() {
    const search = menu?.kind === "editors" ? t("filters.searchPeople") : menu?.kind === "in" ? t("filters.searchPages") : t("phone.searchTeamspaces");
    // Taking a chosen one out, or all, leaves the keys in the search, as Notion's (measured once 2026-11-15 in
    // Chrome 1440 with no script before the press: a letter typed after a chip's × and after ⓧ went in).
    const refocus = (run: () => void) => () => { run(); searchRef.current?.focus(); };
    const icon = (choice: TrashFilterChoice, size: number) => choice.userId
      ? <PersonAvatar userId={choice.userId} name={nameOf(choice)} size={size} />
      : choice.page ? <span className={styles.filterMenuIcon} aria-hidden="true"><PageIconGlyph page={choice.page} size={size} /></span>
        : <TeamspaceIcon pixels={size} aria-hidden="true" />;
    return <>
      <div className={styles.filterMenuField}>
        {chosen.map((choice) => (
          <span key={choice.key} className={styles.filterMenuChosen} data-trash-filter-chip={choice.key}>
            {icon(choice, 16)}
            <span>{nameOf(choice)}</span>
            <button type="button" aria-label={t("phone.removeChoice", { name: nameOf(choice) })} onClick={refocus(choice.toggle)}>
              <X size="micro" aria-hidden="true" />
            </button>
          </span>
        ))}
        <input ref={searchRef} value={menuQuery} autoFocus={fieldFocusOnOpen()} aria-label={search}
          placeholder={chosen.length ? "" : search} {...picker.ownerProps}
          onKeyDown={(event) => {
            // Backspace in an empty field takes the last chosen out.
            if (event.key === "Backspace" && !menuQuery && chosen.length) {
              event.preventDefault();
              chosen[chosen.length - 1].toggle();
              return;
            }
            picker.ownerProps.onKeyDown?.(event);
          }}
          onChange={(event) => setMenuQuery(event.target.value)} />
        {/* Notion's 16px "ⓧ" at the field's end once anything is chosen or typed (seen 2026-11-17 at 1440 with
            words alone): it takes all of it out, the keys staying in the search. */}
        <SearchClearButton value={chosen.length > 0 || menuQuery !== ""} small inputRef={searchRef} className={styles.filterMenuClear}
          label={t("common:actions.clearAll")} onClear={() => { setMenuQuery(""); if (chosen.length) clear(); }} />
      </div>
      <FilterMenuList>
      {rest.length === 0 && <div className={styles.filterMenuEmpty}>{t("phone.noResults")}</div>}
      {rest.map((choice) => (
        <NativeMenuItem key={choice.key} state={picker.state} itemKey={choice.key} role="menuitemcheckbox" aria-checked={false}
          className={`${styles.filterMenuItem} ${scopeStyles.row}`}>
          {({ labelProps }) => <>
            {icon(choice, 20)}
            {choice.page ? (() => {
              // A page with where it sits under its title, as the search window's picker.
              const where = pagePlace(choice.page, pagesById);
              return <span {...labelProps} className={scopeStyles.page}>
                <span>{nameOf(choice)}</span>{where.length ? <small><PlaceLine place={where} /></small> : null}
              </span>;
            })() : <span {...labelProps}>
              {nameOf(choice)}
              {choice.userId === source.session.user.id ? <SelfMark className={styles.filterMenuMark} text={t("phone.meMark")} /> : null}
            </span>}
          </>}
        </NativeMenuItem>
      ))}
      </FilterMenuList>
    </>;
  }

  // Each pill leads with its icon and ends with ⌄, as Notion's (seen 2026-10-11). Notion draws the
  // person and the page 20px tall in boxes as wide as their ink (12.6, 11.8; ink 15.2 high), its
  // teamspace building 20 tall in an 18 wide box and ⌄ 14 tall in an 8.6 box (seen 2026-11-15 and
  // 2026-11-16, the svgs and their paths' boxes): the same sizes here, trimmed to Notion's widths.
  function chip(kind: TrashFilterMenu, label: string, active: boolean) {
    const Icon = kind === "editors" ? UserIcon : kind === "in" ? FileBlank : TeamspaceIcon;
    return (
      <button type="button" className={styles.filterChip} data-active={active ? "true" : undefined} data-trash-filter={kind}
        aria-haspopup="menu" aria-expanded={menu?.kind === kind} onClick={(event) => open(kind, event.currentTarget)}>
        {kind === "teamspace" ? <Icon size="regular" trim aria-hidden="true" />
          : <Icon size="regular" trim glyphWidth={kind === "editors" ? 10.08 : 9.44} aria-hidden="true" />}
        <span>{label}</span>
        <ChevronDown size="tiny" trim glyphWidth={9.83} aria-hidden="true" />
      </button>
    );
  }

  return (
    <div ref={rootRef} className={styles.filterBar} role="group" aria-label={t("filters.label")}>
      {chip("editors", t("filters.editedBy"), filters.editors.length > 0)}
      {chip("in", t("filters.in"), filters.inPages.length > 0)}
      {/* Shown with no teamspace joined too, as Notion's (seen 2026-11-10). */}
      {chip("teamspace", t("filters.teamspace"), filters.teamspaces.length > 0)}
      {menu && (
        <div {...picker.menuProps} ref={menuRef} className={`${styles.filterMenu} ${scopeStyles.menu}`} data-trash-filter-menu={menu.kind}
          style={popoverPlacementStyle(place)}>
          {menuBody()}
        </div>
      )}
    </div>
  );
}
