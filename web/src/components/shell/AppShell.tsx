import {
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useCallback,
  createContext,
  useEffect,
  useLayoutEffect,
  useContext,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useTranslation } from "react-i18next";
import { createFocusManager } from "@react-aria/focus";
import { deviceValue, storeDeviceValue } from "@/lib/browserStorage";
import { notePageFramePress, takePageFramePress } from "@/lib/pageTitleFocus";
import { markAppInteractiveForOfflineWarm } from "@/lib/appInteractive";
import { onDesktopMenuCommand } from "@/lib/desktopMenuCommands";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { windowOverProduct } from "@/lib/productWindows";
import { ROUTE_CHANGE_EVENT, usePathname } from "@/lib/router";
import {
  SIDE_ROW_PEEK_LAYOUT_EVENT,
  sideRowPeekLayoutFromEvent,
} from "@/lib/sideRowPeekLayout";
import styles from "./AppShell.module.css";
import { useInboxDock } from "@/components/updates/inboxDock";
import { inlineSign } from "@/lib/readingDirection";
import { appleTouchDevice } from "@/lib/iosViewport";
import { holdPhoneWindow } from "@/lib/phoneWindowHold";
import { followScreenKind, narrowScreen, phoneScreen, touchScreen } from "@/lib/screenKind";
import { shutBehindModal } from "@/lib/modalFocus";
import { usePhoneDrawerShut } from "./usePhoneDrawerShut";
import { keyCameLast } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { usePeekRoom } from "./usePeekRoom";
import { RouteLabel } from "./RouteLabel";

const SIDEBAR_WIDTH_KEY = "leafen:sidebar-width";
const SIDEBAR_COLLAPSED_KEY = "leafen:sidebar-collapsed";
const LEGACY_DEFAULT_SIDEBAR_WIDTH = 240;
// A side peek folds the sidebar where the page beside them would be left
// under this, and gives it back as the peek is narrowed, as Notion's (seen
// 2026-11-06 at 1280 and 2026-11-17 at 1416 wide, 270px sidebar: open with 430 left, folded
// with 428; a kept 720px peek folds it at 1416, the first one, half the window, under 1400).
const SIDE_PEEK_LEAVES_PAGE = 430;
const DEFAULT_SIDEBAR_WIDTH = 270;
// A touch screen's opens wider, as Notion's iPad (seen 2026-11-13 at 834 and
// 1210 wide: docked, 300).
const TOUCH_SIDEBAR_WIDTH = 300;
function defaultSidebarWidth() {
  return touchScreen() ? TOUCH_SIDEBAR_WIDTH : DEFAULT_SIDEBAR_WIDTH;
}
const MIN_SIDEBAR_WIDTH = 180;
const MAX_SIDEBAR_WIDTH = 480;
// Folded or not as the app opens, before its first paint: folded in a window
// under 1024px wide however it was left, else what this browser last chose,
// as Notion's (seen 2026-11-11: new windows 606–1023 wide folded, 1024–1280
// as chosen; docked by hand at 900 and reloaded, folded again — as on
// 2026-10-27 and 2026-10-30 09:32). Folding here keeps the choice stored.
// A touch screen's goes by the same rule, as Notion's iPad (seen 2026-11-14
// in iPad Safari, its stored choice read and cleared: 834 wide folded with
// nothing stored and with "docked" stored; 1210 wide docked with nothing
// stored, folded once folded).
const FOLDED_UNDER_WIDTH = 1024;
function sidebarFoldedAtStart() {
  if (typeof window === "undefined") return false;
  return window.innerWidth < FOLDED_UNDER_WIDTH || deviceValue(SIDEBAR_COLLAPSED_KEY) === "true";
}

// The skip link's jump focuses the page's frame for that once; a press on the
// frame's empty space (its top bar, the room by a page's head) leaves the focus
// on the page, so Escape closes a side peek beside it, as Notion's (measured
// once 2026-11-15 in Chrome 1440 with no script before the press: after a
// press on the top bar's empty room Escape closed the row's side peek and a
// letter typed then went nowhere).
function focusMainForSkip(main: HTMLElement) {
  main.tabIndex = -1;
  main.addEventListener("blur", () => main.removeAttribute("tabindex"), { once: true });
  main.focus();
}

