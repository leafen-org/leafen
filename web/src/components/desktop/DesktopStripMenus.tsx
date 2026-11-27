import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { PageIconPicker } from "@/components/page/PageIconPicker";
import { nativeIconMarkup } from "@/components/page/nativeIconDrawing";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import { tellStripMenu, useDesktopTabPage, type StripIconPicker, type StripShadows } from "@/lib/desktopTabPage";
import { topLayerZIndex } from "@/lib/overlayLayer";
import styles from "./DesktopTabMenus.module.css";

/**
 * What the shown page draws under a menu of the Mac and Windows apps' strip,
 * which the strip draws over it: under a group's menu a cover taking its
 * presses, each closing the menu with nothing pressed under it, as a press
 * outside Notion 7.44.1's group menu does (on Windows, where the strip
 * leaves the page the presses outside the menu; a Mac strip takes them
 * itself), under a tab's menu nothing, its presses going on — and the icon
 * picker of a group's menu in the strip
 * (desktop/tab_strip_group_menu.js, which draws the menu over the strip), as
 * Notion 7.44.1's tab bar opens it (measured in its Windows app 2026-11-24):
 * its top a pixel over the foot of the menu's icon, its middle under the
 * icon's, 408 wide, over the menu, its filter taking the keys. The shown
 * page draws it, as it has the picker, and tells the app where it stands —
 * the strip leaves it that place — and what was picked. A pick, an Escape or
 * a press outside it closes it alone, the menu staying open (seen in
 * Notion's Windows app the same day). As Notion's (its TabGroupMenu's
 * iconButton: pickerTabs icon and emoji, currentTab by the icon's kind,
 * enforceIconColor gray, hideCustomEmoji; seen in its Windows app
 * 2026-11-24): "아이콘 · 이모지", opening on "이모지" for a group whose icon is
 * an emoji and on "아이콘" else, its icons gray with no color to choose (the
 * strip paints the picked one in the group's color), no workspace emoji,
 * and either tab's random putting its pick in with the picker staying open.
 */
export function DesktopStripMenus() {
  const { menu, shadows } = useDesktopTabPage();
  if (!menu || typeof document === "undefined") return null;
  const cast = shadows && shadows.boxes.length > 0 ? createPortal(<StripMenuShadows shadows={shadows} />, document.body) : null;
  if (menu.kind === "open") return <>{cast}{menu.through ? <StripPressThrough /> : createPortal(<StripCover />, document.body)}</>;
  return <>{cast}{createPortal(<GroupIconPicker key={menu.group.id} picker={menu} />, document.body)}</>;
}

/**
 * The strip's menus' shadow under them, which the Windows strip cannot cast
 * over the page (desktop/window_tabs_strip_shadows_windows.go): each menu's
 * box at its place, less the strip over the page, rounded and shadowed as
 * the page's menus are; the strip covers the boxes themselves.
 */
function StripMenuShadows({ shadows }: { shadows: StripShadows }) {
  // Over all of the page, as the menus are, but the picker opened over them.
  const [zIndex] = useState(stripLayerZIndex);
  return <div className={styles.stripShadows} style={{ zIndex }} aria-hidden="true" data-desktop-strip-menu="shadows">
    {shadows.boxes.map((box, at) => <div key={at} className={styles.stripShadow}
      style={{ insetInlineStart: box.left, top: box.top - shadows.strip, width: box.width, height: box.height }} />)}
  </div>;
}

/**
 * Takes the page's presses while a strip menu stands over it: the menu
 * closes, nothing under it pressed — at the window, before anything of the
 * page, whatever stands over the cover there (the first run's windows,
 * 2026-11-24: a press on one went to it, the group's menu staying open).
 */
function StripCover() {
  useEffect(() => {
    let shut = false;
    const taken = (event: Event) => {
      event.preventDefault();
      event.stopPropagation();
      if (event.type !== "pointerdown" && event.type !== "contextmenu") return;
      if (shut) return;
      shut = true;
      tellStripMenu({ do: "pressed" });
    };
    const kinds = ["pointerdown", "mousedown", "pointerup", "mouseup", "click", "auxclick", "dblclick", "contextmenu"];
    for (const kind of kinds) window.addEventListener(kind, taken, { capture: true });
    return () => { for (const kind of kinds) window.removeEventListener(kind, taken, { capture: true }); };
  }, []);
  const [zIndex] = useState(stripLayerZIndex);
  return <div className={styles.stripCover} style={{ zIndex }} aria-hidden="true" data-desktop-strip-menu="cover" />;
}

/** Over every layer of the page open as a strip menu opens (the first run's windows, a dialog), under one opened after (the group's icon picker). */
function stripLayerZIndex(): number {
  return (topLayerZIndex() ?? STRIP_LAYER_BASE) + 5;
}

// Under the page's first layer (overlayLayer.ts), over its own windows that stand outside them.
const STRIP_LAYER_BASE = 1990;

