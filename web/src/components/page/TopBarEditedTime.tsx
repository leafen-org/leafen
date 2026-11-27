import { useLayoutEffect, useState } from "react";
import type { Page } from "@/types";
import { usePageEditedAt } from "@/lib/pageEditedHere";
import { useEditedLabel } from "./topBarEditedLabel";
import { styles } from "./topBarStyles";

/**
 * When a page was last edited, at the start of its bar's right — before the
 * faces and "공유" — on every page, one only its reader opens too, as Notion's
 * (seen 2026-11-14 and twice on 2026-11-15: a new private page "지금 편집", a
 * private page alone in a one-member workspace "4분 전 편집" after a reload;
 * on 2026-10-28 private pages had none). The same in a page's bar, a row's
 * page opened full and a database opened alone, as Notion's (seen 2026-11-15:
 * a row's full page "8분 전 편집" 14px, 0 8px, 28px high, like a page's): a
 * button in the tertiary text colour which opens the page's updates. A
 * narrow bar leaves it out (useBarRoom).
 */
export function TopBarEditedTime({ updatedAt, onOpenUpdates, ...marks }: {
  updatedAt: string;
  onOpenUpdates?: () => void;
} & { [mark: `data-${string}`]: true | undefined }) {
  const label = useEditedLabel(updatedAt);
  const [node, setNode] = useState<HTMLElement | null>(null);
  const style = useBarRoom(node) ? undefined : { display: "none" };
  if (!label) return null;
  return onOpenUpdates ? (
    <button ref={setNode} type="button" className={styles.editedTime} style={style} {...marks} onClick={onOpenUpdates}>{label}</button>
  ) : <span ref={setNode} className={styles.editedTime} style={style} {...marks}>{label}</span>;
}

// The bar width below which the edited time is left out, as Notion's: its bar
// 1150px wide or more shows it and 1149 leaves it out, whatever the path and
// whatever the page — a page (a one-step and a five-step path), a row's page
// opened full and a database opened alone (seen 2026-11-15 under the width
// lock: windows 1420 and 1419 with the sidebar at 270, 1440·1200·1040·1024
// besides; on 2026-11-07 the sidebar dragged wider at 1440 left it out at 1149).
const EDITED_TIME_MIN_BAR_WIDTH = 1150;

/**
 * Whether the bar holding the edited time (marked data-topbar-bar) is wide
 * enough for it: its whole width, its padding with it, as the observer saw it
 * before the frame was drawn — never laid out again to read it (TopBar).
 */
function useBarRoom(node: HTMLElement | null) {
  const [roomy, setRoomy] = useState(true);
  useLayoutEffect(() => {
    const bar = node?.closest<HTMLElement>("[data-topbar-bar]");
    if (!bar || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      setRoomy((entry.borderBoxSize?.[0]?.inlineSize ?? bar.offsetWidth) >= EDITED_TIME_MIN_BAR_WIDTH);
    });
    observer.observe(bar);
    return () => observer.disconnect();
  }, [node]);
  return roomy;
}

/** A page's, drawn again alone as the page is edited here (lib/pageEditedHere.ts). */
export function PageEditedTime({ page, onOpenUpdates }: { page: Pick<Page, "id" | "updatedAt">; onOpenUpdates(): void }) {
  return <TopBarEditedTime updatedAt={usePageEditedAt(page)} onOpenUpdates={onOpenUpdates} data-topbar-edited-time />;
}
