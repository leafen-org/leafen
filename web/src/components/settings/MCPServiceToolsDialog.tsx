import { SwitchMark } from "@/components/Switch";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  listMCPServiceTools, setMCPServiceAlwaysAllow, setMCPServiceTool,
  type MCPService, type MCPServiceToolSetting,
} from "@/mcpServicesApi";
import type { Session } from "@/types";
import admin from "@/components/admin/AdministratorApps.module.css";
import dialog from "./MCPServices.module.css";
import { serviceName } from "./MCPServices";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { AppSelect } from "@/components/AppSelect";
import { ReadFailed } from "@/components/ReadFailed";

/**
 * A connected service's "관리", as Notion's custom MCP connection (help read
 * 2026-10-12 and 2026-10-13): "항상 허용" for all its tools, then each tool
 * with its toggle and "자동 실행"/"항상 묻기". A tool off isn't offered to
 * agents; one the service doesn't mark read-only or harmless asks first
 * until the person says otherwise. Only the person changes these, here.
 */
export function MCPServiceToolsDialog({ session, service, onChanged, onClose }: {
  session: Session;
  service: MCPService;
  onChanged: () => Promise<unknown>;
  onClose: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [alwaysAllow, setAlwaysAllow] = useState(Boolean(service.alwaysAllow));
  const [tools, setTools] = useState<MCPServiceToolSetting[] | null>(null);
  const [nextCursor, setNextCursor] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // A failed read of the tools, said where they show with a way to read
  // again; a switch that failed to save is `error`.
  const [readFailed, setReadFailed] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose, initialFocus: closeRef,
  });

  const read = useCallback(async (cursor: string, signal?: AbortSignal) => {
    try {
      const page = await listMCPServiceTools(session, service.id, cursor || undefined, signal);
      setTools((shown) => cursor ? [...(shown ?? []), ...page.tools] : page.tools);
      setNextCursor(page.nextCursor ?? "");
      setAlwaysAllow(Boolean(page.service.alwaysAllow));
      setReadFailed("");
    } catch (failure) {
      if (!signal?.aborted) setReadFailed(settingsErrorMessage(failure, t("mcpService.toolsFailed")));
    }
  }, [service.id, session, t]);
  useEffect(() => {
    const controller = new AbortController();
    void read("", controller.signal);
    return () => controller.abort();
  }, [read]);

  async function change(name: string, run: () => Promise<unknown>) {
    setBusy(name);
    setError("");
    try {
      await run();
      await onChanged();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("mcpService.toolsFailed")));
    } finally {
      setBusy("");
    }
  }
  const setTool = (tool: MCPServiceToolSetting, choice: { enabled?: boolean; run?: "auto" | "ask" }) => change(tool.name, async () => {
    const saved = await setMCPServiceTool(session, service.id, tool.name, choice);
    setTools((shown) => (shown ?? []).map((each) => each.name === saved.name ? saved : each));
  });
  const effect = (tool: MCPServiceToolSetting) => tool.readOnly ? t("mcpService.toolReadOnly")
    : tool.destructive ? t("mcpService.toolDestructive") : t("mcpService.toolChanges");

  return createPortal(
    <div ref={overlayRef} className={admin.overlay} style={{ zIndex }} data-mcp-service-tools={service.id}>
      <button type="button" className={admin.backdrop} tabIndex={-1} aria-label={t("mcpService.close")} onClick={onClose} />
      <section ref={dialogRef} className={admin.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div>
          <h2 id={titleId} className={admin.dialogTitle}>{serviceName(service)}</h2>
          <p className={admin.note}>{t("mcpService.toolsSubtitle")}</p>
        </div>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("mcpService.alwaysAllow")}</strong>
            <span>{t("mcpService.alwaysAllowDescription")}</span>
          </div>
          <button type="button" role="switch" aria-checked={alwaysAllow} aria-label={t("mcpService.alwaysAllow")} disabled={busy !== ""}
            data-mcp-service-always-allow onClick={() => void change("*", async () => {
              const answer = await setMCPServiceAlwaysAllow(session, service.id, !alwaysAllow);
              setAlwaysAllow(Boolean(answer.service.alwaysAllow));
            })}>
            <SwitchMark on={alwaysAllow} />
          </button>
        </div>
        <div className={dialog.tools} role="list" aria-label={t("mcpService.toolsLabel")}>
          {tools === null ? (readFailed ? null : <p className={admin.note}>{t("mcpService.toolsLoading")}</p>)
            : tools.length === 0 ? <p className={admin.note}>{t("mcpService.toolsNone")}</p> : tools.map((tool) => (
              <div key={tool.name} className={dialog.tool} role="listitem" data-mcp-service-tool={tool.name} data-enabled={tool.enabled || undefined}>
                <span className={dialog.toolText}>
                  <strong>{tool.title || tool.name}</strong>
                  {tool.description ? <span>{tool.description}</span> : null}
                  <small>{effect(tool)}</small>
                </span>
                <AppSelect look="value" value={alwaysAllow ? "auto" : tool.run} disabled={!tool.enabled || alwaysAllow || busy !== ""}
                  aria-label={t("mcpService.toolRun", { tool: tool.title || tool.name })} data-mcp-service-tool-run
                  onChange={(event) => void setTool(tool, { run: event.target.value as "auto" | "ask" })}>
                  <option value="auto">{t("mcpService.runAuto")}</option>
                  <option value="ask">{t("mcpService.runAsk")}</option>
                </AppSelect>
                <button type="button" role="switch" aria-checked={tool.enabled} aria-label={t("mcpService.toolOn", { tool: tool.title || tool.name })}
                  disabled={busy !== ""} data-mcp-service-tool-on onClick={() => void setTool(tool, { enabled: !tool.enabled })}>
                  <SwitchMark on={tool.enabled} />
                </button>
              </div>
            ))}
          {/* The first read again, or the next page's, as "더 보기" asked it. */}
          {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); void read(tools === null ? "" : nextCursor); }} />
            : nextCursor ? (
            <button type="button" className={dialog.addHeader} data-mcp-service-tools-more onClick={() => void read(nextCursor)}>
              {t("mcpService.toolsMore")}
            </button>
          ) : null}
        </div>
        {error ? <div className={admin.error} role="alert">{error}</div> : null}
        <div className={admin.dialogActions}>
          <button ref={closeRef} type="button" className={admin.secondary} onClick={onClose}>{t("mcpService.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
