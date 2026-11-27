import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import { GlobeIcon, InfoIcon, Settings } from "@/icons/leafen";
import { serverUrl } from "@/lib/serverOrigin";
import { usePageReach } from "@/lib/usePageOnlyReader";
import { onWindowReturn } from "@/lib/windowReturn";
import { CONNECTION_POLICIES_CHANGED_EVENT, readWorkspaceConnectionPolicies } from "@/lib/workspaceConnectionPolicies";
import { listWorkspacePublicContent } from "@/lib/workspacePublicContent";
import type { Session } from "@/types";
import styles from "./PublishedSiteBanner.module.css";
import { openPageShare, PUBLIC_CONTENT_CHANGED_EVENT } from "./pageShareEvents";

// What the bar reads is kept for a minute for the workspace (its policies)
// and the page (its site), so a page opened again — or another page — reads
// neither again (2026-11-12: two of the eleven requests each page open sent).
// A change of the workspace's public content or of its policies here, or
// the window coming back, reads them anew.
const KEPT_MS = 60_000;
const kept = new Map<string, { at: number; read: Promise<unknown> }>();

function keptRead<Value>(key: string, read: () => Promise<Value>): Promise<Value> {
  const held = kept.get(key);
  if (held && Date.now() - held.at < KEPT_MS) return held.read as Promise<Value>;
  const next = read();
  kept.set(key, { at: Date.now(), read: next });
  next.catch(() => { if (kept.get(key)?.read === next) kept.delete(key); });
  return next;
}

function forgetWorkspace(userId: string, workspaceId: string) {
  for (const key of kept.keys()) if (key.startsWith(`${userId}\u0000${workspaceId}\u0000`)) kept.delete(key);
}

/**
 * The bar over a page published as a site, as Notion's (seen 2026-10-02):
 * a line naming the address it is published at, with "사이트 보기" and
 * "사이트 설정", while the workspace's "공개 페이지 → 설정" keeps it on (its default).
 * It has no close button.
 *
 * A page open to anyone with its web link and not published has the same bar
 * saying anyone with the link sees it, with an ⓘ telling that whoever sees
 * the page sees its editors' names, as Notion's
 * (seen 2026-11-13); no setting turns that one off. A workspace's guest, who
 * reads none of its settings, has only that one.
 */
export function PublishedSiteBanner({ session, workspaceId, pageId, guest = false }: {
  session: Session; workspaceId: string; pageId: string; guest?: boolean;
}) {
  const { t } = useTranslation("topBar");
  const [sitePath, setSitePath] = useState("");
  const reach = usePageReach(session, pageId);

  useEffect(() => {
    if (guest) return undefined;
    let controller = new AbortController();
    const prefix = `${session.user.id}\u0000${workspaceId}\u0000`;
    const read = () => {
      controller.abort();
      controller = new AbortController();
      const { signal } = controller;
      Promise.all([
        keptRead(`${prefix}policies`, () => readWorkspaceConnectionPolicies(session, workspaceId)),
        // This page's own site, however many the workspace has.
        keptRead(`${prefix}site\u0000${pageId}`, () => listWorkspacePublicContent(session, workspaceId, undefined, { kind: "sites", pageId })),
      ]).then(([policies, content]) => {
        if (signal.aborted) return;
        const site = content.sites.find((item) => item.pageId === pageId);
        setSitePath(policies.siteBanner && site?.path ? site.path : "");
      }).catch(() => undefined);
    };
    const readAnew = () => {
      forgetWorkspace(session.user.id, workspaceId);
      read();
    };
    read();
    window.addEventListener(PUBLIC_CONTENT_CHANGED_EVENT, readAnew);
    window.addEventListener(CONNECTION_POLICIES_CHANGED_EVENT, readAnew);
    const stopReturn = onWindowReturn(readAnew);
    return () => {
      controller.abort();
      window.removeEventListener(PUBLIC_CONTENT_CHANGED_EVENT, readAnew);
      window.removeEventListener(CONNECTION_POLICIES_CHANGED_EVENT, readAnew);
      stopReturn();
    };
  }, [guest, pageId, session, workspaceId]);

  if (!sitePath) {
    if (reach !== "web") return null;
    return (
      <div className={styles.banner} role="status" data-web-link-banner>
        <span className={styles.message}>{t("webLinkBanner.text")}</span>
        <HoverTip label={t("webLinkBanner.editorsTip")} wide className={styles.info}>
          <InfoIcon size="tiny" aria-label={t("webLinkBanner.editorsTip")} />
        </HoverTip>
      </div>
    );
  }
  const href = serverUrl(sitePath);
  const address = (() => {
    try {
      const url = new URL(href);
      return `${url.host}${url.pathname}`;
    } catch {
      return sitePath;
    }
  })();
  return (
    <div className={styles.banner} role="status" data-published-site-banner>
      <span className={styles.message}>
        <Trans t={t} i18nKey="siteBanner.published" values={{ address }}
          components={{ site: <a href={href} target="_blank" rel="noopener noreferrer" /> }} />
      </span>
      <a className={styles.button} href={href} target="_blank" rel="noopener noreferrer" data-published-site-view>
        <GlobeIcon size="small" aria-hidden="true" /> {t("siteBanner.view")}
      </a>
      <button type="button" className={styles.button} data-published-site-settings
        onClick={(event) => openPageShare({ pageId, tab: "publish", opener: event.currentTarget })}>
        <Settings size="small" aria-hidden="true" /> {t("siteBanner.settings")}
      </button>
    </div>
  );
}
