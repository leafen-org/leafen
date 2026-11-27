import { useEffect, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";
import { Copy, DotsHorizontal, HamburgerIcon, Search, ShareSiteIcon, SignInIcon } from "@/icons/leafen";
import { loadSession } from "@/lib/authSessionStore";
import { duplicatePublicPage } from "@/lib/publicPageDuplicate";
import { TitleWithCustomEmoji, type NamedTitle } from "./page/TitleWithCustomEmoji";
import { PublicSiteSharePanel } from "./PublicSiteSharePanel";
import styles from "./PublicSiteActions.module.css";

type OpenMenu = "share" | "more" | "duplicate" | "sheet" | "sheet-more" | null;

/**
 * A published site's own buttons at the right of its top bar, as Notion's
 * (seen 2026-10-11): "소셜 미디어에 사이트 공유" and "추가 작업" (⋯). The share
 * menu copies the page's address or hands it to a service; "⋯" leads to this
 * server's sign-in. Notion's "페이지 신고" is left out, and its
 * "쿠키 설정" and "지금 시작하기" belong to Notion's own service.
 * Where the site lets visitors duplicate it, "복제" copies the page into a
 * workspace the visitor picks, after they sign in to this server (Notion's
 * help "Duplicate public pages", read 2026-10-12).
 *
 * On a phone the bar keeps its title, the share button and "추가 작업" as
 * three lines (☰), as Notion's site at 390 and 640px (seen 2026-10-19): ☰
 * opens a sheet from the bottom over a dimmed page with the bar's page links,
 * then 검색, 복제 and 더 보기 (the "⋯" menu's items) — at 768px the links and
 * buttons sit in the bar.
 */
export function PublicSiteActions({ title, titleNamed, share = true, duplicate, navigation = [], onOpenPage, onSearch, menuToggleRef }: {
  title: string;
  /** What names the title, for its custom emoji to be drawn where it is shown (TitleWithCustomEmoji). */
  titleNamed?: NamedTitle;
  /** False when the site's editor turned its share button off, as Notion's "공유" switch. */
  share?: boolean;
  duplicate?: { slug: string; pageId: string };
  navigation?: { id: string; label: string; named?: NamedTitle; active: boolean }[];
  onOpenPage?: (pageId: string) => void;
  onSearch?: () => void;
  menuToggleRef?: RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useTranslation("publicSiteView");
  const [open, setOpen] = useState<OpenMenu>(null);
  const [workspaces, setWorkspaces] = useState<{ id: string; name: string }[] | null>(null);
  const [duplicating, setDuplicating] = useState(false);
  const [failure, setFailure] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const sheetRef = useRef<HTMLDivElement>(null);
  const ownToggleRef = useRef<HTMLButtonElement>(null);
  const toggleRef = menuToggleRef ?? ownToggleRef;
  const sheetOpen = open === "sheet" || open === "sheet-more";

  async function openDuplicate() {
    const session = loadSession();
    // A visitor signs in to this server first, as Notion asks its visitors to.
    if (!session) { window.location.assign("/"); return; }
    setFailure("");
    setOpen("duplicate");
    if (workspaces) return;
    try {
      // Every workspace of the person, a window at a time: a window of fewer
      // than a full one ends them (the pages that come along are passed by).
      const listed: { id: string; name: string; guest?: boolean }[] = [];
      let cursor: string | null = null;
      for (;;) {
        const window = await api.bootstrap(session, cursor, undefined, { pageLimit: 1 });
        listed.push(...window.workspaces);
        if (window.workspaces.length < workspaceMembershipBounds.bootstrapWorkspaces || !window.hasMore || !window.nextCursor) break;
        cursor = window.nextCursor;
      }
      setWorkspaces(listed.filter((workspace) => !workspace.guest).map((workspace) => ({ id: workspace.id, name: workspace.name })));
    } catch {
      setWorkspaces([]);
    }
  }

  async function duplicateInto(workspaceId: string) {
    const session = loadSession();
    if (!session || !duplicate) return;
    setDuplicating(true);
    setFailure("");
    try {
      const pageId = await duplicatePublicPage(session, duplicate.slug, duplicate.pageId, workspaceId, new AbortController().signal);
      window.location.assign(`/p/${encodeURIComponent(pageId)}`);
    } catch (error) {
      setFailure(t((error as { code?: string }).code === "LEAFEN_PAGE_DUPLICATION_CONFLICT" ? "duplicateBusy" : "duplicateFailed"));
      setDuplicating(false);
    }
  }

  useEffect(() => {
    if (!open) return undefined;
    const onPointer = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(null);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpen(null);
      if (open === "sheet" || open === "sheet-more") toggleRef.current?.focus();
    };
    document.addEventListener("pointerdown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, toggleRef]);
  useEffect(() => {
    if (open === "sheet" || open === "sheet-more") sheetRef.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus();
  }, [open]);

  const link = typeof window === "undefined" ? "" : window.location.href;
  const toggle = (menu: OpenMenu) => setOpen((current) => current === menu ? null : menu);
  const closeSheet = () => {
    setOpen(null);
    toggleRef.current?.focus();
  };

  return (
    <div ref={rootRef} className={styles.actions} data-site-actions>
      {share ? (
        <button type="button" className={styles.button} aria-label={t("shareSite")} title={t("shareSite")}
          aria-haspopup="dialog" aria-expanded={open === "share"} onClick={() => toggle("share")} data-site-share>
          <ShareSiteIcon size="regular" aria-hidden="true" />
        </button>
      ) : null}
      {/* Share, then "복제", as Notion's site bar (seen 2026-11-05). */}
      {duplicate ? (
        <button type="button" className={`${styles.textButton} ${styles.wide}`} aria-label={t("duplicate")} title={t("duplicate")}
          aria-haspopup="menu" aria-expanded={open === "duplicate"}
          onClick={() => (open === "duplicate" ? setOpen(null) : void openDuplicate())} data-site-duplicate>
          <Copy size="small" aria-hidden="true" /><span className={styles.textLabel}>{t("duplicate")}</span>
        </button>
      ) : null}
      <button type="button" className={`${styles.button} ${styles.wide} ${styles.more}`} aria-label={t("moreActions")} title={t("moreActions")}
        aria-haspopup="menu" aria-expanded={open === "more"} onClick={() => toggle("more")} data-site-more>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button>
      <button ref={toggleRef} type="button" className={`${styles.button} ${styles.narrow}`} aria-label={t("moreActions")}
        title={t("moreActions")} aria-haspopup="menu" aria-expanded={sheetOpen}
        onClick={() => (sheetOpen ? closeSheet() : setOpen("sheet"))} data-site-sheet-toggle>
        <HamburgerIcon size="regular" aria-hidden="true" />
      </button>
      {open === "share" ? <PublicSiteSharePanel link={link} title={title} titleNamed={titleNamed} /> : null}
      {open === "duplicate" ? (
        <div className={styles.menu} role="menu" aria-label={t("duplicateTo")} data-site-menu="duplicate">
          <div className={styles.menuLabel}>{t("duplicateTo")}</div>
          {workspaces === null ? <div className={styles.menuNote}>{t("duplicateLoading")}</div>
            : workspaces.length === 0 ? <div className={styles.menuNote}>{t("duplicateNoWorkspaces")}</div>
              : workspaces.map((workspace) => (
                <button key={workspace.id} type="button" role="menuitem" className={styles.item} disabled={duplicating}
                  onClick={() => void duplicateInto(workspace.id)} data-site-duplicate-workspace={workspace.id}>
                  {workspace.name}
                </button>
              ))}
          {duplicating ? <div className={styles.menuNote} role="status">{t("duplicating")}</div> : null}
          {failure ? <div className={styles.menuNote} role="alert" data-error>{failure}</div> : null}
        </div>
      ) : null}
      {open === "more" ? (
        <div className={styles.menu} role="menu" data-site-menu="more">
          <a role="menuitem" className={styles.item} href="/">
            <SignInIcon size="regular" aria-hidden="true" />{t("signIn")}
          </a>
        </div>
      ) : null}
      {sheetOpen ? (
        <>
          <div className={styles.sheetBackdrop} onClick={closeSheet} data-site-sheet-backdrop />
          <div ref={sheetRef} className={styles.sheet} role="menu" aria-label={t("moreActions")} data-site-sheet={open}>
            {open === "sheet" ? (
              <>
                {navigation.map((link) => (
                  <button key={link.id} type="button" role="menuitem" className={styles.sheetItem}
                    aria-current={link.active ? "page" : undefined} onClick={() => { setOpen(null); onOpenPage?.(link.id); }}>
                    <span><TitleWithCustomEmoji text={link.label} named={link.named} /></span>
                  </button>
                ))}
                {onSearch ? (
                  <button type="button" role="menuitem" className={styles.sheetItem} onClick={() => { setOpen(null); onSearch(); }}>
                    <Search size="regular" aria-hidden="true" /><span>{t("searchShort")}</span>
                  </button>
                ) : null}
                {duplicate ? (
                  <button type="button" role="menuitem" className={styles.sheetItem} onClick={() => void openDuplicate()}>
                    <Copy size="regular" aria-hidden="true" /><span>{t("duplicate")}</span>
                  </button>
                ) : null}
                <button type="button" role="menuitem" className={styles.sheetItem} onClick={() => setOpen("sheet-more")}>
                  <DotsHorizontal size="regular" aria-hidden="true" /><span>{t("more")}</span>
                </button>
              </>
            ) : (
              <a role="menuitem" className={styles.sheetItem} href="/">
                <SignInIcon size="regular" aria-hidden="true" /><span>{t("signIn")}</span>
              </a>
            )}
          </div>
        </>
      ) : null}
    </div>
  );
}
