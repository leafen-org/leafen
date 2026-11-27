import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { useProductPages } from "@/components/product/productPagesContext";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { listedPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { openPageInNewTab } from "@/lib/databasePageLink";
import { pagePlace, placeText } from "@/lib/pagePath";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { Session, Workspace } from "@/types";
import type { WorkspaceGuestPage } from "@/workspaceMembershipWire.generated";
import guestStyles from "./WorkspaceGuestList.module.css";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { plainTitleOf } from "@/lib/closedMentions";
import { pagesByIdOf } from "@/lib/pageIndex";
import { SettingsChevron } from "./SettingsChevron";
import { SettingsPhoneMenuScreen } from "./SettingsPhoneMenuScreen";

type GuestPages = { pages: WorkspaceGuestPage[]; noAccess: number };

/**
 * A guest's "페이지 N개 ⌄", as Notion's 사람 › 게스트 (seen 2026-11-09): a
 * 300px menu right under it of the pages the reader has full access to —
 * each its icon, its title and where it sits — opening the page in a new
 * tab, the settings staying open here, as Notion's does; the rest,
 * shared pages the reader can't manage or private ones, a last dim row
 * "공유된 페이지 또는 개인 페이지 N개 · 사용 권한 없음". On a phone it opens
 * the whole "사용 권한" screen settings menus take there
 * (SettingsPhoneMenuScreen), the guest's own actions under their pages.
 */
export function WorkspaceGuestPagesButton({ session, workspace, guestKey, name, count, disabled, actions }: {
  session: Session;
  workspace: Workspace;
  /** The guest as the directory names them: an account id or "email:" and an address. */
  guestKey: string;
  name: string;
  count: number;
  disabled: boolean;
  /** On a phone, the guest's "…" rows, under their pages. */
  actions?: SidebarMenuRow[];
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [read, setRead] = useState<{ key: string; pages: GuestPages | null; failed: boolean }>({ key: "", pages: null, failed: false });
  const [attempt, setAttempt] = useState(0);
  const productPages = useProductPages();
  const { phone } = useSidebarLayout(true);
  const pagesById = pagesByIdOf(productPages?.pages ?? []);
  usePathsAboveShown((read.pages?.pages ?? []).flatMap((page) => pagesById[page.pageId] ? [pagesById[page.pageId]] : []), pagesById);
  // Every window of the guest's pages, read each time the menu opens.
  useEffect(() => {
    if (!open) return undefined;
    const controller = new AbortController();
    void (async () => {
      const pages: GuestPages = { pages: [], noAccess: 0 };
      let cursor = "";
      do {
        const window = await api.listWorkspaceGuestPages(session, workspace.id, guestKey, cursor, controller.signal);
        if (controller.signal.aborted) return;
        pages.pages.push(...window.pages);
        pages.noAccess += window.noAccess ?? 0;
        cursor = window.nextCursor ?? "";
      } while (cursor);
      setRead({ key: guestKey, pages, failed: false });
    })().catch(() => {
      if (!controller.signal.aborted) setRead({ key: guestKey, pages: null, failed: true });
    });
    return () => controller.abort();
  }, [attempt, guestKey, open, session, workspace.id]);

  const current = read.key === guestKey ? read : { pages: null, failed: false };
  const rows: SidebarMenuRow[] = [];
  if (current.failed) {
    rows.push({ key: "failed", label: t("guestPagesLoadFailed"), description: t("common:retry"), run: () => setAttempt((value) => value + 1) });
  } else if (!current.pages) {
    rows.push({ key: "loading", label: t("guestPagesLoading"), disabled: true });
  } else {
    for (const page of current.pages.pages) {
      const known = pagesById[page.pageId];
      rows.push({
        key: page.pageId,
        label: plainTitleOf(page) || untitledPageDisplayTitle(listedPageNameKind(page)),
        icon: <PageIconGlyph page={listedPageIcon(known, page, workspace.id)} size={20} />,
        description: known ? placeText(pagePlace(known, pagesById)) || undefined : undefined,
        run: () => { setOpen(false); openPageInNewTab(known ?? { id: page.pageId }, session.user.id); },
      });
    }
    if (current.pages.noAccess > 0) {
      rows.push({ key: "no-access", label: t("guestPagesNoAccess", { count: current.pages.noAccess }),
        value: t("guestPagesNoAccessTag"), disabled: true });
    }
    if (rows.length === 0) rows.push({ key: "none", label: t("guestPagesNone"), disabled: true });
  }
  return <>
    <button ref={trigger} type="button" className={guestStyles.pagesButton} aria-haspopup="menu" aria-expanded={open}
      disabled={disabled} data-workspace-guest-pages onClick={() => setOpen((value) => !value)}>
      {t("guestPageCount", { count })}
      <SettingsChevron />
    </button>
    {open && phone ? (
      <SettingsPhoneMenuScreen title={t("guestColumnAccess")} label={t("ariaGuestPages", { name })} close={() => setOpen(false)}
        rows={[...rows, ...(actions ?? []).map((row, index) => ({ ...row, separatorBefore: index === 0 }))]}
        data={{ "data-workspace-guest-pages-menu": "" }} />
    ) : open ? (
      <SidebarActionMenu button={trigger} rows={rows} width={300} gap={0} backdropLabel={t("common:actions.close")}
        label={t("ariaGuestPages", { name })} close={() => setOpen(false)} data={{ "data-workspace-guest-pages-menu": "" }} />
    ) : null}
  </>;
}
