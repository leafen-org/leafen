import { type CSSProperties, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import sheet from "@/components/database/databaseSheet";
import { sidePeekPressBeside } from "@/components/database/rowOpenPress";
import { storedRowPeekWidth } from "@/components/database/sidePeekWidth";
import { DoubleChevronRight, OpenAsPage } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { personProfileHref } from "@/lib/peopleProfiles";
import { pushRoute } from "@/lib/router";
import { useSideRowPeekPlace } from "@/lib/sideRowPeekLayout";
import { useSidePeekMotion } from "@/lib/sidePeekMotion";
import type { Session, Workspace } from "@/types";
import { PersonProfileView } from "./PersonProfileView";

/**
 * A person's profile over the people list, as Notion's People opens a row:
 * beside it (seen 2026-11-15, `?p=…&pm=s`), or in the middle of the window
 * where the address says so (`pm=c`), its bar's "⤢" opening the profile as
 * a page; Escape and the way back close it, and the backdrop in the middle.
 * A person mention pressed in a peek opens the profile in that peek, beside
 * the page or in its middle (`pm=s|c`), as Notion's (seen 2026-11-12);
 * beside it, "»" closes it too and the page makes room.
 */
export function PersonPeek({ session, workspace, userId, mode = "center", onClose: closeGiven }: {
  session: Session;
  workspace: Workspace;
  userId: string;
  mode?: "side" | "center";
  onClose(): void;
}) {
  const { t } = useTranslation(["pagePeek", "databaseView"]);
  const panelRef = useRef<HTMLElement>(null);
  const [entered, setEntered] = useState(false);
  const [width] = useState(storedRowPeekWidth);
  const side = mode === "side";
  const leave = useSidePeekMotion(panelRef, side, userId);
  const onClose = () => leave(closeGiven);
  useOverlayLayer(true, { kind: side ? "side-peek" : "dialog", ref: panelRef, modal: !side, modalOwned: () => [panelRef.current?.previousElementSibling], onEscape: onClose,
    ...(side ? sidePeekPressBeside(onClose) : {}) });
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => { setEntered(true); panelRef.current?.focus(); });
    return () => window.cancelAnimationFrame(frame);
  }, [userId]);
  useSideRowPeekPlace(panelRef, side, width, userId);
  const motion = entered ? "open" : "opening";
  return createPortal(
    <>
      {side ? null : <button type="button" className={sheet.rowPeekBackdrop} data-mode="center" data-motion-state={motion}
        tabIndex={-1} aria-label={t("pagePeek:close")} onClick={onClose} />}
      <aside ref={panelRef} className={sheet.rowPeek} data-mode={mode} data-motion-state={motion} data-person-peek={userId}
        style={side ? { "--row-peek-width": `${width}px` } as CSSProperties : undefined}
        role="dialog" aria-modal={side ? undefined : true} tabIndex={-1} aria-label={t("pagePeek:close")}>
        <div className={sheet.rowPeekTop}>
          <div className={sheet.rowPeekChromeSide}>
            {side ? (
              <button type="button" className={sheet.rowPeekIconAction} data-person-peek-close
                aria-label={t("pagePeek:close")} title={t("pagePeek:close")} onClick={onClose}>
                <DoubleChevronRight size="regular" />
              </button>
            ) : null}
            <button type="button" className={sheet.rowPeekIconAction} data-person-peek-open
              aria-label={t("databaseView:openAsFullPage")} title={t("databaseView:openAsFullPage")}
              onClick={() => pushRoute(personProfileHref(userId))}>
              <OpenAsPage size="regular" />
            </button>
          </div>
        </div>
        <div className={`${sheet.rowPeekScroll} nscroll`}>
          <PersonProfileView key={userId} session={session} workspace={workspace} userId={userId} embedded />
        </div>
      </aside>
    </>,
    document.body,
  );
}
