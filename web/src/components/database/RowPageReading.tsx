import { useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { TopBarBreadcrumbs } from "@/components/page/TopBarBreadcrumbs";
import { TopBarSidebarToggle } from "@/components/page/TopBarSidebarToggle";
import { TopBarTeamspaceCrumb } from "@/components/page/TopBarTeamspaceCrumb";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { useProductPages } from "@/components/product/productPagesContext";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { knownPathAboveId, knownPathPage } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import sheet from "./databaseSheet";
import { useMainContentRect } from "./useMainContentRect";

const NO_PAGES: readonly [] = [];

/** Where the main content area is now, for the frame's first drawing. */
function mainContentRect() {
  const bounds = typeof document === "undefined" ? null : document.getElementById("app-main-content")?.getBoundingClientRect();
  return bounds ? { left: bounds.left, top: bounds.top, width: bounds.width, height: bounds.height } : null;
}

/**
 * A database page opened as its own page, while its address is asked about
 * and it is read (AuthenticatedProduct, StandaloneRowPage): its frame, and
 * in its bar the path this window already knows — the page as it was last
 * drawn here (pagePathPieces.ts rememberDrawnRowPiece) under the pieces the
 * server named above it — from its first frame, in the place the page's own
 * bar then draws it (RowPagePath). Coming back to such a page from a page
 * under it, by the parent key or the way back, its bar stood empty until
 * both answers came (some 290 ms on a NAS-class server, 2026-11-18), where
 * Notion's draws the whole path with the page at once (seen 2026-11-18,
 * Back and ⌘⇧U: "… / DB / 행" some 100–115 ms after, never an empty bar).
 * A page this window hasn't drawn, a phone's, whose bar draws the page's
 * own piece alone, and a database of the page opened alone wait as before.
 * It asks the server nothing, beside the two answers the page waits on: the
 * page's own bar reads the path again once the page is here, as before.
 */
export function RowPageReading({ rowId }: { rowId: string }) {
  const productPages = useProductPages();
  const { phone } = useSidebarLayout(true);
  // The paths read meanwhile draw it again.
  usePagePathsAbove(NO_PAGES);
  const [first] = useState(mainContentRect);
  const rect = useMainContentRect(true) ?? first;
  const page = knownPathPage(rowId), named = knownPathAboveId(rowId);
  const pages = productPages?.pages, session = productPages?.session;
  // Its path as its own bar draws it: through the piece the server named above it, else from the page holding its database.
  const known = page && pages && session && named !== undefined && (named || pages.some(({ id }) => id === page.parentId));
  if (!known || phone || inlineDatabaseOpenedAlone() || typeof document === "undefined") return <div aria-busy="true" data-standalone-row-loading />;
  return createPortal((
    <aside className={sheet.rowPeek} data-mode="full" aria-busy="true" data-standalone-row-loading
      style={rect ? ({ left: rect.left, top: rect.top, width: rect.width, height: rect.height } as CSSProperties) : undefined}>
      <div className={sheet.rowPeekTop} data-topbar-bar>
        <div className={sheet.rowPeekChromeSide} data-row-peek-chrome-side data-path="true">
          <TopBarSidebarToggle />
          <div className={topBarStyles.left} data-row-page-path>
            <TopBarTeamspaceCrumb page={page} pages={pages} session={session} />
            <TopBarBreadcrumbs page={page} pages={pages} known />
          </div>
        </div>
      </div>
    </aside>
  ), document.body);
}