function clampSidebarWidth(width: number) {
  return Math.min(MAX_SIDEBAR_WIDTH, Math.max(MIN_SIDEBAR_WIDTH, Math.round(width)));
}

export type SidebarLayoutState = {
  collapsed: boolean;
  mobile: boolean;
  open: boolean;
  resizing: boolean;
  width: number;
  minWidth: number;
  maxWidth: number;
  onResizeNudge: (delta: number) => void;
  onResizeReset: () => void;
  onResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onToggle: () => void;
};

type SidebarLayoutContextValue = {
  mobile: boolean;
  /** Narrow touch devices use the phone's content as well as its drawer. */
  phone: boolean;
  /**
   * A touch screen with no pointer to hover with, a phone's or wider (a
   * tablet's, an opened fold's): what Notion's iPad shows as its phone does
   * (TouchSheetLayout) or leaves out goes by this.
   */
  touch: boolean;
  sidebarCollapsed: boolean;
  sidebarOpen: boolean;
  /** The screen stands in the app's frame, beside a sidebar it can open;
   * a published page, a share or a view link has none. */
  frame: boolean;
  /** The desktop sidebar's width as it takes room now, 0 while collapsed. */
  sidebarWidth: number;
  toggleSidebarChrome: () => void;
  /** Collapses the desktop sidebar, as a panel that needs its room does. */
  collapseSidebar: () => void;
  /** Leaves Leafen's sidebar out while a screen takes the whole window, as
   * an app in its full window does. */
  setChromeHidden: (hidden: boolean) => void;
};

const SidebarLayoutContext = createContext<SidebarLayoutContextValue | null>(null);
const standaloneSidebarLayout: SidebarLayoutContextValue = {
  mobile: false,
  phone: false,
  touch: false,
  sidebarCollapsed: false,
  sidebarOpen: false,
  frame: false,
  sidebarWidth: 0,
  toggleSidebarChrome: () => {},
  collapseSidebar: () => {},
  setChromeHidden: () => {},
};

// A screen outside the app's frame — a published page, a share or view
// link — is a phone's by the frame's own rule (screenKind.ts), as Notion's
// published database on a phone has its views behind one picker (seen
// 2026-11-11 in iOS Safari, signed out).
const standalonePhoneLayout: SidebarLayoutContextValue = { ...standaloneSidebarLayout, mobile: true, phone: true, touch: true };
const standaloneTouchLayout: SidebarLayoutContextValue = { ...standaloneSidebarLayout, touch: true };
const noSubscription = () => () => {};
function standaloneScreen() {
  if (!touchScreen()) return "desktop";
  return phoneScreen() ? "phone" : "touch";
}

export function useSidebarLayout(standalone = false) {
  const value = useContext(SidebarLayoutContext);
  const outside = !value && standalone;
  const screen = useSyncExternalStore(outside ? followScreenKind : noSubscription,
    () => outside ? standaloneScreen() : "desktop", () => "desktop");
  if (outside) return screen === "phone" ? standalonePhoneLayout : screen === "touch" ? standaloneTouchLayout : standaloneSidebarLayout;
  if (!value) {
    throw Object.assign(new Error("The sidebar layout context is missing."), {
      code: "LEAFEN_SIDEBAR_LAYOUT_CONTEXT_MISSING" as const,
    });
  }
  return value;
}

/**
 * Draws what it holds as a phone's on a touch screen wider than one, as
 * Notion's iPad opens its share as the phone's "공유 설정" sheet (seen
 * 2026-11-12 in iPad Safari, 834 wide): the windows inside read `phone`.
 */
export function TouchSheetLayout({ children }: { children: ReactNode }) {
  const layout = useSidebarLayout(true);
  const sheets = useMemo(() => layout.touch && !layout.phone ? { ...layout, phone: true } : layout, [layout]);
  return <SidebarLayoutContext.Provider value={sheets}>{children}</SidebarLayoutContext.Provider>;
}

function subscribeWindowWidth(onChange: () => void) {
  window.addEventListener("resize", onChange);
  return () => window.removeEventListener("resize", onChange);
}

