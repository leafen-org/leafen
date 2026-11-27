import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useWorkspaceAIConnections } from "@/components/settings/useWorkspaceAIConnections";
import { KeyLifetimeRow } from "@/components/settings/KeyLifetimeRow";
import type { PersonalAccessTokenLifetime } from "@/lib/personalAccessTokens";
import { connectionKey } from "@/lib/agentConnection";
import { changeMcpToken, rememberMcpTokenKey } from "@/lib/mcpTokens";
import { X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { Session, Workspace } from "@/types";
import dialogStyles from "@/components/settings/DeveloperSettings.module.css";

/**
 * "+ 새 MCP 토큰" on its tab, as Notion makes one there (its documentation:
 * a token an MCP client sends as a bearer, with no sign-in): its name and
 * how long it lives, a year unless chosen, as Notion's personal access
 * tokens (agent key decision of 2026-11-16) — then straight to the token's
 * page, as Notion goes (seen 2026-11-09), which shows the key until it is
 * left and chooses its tools. It acts as the person who makes it, with every
 * tool on, as the settings' manual tokens do.
 */
export function DeveloperMcpTokenDialog({ session, workspace, onClose, onMade }: {
  session: Session; workspace: Workspace; onClose(made: boolean): void; onMade(id: string): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const connections = useWorkspaceAIConnections({ session, workspace });
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [lifetime, setLifetime] = useState<PersonalAccessTokenLifetime>("1y");
  // The token it sends as its bearer, held for its page to show.
  const made = connectionKey(connections.createdConnectionUri);
  const madeId = connections.createdConnectionId;
  useEffect(() => {
    if (!made || !madeId) return;
    rememberMcpTokenKey(madeId, made);
    // Its description is kept with it before its page opens, as Notion's.
    const words = description.trim();
    void (words ? changeMcpToken(session, madeId, { description: words }).catch(() => undefined) : Promise.resolve())
      .then(() => onMade(madeId));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [made, madeId]);
  const busy = connections.busyActions.has("manual-token");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(Boolean(made)); }, initialFocus: nameRef,
  });
  return createPortal(
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }} data-developer-mcp-token-dialog>
      <button type="button" className={dialogStyles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy}
        onClick={() => onClose(Boolean(made))} />
      <section ref={dialogRef} className={dialogStyles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        {/* Notion's window: its × at the top end, no "취소" (seen 2026-11-09). */}
        <div className={dialogStyles.dialogHead}>
          <h2 id={titleId} className={dialogStyles.dialogTitle}>{t("developerTools.newMcpToken")}</h2>
          <button type="button" className={dialogStyles.dialogClose} aria-label={t("common:actions.close")} disabled={busy}
            onClick={() => onClose(Boolean(made))}><X size="small" aria-hidden="true" /></button>
        </div>
        <div className={dialogStyles.dialogBody}>
          <p className={dialogStyles.dialogNote}>{t("developerTools.mcpTokenNote")}</p>
          <label className={dialogStyles.field}>
            <span className={dialogStyles.fieldLabel}>{t("developerTools.mcpTokenName")}</span>
            <input ref={nameRef} type="text" value={name} maxLength={255} placeholder={t("developerTools.mcpTokenNamePlaceholder")}
              onChange={(event) => setName(event.target.value)} />
            <span className={dialogStyles.dialogNote}>{t("developerTools.mcpTokenNameHint")}</span>
          </label>
          <label className={dialogStyles.field}>
            <span className={dialogStyles.fieldLabel}>{t("developerTools.mcpTokenDescription")}</span>
            <input type="text" value={description} maxLength={500} placeholder={t("developerTools.mcpTokenDescriptionPlaceholder")}
              data-developer-mcp-token-description onChange={(event) => setDescription(event.target.value)} />
            <span className={dialogStyles.dialogNote}>{t("developerTools.mcpTokenDescriptionHint")}</span>
          </label>
          <KeyLifetimeRow value={lifetime} onChange={setLifetime} />
          {connections.error ? <div className={dialogStyles.error} role="alert">{connections.error}</div> : null}
          <div className={dialogStyles.dialogActions}>
            <button type="button" className={dialogStyles.primary} disabled={busy || !name.trim()} data-developer-mcp-token-create
              onClick={() => void connections.createManualConnection({ clientName: name.trim(), expiresIn: lifetime })}>{t("developerTools.mcpTokenCreate")}</button>
          </div>
        </div>
      </section>
    </div>,
    document.body,
  );
}
