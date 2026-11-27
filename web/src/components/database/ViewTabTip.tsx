import { type ReactNode, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { tipRequest } from "@/lib/tipPlacement";
import { touchScreen } from "@/lib/screenKind";
import { keysLedFocus } from "@/lib/lastPress";
import { useTipPress } from "@/lib/tipTiming";
import tipStyles from "../IconTipButton.module.css";

/** How long the pointer rests on a view's tab before its tip shows. */
const TIP_DELAY_MS = 500;

type ViewTabTipHandlers = {
  onMouseEnter(event: { currentTarget: HTMLElement }): void;
  onMouseLeave(): void;
  onPointerDown(): void;
  onFocus(event: { currentTarget: HTMLElement }): void;
  onBlur(): void;
};

/**
 * A view tab's dark tip under it while the pointer rests there, or at once
 * while the keyboard holds the tab (its ring showing): the view's name and,
 * under it, what it shows — "데이터베이스" — as Notion's (seen 2026-11-07:
 * "보드 / 데이터베이스", "표 / 데이터베이스"; 2026-11-12: a tab the keys
 * reach shows it within a frame, and it goes as the focus leaves). A press
 * puts it away; the pointer's goes as the app's other tips go
 * (lib/tipTiming.ts): off its tab or with a press anywhere.
 */
export function useViewTabTip(name: string, kind: string): { handlers: ViewTabTipHandlers; tip: ReactNode } {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null), timer = useRef<number | null>(null);
  // The pointer resting on the tab keeps its tip as the focus leaves.
  const pointed = useRef(false);
  const [pointedTab, setPointedTab] = useState<HTMLElement | null>(null);
  // From the tab's start, wider or narrower than the tab, as Notion's (seen 2026-11-15: its iPad's at 834 "표 /
  // 데이터베이스" at x 104, its tab's start; at 1440 in Chrome "Default view / 데이터베이스" at 374, its tab's start).
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, "bottom-start", 6) : null, [anchor], undefined, false);
  const stop = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
    setAnchor(null);
  };
  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);
  useTipPress(pointedTab !== null, () => { pointed.current = false; setPointedTab(null); stop(); }, () => pointedTab);
  const handlers: ViewTabTipHandlers = {
    onMouseEnter(event) {
      // A tap on a touch screen sends the mouse's enter after its press: a
      // tip shown then stayed over the tab's menu (2026-11-13, iPad Safari
      // 834). A touch screen has no pointer to rest, and no tip, as Notion's
      // iPad (seen 2026-11-15: a tap on a tab acts at once, with no tip).
      if (touchScreen()) return;
      const target = event.currentTarget;
      pointed.current = true;
      setPointedTab(target);
      stop();
      timer.current = window.setTimeout(() => setAnchor(target.getBoundingClientRect()), TIP_DELAY_MS);
    },
    onMouseLeave() {
      pointed.current = false;
      setPointedTab(null);
      stop();
    },
    onPointerDown: stop,
    onFocus(event) {
      if (!keysLedFocus(event.currentTarget)) return;
      stop();
      setAnchor(event.currentTarget.getBoundingClientRect());
    },
    onBlur() {
      if (!pointed.current) stop();
    },
  };
  const tip = anchor && typeof document !== "undefined" ? createPortal(
    <span ref={tipRef} className={tipStyles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-view-tab-tip>
      {name}<span className={tipStyles.tipKeys}>{kind}</span>
    </span>, document.body) : null;
  return { handlers, tip };
}
