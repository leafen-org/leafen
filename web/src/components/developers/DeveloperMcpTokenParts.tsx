import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { listedPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { ChevronDown, ChevronRight, FileText, UserIcon } from "@/icons/leafen";
import { chooseMcpTokenPages, type McpTokenSettings } from "@/lib/mcpTokens";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pagesByIdOf } from "@/lib/pageIndex";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { SwitchInput } from "@/components/Switch";
import { InternalConnectionPagesPicker } from "./InternalConnectionPagesPicker";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import token from "./McpToken.module.css";

/** A checkbox ticked, empty or, for some of its tools, half. */
function Check({ on, of, label, onChange, mark }: { on: number; of: number; label: string; onChange(next: boolean): void; mark?: string }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => { if (ref.current) ref.current.indeterminate = on > 0 && on < of; }, [on, of]);
  return <input ref={ref} type="checkbox" className={token.check} checked={of > 0 && on === of} aria-label={label}
    data-mcp-token-check={mark} onChange={(event) => onChange(event.currentTarget.checked)} />;
}

/**
 * A tool's name as Notion's token page shows it (seen 2026-11-10): its MCP
 * name without "notion-", "_" for "-" — notion-get-self is get_self — and
 * Leafen's own leafen_* as they are. Its line saying what it does is keyed
 * by this name.
 */
const shownToolName = (name: string) => name.replace(/^notion-/u, "").replaceAll("-", "_");

/**
 * "도구", as Notion's (seen 2026-11-09): "모든 도구" with "도구 N/M개", then
 * a grey row for each of its six groups that opens on its tools, each a box,
 * its name in bold and under it a grey line, in the screen's language, saying
 * what it does (seen 2026-11-10). A tool turned off is not offered to the
 * agent; the scope the token holds on the API follows what is left on
 * (mcp_token_tools.go).
 */
