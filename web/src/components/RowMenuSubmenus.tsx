import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { WorkspaceApp } from "@/appRegistrationWire.generated";
import { CheckIcon, ChevronRight } from "@/icons/leafen";
import { NativeActionMenu, NativeActionMenuItem, NativeActionMenuKeysLead, NativeActionMenuOpening } from "@/lib/nativeActionMenu";
import { keyCameLast } from "@/lib/lastPress";
import { pointerMoved } from "@/lib/pointerHover";
import { besideRowOffset } from "@/lib/besideRowOffset";
import { isTextField } from "@/lib/menuKeyboard";
import { noteBesideList, trackPointer } from "@/lib/submenuHoverHold";
import { useBesideListLeaving } from "@/lib/besideListLeaving";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { useEdgeFadeAsSized } from "@/lib/edgeFade";
import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { publicMutationService } from "@/lib/publicMutationService";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { PageNotificationLevel } from "@/notificationSettingsApi";
import type { PublicMutationAppConnections, PublicMutationNotionConnection } from "@/publicMutationWire.generated";
import type { Page, Session } from "@/types";
import { listWorkspaceApps } from "./settings/WorkspaceAppAccounts";
import styles from "./RowMenu.module.css";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A page menu row whose choices open beside the menu, as Notion's "알림 받기 ›"
 * and "연결 ›" (seen 2026-10-07): its mark, name, the value now in gray and ›.
 * The page menu draws the choices, outside its own list.
 */
export function RowMenuSubmenuRow({ kind, icon, label, value, open, disabled, onOpen, data }: {
  /** Names the choices for the pointer, which opens them (none for a row that only acts when chosen). */
  kind?: string;
  icon: ReactNode;
  label: string;
  value: string;
  open: boolean;
  disabled: boolean;
  onOpen: (row: HTMLElement) => void;
  data: Record<string, string>;
}) {
  const row = useRef<HTMLElement | null>(null);
  // → (← reading from the right) on the lit row opens its choices, as
  // Notion's "테마와 레이아웃 ›" (seen 2026-11-16 at 1440: ↓ to the row, →
  // opened it, "기본" ringed) and the "인라인 댓글 ›" in that list (seen
  // 2026-11-18 at 1440 in two tabs, judged by the screen: → and Enter on the
  // ringed row each opened its choices with the checked "기본" ringed); the
  // menu's search hands the key on to a row that says it opens a menu
  // (forwardMenuItemKey). It says so on the element itself: told to Aria's
  // menu item, the row would be one of Aria's own submenu triggers, which
  // Enter and a press leave unchosen. Bound as the element is drawn, not
  // after this row's own pass: a list beside the menu draws its rows a pass
  // after their collection, and is not drawn again while the keys walk it
  // (2026-11-18: "인라인 댓글 ›" said nothing, and → did nothing there).
  const latest = useRef({ disabled, onOpen }); latest.current = { disabled, onOpen };
  const unbind = useRef<(() => void) | null>(null);
  const bind = useCallback((element: HTMLElement | null) => {
    unbind.current?.(); unbind.current = null;
    row.current = element;
    if (!element) return;
    element.setAttribute("aria-haspopup", "menu");
    // Its choices give the focus back to it as they close, and its menu lights it again (NativeActionMenu).
    if (!element.hasAttribute("tabindex")) element.tabIndex = -1;
    const key = (event: KeyboardEvent) => {
      if (event.key !== inlineEndKey(element) || latest.current.disabled) return;
      event.preventDefault();
      latest.current.onOpen(element);
    };
    element.addEventListener("keydown", key);
    unbind.current = () => element.removeEventListener("keydown", key);
  }, []);
  return (
    <NativeActionMenuItem type="button" className={styles.item} data-menu-item role="menuitem" aria-expanded={open}
      data-page-submenu-row={kind} disabled={disabled} actionRef={bind} onClick={() => { if (row.current) onOpen(row.current); }} {...data}>
      {icon}
      <span>{label}</span>
      {value ? <span className={styles.itemValue}>{value}</span> : null}
      <ChevronRight className={styles.itemChevron} size="small" aria-hidden="true" />
    </NativeActionMenuItem>
  );
}

/**
 * Marks a row whose list beside it is open with the keys left in the row's
 * own list: ← there closes that list, not the row's list (RowMenuBesideSubmenu).
 */
export const BESIDE_KEEPS_KEYS = "data-beside-keeps-keys";

