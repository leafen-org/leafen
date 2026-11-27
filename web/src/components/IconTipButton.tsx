import { type ButtonHTMLAttributes, type ReactNode, type Ref, type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { type GROUP_BAR_TIP, tipRequest } from "@/lib/tipPlacement";
import { fadeTipOut, type TipWait, useTipClock, useTipPress } from "@/lib/tipTiming";
import { Trans } from "react-i18next";
import { i18next } from "@/i18n";
import { optionKeyName, shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { InfoIcon } from "@/icons/leafen";
import { keysLedFocus } from "@/lib/lastPress";
import styles from "./IconTipButton.module.css";

/**
 * A button drawn as an icon alone, named in a dark tip above it while the
 * pointer is on it or the keyboard brings it into focus, as Notion's tools
 * and Hermes desktop's message actions (seen 2026-10-28). The name is the
 * button's accessible name too; the browser's own title tip is not used.
 * The pointer's tip waits Notion's time (`wait`, lib/tipTiming.ts), the
 * keyboard's comes at once.
 * A finger's tap shows none, as Notion's phones show none (the mouse events a
 * tap brings after it would leave the tip standing until the next tap
 * elsewhere, seen 2026-11-17 on the icon picker's random in iPhone Safari).
 */
export function IconTipButton({ label, tip, said, placement = "top", wait = "control", ref, children, ...button }: {
  label: string;
  /** What the tip says when it says more than the name, as the "+" handles' two lines. */
  tip?: ReactNode;
  /** Said in the tip for a moment after a press, in place of the name, the tip shown while it is
   *  and closed when it goes (CopyTipButton's "복사됨"). */
  said?: ReactNode;
  /** Under the button, as Notion's "+" handles show theirs (seen 2026-11-08); from its start
   *  under a bar of groups (GROUP_BAR_TIP, lib/tipPlacement.ts). */
  placement?: "top" | "bottom" | typeof GROUP_BAR_TIP;
  /** How long the pointer rests before the tip: a block's or a row's handles wait longer and keep it a moment after. */
  wait?: TipWait;
  ref?: Ref<HTMLButtonElement>;
  children: ReactNode;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type" | "aria-label" | "title">) {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  // A click that copies moves the focus away for a moment; the tip stays while the pointer does.
  const pointed = useRef(false);
  // Whether the pointer now on it is a finger, whose tap brings mouse events that show no tip.
  const touched = useRef(false);
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, placement, 6) : null,
    [anchor], undefined, false);
  const clock = useTipClock(wait);
  const show = (target: HTMLElement) => { clock.stop(); setAnchor(target.getBoundingClientRect()); };
  const rest = (target: HTMLElement) => clock.rest(() => { if (target.isConnected) setAnchor(target.getBoundingClientRect()); }, target);
  const hide = (atOnce = false) => { clock.stop(); if (!atOnce) fadeTipOut(tipRef.current, { copy: true }); setAnchor(null); };
  const own = useRef<HTMLButtonElement | null>(null), wasSaid = useRef(false);
  const setRef = useCallback((element: HTMLButtonElement | null) => {
    own.current = element;
    if (typeof ref === "function") ref(element); else if (ref) (ref as RefObject<HTMLButtonElement | null>).current = element;
  }, [ref]);
  useLayoutEffect(() => {
    if (said) { wasSaid.current = true; if (own.current) show(own.current); } else if (wasSaid.current) { wasSaid.current = false; hide(); }
  }, [said]);
  return (
    <>
      <button {...button} ref={setRef} type="button" aria-label={label}
        onPointerEnter={(event) => { touched.current = event.pointerType === "touch"; button.onPointerEnter?.(event); }}
        // A press takes the tip and drops a wait not yet over, as Notion's every tip goes as the mouse goes down; what a
        // press says after it (`said`, a copy's "복사됨") comes back as it is said.
        onPointerDown={(event) => {
          touched.current = event.pointerType === "touch";
          if (anchor) hide(); else clock.stop();
          button.onPointerDown?.(event);
        }}
        onMouseEnter={(event) => { pointed.current = true; if (!touched.current) rest(event.currentTarget); button.onMouseEnter?.(event); }}
        onMouseLeave={(event) => { pointed.current = false; clock.leave(hide, own.current); button.onMouseLeave?.(event); }}
        // A button drawn under a pointer already there (a handle that wakes as it is reached) never sees it enter.
        onMouseMove={(event) => { if (!pointed.current) { pointed.current = true; if (!touched.current) rest(event.currentTarget); } button.onMouseMove?.(event); }}
        onFocus={(event) => { if (keysLedFocus(event.currentTarget)) show(event.currentTarget); button.onFocus?.(event); }}
        onBlur={(event) => { if (!pointed.current) hide(); button.onBlur?.(event); }}>
        {children}
      </button>
      {anchor && typeof document !== "undefined" ? createPortal(
        <span ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-icon-tip>{said || (tip ?? label)}</span>,
        document.body,
      ) : null}
    </>
  );
}

/**
 * Words that name more in the same dark tip while the pointer is on them, as
 * Notion's "..." in a path (seen 2026-10-31) — for a spot inside a link,
 * where a button cannot go — after the tips' wait (lib/tipTiming.ts).
 */
export function HoverTip({ label, className, placement = "bottom", gap = 6, width, wide = false, wrap = false, around, children }: {
  /** Its words, or more than words (the property access window's list of who has full access). */
  label: ReactNode; className?: string;
  /** The box it is placed by, when not the words' own (a setting's name with its ⓘ: SettingInfoTip). */
  around?: (element: HTMLElement) => PopoverRect;
  /** Above, as a comment file's whole name over its card (Notion's, seen 2026-11-06); before it,
   *  as the settings teamspace table's dim "기본 ⌄" (seen 2026-11-11); above from its start, as a
   *  home section's head (seen 2026-11-11); after it, as "관계형 대상"'s ⓘ (seen 2026-11-16). */
  placement?: "top" | "top-start" | "bottom" | "left" | "right";
  /** How far from the words, 6px unless said (a home section's head: 8px, seen 2026-11-11). */
  gap?: number;
  /** Lines wrapped at this width, as Notion's ⓘ by a setting (200px, seen 2026-11-07). */
  width?: number;
  /** As wide as its words up to 300px, then wrapped, as Notion's tip over a dim
   *  settings button (seen 2026-11-11); its class holds the cap, so it is placed by its wrapped width. */
  wide?: boolean;
  /** As wide as its words up to the tip's own 240px, then wrapped (an option's ⓘ: OptionInfoTip). */
  wrap?: boolean;
  children: ReactNode;
}) {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  const clock = useTipClock();
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, placement, gap) : null,
    [anchor], undefined, false);
  return (
    <>
      <span className={className} onMouseEnter={(event) => {
        const words = event.currentTarget;
        clock.rest(() => { if (words.isConnected) setAnchor(around?.(words) ?? words.getBoundingClientRect()); });
      }} onMouseLeave={() => clock.leave(() => { fadeTipOut(tipRef.current, { copy: true }); setAnchor(null); })}
        onPointerDown={() => { clock.stop(); fadeTipOut(tipRef.current, { copy: true }); setAnchor(null); }}>
        {children}
      </span>
      {anchor && typeof document !== "undefined" ? createPortal(
        <span ref={tipRef} className={wide ? `${styles.tip} ${styles.tipWide}` : wrap ? `${styles.tip} ${styles.tipWrapped}` : styles.tip} role="tooltip"
          style={{ ...popoverPlacementStyle(place), ...(width ? { width, whiteSpace: "normal" } : null) }}>{label}</span>,
        document.body,
      ) : null}
    </>
  );
}