export function AppShell({
  children,
  sidebar,
  routeLabel,
  overlays,
  status,
  notice,
}: {
  children: ReactNode;
  sidebar: (state: SidebarLayoutState) => ReactNode;
  routeLabel: string;
  overlays?: ReactNode;
  status?: ReactNode;
  /** A band across the top of the page area, above everything in it. */
  notice?: ReactNode;
}) {
  const { t } = useTranslation("appShell");
  const pathname = usePathname();
  const [sidebarWidth, setSidebarWidth] = useState(defaultSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(sidebarFoldedAtStart);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  // A touch phone's sidebar is a drawer under the page, which slides aside to
  // show it, as Notion's in iPhone Safari (seen 2026-11-16, upright and on its
  // side; AppShell.module.css); a narrow desktop window gives it room beside
  // the page instead (seen 2026-10-04).
  const [touchPhone, setTouchPhone] = useState(false);
  const [sideRowPeek, setSideRowPeek] = useState<{ open: boolean; width?: number }>({ open: false });
  const sideRowPeekOpen = sideRowPeek.open;
  const [chromeHidden, setChromeHidden] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  // How tall the band came out, which the page moves down by: a phone's takes
  // the lines its text needs (2026-11-05: a band about a moved server was cut
  // after a few words, with no pointer to read the rest by).
  const noticeRef = useRef<HTMLDivElement>(null);
  const hasNotice = Boolean(notice);
  useLayoutEffect(() => {
    const band = noticeRef.current;
    if (!band) return undefined;
    const root = document.documentElement;
    const note = () => root.style.setProperty("--app-notice-height", `${band.offsetHeight}px`);
    note();
    // The root is told the band is there, for the panels laid outside the
    // page (AppShell.module.css --page-panel-top).
    root.dataset.appNotice = "true";
    const observer = new ResizeObserver(note);
    observer.observe(band);
    return () => {
      observer.disconnect();
      root.style.removeProperty("--app-notice-height");
      delete root.dataset.appNotice;
    };
  }, [hasNotice]);
  const navigationCleanupPathnameRef = useRef(pathname);

  useEffect(() => {
    markAppInteractiveForOfflineWarm();
  }, []);

  useEffect(() => {
    const update = () => {
      setIsMobile(narrowScreen());
      setTouchPhone(touchScreen());
    };
    update();
    return followScreenKind(update);
  }, []);

  useLayoutEffect(() => {
    if (navigationCleanupPathnameRef.current === pathname) return;
    navigationCleanupPathnameRef.current = pathname;
    setSidebarOpen(false);
  }, [pathname]);
  // Opening the place already shown closes the sidebar over the page too, as
  // Notion's phone drawer (seen 2026-11-13 in iPhone Safari: the open page's
  // row tapped there closed it, the page left as it was). The address does
  // not move then, so the move that went nowhere says so.
  useEffect(() => {
    const here = () => `${window.location.pathname}${window.location.search}${window.location.hash}`;
    let shown = here();
    const onMove = (event: Event) => {
      const went = here();
      const stayed = went === shown && (event as CustomEvent<{ replace?: boolean }>).detail?.replace !== true;
      shown = went;
      if (stayed) setSidebarOpen(false);
    };
    const onHistory = () => { shown = here(); };
    window.addEventListener(ROUTE_CHANGE_EVENT, onMove);
    window.addEventListener("popstate", onHistory);
    return () => {
      window.removeEventListener(ROUTE_CHANGE_EVENT, onMove);
      window.removeEventListener("popstate", onHistory);
    };
  }, []);

  useLayoutEffect(() => {
    function onSideRowPeekLayout(event: Event) {
      const { open, width } = sideRowPeekLayoutFromEvent(event);
      setSideRowPeek((current) => current.open === open && current.width === width ? current : { open, width });
    }
    window.addEventListener(SIDE_ROW_PEEK_LAYOUT_EVENT, onSideRowPeekLayout);
    return () => window.removeEventListener(SIDE_ROW_PEEK_LAYOUT_EVENT, onSideRowPeekLayout);
  }, []);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      // The sidebar's width is this browser's, for whoever uses it.
      const raw = deviceValue(SIDEBAR_WIDTH_KEY);
      const width = raw ? Number(raw) : Number.NaN;
      if (Number.isFinite(width)) {
        const nextWidth = width === LEGACY_DEFAULT_SIDEBAR_WIDTH ? defaultSidebarWidth() : width;
        setSidebarWidth(clampSidebarWidth(nextWidth));
        if (nextWidth !== width) storeDeviceValue(SIDEBAR_WIDTH_KEY, String(nextWidth));
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, []);

  const toggleCollapsed = useCallback(() => {
    setSidebarCollapsed((current) => {
      const next = !current;
      storeDeviceValue(SIDEBAR_COLLAPSED_KEY, String(next));
      return next;
    });
  }, []);

  useEffect(() => {
    function toggle() {
      if (isMobile) setSidebarOpen((current) => !current);
      else toggleCollapsed();
    }
    function onKeyDown(event: KeyboardEvent) {
      // A key of the whole app (keyboardShortcuts.ts "global"), wherever the focus is — a page's text or title, a database's cell,
      // a comment — as Notion's web page (seen 2026-11-18); not under search or another window over the product (productWindows.ts).
      if (windowOverProduct() || !matchesKeyboardShortcut("toggleSidebar", event)) return;
      event.preventDefault();
      toggle();
    }
    window.addEventListener("keydown", onKeyDown);
    // The desktop app's View › 사이드바 표시/숨기기, the key the page leaves to it under a window.
    const stopMenu = onDesktopMenuCommand("toggleSidebar", toggle);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      stopMenu();
    };
  }, [isMobile, toggleCollapsed]);

  function startSidebarResize(event: ReactPointerEvent<HTMLDivElement>) {
    if (isMobile) return;
    event.preventDefault();
    const startX = event.clientX;
    // Away from the sidebar's own edge widens it: rightward left to right, leftward right to left.
    const sign = inlineSign(event.currentTarget);
    const startWidth = sidebarWidth;
    let nextWidth = startWidth;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    setSidebarResizing(true);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";

    function onPointerMove(pointerEvent: PointerEvent) {
      nextWidth = clampSidebarWidth(startWidth + (pointerEvent.clientX - startX) * sign);
      setSidebarWidth(nextWidth);
    }

    function onPointerUp() {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      setSidebarResizing(false);
      storeDeviceValue(SIDEBAR_WIDTH_KEY, String(nextWidth));
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
    }

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
  }

  function nudgeSidebarWidth(delta: number) {
    setSidebarWidth((current) => {
      const next = clampSidebarWidth(current + delta);
      storeDeviceValue(SIDEBAR_WIDTH_KEY, String(next));
      return next;
    });
  }

  function resetSidebarWidth() {
    setSidebarWidth(defaultSidebarWidth());
    storeDeviceValue(SIDEBAR_WIDTH_KEY, null);
  }

  const effectiveMobileSidebarOpen = sidebarOpen && !sideRowPeekOpen;
  const mobileSidebarLayout = isMobile && effectiveMobileSidebarOpen ? touchPhone ? "drawer" : "push" : undefined;
  const inboxDock = useInboxDock();
  const besidePeek = sidebarWidth + (inboxDock.docked && !isMobile && !chromeHidden ? inboxDock.width : 0) + (sideRowPeek.width ?? 0);
  const sidePeekFoldsSidebar = useSyncExternalStore(subscribeWindowWidth,
    () => sideRowPeekOpen && window.innerWidth - besidePeek < SIDE_PEEK_LEAVES_PAGE, () => false);
  const effectiveDesktopSidebarCollapsed = sidebarCollapsed || sidePeekFoldsSidebar;
  const mobileSidebarSlot = useRef<HTMLDivElement>(null);
  const drawerOpener = useRef<HTMLElement | null>(null);
  const toggleSidebarChrome = useCallback(() => {
    if (isMobile) {
      // The page behind a phone's drawer is shut and takes the opener's focus
      // with it (usePhoneDrawerShut, below), so the opener is kept to give it back.
      const active = document.activeElement;
      if (active instanceof HTMLElement && !mobileSidebarSlot.current?.contains(active)) drawerOpener.current = active;
      setSidebarOpen((current) => !current);
    } else {
      storeDeviceValue(SIDEBAR_COLLAPSED_KEY, "false");
      setSidebarCollapsed(false);
    }
  }, [isMobile]);
  // A phone's drawer opened from the keys takes the focus on its first
  // control, as Notion's (seen 2026-10-29: its workspace button); opened by a
  // tap it leaves the focus where it was, as Notion's in iPhone Safari (seen
  // 2026-11-13: the page's body kept it). Every move of the focus there walks
  // the whole page, some 70 ms on a page of 38k elements (2026-11-13), so a
  // tap moves none. Closed where it was opened, it gives the focus back to
  // its opener. A page chosen in it keeps the page's own focus.
  const drawerOpen = mobileSidebarLayout === "drawer" && !chromeHidden;
  // Beside the drawer an iPhone's page keeps its light (AppShell.module.css).
  const [appleTouch] = useState(appleTouchDevice);
  useEffect(() => {
    if (!drawerOpen) return undefined;
    const openedAt = window.location.pathname;
    const frame = !keyCameLast() ? 0 : window.requestAnimationFrame(() => {
      const slot = mobileSidebarSlot.current;
      if (slot && !slot.contains(document.activeElement)) createFocusManager(mobileSidebarSlot).focusFirst({ tabbable: true });
    });
    return () => {
      window.cancelAnimationFrame(frame);
      const opener = drawerOpener.current;
      drawerOpener.current = null;
      giveFocusBack({ to: opener, keepScroll: true, when: () => {
        const active = document.activeElement;
        const lost = !active || active === document.body || Boolean(mobileSidebarSlot.current?.contains(active));
        return Boolean(opener?.isConnected && opener !== document.body && lost && window.location.pathname === openedAt && !shutBehindModal(opener));
      } });
    };
  }, [drawerOpen]);
  // All beside the drawer is shut while it is open, Tab going round it (usePhoneDrawerShut).
  usePhoneDrawerShut(drawerOpen, mobileSidebarSlot);
  const collapseSidebar = useCallback(() => {
    if (isMobile) return;
    storeDeviceValue(SIDEBAR_COLLAPSED_KEY, "true");
    setSidebarCollapsed(true);
  }, [isMobile]);
  // A phone's window stays at the top, the page scrolling under its top bar.
  const phone = isMobile && touchPhone;
  useEffect(() => phone && mainRef.current ? holdPhoneWindow(mainRef.current) : undefined, [phone]);
  // Sheets, windows and menus drawn into the body, outside the shell, are
  // reached by the body's phone and touch marks (screenKind.ts).
  const sidebarLayout = useMemo(() => ({
    mobile: isMobile,
    phone: isMobile && touchPhone,
    touch: touchPhone,
    sidebarCollapsed: effectiveDesktopSidebarCollapsed,
    sidebarOpen: effectiveMobileSidebarOpen,
    frame: true,
    sidebarWidth: isMobile || effectiveDesktopSidebarCollapsed ? 0 : sidebarWidth,
    toggleSidebarChrome,
    collapseSidebar,
    setChromeHidden,
  }), [collapseSidebar, effectiveDesktopSidebarCollapsed, effectiveMobileSidebarOpen, isMobile, touchPhone, sidebarWidth, toggleSidebarChrome]);

  // A pinned inbox docks beside the sidebar and the page moves over for it.
  const inboxDocked = inboxDock.docked && !isMobile && !effectiveDesktopSidebarCollapsed && !chromeHidden;
  // The sidebar's widths are set where they are read — its slot, the band
  // above the page, the page itself — never on the shell: a variable set
  // there worked out every style in the app again, some 240 ms on a page of
  // 35k elements each time the sidebar folded or unfolded (2026-11-13), as
  // the side peek's room did (below).
  const sidebarWidthStyle = { "--sidebar-width": `${sidebarWidth}px` } as CSSProperties;
  const noticeStyle = {
    "--sidebar-effective-width": `${
      chromeHidden ? 0
        : isMobile
        ? mobileSidebarLayout === "push" ? DEFAULT_SIDEBAR_WIDTH : 0
        : effectiveDesktopSidebarCollapsed ? 0 : sidebarWidth + (inboxDocked ? inboxDock.width : 0)
    }px`,
  } as CSSProperties;

  // The page gives the side peek its room (AppShell.module.css) on itself: a
  // variable set on the shell worked out every style in the app again, some
  // 120ms for a page holding a 120-row table, each time a peek opened or
  // closed (seen 2026-11-12). It gives it with the peek's motion, a large
  // page once the peek is in (usePeekRoom). A pinned inbox lies beside the
  // sidebar, and the page starts after it.
  const peekRoom = usePeekRoom(mainRef, sideRowPeek, isMobile && touchPhone);
  const roomForPeek = peekRoom.open && peekRoom.width && !(isMobile && touchPhone);
  const pageStyle: CSSProperties | undefined = roomForPeek || inboxDocked ? {
    marginInlineStart: inboxDocked ? `${inboxDock.width}px` : undefined,
    marginInlineEnd: roomForPeek ? `min(${peekRoom.width}px, 100vw)` : undefined,
  } : undefined;

  return (
    <SidebarLayoutContext.Provider value={sidebarLayout}>
    <div
      className={styles.shell}
      data-phone={isMobile && touchPhone ? "true" : undefined}
      data-mobile-sidebar-layout={mobileSidebarLayout}
      data-apple-touch={appleTouch ? "true" : undefined}
      data-side-row-peek-open={peekRoom.open ? "true" : undefined}
      data-inbox-docked={inboxDocked ? "true" : undefined}
      data-chrome-hidden={chromeHidden ? "true" : undefined}
    >
      <a className={styles.skipLink} href="#app-main-content"
        onClick={(event) => { if (mainRef.current) { event.preventDefault(); focusMainForSkip(mainRef.current); } }}>
        {t("skipToMain")}
      </a>
      {chromeHidden ? null : isMobile ? (
        <div
          ref={mobileSidebarSlot}
          className={styles.mobileSidebarSlot}
          data-mobile-sidebar-slot="true"
          data-open={effectiveMobileSidebarOpen ? "true" : undefined}
          style={sidebarWidthStyle}
        >
          {sidebar({
            mobile: true,
            open: effectiveMobileSidebarOpen,
            collapsed: false,
            resizing: false,
            width: DEFAULT_SIDEBAR_WIDTH,
            minWidth: MIN_SIDEBAR_WIDTH,
            maxWidth: MAX_SIDEBAR_WIDTH,
            onResizeStart: startSidebarResize,
            onResizeNudge: nudgeSidebarWidth,
            onResizeReset: resetSidebarWidth,
            onToggle: () => setSidebarOpen(false),
          })}
        </div>
      ) : (
        <div
          className={styles.sidebarSlot}
          data-sidebar-slot
          data-collapsed={effectiveDesktopSidebarCollapsed ? "true" : undefined}
          data-resizing={sidebarResizing ? "true" : undefined}
          style={sidebarWidthStyle}
        >
          {sidebar({
            mobile: false,
            open: !effectiveDesktopSidebarCollapsed,
            collapsed: effectiveDesktopSidebarCollapsed,
            resizing: sidebarResizing,
            width: sidebarWidth,
            minWidth: MIN_SIDEBAR_WIDTH,
            maxWidth: MAX_SIDEBAR_WIDTH,
            onResizeStart: startSidebarResize,
            onResizeNudge: nudgeSidebarWidth,
            onResizeReset: resetSidebarWidth,
            onToggle: toggleCollapsed,
          })}
        </div>
      )}
      {notice ? <div ref={noticeRef} className={styles.notice} data-app-band="" aria-hidden={drawerOpen ? true : undefined} style={noticeStyle}>{notice}</div> : null}
      <main
        ref={mainRef}
        id="app-main-content"
        className={`${styles.main} nscroll`}
        data-app-main="true"
        data-app-notice={notice ? "true" : undefined}
        aria-hidden={drawerOpen ? true : undefined}
        style={pageStyle}
        onPointerDownCapture={(event) => notePageFramePress(event.currentTarget, event.nativeEvent)}
        onClick={(event) => takePageFramePress(event.currentTarget, event.target)}
      >
        {children}
      </main>
      {/* Beside a phone's drawer the page and a band above it take no taps or
          focus (and dim on Android); a tap there closes it. Laid
          after the page, not before it: a box coming or going before the page
          had the style of all of it worked out again in WebKit (2026-11-13 in
          iPhone Safari, a page of 38k elements: some 270 ms as the drawer
          opened and as it closed; 1 ms after it). A tap takes no focus to
          it, so closing moves none (the focus taken there and dropped with it
          cost some 60 ms). */}
      {drawerOpen ? (
        <button type="button" className={styles.drawerScrim} tabIndex={-1} aria-label={t("closeSidebar")} data-mobile-sidebar-scrim
          onMouseDown={(event) => event.preventDefault()} onClick={() => setSidebarOpen(false)} />
      ) : null}
      {overlays}
      <RouteLabel label={routeLabel} main={mainRef} className={styles.srOnly} />
      {status}
    </div>
    </SidebarLayoutContext.Provider>
  );
}