/** The row the keys are on in a list: the row itself, or the one its focus owner names. */
function litRowOf(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return null;
  if (target.hasAttribute("data-native-menu-item")) return target;
  const id = target.getAttribute("aria-activedescendant");
  return id ? target.ownerDocument.getElementById(id) : null;
}

type BesideSubmenuProps = {
  row: HTMLElement;
  width: number;
  label: string;
  onClose: () => void;
  /** Takes the keys at its first choice as it opens; one a pointer opens in passing leaves them. */
  focusFirst?: boolean;
  /** Where the focus it took goes back as it closes, where not to its row. */
  returnTo?: HTMLElement;
  /** The choices are a menu of their own, which names itself. */
  ownMenu?: boolean;
  children: ReactNode;
};

// Each row's list is an opening of its own: another row's list drawn in the same place (a table row's "속성
// 편집 ›" left for "다음에서 열기 ›") opens anew — its first row lit, the focus in it, the one left fading —
// rather than redrawing the list open there, whose focused row went with its rows (2026-11-27: the focus fell
// to the page, the new list unlit, as Notion's lights it).
const rowKeys = new WeakMap<HTMLElement, number>();
let lastRowKey = 0;
function rowKeyOf(row: HTMLElement) {
  let key = rowKeys.get(row);
  if (key === undefined) { key = ++lastRowKey; rowKeys.set(row, key); }
  return key;
}
/** The choices beside a row, over the window, closing on Escape, or with its menu on a press outside both. */
export function RowMenuBesideSubmenu(props: BesideSubmenuProps) {
  return <BesideSubmenu key={rowKeyOf(props.row)} {...props} />;
}

