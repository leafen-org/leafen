import { Activity, memo, useEffect, useRef, useState, type ComponentProps } from "react";
import { RowMenu } from "@/components/RowMenu";
import { onDesktopMenuCommand } from "@/lib/desktopMenuCommands";
import { afterStart } from "@/lib/startSettled";

type RowMenuProps = ComponentProps<typeof RowMenu>;

// Put away, the menu keeps what it was drawn with: each new drawing of the
// bar doesn't draw it again too. Shown, it takes the bar's own, and its layer
// and the focus to give back as it shows (RowMenu's "ahead").
const HeldRowMenu = memo(function HeldRowMenu({ shown, ...props }: RowMenuProps & { shown: boolean }) {
  return <RowMenu {...props} ahead={!shown} />;
}, (_before, next) => !next.shown);

/**
 * The page's "…" menu in the top bar, and in a page peek's (PagePeekActions).
 * Once warm it is drawn ahead, hidden (React's Activity): its press shows
 * what is there and draws only its
 * items, which React Aria lists as a menu shows (2026-11-13, a database page
 * of 14,000 elements on a NAS-class server: a first press's script 34 ms
 * where it was made whole in 40, a second's 23 where 30). Hidden it runs
 * nothing: what it reads (notifications, the last edit, apps, connections,
 * offline) is asked as it shows, its place, its layer and the focus it goes
 * back to are taken then, and each opening is a new menu, the one drawn
 * ahead after the last one closed. Opened at the page's updates, or as a
 * phone's sheet (which takes its layer as it is made, PhoneSheet), it is
 * made as it opens. A touch screen wider than a phone's draws it as the
 * phone's "작업" sheet in the screen's middle, as Notion's iPad (RowMenu).
 */
export function TopBarPageMenu({ open, warm, ...props }: Omit<RowMenuProps, "initialPanel" | "ahead"> & {
  open: false | "menu" | "updates" | "export";
  warm: boolean;
}) {
  const [round, setRound] = useState(0);
  const [was, setWas] = useState(open);
  if (was !== open) {
    setWas(open);
    if (!open) setRound((count) => count + 1);
  }
  if (!open && !warm) return null;
  const panel = open === "updates" || open === "export" ? open : undefined;
  return (
    <Activity mode={open ? "visible" : "hidden"}>
      <HeldRowMenu key={`${props.page.id}:${panel ?? "menu"}:${round}`} shown={Boolean(open)}
        initialPanel={panel} {...props} />
    </Activity>
  );
}

/**
 * The computer app's 파일 › 인쇄 opens the shown page's export window, as
 * Notion's app's does (its print asks the page "notion:export-active-page",
 * 7.43 source; seen 2026-11-16): the page's own bar answers while `answers`.
 */
export function usePrintOpensExport(answers: boolean, open: () => void): void {
  const latest = useRef(open);
  latest.current = open;
  useEffect(() => (answers ? onDesktopMenuCommand("exportPage", () => latest.current()) : undefined), [answers]);
}

/**
 * Whether the page's "…" menu is drawn ahead (TopBarPageMenu): once the
 * pointer or focus reaches its button, or at an idle moment once the page's
 * start has settled (startSettled.ts; a touch has no pointer before it). The
 * first idle moment comes while the start waits on the network, and the menu
 * was drawn beside the start's own drawing.
 */
export function usePageMenuWarm(pageId: string | undefined): [boolean, () => void] {
  const [warm, setWarm] = useState(false);
  useEffect(() => {
    if (!pageId || warm) return undefined;
    let live = true;
    afterStart(() => { if (live) setWarm(true); });
    return () => { live = false; };
  }, [pageId, warm]);
  return [warm, () => setWarm(true)];
}
