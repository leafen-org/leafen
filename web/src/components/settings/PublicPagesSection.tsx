import { api } from "@/api";
import { SwitchMark } from "@/components/Switch";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { openPageShare, PUBLIC_CONTENT_CHANGED_EVENT } from "@/components/page/pageShareEvents";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useProductPages } from "@/components/product/productPagesContext";
import { ArrowUp, ChevronLeft, ChevronRight, Copy, DotsHorizontal as More, GlobeIcon, PageLinkIcon, Settings } from "@/icons/leafen";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { pageHref } from "@/lib/navigation";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { serverUrl } from "@/lib/serverOrigin";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  changeWorkspaceConnectionPolicies,
  readWorkspaceConnectionPolicies,
  type WorkspaceConnectionPolicies,
} from "@/lib/workspaceConnectionPolicies";
import {
  listAllWorkspacePublicContent,
  type PublicContentItem,
  type WorkspacePublicContent,
} from "@/lib/workspacePublicContent";
import type { Session, Workspace } from "@/types";
import table from "./DeveloperSettings.module.css";
import menu from "./SidebarPresetsSection.module.css";
import rowStyles from "./PublicPagesSection.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { activeDateLocale } from "@/lib/i18n";
import { useCopyNotice } from "@/lib/copyNotice";
import { newId } from "@/lib/ids";
import { publicMutationService } from "@/lib/publicMutationService";
import { publicMutationActions } from "@/publicMutationWire.generated";

type Kind = "sites" | "forms" | "links";
type SortKey = "title" | "publisher" | "date";

const KINDS: Array<{ kind: Kind; title: string; empty: string }> = [
  { kind: "sites", title: "publicSitesTitle", empty: "publicSitesEmpty" },
  { kind: "forms", title: "publicFormsTitle", empty: "publicFormsEmpty" },
  { kind: "links", title: "publicLinksTitle", empty: "publicLinksEmpty" },
];

/**
 * The features' "공개 페이지" item, as Notion's (seen 2026-10-02, and its
 * help): counts of the workspace's published sites, public forms and pages
 * anyone with the link can open — those the reader can open — each opening
 * a table of the page, its address, who first made it public and when,
 * sorted by a column; under "설정", whether a page published as a site
 * always says so in a banner over it (on by default, its owners' to change).
 * Notion's shared AI chats aren't in Leafen (no AI) and its per-site domains
 * are left out. A workspace moved to another server says above them that
 * what it made public still opens here until the same page is made public
 * there, and each site or link can be taken down here (workspace_move_
 * follow.go on the server, user decision 2026-11-07).
 */