/**
 * Under a tab's menu a press on the page closes it and goes on there, the
 * page taking the keys, as a press outside Notion 7.44.1's tab menu does
 * (seen twice in its Windows app 2026-11-24: the caret at the press, a
 * letter typed after in the page).
 */
function StripPressThrough() {
  useEffect(() => {
    const pressed = () => tellStripMenu({ do: "pressed" });
    window.addEventListener("pointerdown", pressed, { capture: true, once: true });
    return () => window.removeEventListener("pointerdown", pressed, { capture: true });
  }, []);
  return null;
}

const closed = () => tellStripMenu({ do: "pickerClosed" });

function GroupIconPicker({ picker }: { picker: StripIconPicker }) {
  const host = useRef<HTMLDivElement>(null);
  const id = picker.group.id;
  // Over the menus' shadow and the cover, as the picker over Notion's menu.
  const [zIndex] = useState(() => stripLayerZIndex() + 5);
  // Its top in the page: a pixel over the icon's foot on the strip, less the strip over the page.
  const top = picker.at.bottom - 1 - picker.strip;
  useLayoutEffect(() => {
    const box = host.current;
    const node = box?.querySelector<HTMLElement>("[data-placement='anchored']");
    if (!box || !node) return;
    const place = () => {
      const width = node.offsetWidth;
      const room = document.documentElement.clientWidth;
      const edge = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--popover-edge")) || 12;
      const left = Math.max(edge, Math.min((picker.at.left + picker.at.right) / 2 - width / 2, room - edge - width));
      const rtl = getComputedStyle(box).direction === "rtl";
      box.style.insetInlineStart = `${Math.round(rtl ? room - left - width : left)}px`;
      box.style.visibility = "";
      const r = node.getBoundingClientRect();
      tellStripMenu({ do: "picker", rect: { left: Math.floor(r.left), top: Math.floor(r.top), right: Math.ceil(r.right), bottom: Math.ceil(r.bottom) } });
    };
    place();
    const watch = new ResizeObserver(place);
    watch.observe(node);
    window.addEventListener("resize", place);
    // The filter takes the keys as the page gets them from the strip, which
    // the app gives it as the picker opens: a web view given the keys puts
    // its page's focus back where it last was (WebView2) or drops it
    // (WebKit), after the filter took it.
    // Till it is pressed or typed in, the focus left on the page or on the
    // picker itself (the layer's own, overlayLayer.ts) goes to the filter.
    let frame = 0;
    let used = false;
    const takeKeys = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        const filter = node.querySelector<HTMLInputElement>("input");
        const held = document.activeElement;
        if (!used && filter && (!held || held === document.body || held === node)) filter.focus({ preventScroll: true });
      });
    };
    const use = () => { used = true; };
    const toPicker = (event: FocusEvent) => { if (event.target === node) takeKeys(); };
    window.addEventListener("focus", takeKeys);
    node.addEventListener("focusin", toPicker);
    node.addEventListener("pointerdown", use);
    node.addEventListener("keydown", use);
    if (document.hasFocus()) takeKeys();
    return () => {
      watch.disconnect();
      window.removeEventListener("resize", place);
      window.removeEventListener("focus", takeKeys);
      node.removeEventListener("focusin", toPicker);
      node.removeEventListener("pointerdown", use);
      node.removeEventListener("keydown", use);
      window.cancelAnimationFrame(frame);
    };
  }, [picker.at.left, picker.at.right, top]);
  // A pick puts the icon in the group and closes the picker alone.
  const pick = (icon: string) => {
    tellStripMenu({ do: "icon", group: id, icon });
    closed();
  };
  return (
    <div ref={host} className={styles.pickerHost} data-desktop-strip-menu="group-icon"
      style={{ top, zIndex, visibility: "hidden", "--picker-room": `calc(100dvh - ${top}px - var(--popover-edge))` } as CSSProperties}>
      <PageIconPicker placement="anchored" tabOrder={GROUP_ICON_TABS} openingTab={emojiIcon(picker.group.icon) ? "emoji" : "icons"}
        iconColors={false}
        onPickBuiltInIcon={(value) => pick(builtInPicture(value))}
        onPick={(emoji) => pick(emoji)}
        onShuffle={(value, kind) => tellStripMenu({ do: "icon", group: id, icon: kind === "icon" ? builtInPicture(value) : value })}
        onRemove={() => pick("")}
        onClose={closed} />
    </div>
  );
}

/** A group's icon picker's tabs, as Notion's: "아이콘 · 이모지". */
const GROUP_ICON_TABS = ["icons", "emoji"] as const;

/** Whether a group's icon is an emoji: not its picture (builtInPicture) nor the folder it has without one. */
function emojiIcon(icon: string): boolean {
  return icon !== "" && !icon.startsWith("data:");
}

/** One of the app's icons as a group's picture: its shape, drawn in the group's color by the strip. */
function builtInPicture(value: string): string {
  const icon = parseNativeIconValue(value);
  return icon ? `data:image/svg+xml,${encodeURIComponent(nativeIconMarkup(icon.name, "#000"))}` : "";
}
