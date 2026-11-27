import { useLayoutEffect, useRef, useState } from "react";
import type { Session } from "@/types";
import { opensPageUpdates } from "@/lib/pageAnalyticsPanelService";
import { usePageEditedAt } from "@/lib/pageEditedHere";
import { type TopBarActivitySource, useTopBarActivityCard } from "./TopBarActivityCard";
import { useEditedLabel } from "./topBarEditedLabel";
import own from "./TopBarEditedTime.module.css";
import { styles } from "./topBarStyles";

/**
 * When a page was last edited, at the start of its bar's right — before the
 * faces and "공유" — on every page, one only its reader opens too, as Notion's
 * (seen 2026-11-14 and twice on 2026-11-15: a new private page "지금 편집", a
 * private page alone in a one-member workspace "4분 전 편집" after a reload;
 * on 2026-10-28 private pages had none). The same in a page's bar, a row's
 * page opened full and a database opened alone, as Notion's (seen 2026-11-15:
 * a row's full page "8분 전 편집" 14px, 0 8px, 28px high, like a page's), in
 * the tertiary text colour: a button which opens the page's updates for one
 * who may open them, else (no `onOpenUpdates`) the time alone, which takes
 * no press, pointer, hover or focus (opensPageUpdates). With the pointer on
 * it, who edited the page last and who made it (`activity`, its card under
 * it, for the workspace's own people: useTopBarActivityCard). A narrow bar
 * leaves it out (useBarRoom).
 */
export function TopBarEditedTime({ updatedAt, onOpenUpdates, activity, ...marks }: {
  updatedAt: string;
  onOpenUpdates?: () => void;
  activity?: TopBarActivitySource;
} & { [mark: `data-${string}`]: true | undefined }) {
  const label = useEditedLabel(updatedAt);
  const [node, setNode] = useState<HTMLElement | null>(null);
  const anchor = useRef<HTMLElement | null>(null);
  anchor.current = node;
  const { hover, card } = useTopBarActivityCard(activity, anchor);
  const style = useBarRoom(node) ? undefined : { display: "none" };
  if (!label) return null;
  return <>{onOpenUpdates ? (
    <button ref={setNode} type="button" className={styles.editedTime} style={style} {...marks} {...hover} onClick={onOpenUpdates}>{label}</button>
  ) : <span ref={setNode} className={`${styles.editedTime} ${own.shownOnly}`} style={style} {...marks} {...hover}>{label}</span>}{card}</>;
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

/**
 * A page's, drawn again alone as the page is edited here (lib/pageEditedHere.ts):
 * pressed, its updates for one who may edit the page (opensPageUpdates); under
 * the pointer, its activity read again from the server (TopBarActivityCard).
 */
export function PageEditedTime({ page, session, canEdit, onOpenUpdates }: {
  page: TopBarActivitySource["page"]; session: Session; canEdit: boolean; onOpenUpdates(): void;
}) {
  return <TopBarEditedTime updatedAt={usePageEditedAt(page)} onOpenUpdates={opensPageUpdates(canEdit) ? onOpenUpdates : undefined}
    activity={{ session, page, fresh: true }} data-topbar-edited-time />;
}