export function McpTokenTools({ settings, onChange }: { settings: McpTokenSettings; onChange(disabledTools: string[]): void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const tools = settings.toolGroups.flatMap((group) => group.tools);
  const count = (list: typeof tools) => list.filter((tool) => tool.enabled).length;
  // What the change turns on or off, the rest as it is.
  const set = (names: readonly string[], enabled: boolean) => onChange(tools
    .filter((tool) => (names.includes(tool.name) ? !enabled : !tool.enabled)).map((tool) => tool.name));
  return (
    <section className={styles.card} data-mcp-token-card="tools">
      <h2 className={styles.cardTitle}>{t("developerTools.mcpToken.tools")}</h2>
      <p className={`${styles.description} ${own.description}`}>{t("developerTools.mcpToken.toolsDescription")}</p>
      <label className={token.allRow}>
        <Check on={count(tools)} of={tools.length} label={t("developerTools.mcpToken.allTools")} mark="all"
          onChange={(next) => set(tools.map((tool) => tool.name), next)} />
        <span>{t("developerTools.mcpToken.allTools")}</span>
        <strong className={token.count}>{t("developerTools.mcpToken.toolCount", { on: count(tools), count: tools.length })}</strong>
      </label>
      {settings.toolGroups.map((group) => {
        const expanded = open.has(group.id);
        const label = t(`developerTools.mcpToken.group.${group.id}`);
        return (
          <div key={group.id} className={token.group} data-mcp-token-group={group.id}>
            <div className={token.groupRow}>
              <button type="button" className={token.chevron} aria-expanded={expanded} aria-label={label}
                onClick={() => setOpen((current) => { const next = new Set(current); if (expanded) next.delete(group.id); else next.add(group.id); return next; })}>
                {expanded ? <ChevronDown size="micro" aria-hidden="true" /> : <ChevronRight size="micro" aria-hidden="true" />}</button>
              <Check on={count(group.tools)} of={group.tools.length} label={label} mark={group.id}
                onChange={(next) => set(group.tools.map((tool) => tool.name), next)} />
              <span className={token.groupName}>{label}</span>
              <span className={token.groupCount}>{t("developerTools.mcpToken.toolCount", { on: count(group.tools), count: group.tools.length })}</span>
            </div>
            {expanded ? <div className={token.toolList}>
              {group.tools.map((tool) => {
                const name = shownToolName(tool.name);
                const text = t(`developerTools.mcpToken.toolText.${name}`, { defaultValue: "" });
                return (
                  <label key={tool.name} className={token.toolRow} data-mcp-token-tool={tool.name}>
                    <input type="checkbox" className={token.check} checked={tool.enabled} onChange={(event) => set([tool.name], event.currentTarget.checked)} />
                    <span className={token.toolText}>
                      <strong>{name}</strong>
                      {text ? <small>{text}</small> : null}
                    </span>
                  </label>
                );
              })}
            </div> : null}
          </div>
        );
      })}
    </section>
  );
}

/**
 * "콘텐츠 사용 권한", as Notion's (seen 2026-11-09): what the token reaches,
 * "편집 권한" to choose pages, and who may change that — with "모든 페이지"
 * at its top, on for a new token (MCP token decision of 2026-11-10, 3):
 * every page its person reads, later ones too. Off, or once a page is
 * chosen, it reaches the chosen pages and those under them, none chosen
 * reaching no page, as Notion's "페이지 사용 권한 없음".
 */
export function McpTokenAccess({ session, settings, onSettings, onError }: {
  session: Session; settings: McpTokenSettings; onSettings(settings: McpTokenSettings): void; onError(message: string): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const pagesById = pagesByIdOf(useProductPages()?.pages ?? []);
  const [editing, setEditing] = useState(false);
  const chosen = settings.allPages ? [] : settings.pages.map((page) => page.id);
  async function reach(next: Parameters<typeof chooseMcpTokenPages>[2]) {
    try {
      onSettings(await chooseMcpTokenPages(session, settings.id, next));
    } catch (failure) {
      onError(settingsErrorMessage(failure, t("appFailed")));
      throw failure;
    }
  }
  const toggle = (pageId: string, connect: boolean) =>
    reach({ allPages: false, pageIds: connect ? [...chosen, pageId] : chosen.filter((id) => id !== pageId) });
  return (
    <section className={styles.card} data-mcp-token-card="access">
      <div className={own.cardHead}>
        <div>
          <h2 className={styles.cardTitle}>{t("developerTools.mcpToken.access")}</h2>
          <p className={`${styles.description} ${own.description}`}>{t("developerTools.mcpToken.accessDescription")}</p>
        </div>
        <button type="button" className={token.editAccess} data-mcp-token-access-edit onClick={() => setEditing(true)}>
          {t("developerTools.internal.editAccess")}</button>
      </div>
      <div className={token.pagesBox} data-mcp-token-pages={settings.allPages ? "all" : settings.pages.length}>
        <label className={token.allPagesRow} data-mcp-token-all-pages>
          <FileText size="regular" aria-hidden="true" />
          <span className={own.ownerText}>
            <strong>{t("developerTools.mcpToken.allPages")}</strong>
            <small>{t("developerTools.mcpToken.allPagesNote")}</small>
          </span>
          <SwitchInput checked={settings.allPages}
            onChange={(event) => void reach(event.currentTarget.checked ? { allPages: true } : { allPages: false, pageIds: [] }).catch(() => undefined)} />
        </label>
        {settings.allPages ? null : settings.pages.length ? <>
          <p className={own.note}>{t("developerTools.mcpToken.pagesNote")}</p>
          {settings.pages.map((page) => (
            <div key={page.id} className={own.pageRow} data-mcp-token-page={page.id}>
              <PageIconGlyph page={listedPageIcon(pagesById[page.id], page)} size={18} />
              <span>{page.title || untitledPageDisplayTitle(listedPageNameKind(page))}</span>
            </div>
          ))}
        </> : <div className={token.empty}>
          <FileText size="card" aria-hidden="true" />
          <strong>{t("developerTools.mcpToken.noPages")}</strong>
          <span>{t("developerTools.mcpToken.noPagesNote")}</span>
        </div>}
      </div>
      <h3 className={token.managersTitle}>{t("developerTools.mcpToken.managers")}</h3>
      <div className={own.ownerRow}>
        <span className={token.managerMark} aria-hidden="true"><UserIcon size="regular" /></span>
        <span className={own.ownerText}>
          <strong>{t("developerTools.mcpToken.managersOwner")}</strong>
          <small>{t("developerTools.mcpToken.managersOwnerNote")}</small>
        </span>
      </div>
      {editing ? <InternalConnectionPagesPicker session={session} toggle={toggle} chosen={new Set(chosen)}
        onChanged={() => undefined} onClose={() => setEditing(false)} /> : null}
    </section>
  );
}
