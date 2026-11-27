import { type CSSProperties, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import sheet from "@/components/database/databaseSheet";
import type { DatabaseViewOpenMode } from "@/components/database/DatabaseView";
import { sidePeekPressBeside } from "@/components/database/rowOpenPress";
import { RowPeekViewOptions } from "@/components/database/RowPageTopBarItems";
import { storedRowPeekWidth } from "@/components/database/sidePeekWidth";
import { SuggestingModeContext, type SuggestingMode } from "@/components/editor/suggestingMode";
import { DoubleChevronRight, OpenAsPage } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { pagePeekHref } from "@/lib/pagePeek";
import { peoplePeekHref } from "@/lib/peopleDatabasePage";
import { openPersonProfile, PEOPLE_DIRECTORY_HREF, usePeopleProfileSettings } from "@/lib/peopleProfiles";
import { pushRoute, replaceRoute } from "@/lib/router";
import { useSideRowPeekPlace } from "@/lib/sideRowPeekLayout";
import { useSidePeekMotion } from "@/lib/sidePeekMotion";
import type { Session, Workspace } from "@/types";
import { PeopleDatabaseTopBarActions } from "./PeopleDatabaseTopBarActions";
import { PeopleDirectoryBody, usePeopleDirectoryRead } from "./PeopleDirectoryBody";
import styles from "./PeopleView.module.css";

// A database in a peek: its view names no window title (useDatabaseDocumentViewName).
const IN_PEEK: SuggestingMode = { active: false, suggest: async () => {}, inPeek: true };

/**
 * People beside the page open, as Notion's People opens from a favorite
 * ⌥-pressed or its "사이드 보기에서 열기" (seen 2026-11-16 in Chrome at
 * 1440, in two tabs at two times: `?p=〈People〉&pm=s`, a 720-wide peek
 * whose bar has "»", "⤢" and the view options on its left and People's
 * "공유 · 링크 · ☆ · ⋯" at 1276, 1342, 1372 and 1402 on its right, with no
 * edit time; under it People's title and views, its tabs 84px in). In the
 * middle of the window where the address says so (`pm=c`). A person opened
 * there takes the peek's place, as Notion's row opened in it; "⤢" opens
 * People as its own screen. People's own screen keeps its peek for its
 * people (PeopleView).
 */
export function PeoplePeek({ session, workspace, pageId, mode = "side", onClose: closeGiven }: {
  session: Session;
  workspace: Workspace;
  /** People's page, the address's `p`. */
  pageId: string;
  mode?: "side" | "center";
  onClose(): void;
}) {
  const { t } = useTranslation(["pagePeek", "databaseView", "peopleProfiles"]);
  const profiles = usePeopleProfileSettings();
  const directory = profiles?.workspaceId === workspace.id && profiles.settings.directory && !workspace.guest;
  const read = usePeopleDirectoryRead(session, workspace, directory);
  const panelRef = useRef<HTMLElement>(null);
  const [entered, setEntered] = useState(false);
  const [width] = useState(storedRowPeekWidth);
  const side = mode === "side";
  const leave = useSidePeekMotion(panelRef, side, pageId);
  const onClose = () => leave(closeGiven);
  useOverlayLayer(true, { kind: side ? "side-peek" : "dialog", ref: panelRef, modal: !side, modalOwned: () => [panelRef.current?.previousElementSibling], onEscape: onClose,
    ...(side ? sidePeekPressBeside(onClose) : {}) });
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => { setEntered(true); panelRef.current?.focus(); });
    return () => window.cancelAnimationFrame(frame);
  }, [pageId]);
  useSideRowPeekPlace(panelRef, side, width, pageId);
  const openMode = (next: DatabaseViewOpenMode) => {
    if (next === "full") pushRoute(PEOPLE_DIRECTORY_HREF);
    else if (next === "new_tab") window.open(PEOPLE_DIRECTORY_HREF, "_blank", "noopener");
    else replaceRoute(peoplePeekHref(pageId, next === "center" ? "center" : "side"));
  };
  const motion = entered ? "open" : "opening";
  const database = read.database;
  return createPortal(
    <>
      {side ? null : <button type="button" className={sheet.rowPeekBackdrop} data-mode="center" data-motion-state={motion}
        tabIndex={-1} aria-label={t("pagePeek:close")} onClick={onClose} />}
      <aside ref={panelRef} className={sheet.rowPeek} data-mode={mode} data-motion-state={motion} data-people-peek={pageId}
        style={side ? { "--row-peek-width": `${width}px` } as CSSProperties : undefined}
        role="dialog" aria-modal={side ? undefined : true} tabIndex={-1} aria-label={t("peopleProfiles:people")}>
        <div className={sheet.rowPeekTop}>
          <div className={sheet.rowPeekChromeSide} aria-label={t("databaseView:peekOptions", { title: t("peopleProfiles:people") })}>
            {side ? (
              <button type="button" className={sheet.rowPeekIconAction} data-people-peek-close
                aria-label={t("pagePeek:close")} title={t("pagePeek:close")} onClick={onClose}>
                <DoubleChevronRight size="regular" />
              </button>
            ) : null}
            <button type="button" className={sheet.rowPeekIconAction} data-people-peek-open
              aria-label={t("databaseView:openAsFullPage")} title={t("databaseView:openAsFullPage")}
              onClick={() => openMode("full")} onAuxClick={(event) => { if (event.button === 1) openMode("new_tab"); }}>
              <OpenAsPage size="small" />
            </button>
            <RowPeekViewOptions mode={mode} onOpenMode={openMode} />
          </div>
          {database?.updatedAt ? (
            <div className={sheet.rowPeekActions} data-people-peek-actions>
              <PeopleDatabaseTopBarActions session={session} workspace={workspace} database={database} title={t("peopleProfiles:people")}
                editedTime={false} />
            </div>
          ) : null}
        </div>
        <div className={`${sheet.rowPeekScroll} nscroll`}>
          <div className={sheet.rowPeekDoc} data-database-peek="true" data-people-directory data-peek-mode={mode}>
            <div className={styles.peekDirectory}>
              <SuggestingModeContext.Provider value={IN_PEEK}>
                <PeopleDirectoryBody session={session} workspace={workspace} directory={directory} read={read}
                  onOpenPerson={(userId, open) => open === "full" || open === "new_tab" ? openPersonProfile(userId, open === "new_tab")
                    : pushRoute(pagePeekHref(userId, open === "center" ? "center" : "side"))} />
              </SuggestingModeContext.Provider>
            </div>
          </div>
        </div>
      </aside>
    </>,
    document.body,
  );
}
