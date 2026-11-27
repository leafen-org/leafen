import { placeBeside, popoverAnchorOf, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { appText } from "@/components/admin/administratorAppsApi";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CheckIcon, CheckboxIcon, ComposeIcon, DragHandleIcon, FileText, LibraryIcon, Plus, ServerAppsIcon, Settings, TableIcon, X } from "@/icons/leafen";
import { pageNamesNow } from "@/lib/pageNames";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useDragReorder } from "@/lib/useDragReorder";
import type { Page, Session } from "@/types";
import { workspaceMembershipBounds, type SidebarLayoutShortcut, type SidebarShortcutKind } from "@/workspaceMembershipWire.generated";
import { SidebarSubmenuRow, type SidebarMenuRow } from "./SidebarActionMenu";
import { useDatabaseShortcutTitles } from "./SidebarDatabaseShortcut";
import { SidebarPagePicker } from "./SidebarPagePicker";
import { SidebarShortcutAhead } from "./sidebarViewNewRow";
import { sidebarItemName } from "./sidebarLayoutModel";
import { styles } from "./sidebarStyles";
import { pageMapOf } from "@/lib/pageIndex";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

type Labels = (key: string, options?: Record<string, unknown>) => string;

/** A shortcut's words: its own name, or what it does; a database's new page names its database once read. */
export function sidebarShortcutLabel(shortcut: SidebarLayoutShortcut, page: Page | undefined, t: Labels, databaseTitle?: string) {
  if (shortcut.name.trim()) return shortcut.name.trim();
  return sidebarShortcutDefaultLabel(shortcut, page, t, databaseTitle);
}

/** What a shortcut does, the name it has until it is given one. */
function sidebarShortcutDefaultLabel(shortcut: SidebarLayoutShortcut, page: Page | undefined, t: Labels, databaseTitle?: string) {
  switch (shortcut.kind) {
    case "new_page":
      return shortcut.pageId ? t("shortcutNewPageIn", { title: pageNamesNow.listed(page) }) : t("newPage");
    case "new_database_page":
      return t("shortcutNewPageIn", { title: databaseTitle ?? "…" });
    case "new_database":
      return t("newDatabase");
    case "page":
      return pageNamesNow.listed(page);
    case "settings":
      return t("shortcutSettings");
    case "library":
      return t("library");
    case "documents":
      return t("shortcutDocuments");
    case "app":
      return shortcut.pageId;
    default:
      return t("myTasks");
  }
}

export function SidebarShortcutIcon({ shortcut, page }: { shortcut: SidebarLayoutShortcut; page: Page | undefined }) {
  // A page the reader can't open (a workspace preset's) keeps a page's icon, not a new page's "+".
  if (shortcut.kind === "page") return page ? <PageIconGlyph page={page} size={18} /> : <FileText size="regular" aria-hidden="true" />;
  if (shortcut.kind === "new_database") return <TableIcon size="regular" aria-hidden="true" />;
  // A new page's, in a page or a database, is Notion's ✎ (seen 2026-10-30).
  if (shortcut.kind === "new_database_page" || shortcut.kind === "new_page") return <ComposeIcon size="regular" aria-hidden="true" />;
  if (shortcut.kind === "settings") return <Settings size="regular" aria-hidden="true" />;
  if (shortcut.kind === "library") return <LibraryIcon size="regular" aria-hidden="true" />;
  if (shortcut.kind === "documents") return <FileText size="regular" aria-hidden="true" />;
  if (shortcut.kind === "my_tasks") return <CheckboxIcon size="regular" aria-hidden="true" />;
  if (shortcut.kind === "app") return <ServerAppsIcon size="regular" aria-hidden="true" />;
  return <Plus size="regular" aria-hidden="true" />;
}