/**
 * A setting's ⓘ after its name, its tip wrapped at 200px and centred 6px
 * under the name with the ⓘ, as Notion's every setting row's (seen
 * 2026-11-14: "관련된 속성 추가하기 ⓘ" at 529..668 in the new relation window
 * put its 200px tip at 499, 6 under it). Where its row shows a value the
 * name reaches up to it, the row's gap short of it (seen the same day in a
 * relation's settings: a row at 1047..1321 with "꺼짐", its name 1083..1259,
 * the tip at 1071). The name is the element marked `data-setting-name`
 * holding the ⓘ, or the one beside it; the value `data-property-setting-value`.
 */
export function SettingInfoTip({ label, className }: { label: string; className?: string }) {
  return (
    <HoverTip label={label} width={200} className={className} around={settingNameBox}>
      <QuestionCircleIcon size="small" aria-label={label} />
    </HoverTip>
  );
}

/**
 * An option's ⓘ after its name, as Notion's layout builder's page settings
 * ("전체 너비 ⓘ", "데이터베이스 템플릿 ⓘ", seen 2026-11-14 at 1440): a 16px
 * circled "i" in the icons' grey, its tip centred 6px under the ⓘ alone, as
 * wide as its words up to 240px and then wrapped.
 */
export function OptionInfoTip({ label, className }: { label: string; className?: string }) {
  return (
    <HoverTip label={label} wrap className={className}>
      <InfoIcon size="small" aria-hidden="true" />
    </HoverTip>
  );
}

function settingNameBox(info: HTMLElement): PopoverRect {
  const row = info.parentElement;
  const name = info.closest<HTMLElement>("[data-setting-name]") ?? row?.querySelector<HTMLElement>(":scope > [data-setting-name]");
  const own = info.getBoundingClientRect(), named = name?.getBoundingClientRect() ?? own;
  const box = { top: Math.min(own.top, named.top), bottom: Math.max(own.bottom, named.bottom),
    left: Math.min(own.left, named.left), right: Math.max(own.right, named.right) };
  const value = row?.querySelector<HTMLElement>(":scope > [data-property-setting-value]")?.getBoundingClientRect();
  if (!row || !value) return box;
  // Whichever way the row reads: the value after the name, toward its end.
  const gap = Number.parseFloat(getComputedStyle(row).columnGap) || 0;
  if (value.left >= box.right) box.right = value.left - gap;
  else if (value.right <= box.left) box.left = value.right + gap;
  return box;
}

