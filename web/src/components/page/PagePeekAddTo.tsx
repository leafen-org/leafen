import { Suspense, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import { movePopoverAnchorOf } from "@/components/movePopoverPlacement";
import { lazyScreen } from "@/lib/lazyScreen";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { Page, Session } from "@/types";
import { PageIconGlyph } from "./PageIcon";
import styles from "./PagePeekAddTo.module.css";

const MoveToDialog = lazyScreen(() => import("@/components/MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog })), { typedOnOpen: true });

/**
 * "추가 대상 ⌄" in the bar of a peek over a page the sidebar's "+" just made,
 * as Notion's (seen 2026-11-01): after a line, grey "추가 대상", the page it
 * went into with its icon and a faint ⌄; pressed, the move list hangs under
 * it and moves the page where it is chosen. Pressed, it leaves the focus
 * where it was — in the page, mostly — which gets it back as the list closes
 * (Notion's, seen 2026-11-13: Escape put the caret back at the body's end).
 */
export function PagePeekAddTo({ page, pages, session, onPageChange, onStatus }: {
  page: Page;
  pages: Page[];
  session: Session;
  onPageChange: (page: Page) => void;
  onStatus: ProductStatusSink;
}) {
  const { t } = useTranslation("sidebar");
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const parent = pages.find((candidate) => candidate.id === page.parentId);
  return (
    <span className={styles.addTo}>
      <button ref={buttonRef} type="button" className={styles.addToButton} data-page-peek-add-to data-press-keeps-focus
        aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>
        <span className={styles.addToLabel}>{t("addWindow.addTo")}</span>
        {parent ? <PageIconGlyph page={parent} size={18} /> : null}
        <span className={styles.addToTitle}>{parent ? <PageTitleText page={parent} /> : t("privatePages")}</span>
        <ChevronDown size="tiny" className={styles.addToChevron} aria-hidden="true" />
      </button>
      {/* Drawn outside the peek's bar, whose buttons' look its rows would take. */}
      {open ? createPortal(
        <Suspense fallback={null}>
          <MoveToDialog pageId={page.id} pages={pages} session={session} anchor={movePopoverAnchorOf(buttonRef.current)} addTo
            onClose={() => setOpen(false)} onMoved={() => setOpen(false)} onPageChange={onPageChange} onStatus={onStatus} />
        </Suspense>,
        document.body,
      ) : null}
    </span>
  );
}