/**
 * "단축키 이름 변경" turns the shortcut's row into its name's field, as
 * Notion's does (seen 2026-10-02): the name it has until renamed as the
 * field's placeholder and a ✓ at the end. Enter, ✓, Escape or leaving the
 * field keeps the name typed, and an empty name gives the shortcut back what
 * it does. The field opens with the whole name chosen, a letter typing over
 * it: Notion's tab "이름 변경", measured 2026-11-16 in Chrome at 1440 with no
 * script before the press, twice in two tabs ("z" and "Z" replaced the name
 * and Escape kept them), as its page rename box keeps what was typed on
 * Escape (the same day) and so every rename field here does.
 */
function ShortcutNameField({ placeholder, name, onDone }: {
  placeholder: string;
  name: string;
  onDone: (name: string | null) => void;
}) {
  const { t } = useTranslation("sidebar");
  const input = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);
  const finish = (value: string | null) => {
    if (done.current) return;
    done.current = true;
    onDone(value === null ? null : sidebarItemName(value));
  };
  return (
    <span className={styles.shortcutNameField} data-sidebar-shortcut-name-field>
      <input ref={input} defaultValue={name} placeholder={placeholder} aria-label={t("shortcutRename")}
        onKeyDown={(event) => {
          if (imeComposing(event)) return;
          if (fieldEnter(event)) {
            event.preventDefault();
            finish(event.currentTarget.value);
          } else if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            finish(event.currentTarget.value);
          }
        }}
        onBlur={(event) => finish(event.currentTarget.value)} />
      <button type="button" className={styles.sectionAction} aria-label={t("shortcutRenameDone")}
        onMouseDown={(event) => event.preventDefault()} onClick={() => finish(input.current?.value ?? "")}>
        <CheckIcon size="small" aria-hidden="true" />
      </button>
    </span>
  );
}

export type SidebarShortcutMenuRender = (shortcut: SidebarLayoutShortcut, label: string, rename: () => void, databaseTitle?: string) => ReactNode;

/**
 * A tab's shortcuts at the top of its list, as Notion's (seen 2026-10-01 and
 * 2026-10-02): 30px rows of 14px words, a new page's in gray, each doing what
 * it names, with the shortcut's "…" at the end of the row under the pointer.
 */