/**
 * The same dark tip for a whole card, as Notion's on an automation's
 * condition (seen 2026-11-10): what the card says, centred 6px under it once
 * the pointer has stayed on it 0.4s, its lines wrapped at 300px. `enter`
 * takes the card and its words; a press or leaving it hides the tip.
 * A dim menu row's reason sits before the row instead, level with it and
 * wrapped at the row's width (`placement: "left"`, `maxWidth: "anchor"`).
 */
export function useHoverTip({ delay = 400, maxWidth = 300, placement = "bottom" }: {
  delay?: number; maxWidth?: number | "anchor"; placement?: "bottom" | "left";
} = {}) {
  const [shown, setShown] = useState<{ anchor: PopoverRect; label: string } | null>(null);
  const [waiting, setWaiting] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const tipRef = useRef<HTMLSpanElement>(null);
  const anchor = shown?.anchor ?? null;
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, placement, 6) : null,
    [anchor, placement], undefined, false);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const leave = () => {
    window.clearTimeout(timer.current);
    setWaiting(false);
    fadeTipOut(tipRef.current, { copy: true });
    setShown(null);
  };
  const enter = (element: HTMLElement, label: string) => {
    window.clearTimeout(timer.current);
    if (!label) return;
    setWaiting(true);
    timer.current = window.setTimeout(() => { setWaiting(false); setShown({ anchor: element.getBoundingClientRect(), label }); }, delay);
  };
  // A press takes it, and a wait not yet over.
  useTipPress(waiting || shown !== null, leave);
  const tip = shown && typeof document !== "undefined" ? createPortal(
    <span ref={tipRef} className={styles.tip} role="tooltip"
      style={{ ...popoverPlacementStyle(place), maxWidth: maxWidth === "anchor" ? shown.anchor.right - shown.anchor.left : maxWidth, whiteSpace: "normal" }}>{shown.label}</span>,
    document.body,
  ) : null;
  return { enter, leave, tip };
}

/**
 * The name of a grid's lit cell in the same dark tip, centred 6px under it,
 * as Notion's emoji and icon grids (seen 2026-11-14): shown once the keys or
 * the pointer moved the highlight (`cellKey`), following it, and kept while
 * the cell is in the grid's sight as it scrolls; a cell the pointer only
 * rests on after the keys has none.
 */
export function GridCellTip({ gridRef, cellKey, label }: {
  gridRef: RefObject<HTMLElement | null>; cellKey: string | null; label?: string;
}) {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const grid = gridRef.current;
    const cell = grid && cellKey !== null && label ? grid.querySelector<HTMLElement>(`[data-key="${CSS.escape(cellKey)}"]`) : null;
    if (!grid || !cell) { setAnchor(null); return; }
    const measure = () => {
      const seen = grid.getBoundingClientRect(), box = cell.getBoundingClientRect();
      setAnchor(box.bottom <= seen.top || box.top >= seen.bottom ? null : popoverRectOf(cell));
    };
    measure();
    grid.addEventListener("scroll", measure, { passive: true });
    return () => grid.removeEventListener("scroll", measure);
  }, [gridRef, cellKey, label]);
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, "bottom", 6) : null,
    [anchor], undefined, false);
  return anchor && label && typeof document !== "undefined" ? createPortal(
    <span ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-grid-cell-tip>{label}</span>,
    document.body,
  ) : null;
}

/**
 * What a "+" that adds a block or a row says, as Notion's (seen 2026-11-08,
 * the block handle's and a table row's alike): two lines, the press in the
 * tip's own color and what it does in its grey — "클릭해서 아래에 추가",
 * "옵션-클릭해서 위에 추가" (Alt on a computer with no Option key).
 */
export function AddHereTip() {
  const mac = optionKeyName() === "Option";
  return (
    <>
      <span className={styles.tipLine}>{i18next.t("blockHandle:tip.click")} <span className={styles.tipNote}>{i18next.t("blockHandle:tip.below")}</span></span>
      <span className={styles.tipLine}>{i18next.t("blockHandle:tip.optionClick", { key: mac ? i18next.t("blockHandle:tip.optionKey") : "Alt" })} <span className={styles.tipNote}>{i18next.t("blockHandle:tip.above")}</span></span>
    </>
  );
}

/**
 * What a block's "⋮⋮" says, as Notion's (its language file and Chrome at
 * 1440, 2026-11-18): "드래그해서 옮기기" where the block can be moved, then
 * "클릭하거나 ⌘/로 메뉴 열기" — the drag line left out where it cannot (a
 * page locked, only read, or suggested in), the press in the tip's own color
 * and the rest in its grey as each language's words put them.
 */
export function HandleTip({ drag }: { drag: boolean }) {
  const note = <span className={styles.tipNote} />;
  return (
    <>
      {drag ? <span className={styles.tipLine}><Trans i18n={i18next} i18nKey="blockHandle:tip.drag" components={{ note }} /></span> : null}
      <span className={styles.tipLine}>
        <Trans i18n={i18next} i18nKey="blockHandle:tip.menu" values={{ key: shortcutCompactLabel("openBlockMenu") }}
          components={{ note, kbd: <kbd className={styles.tipKbd} /> }} />
      </span>
    </>
  );
}
