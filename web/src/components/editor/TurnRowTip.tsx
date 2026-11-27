import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { placeRowTip, type PopoverPlacement, type PopoverRect, rowTipStyle, samePopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { BlockType } from "@/lib/types";
import { blockDefDescription } from "./blockDefinitionLabels";
import { getDef } from "./blockDefinitionOrder";
import type { BlockTurnChoice } from "./blockTurnInto";
import { SlashTipPreview } from "./SlashTipPreview";
import styles from "./editorStyles.generated.module.css";

/**
 * The tip beside a turn list's lit row, a small sample of the block over what
 * it is, as Notion's "전환" lists show it — the block menu's and the selection
 * toolbar's alike: once the keys ring the row (↓, Tab, the list opened by the
 * keys) or the pointer has moved in the list; the row a press opened the list
 * on shows none (seen 2026-11-01 and 2026-11-11 on the block menu's; the
 * toolbar's on 2026-11-26 in Chrome 1440: Tab ringed "텍스트" with its tip in
 * two tabs, ↓ "제목1" with its, the pointer moving on into "코드" took both).
 * `shown` says it for a list that keeps its own count of that. A sheet has no
 * tip, as Notion's phone and iPad "전환" (seen 2026-11-13).
 */
export function TurnRowTip({ menuRef, choice, place, shown }: {
  menuRef: RefObject<HTMLElement | null>; choice: BlockTurnChoice | undefined; place: PopoverPlacement | null; shown?: boolean;
}) {
  const { phone } = useSidebarLayout(true);
  const type = !phone && choice && choice.type !== "column_list" && !choice.placeIn ? choice.type as BlockType : undefined;
  const [pointerMoved, setPointerMoved] = useState(false);
  useEffect(() => {
    const menu = menuRef.current;
    if (shown !== undefined || !menu) return;
    const moved = (event: PointerEvent) => { if (event.pointerType !== "touch" && (event.movementX || event.movementY)) setPointerMoved(true); };
    menu.addEventListener("pointermove", moved);
    return () => menu.removeEventListener("pointermove", moved);
  }, [menuRef, shown]);
  const [row, setRow] = useState<PopoverRect | null>(null), tipRef = useRef<HTMLDivElement>(null);
  const tip = usePopoverPlacement(tipRef, row ? (size, viewport) => placeRowTip(row, size, viewport) : null, [row], undefined, false);
  // Aria may keep the DOM focus on the list; its lit row owns the tip, read as its rows are drawn.
  const placeTip = useCallback(() => {
    const box = menuRef.current?.getBoundingClientRect();
    const lit = menuRef.current?.querySelector<HTMLElement>('[data-focused="true"]'), at = lit?.getBoundingClientRect();
    const on = shown ?? (pointerMoved || lit?.dataset.focusVisible === "true");
    if (!box || !at || !on || !type || !blockDefDescription(getDef(type)) || at.bottom <= box.top || at.top >= box.bottom) {
      setRow(null); return;
    }
    const next = { top: at.top, bottom: at.bottom, left: at.left, right: at.right };
    setRow(current => samePopoverRect(current, next) ? current : next);
  }, [menuRef, shown, pointerMoved, type]);
  useLayoutEffect(placeTip);
  useEffect(() => {
    const menu = menuRef.current;
    menu?.addEventListener("scroll", placeTip);
    return () => menu?.removeEventListener("scroll", placeTip);
  }, [menuRef, placeTip]);
  return row && type ? <div ref={tipRef} className={styles.slashTip} style={rowTipStyle(tip, place)} role="tooltip" data-block-turn-tip>
    <SlashTipPreview def={getDef(type)} />{blockDefDescription(getDef(type))}</div> : null;
}