export function SidebarShortcuts({ shortcuts, pages, session, onOpen, menu, onRename }: {
  shortcuts: readonly SidebarLayoutShortcut[];
  pages: readonly Page[];
  session: Session;
  onOpen: (shortcut: SidebarLayoutShortcut) => void;
  /** The shortcut's "…" menu. */
  menu: SidebarShortcutMenuRender;
  onRename: (key: string, name: string) => void;
}) {
  const { t } = useTranslation("sidebar");
  const [renaming, setRenaming] = useState<string | null>(null);
  const titles = useDatabaseShortcutTitles(session, shortcuts);
  if (shortcuts.length === 0) return null;
  const byId = pageMapOf(pages);
  return (
    <div className={styles.shortcutList} data-sidebar-shortcuts>
      {/* A database's new page reads its database ahead, so the press makes it at once. */}
      {shortcuts.filter((shortcut) => shortcut.kind === "new_database_page").map((shortcut) => (
        <SidebarShortcutAhead key={`ahead:${shortcut.key}`} session={session} shortcut={shortcut} />
      ))}
      {shortcuts.map((shortcut) => {
        const page = shortcut.pageId ? byId.get(shortcut.pageId) : undefined;
        const label = sidebarShortcutLabel(shortcut, page, t, titles.get(shortcut.key));
        return (
          <div key={shortcut.key} className={styles.shortcutItem} data-renaming={renaming === shortcut.key ? "true" : undefined}>
            {renaming === shortcut.key ? (
              <span className={styles.shortcutRow}>
                <span className={styles.viewRowIcon}><SidebarShortcutIcon shortcut={shortcut} page={page} /></span>
                <ShortcutNameField placeholder={sidebarShortcutDefaultLabel(shortcut, page, t, titles.get(shortcut.key))} name={shortcut.name}
                  onDone={(name) => {
                    setRenaming(null);
                    if (name !== null && name !== shortcut.name) onRename(shortcut.key, name);
                  }} />
              </span>
            ) : (
              <>
                <button type="button" className={styles.shortcutRow} data-sidebar-shortcut={shortcut.kind}
                  onClick={() => onOpen(shortcut)}>
                  <span className={styles.viewRowIcon}><SidebarShortcutIcon shortcut={shortcut} page={page} /></span>
                  <span className={styles.viewRowTitle}>{label}</span>
                </button>
                <span className={styles.shortcutItemActions}>{menu(shortcut, label, () => setRenaming(shortcut.key), titles.get(shortcut.key))}</span>
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}

const ADDED_AT_ONCE: readonly SidebarShortcutKind[] = ["settings", "library", "documents", "my_tasks"];

/**
 * The customize mode's shortcuts, as Notion's (seen 2026-10-01, 2026-10-02
 * and 2026-10-31): the tab's shortcuts with "단축키 메뉴 열기" and "사이드바에서
 * 제거", then "단축키 추가", whose menu beside it offers "만들기" — 새 페이지 ›,
 * which asks where, and 새 데이터베이스, added at once — and
 * "이동:" — 페이지 ›, which opens which, 설정, 라이브러리, Leafen's 문서 — the
 * library's documents — and 내 작업. Notion's chats and chat threads
 *, meeting notes, skills, calendar and
 * marketplace are left out. Each shortcut drags by its ⠿ into order (Notion,
 * seen 2026-11-09), with its ⚙ menu and × under the pointer; `mark` adds what
 * a preset's editor says of it (its ⚠).
 */
export function SidebarShortcutsEditor({ shortcuts, pages, session, workspaceId, canAdd, onAdd, onRemove, onMove, menu, onRename, mark }: {
  shortcuts: readonly SidebarLayoutShortcut[];
  pages: readonly Page[];
  session: Session;
  workspaceId: string;
  canAdd: boolean;
  onAdd: (kind: SidebarShortcutKind, pageId: string, name?: string) => void;
  onRemove: (key: string) => void;
  /** Puts a dragged shortcut before another, or last. */
  onMove: (key: string, before: string | null) => void;
  menu: SidebarShortcutMenuRender;
  onRename: (key: string, name: string) => void;
  mark?: (shortcut: SidebarLayoutShortcut) => ReactNode;
}) {
  const { t, i18n } = useTranslation("sidebar");
  const addRef = useRef<HTMLButtonElement>(null);
  const [open, setOpenState] = useState(false);
  const { apps } = useUsableAppList(session);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const setOpen = (next: boolean) => {
    setOpenState(next);
    setQuery("");
  };
  const [renaming, setRenaming] = useState<string | null>(null);
  const titles = useDatabaseShortcutTitles(session, shortcuts);
  const byId = pageMapOf(pages);
  // A shortcut lands on the side of the row it is dropped on.
  const reorder = useDragReorder("sidebar-customize-shortcut", (dragged, target, side) => {
    const rest = shortcuts.filter((shortcut) => shortcut.key !== dragged).map((shortcut) => shortcut.key);
    onMove(dragged, rest[rest.indexOf(target) + (side === "after" ? 1 : 0)] ?? null);
  });
  type AddChoice = SidebarMenuRow & { group: string };
  const atOnce = (kind: SidebarShortcutKind, group: string): AddChoice => {
    const shortcut: SidebarLayoutShortcut = { key: kind, kind, pageId: "", name: "" };
    return { key: kind, label: sidebarShortcutLabel(shortcut, undefined, t), run: () => { setOpen(false); onAdd(kind, ""); },
      icon: <SidebarShortcutIcon shortcut={shortcut} page={undefined} />, group, data: { "data-sidebar-shortcut-add": kind } };
  };
  // "새 페이지 ›" and "페이지 ›" open their page list beside the row, or
  // under it where the window has no room beside, as Notion's (placeSidebarPickerByRow).
  const pageChoice = (kind: "new_page" | "page", label: string, icon: ReactNode, group: string): AddChoice => ({
    key: kind, label, icon, group, data: { "data-sidebar-shortcut-add": kind }, submenu: { label, custom: ({ trigger, back, byKeys }) => (
      <SidebarPagePicker anchor={popoverAnchorOf(trigger.current!)} underRow highlightFirst={false} pages={pages} session={session} workspaceId={workspaceId}
        opening={{ byKeys, search: kind === "new_page" ? "shortcutNewPage" : "shortcutPage" }}
        placeholder={t(kind === "new_page" ? "shortcutNewPagePlaceholder" : "shortcutPagePlaceholder")}
        rootLabel={kind === "new_page" ? t("shortcutPrivateRoot") : undefined}
        rootIcon={<PersonAvatar userId={session.user.id} name={session.user.name} size={20} />}
        onClose={back}
        onChoose={(pageId) => {
          setOpen(false);
          if (kind === "page" && !pageId) return;
          onAdd(kind, pageId);
        }} />
    ) },
  });
  const allChoices: AddChoice[] = [
    pageChoice("new_page", t("newPage"), <ComposeIcon size="regular" aria-hidden="true" />, t("shortcutCreate")),
    atOnce("new_database", t("shortcutCreate")),
    pageChoice("page", t("shortcutPage"), <FileText size="regular" aria-hidden="true" />, t("shortcutGoTo")),
    ...ADDED_AT_ONCE.map((kind) => atOnce(kind, t("shortcutGoTo"))),
    // The apps the person may use (the app platform's decision, 5).
    ...apps.filter((app, index) => app.fullPage && apps.findIndex((other) => other.slug === app.slug) === index).map((app): AddChoice => {
      const name = appText(app.name, i18n.language);
      return { key: `app:${app.slug}`, label: name, group: t("shortcutGoTo"), data: { "data-sidebar-shortcut-add": `app:${app.slug}` },
        icon: <ServerAppsIcon size="regular" aria-hidden="true" />, run: () => { setOpen(false); onAdd("app", app.slug, name); } };
    }),
  ];
  // The menu's search finds a shortcut by name, as Notion's does (seen 2026-10-08).
  const needle = query.trim().toLocaleLowerCase();
  const addChoices = needle ? allChoices.filter((choice) => choice.label.toLocaleLowerCase().includes(needle)) : allChoices;
  const menuRef = useRef<HTMLDivElement>(null), shadeRef = useRef<HTMLButtonElement>(null);
  // Its shade is its own: a press outside a page list beside it closes it too (sidebarPickerBesideRow).
  const layer = useOverlayLayer(open, { kind: "menu", floats: false, ref: menuRef, onEscape: () => setOpen(false), inside: () => [addRef.current],
    modalOwned: () => [shadeRef.current], returnFocusRef: addRef });
  // It opens beside the button, out of the sidebar, as "섹션 추가"'s.
  const place = usePopoverPlacement(menuRef, open
    ? (size, viewport) => placeBeside(popoverRectOf(addRef.current)!, size, viewport, { gap: 4 }) : null, [open], undefined, false);
  const picker = useNativeMenu({ items: addChoices, open, menuRef, label: t("addShortcut"),
    focusOnOpen: true, highlightOnOpen: true, focusOwnerRef: searchRef, // its first row lit as it opens, as "섹션 추가"'s
    keyOf: (choice) => choice.key, textOf: (choice) => choice.label, onAction: (choice) => choice.run?.() });
  return (
    <div className={styles.customizeAddWrap}>
      {shortcuts.map((shortcut) => {
        const page = shortcut.pageId ? byId.get(shortcut.pageId) : undefined;
        const label = sidebarShortcutLabel(shortcut, page, t, titles.get(shortcut.key));
        const { "data-drop-side": dropSide, ...row } = reorder.row(shortcut.key, () => true, renaming !== shortcut.key);
        return (
          <div key={shortcut.key} className={styles.customizeRow} data-sidebar-customize-shortcut={shortcut.kind}
            data-renaming={renaming === shortcut.key ? "true" : undefined} {...row} data-drop={dropSide}>
            <span className={styles.customizeHandle} aria-hidden="true"><DragHandleIcon size="small" /></span>
            <span className={styles.viewRowIcon}><SidebarShortcutIcon shortcut={shortcut} page={page} /></span>
            {renaming === shortcut.key ? (
              <ShortcutNameField placeholder={sidebarShortcutDefaultLabel(shortcut, page, t, titles.get(shortcut.key))} name={shortcut.name}
                onDone={(name) => {
                  setRenaming(null);
                  if (name !== null && name !== shortcut.name) onRename(shortcut.key, name);
                }} />
            ) : (
              <>
                <span className={styles.customizeName}>{label}</span>
                {mark?.(shortcut)}
                <span className={styles.customizeActions}>
                  {menu(shortcut, label, () => setRenaming(shortcut.key), titles.get(shortcut.key))}
                  <button type="button" className={styles.sectionAction} data-sidebar-shortcut-remove={shortcut.key}
                    aria-label={t("removeFromSidebar")} title={t("removeFromSidebar")} onClick={() => onRemove(shortcut.key)}>
                    <X size="small" aria-hidden="true" />
                  </button>
                </span>
              </>
            )}
          </div>
        );
      })}
      <button ref={addRef} type="button" className={styles.customizeAdd} data-sidebar-shortcut-add-open aria-haspopup="menu"
        aria-expanded={open} disabled={!canAdd} onClick={() => setOpen(!open)}>
        <Plus size="small" aria-hidden="true" />
        <span>{t("addShortcut")}</span>
      </button>
      {!canAdd ? (
        <p className={styles.customizeNote} data-sidebar-shortcuts-full>
          {t("shortcutsFull", { limit: workspaceMembershipBounds.sidebarShortcutsPerTab })}
        </p>
      ) : null}
      {open ? (
        <button ref={shadeRef} type="button" className={styles.moreMenuBackdrop} tabIndex={-1} aria-label={t("addShortcut")}
          onClick={() => setOpen(false)} />
      ) : null}
      {open ? createPortal(
        <div {...picker.menuProps} ref={menuRef} className={`${styles.customizeMenu} ${styles.sectionAddMenu}`} data-ringless="true"
          style={{ ...popoverPlacementStyle(place), zIndex: layer.zIndex }}
          onKeyDown={(event) => { picker.menuProps.onKeyDown(event); layer.onKeyDown(event); }}>
          <label className={styles.viewChoiceSearch}>
            <input ref={searchRef} value={query} placeholder={t("searchShortcuts")} aria-label={t("searchShortcuts")}
              data-sidebar-shortcut-add-search {...picker.ownerProps} onKeyDown={picker.ownerProps.onKeyDown}
              onChange={(event) => setQuery(event.target.value)} />
          </label>
          {addChoices.length === 0 ? <p className={styles.customizeEmpty}>{t("noMatchingShortcuts")}</p> : null}
          {addChoices.map((choice, index) => (
            <Fragment key={choice.key}>
              {choice.group !== addChoices[index - 1]?.group ? <div className={styles.shortcutMenuLabel}>{choice.group}</div> : null}
              {choice.submenu ? <SidebarSubmenuRow row={choice} picker={picker} main={menuRef} /> : (
                <NativeMenuItem state={picker.state} itemKey={choice.key} className={styles.sectionMenuItem} {...choice.data}>
                  {({ labelProps }) => <>{choice.icon}<span {...labelProps} className={styles.sectionMenuLabel}>{choice.label}</span></>}
                </NativeMenuItem>
              )}
            </Fragment>
          ))}
        </div>, document.body,
      ) : null}
    </div>
  );
}
