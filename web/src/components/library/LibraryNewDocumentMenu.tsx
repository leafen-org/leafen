import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { UsableApp, UsableAppFileOpener, UsableAppNewFile } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { SidebarPagePicker } from "@/components/sidebar/SidebarPagePicker";
import { ChevronDown, ChevronRight } from "@/icons/leafen";
import { usePageNames } from "@/lib/pageNames";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement, type PopoverAnchor } from "@/lib/popoverPlacement";
import type { Page, Session } from "@/types";
import type { WorkspaceDocumentKind } from "@/workspaceMembershipWire.generated";
import documentStyles from "./LibraryDocuments.module.css";
import tabStyles from "./LibraryViewTabs.module.css";

/** A kind of new document and the app that starts it. */
export type LibraryNewDocumentChoice = { app: UsableApp; opener: UsableAppFileOpener; made: UsableAppNewFile };

/**
 * The kinds of new document the reader's apps start (manifest newFiles), a
 * kind once, the app of the page's organization first.
 */
export function libraryNewDocumentChoices(apps: readonly UsableApp[], organizationId?: string): LibraryNewDocumentChoice[] {
  const byType = new Map<string, LibraryNewDocumentChoice>();
  for (const app of apps) {
    for (const opener of app.fileOpeners ?? []) {
      if (!opener.edit) continue;
      for (const made of opener.newFiles ?? []) {
        if (!byType.has(made.type) || app.tenancyKey === `org:${organizationId}`) byType.set(made.type, { app, opener, made });
      }
    }
  }
  const order = [".docx", ".xlsx", ".pptx", ".hwpx"];
  return [...byType.values()].sort((left, right) => (order.indexOf(left.made.type) + 1 || 99) - (order.indexOf(right.made.type) + 1 || 99));
}

/**
 * "새 문서" in the documents' tab row (document apps decision 12):
 * a menu of the kinds the reader's apps start, then where the new document
 * goes — the reader's own "문서" page until another is chosen.
 */
export function LibraryNewDocumentMenu({ choices, pages, session, workspaceId, busy, icon, onCreate }: {
  choices: readonly LibraryNewDocumentChoice[];
  pages: readonly Page[];
  session: Session;
  workspaceId: string;
  busy: boolean;
  icon: (kind: WorkspaceDocumentKind) => ReactNode;
  /** Makes the kind in the page chosen, or "" for the reader's own "문서" page. */
  onCreate: (choice: LibraryNewDocumentChoice, pageId: string) => void;
}) {
  const pageNames = usePageNames();
  const { t, i18n } = useTranslation("library");
  const [anchor, setAnchor] = useState<PopoverAnchor | null>(null);
  const [target, setTarget] = useState("");
  const [picking, setPicking] = useState<DOMRect | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const close = () => { setAnchor(null); setPicking(null); };
  const place = usePopoverPlacement(ref, anchor ? (size, viewport) => placeAround(anchor, size, viewport, { gap: 4 }) : null, [anchor],
    undefined, { kind: "menu", inside: () => [anchor?.contextElement ?? null], onEscape: () => { if (!picking) close(); }, onOutsidePress: () => { if (!picking) close(); } });
  const targetPage = pages.find((page) => page.id === target);
  if (choices.length === 0) return null;
  return (
    <>
      <button type="button" className={`${documentStyles.choiceButton} ${documentStyles.newDocument}`} aria-haspopup="menu"
        aria-expanded={anchor !== null} disabled={busy} data-library-new-document
        onClick={(event) => { const button = event.currentTarget; setAnchor((open) => (open ? null : popoverAnchorOf(button))); }}>
        {t("documentList.newDocument")}<ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {anchor ? createPortal(
        <div ref={ref} className={tabStyles.viewOptions} role="menu" aria-label={t("documentList.newDocument")} data-library-new-document-menu
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          {choices.map((choice) => (
            <button key={choice.made.type} type="button" role="menuitem" className={tabStyles.viewOptionsItem}
              data-library-new-document-kind={choice.made.type}
              onClick={() => { close(); onCreate(choice, target); }}>
              {icon(choice.made.type.slice(1) as WorkspaceDocumentKind)}
              <span>{appText(choice.made.name, i18n.language)}</span>
            </button>
          ))}
          <div className={documentStyles.menuDivider} role="separator" />
          <button type="button" role="menuitem" className={`${tabStyles.viewOptionsItem} ${documentStyles.where}`} data-library-new-document-where
            onClick={(event) => setPicking(event.currentTarget.getBoundingClientRect())}>
            <span className={documentStyles.whereLabel}>{t("documentList.where")}</span>
            <span className={documentStyles.whereValue}>{targetPage ? pageNames.title(targetPage) : t("documentList.homeTitle")}</span>
            <ChevronRight size="tiny" aria-hidden="true" />
          </button>
        </div>,
        document.body,
      ) : null}
      {picking ? (
        <SidebarPagePicker anchor={picking} placeholder={t("documentList.choosePage")} pages={pages} session={session} workspaceId={workspaceId}
          onChoose={(pageId) => { setTarget(pageId); setPicking(null); }} onClose={() => setPicking(null)} />
      ) : null}
    </>
  );
}