function BesideSubmenu({ row, width, label, onClose, ownMenu = false, focusFirst = true, returnTo, children }: BesideSubmenuProps) {
  const ref = useRef<HTMLDivElement>(null);
  // Beside the list the row is in: the menu, or the rows beside it.
  const menu = row.closest<HTMLElement>("[data-page-submenu], [data-row-menu-variant]") ?? row;
  const place = usePopoverPlacement(ref, (size, viewport) => {
    const beside = popoverRectOf(menu)!;
    const at = popoverRectOf(row)!;
    // Around its row's middle, 4px over the menu, as Notion's (seen 2026-11-05);
    // from the row's top or foot where that would leave the window (besideRowOffset).
    return placeBeside({ ...beside, top: at.top, bottom: at.bottom }, size, viewport,
      { gap: -4, offsetTop: besideRowOffset(at, size, viewport), prefer: wentTo(menu) });
  // Closed, the focus goes back to its row, which its menu lights again (NativeActionMenu), ringed or
  // shaded as the keys or the pointer led before an Escape (menuInputLead.ts). A press outside it and
  // its menu closes both (closesHost), and that menu answers the pointer through the page's shield, as
  // Notion's: pointed at, another of its rows closes it or opens that row's list, and pressed, that row
  // acts (useSubmenuHover; 2026-11-18: shielded, the page's "⋯" kept "테마와 레이아웃" open under the
  // pointer on "페이지 내용 복사하기", and a press on "더 보기" only closed it).
  }, [row], undefined, { kind: "submenu", inside: () => [row], onEscape: onClose, onOutsidePress: onClose,
    returnFocusRef: { current: returnTo ?? row }, initialFocus: focusFirst ? "first" : undefined });
  // Its choices scroll in a list inside the box, fading at the edges they run
  // on past, as Notion's (seen 2026-11-03), measured as its rows are laid out:
  // drawn again with the page under it (its rows made anew each time), it
  // reads nothing of the page then (useEdgeFadeAsSized).
  const listRef = useRef<HTMLDivElement>(null);
  useEdgeFadeAsSized(listRef);
  // Noted by its row, for the pointer on its way to it across the menu's other rows (useSubmenuHover).
  useLayoutEffect(() => {
    trackPointer();
    return ref.current ? noteBesideList(row, ref.current) : undefined;
  }, [row]);
  // Closed with its menu staying, it fades out (besideListLeaving.ts).
  useBesideListLeaving(true, () => ref.current, () => row);
  // Closed by the pointer resting on another row of its menu (useSubmenuHover), the focus it held goes to that
  // menu's own owner, its search or itself, and the row the pointer is on stays the menu's one lit row, as Notion's
  // (seen 2026-11-25 in Chrome at 1440: a page "⋯"'s "알림 받기 ›" left for "휴지통으로 이동", that row alone lit as the
  // list faded): given back to its row, the menu lit that row again beside the pointer's.
  useLayoutEffect(() => () => {
    const list = ref.current, menu = row.closest<HTMLElement>("[data-native-menu]");
    if (!list?.contains(document.activeElement) || !menu?.isConnected) return;
    // The pointer's row lit: it took the menu's light, as a row the pointer only passes on its way to the list doesn't.
    const pointed = menu.querySelector('[data-native-menu-item][data-focused="true"]:hover');
    if (pointed && pointed !== row) (menu.querySelector<HTMLElement>("[data-native-menu-search]") ?? menu).focus({ preventScroll: true });
  }, [row]);
  // It starts on its first row however it opened: ringed where a key came
  // after the last press — the keys' own → or Enter on its row, or the
  // pointer resting there after them — and shaded with no ring where the
  // press came last, as Notion's (seen 2026-11-16 at 1440 in Chrome:
  // "테마와 레이아웃 ›" by → and by Enter ringed "기본"; measured 2026-11-27
  // in Chrome at 1440, dpr 2, in two tabs at different times, after the
  // action only: a page "⋯" pressed open, the pointer resting on "더 보기 ›"
  // shaded "프레젠테이션 모드" and on "테마와 레이아웃 ›" the "기본" tile, a
  // table row's "⋮⋮" pressed, on "속성 편집 ›" "Name" and on "다음에서
  // 열기 ›" "새 탭"; a key since the press, ringed). The ring goes with the
  // focus the keys lead (an item's own focus-visible). A list of choices
  // starts on its checked one instead (RowMenuChoiceSubmenu). A list of its
  // own buttons shows the one the focus starts on shaded until the pointer
  // moves in it (`openingLit`, RowMenu.module.css .submenuItem).
  const opening = focusFirst ? "first" as const : undefined;
  const [openingLit, setOpeningLit] = useState(() => focusFirst && !ownMenu);
  const [keysLed, setKeysLed] = useState(() => keyCameLast());
  // ← (→ reading from the right) from a row of it closes it and rings its
  // row again, as Notion's (seen 2026-11-18 at 1440 in two tabs, judged by
  // the screen: ← in "인라인 댓글 ›"'s choices closed them, "인라인 댓글"
  // ringed; ← again closed "테마와 레이아웃 ›", that row ringed). Taken
  // before the list's own keys, which hand an arrow on to the lit row; a
  // text field keeps its caret keys, and a list opened from a row of this
  // one, drawn outside it, closes itself — one that left the keys here
  // too, the key going on to its row (BESIDE_KEEPS_KEYS).
  const back = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== inlineStartKey(event.currentTarget) || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey
      || imeComposing(event)) return;
    if (!(event.target instanceof Node) || !event.currentTarget.contains(event.target) || isTextField(event.target)) return;
    if (litRowOf(event.target)?.hasAttribute(BESIDE_KEEPS_KEYS)) return;
    event.preventDefault(); event.stopPropagation();
    onClose();
  };
  return createPortal(
    <div ref={ref} className={styles.submenu} role={ownMenu ? undefined : "menu"} aria-label={ownMenu ? undefined : label} data-page-submenu
      onKeyDownCapture={back} data-side={place?.side} data-opening-lit={openingLit || undefined}
      onPointerMove={openingLit ? (event) => { if (pointerMoved(event)) setOpeningLit(false); } : undefined}
      onPointerDownCapture={keysLed ? () => setKeysLed(false) : undefined} style={{ position: "fixed", width, ...popoverPlacementStyle(place), overflowY: undefined }}>
      <div ref={listRef} className={styles.submenuList} data-edge-fade="">
        <NativeActionMenuOpening value={opening}><NativeActionMenuKeysLead value={keysLed}>{children}</NativeActionMenuKeysLead></NativeActionMenuOpening>
      </div>
    </div>,
    document.body,
  );
}

/**
 * The side a list beside a list went (its data-side, as placed), for a list
 * opened from it to keep going that way, turning back only where it doesn't
 * fit, as Notion's (seen 2026-11-16 in Chrome: a page's "⋯" at the window's
 * right opened "테마와 레이아웃 ›" on its left and that one's "인라인 댓글 ›"
 * further left — at 1440, and at 816 with 16px left at its side — and on its
 * right at 808 and 760, where the left had 8px and less; "연결 ›"'s
 * connections, seen 2026-11-13, the same). A list beside a menu of its own
 * starts as any list beside a row does.
 */
function wentTo(menu: HTMLElement): "left" | "right" | undefined {
  const side = menu.matches("[data-page-submenu]") ? menu.dataset.side : undefined;
  return side === "left" || side === "right" ? side : undefined;
}

const NOTIFY_LEVELS = ["all_comments", "replies_and_mentions"] as const;