export function PublicPagesSection({
  session,
  workspace,
  onOpenPage,
}: {
  session: Session;
  workspace: Workspace;
  onOpenPage: (pageId: string) => void;
}) {
  const { t, i18n } = useTranslation("workspaceSettingsDialog");
  const [content, setContent] = useState<WorkspacePublicContent | null>(null);
  const [open, setOpen] = useState<Kind | null>(null);
  const [sort, setSort] = useState<SortKey>("date");
  const [error, setError] = useState("");
  // A failed read of what is public, in the tiles' place with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const [policies, setPolicies] = useState<WorkspaceConnectionPolicies | null>(null);
  const [policyBusy, setPolicyBusy] = useState(false);
  const [menuFor, setMenuFor] = useState("");
  const [moved, setMoved] = useState(false);
  const pages = useProductPages()?.pages;

  useEffect(() => {
    setContent(null);
    setOpen(null);
    setError("");
    setReadFailed("");
    const controller = new AbortController();
    listAllWorkspacePublicContent(session, workspace.id, controller.signal)
      .then(setContent)
      .catch((failure) => {
        if (!controller.signal.aborted) setReadFailed(settingsErrorMessage(failure, t("publicContentLoadFailed")));
      });
    readWorkspaceConnectionPolicies(session, workspace.id, controller.signal).then(setPolicies).catch(() => undefined);
    api.workspaceMoveOut(session, workspace.id, controller.signal).then((move) => setMoved(move.state === "moved"), () => undefined);
    return () => controller.abort();
  }, [readTurn, session, t, workspace.id]);

  // A moved workspace's site taken down, or its link turned off, here alone.
  async function turnOff(item: PublicContentItem, kind: Kind) {
    const signal = new AbortController().signal;
    try {
      const { state } = await publicMutationService.state(session, item.pageId, signal);
      await publicMutationService.mutate(session, item.pageId, kind === "sites"
        ? { requestId: newId(), expectedRevision: state.revision, action: publicMutationActions.unpublishSite }
        : { requestId: newId(), expectedRevision: state.revision, action: publicMutationActions.setWebSharing, enabled: false }, signal);
      window.dispatchEvent(new Event(PUBLIC_CONTENT_CHANGED_EVENT));
      setContent(await listAllWorkspacePublicContent(session, workspace.id));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("publicTurnOffFailed")));
    }
  }

  async function changeSiteBanner(siteBanner: boolean) {
    setPolicyBusy(true);
    try {
      setPolicies(await changeWorkspaceConnectionPolicies(session, workspace.id, { siteBanner }));
      window.dispatchEvent(new Event(PUBLIC_CONTENT_CHANGED_EVENT));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("siteBannerFailed")));
    } finally {
      setPolicyBusy(false);
    }
  }

  const rows = useMemo(() => {
    const items = open && content ? [...content[open]] : [];
    const key = (item: PublicContentItem) => sort === "title" ? item.title : sort === "publisher" ? item.publishedBy.name : item.publishedAt;
    items.sort((left, right) => sort === "date" ? key(right).localeCompare(key(left)) : key(left).localeCompare(key(right), i18n.language));
    return items;
  }, [content, i18n.language, open, sort]);

  const date = (value: string) => {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? "" : new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(parsed));
  };
  // The sorted column says so with an arrow after its name, as Notion's "게시된 날짜 ↑" (seen 2026-11-15).
  const heading = (key: SortKey, label: string) => (
    <th scope="col" aria-sort={sort === key ? (key === "date" ? "descending" : "ascending") : undefined}>
      <button type="button" className={`${table.secondary} ${rowStyles.sort}`} onClick={() => setSort(key)}>
        {label}{sort === key ? <ArrowUp size="tiny" aria-hidden="true" /> : null}</button>
    </th>
  );
  const kind = KINDS.find((entry) => entry.kind === open);

  // The address stays on its chip while a toast says it was copied.
  const notice = useCopyNotice();
  function copy(path: string) {
    return notice(serverUrl(path));
  }

  return (
    <div data-public-pages>
      {moved && content && content.sites.length + content.links.length > 0
        ? <p className={rowStyles.moved} role="note" data-public-moved>{t("publicMovedNotice")}</p> : null}
      {kind ? (
        <section className={settingsStyles.section} role="region" aria-label={t(kind.title)}>
          {/* One line, "‹ 〈kind〉", the way back and the list's name, as Notion's (seen 2026-11-15). */}
          <button type="button" className={rowStyles.back} onClick={() => setOpen(null)} aria-label={`${t("connectionsBack")}, ${t(kind.title)}`} data-public-back>
            <ChevronLeft size="small" aria-hidden="true" /><span>{t(kind.title)}</span>
          </button>
          {/* Empty, the table keeps its head over a centred line, as Notion's. */}
            <table className={`${table.tokens} ${rowStyles.table}`} data-public-content={kind.kind}>
              <thead>
                <tr>
                  {heading("title", t("publicColumnPage"))}
                  <th scope="col">{t(kind.kind === "sites" ? "publicColumnAddress" : "publicColumnLink")}</th>
                  {heading("publisher", t("publicColumnPublisher"))}
                  {heading("date", t("publicColumnDate"))}
                  <th scope="col"><span className={rowStyles.hidden}>{t("publicRowActions")}</span></th>
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? <tr><td colSpan={5} className={rowStyles.empty}>{t("publicContentNone")}</td></tr> : null}
                {rows.map((item) => {
                  const key = `${item.pageId}:${item.path ?? ""}`;
                  const page = pages?.find((candidate) => candidate.id === item.pageId);
                  // Its title as the page list has it, its spans saying its mentions and drawing its custom emoji (TitleWithCustomEmoji).
                  const named = page && page.title === item.title ? page : undefined;
                  const title = titleWithClosedMentions(item.title, named?.titleSpans ?? item.titleSpans);
                  // The public address: a site's own, or the page's for one anyone with its link opens; a form's is in its share menu.
                  const link = item.path ?? (kind.kind === "links" ? pageHref(item.pageId) : "");
                  return (
                    <tr key={key} data-public-page={item.pageId}>
                      <td className={table.name}>
                        <button type="button" className={`${table.secondary} ${rowStyles.page}`} onClick={() => onOpenPage(item.pageId)}>
                          {/* A site without its own icon is a page with its arrow out, as Notion's list draws one. */}
                          <span className={rowStyles.icon} aria-hidden="true">
                            {kind.kind === "sites" && !page?.icon ? <PageLinkIcon size="regular" />
                              : <PageIconGlyph page={page ?? { icon: "", iconType: "" }} size={18} />}
                          </span>
                          <span className={rowStyles.title}>{title ? <TitleWithCustomEmoji text={title} named={named} /> : t("publicUntitled")}</span>
                        </button>
                      </td>
                      <td>
                        {item.path ? (
                          <button type="button" className={rowStyles.chip} data-public-address onClick={() => void copy(item.path!)}>
                            {`${window.location.host}${item.path}`}
                          </button>
                        ) : t("publicLinkInShareMenu")}
                      </td>
                      <td>
                        <span className={rowStyles.person}>
                          <PersonAvatar userId={item.publishedBy.id} name={item.publishedBy.name} size={20} />
                          <span>{item.publishedBy.name || t("connectionUnknownPerson")}</span>
                        </span>
                      </td>
                      <td>{date(item.publishedAt)}</td>
                      <td className={rowStyles.actions}>
                        <button type="button" className={rowStyles.more} aria-haspopup="menu" aria-expanded={menuFor === key}
                          aria-label={t("publicRowActions")} data-public-row-menu onClick={() => setMenuFor((open) => open === key ? "" : key)}>
                          <More size="small" aria-hidden="true" />
                        </button>
                        {menuFor === key ? (
                          <RowMenu onClose={() => setMenuFor("")}>
                            {link ? <button type="button" role="menuitem" className={menu.item} data-public-copy
                              onClick={() => { setMenuFor(""); void copy(link); }}><Copy size="small" aria-hidden="true" />{t("publicCopyLink")}</button> : null}
                            <button type="button" role="menuitem" className={menu.item} data-public-view onClick={() => {
                              setMenuFor("");
                              if (item.path) window.open(serverUrl(item.path), "_blank", "noopener,noreferrer");
                              else onOpenPage(item.pageId);
                            }}><GlobeIcon size="small" aria-hidden="true" />{t("publicView")}</button>
                            <div role="separator" className={rowStyles.menuLine} />
                            <button type="button" role="menuitem" className={menu.item} data-public-settings onClick={() => {
                              setMenuFor("");
                              onOpenPage(item.pageId);
                              openPageShare({ pageId: item.pageId, tab: kind.kind === "sites" ? "publish" : "share" });
                            }}><Settings size="small" aria-hidden="true" />{t("publicSettings")}</button>
                            {moved && (kind.kind === "sites" || kind.kind === "links") ? (
                              <button type="button" role="menuitem" className={menu.item} data-public-turn-off onClick={() => {
                                setMenuFor("");
                                void turnOff(item, kind.kind);
                              }}>{t(kind.kind === "sites" ? "publicUnpublish" : "publicRemoveLink")}</button>
                            ) : null}
                          </RowMenu>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
        </section>
      ) : readFailed ? (
        <ListNotice role="alert" onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} data-public-read-failed>{readFailed}</ListNotice>
      ) : (
        <section className={`${settingsStyles.section} ${rowStyles.tiles}`} role="region" aria-label={t("navPublicPages")}>
          {KINDS.map((entry) => {
            const items = content ? content[entry.kind] : null;
            // Under the count, the one made public last, as Notion's tile names it (seen 2026-11-14), or that there is none.
            const latest = items?.reduce<PublicContentItem | null>((last, item) => !last || item.publishedAt > last.publishedAt ? item : last, null);
            const page = latest ? pages?.find((candidate) => candidate.id === latest.pageId) : undefined;
            const named = page && latest && page.title === latest.title ? page : undefined;
            const title = latest ? titleWithClosedMentions(latest.title, named?.titleSpans ?? latest.titleSpans) : "";
            return (
              <button key={entry.kind} type="button" className={rowStyles.tile} onClick={() => setOpen(entry.kind)} data-public-tile={entry.kind} disabled={!content}>
                <span className={rowStyles.tileText}>
                  <span className={rowStyles.tileHead}><span>{t(entry.title)}</span><strong>{items?.length ?? "…"}</strong></span>
                  <span className={rowStyles.tileFoot}>
                    {items?.length === 0 ? <small>{t(entry.empty)}</small> : latest ? (
                      <span className={rowStyles.tileChip} data-public-tile-latest={latest.pageId}>
                        <span className={rowStyles.tileChipIcon} aria-hidden="true"><PageIconGlyph page={page ?? { icon: "", iconType: "" }} size={15} /></span>
                        <span>{title ? <TitleWithCustomEmoji text={title} named={named} /> : t("publicUntitled")}</span>
                      </span>
                    ) : null}
                  </span>
                </span>
                <span className={rowStyles.tileMore} aria-hidden="true"><ChevronRight size="small" /></span>
              </button>
            );
          })}
        </section>
      )}
      {open || !policies ? null : (
        <section className={settingsStyles.section} aria-labelledby="public-pages-settings">
          <div id="public-pages-settings" className={settingsStyles.sectionTitle}>{t("publicSettingsTitle")}</div>
          <div className={settingsStyles.securityRow}>
            <div className={settingsStyles.securityInstruction}>
              <strong>{t("siteBannerTitle")}</strong>
              <span>{t("siteBannerDescription")}</span>
            </div>
            <button type="button" role="switch" aria-checked={policies.siteBanner} aria-label={t("siteBannerTitle")}
              disabled={policyBusy || !policies.canManage || moved} data-site-banner-setting
              onClick={() => void changeSiteBanner(!policies.siteBanner)}>
              <SwitchMark on={policies.siteBanner} />
            </button>
          </div>
        </section>
      )}
      {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
    </div>
  );
}

function RowMenu({ onClose, children }: { onClose(): void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  usePopoverAppear(true, ref);
  useOverlayLayer(true, { kind: "menu", restoreFocus: false, ref, onEscape: onClose, onOutsidePress: onClose });
  return <div ref={ref} className={`${menu.popover} ${rowStyles.menu}`} data-align="end" role="menu">{children}</div>;
}