/** One choice beside the menu, with what it means under it where Notion says. */
export type RowMenuChoice<Value extends string> = { value: Value; label: string; hint?: string; disabled?: boolean;
  /** A mark before the label, as Notion's start page choices (seen 2026-11-13). */
  icon?: ReactNode };

/**
 * Choices beside a page menu row, the chosen one checked; choosing closes them.
 * They are a menu of their own, which starts on its checked choice however it
 * was opened: shaded, and ringed when the keys lead, as Notion's (seen
 * 2026-11-16 at 1440 in Chrome: a page's "⋯" "알림 받기 ›" with "답글과
 * @멘션" checked, opened by → ringed it, by the pointer resting on the row
 * shaded it, the pointer moving onto "모든 댓글" lit that one alone; a block
 * menu's "전환 ›" by → ringed the checked "텍스트"). With none checked it
 * starts as any list beside a row does (RowMenuBesideSubmenu).
 */
export function RowMenuChoiceSubmenu<Value extends string>({ row, label, choices, value, onChoose, onClose, width = 300, marker = "page-notify-choice" }: {
  row: HTMLElement;
  label: string;
  choices: readonly RowMenuChoice<Value>[];
  value: Value | null;
  onChoose: (value: Value) => void;
  onClose: () => void;
  width?: number;
  /** Names each choice, as data-{marker}. */
  marker?: string;
}) {
  const checked = choices.some((choice) => choice.value === value && !choice.disabled) ? value : null;
  return (
    <RowMenuBesideSubmenu row={row} width={width} label={label} onClose={onClose} ownMenu>
      <NativeActionMenu aria-label={label} selectionMode="single"
        selectedKeys={checked === null ? [] : [checked]} autoFocus={checked === null ? undefined : true}>
        {choices.map((choice) => (
          <NativeActionMenuItem key={choice.value} id={choice.value} role="menuitemradio" aria-checked={value === choice.value}
            className={styles.submenuItem} disabled={choice.disabled} {...{ [`data-${marker}`]: choice.value }}
            onClick={() => { onClose(); onChoose(choice.value); }}>
            {choice.icon}
            {choice.hint ? (
              <span className={styles.submenuChoice}><span>{choice.label}</span><small>{choice.hint}</small></span>
            ) : <span>{choice.label}</span>}
            {value === choice.value ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </NativeActionMenuItem>
        ))}
      </NativeActionMenu>
    </RowMenuBesideSubmenu>
  );
}

/** "알림 받기 ›" beside the menu (300px): all comments, or replies and @mentions. */
export function RowMenuNotifySubmenu({ row, level, onLevel, onClose }: {
  row: HTMLElement;
  level: PageNotificationLevel | null;
  onLevel: (level: PageNotificationLevel) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("rowMenu");
  const choices = NOTIFY_LEVELS.map((value) => ({
    value, label: t(value === "all_comments" ? "notify.allComments" : "notify.repliesAndMentions"),
  }));
  return <RowMenuChoiceSubmenu row={row} label={t("notify.label")} choices={choices} value={level} onChoose={onLevel} onClose={onClose} />;
}

/** The value "알림 받기" shows, as Notion's (seen 2026-10-08): "댓글" for every comment, "멘션" for replies and @mentions. */
export function notifyValueKey(level: PageNotificationLevel) {
  return level === "all_comments" ? "notify.shortAll" : "notify.shortMentions";
}

/** A share of the page with one account, as connections read them. */
type PageConnectionShare = { id: string; principalId: string };

/** An app installed in the workspace as a guest, which a page may be connected to. */
type ConnectableApp = Pick<WorkspaceApp, "appId" | "name" | "userId">;

/** Who the page is shared with, read and changed the way its share menu does. */
export type PageConnectionShares = {
  /**
   * The shares, or null when this person may not see them; manageable when
   * they may change them, and the apps when the workspace's list isn't theirs to read.
   */
  read(signal: AbortSignal): Promise<{
    shares: PageConnectionShare[]; manageable: boolean; apps?: ConnectableApp[]; notion?: PublicMutationNotionConnection[];
  } | null>;
  grant(principalId: string): Promise<PageConnectionShare[]>;
  remove(shareId: string): Promise<PageConnectionShare[]>;
  /** Puts the page in a Notion-compatible connection's pages, or takes it out. */
  changeNotion?(grantId: string, connect: boolean): Promise<PublicMutationNotionConnection[]>;
};

/**
 * A page's app connections, against the revision last read: those who may
 * edit the page read them, and change them when they have full access or
 * the page lets everyone with access connect agents.
 */
function publicPageShares(session: Session, pageId: string): PageConnectionShares {
  let revision = 0;
  const shares = (state: PublicMutationAppConnections) => {
    revision = state.revision;
    return state.connections.map((connection) => ({ id: connection.shareId, principalId: connection.principalId }));
  };
  return {
    read: async (signal) => {
      const state = await publicMutationService.appConnections(session, pageId, signal);
      return { shares: shares(state), manageable: state.manageable, apps: state.apps, notion: state.notionConnections };
    },
    grant: async (principalId) => shares(await publicMutationService.changeAppConnection(session, pageId,
      { expectedRevision: revision, action: "connect", principalId })),
    remove: async (shareId) => shares(await publicMutationService.changeAppConnection(session, pageId,
      { expectedRevision: revision, action: "disconnect", shareId })),
    changeNotion: async (grantId, connect) => {
      const state = await publicMutationService.changeAppConnection(session, pageId,
        { expectedRevision: revision, action: connect ? "connect" : "disconnect", grantId });
      shares(state);
      return state.notionConnections;
    },
  };
}

/**
 * The apps installed in the workspace as guests, each connected to this page
 * (and so to the pages under it) or not, read as the page menu opens.
 * Connecting shares the page with the app's account the way sharing with a
 * person does — through the page's public shares, or the ones given. The
 * workspace's Notion-compatible connections follow, as Notion's "연결" lists
 * them: connecting puts the page in the connection's pages; taking off one
 * that reaches it through a page above unlinks the page's sharing from its
 * parent's, the server's to do (page_notion_connections_unlink.go).
 */
export function usePageConnections(page: Pick<Page, "id" | "workspaceId">, session: Session, enabled: boolean,
  given?: PageConnectionShares) {
  const { t } = useTranslation("rowMenu");
  const [apps, setApps] = useState<ConnectableApp[] | null>(null);
  const [state, setState] = useState<PageConnectionShare[] | null>(null);
  const [notion, setNotion] = useState<PublicMutationNotionConnection[]>([]);
  const [manageable, setManageable] = useState(true);
  const [changing, setChanging] = useState(false);
  const [message, setMessage] = useState("");
  const shares = useMemo(() => given ?? publicPageShares(session, page.id), [given, page.id, session]);

  useEffect(() => {
    if (!enabled) return undefined;
    const controller = new AbortController();
    shares.read(controller.signal).then(async (read) => {
      const listed = read?.apps ?? (await listWorkspaceApps(session, page.workspaceId, controller.signal)).filter((app) => app.role === "guest");
      setApps(listed);
      setNotion(read?.notion ?? []);
      setManageable(read?.manageable ?? false);
      setState(read?.shares ?? []);
    }).catch((failure) => {
      if (!controller.signal.aborted) setMessage(settingsErrorMessage(failure, t("connections.loadFailed")));
    });
    return () => controller.abort();
  }, [enabled, page.workspaceId, session, shares, t]);

  const connectedTo = (app: ConnectableApp) => state?.find((share) => share.principalId === app.userId);

  // Each change is the server's to make first, as a share is: the buttons wait meanwhile; true once made.
  async function toggle(app: ConnectableApp) {
    if (!state || changing || !manageable) return false;
    const share = connectedTo(app);
    setChanging(true);
    setMessage("");
    try {
      setState(await (share ? shares.remove(share.id) : shares.grant(app.userId)));
      return true;
    } catch (failure) {
      setMessage(settingsErrorMessage(failure, t("connections.failed")));
      return false;
    } finally {
      setChanging(false);
    }
  }

  async function toggleNotion(connection: PublicMutationNotionConnection) {
    if (!shares.changeNotion || changing || !manageable) return false;
    setChanging(true);
    setMessage("");
    try {
      setNotion(await shares.changeNotion(connection.grantId, connection.reach === "none"));
      return true;
    } catch (failure) {
      setMessage(settingsErrorMessage(failure, t("connections.failed")));
      return false;
    } finally {
      setChanging(false);
    }
  }

  const loaded = apps !== null && state !== null;
  const connected = loaded
    ? apps.filter((app) => connectedTo(app)).length + notion.filter((connection) => connection.reach !== "none").length : 0;
  // The row's value: "없음", or how many are connected.
  const value = loaded ? (connected > 0 ? String(connected) : t("connections.none")) : "";
  return {
    apps, notion, loaded, manageable, value, changing, message,
    connected: (app: ConnectableApp) => Boolean(connectedTo(app)), toggle, toggleNotion,
  };
}
