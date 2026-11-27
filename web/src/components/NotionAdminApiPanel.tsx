"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { displayDateTime } from "@/lib/displayDate";

import { confirmAction } from "@/components/ConfirmActionDialog";
import {
  NOTION_ADMIN_CAPABILITIES,
  notionAdminBoundSecret,
  notionAdminBoundTokens,
  notionAdminCreateRequest,
  notionAdminFailureMessage,
  notionAdminRevokeRequest,
  type NotionAdminApiBounds,
  type NotionAdminApiService,
} from "@/lib/notionAdminApiView";
import type {
  NotionAdminCapability,
  OrganizationAdminToken,
} from "@/lib/types";

import { Plus } from "@/icons/leafen";
import { serverUrl } from "@/lib/serverOrigin";
import { CopyableValue } from "./settings/CopyableValue";
import dialogs from "./settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "./settings/SettingsFormDialog";
import local from "./settings/SettingsRows.module.css";
import { SettingsRowMenu } from "./settings/SettingsRowMenu";
import { styles } from "./settings/workspaceSettingsOverviewStyles";
import { AppDateTimeField } from "./database/ScheduleFieldPickers";

type NotionAdminApiPanelProps = {
  organizationId: string;
  canManageSecurity: boolean;
  bounds: NotionAdminApiBounds;
  service: NotionAdminApiService;
};

export function NotionAdminApiPanel({
  organizationId,
  canManageSecurity,
  bounds,
  service,
}: NotionAdminApiPanelProps) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const labelRef = useRef<HTMLInputElement>(null);
  const [creating, setCreating] = useState(false);
  const [tokens, setTokens] = useState<OrganizationAdminToken[]>([]);
  // Revoked and expired tokens can do nothing more; the list opens on the live
  // ones, as the developer tools' token table does, the rest one press away.
  const [showEnded, setShowEnded] = useState(false);
  const [label, setLabel] = useState("");
  const [capabilities, setCapabilities] = useState<NotionAdminCapability[]>(
    NOTION_ADMIN_CAPABILITIES,
  );
  const [workspaceIds, setWorkspaceIds] = useState("*");
  const [legalHoldIds, setLegalHoldIds] = useState("*");
  const [expiresAt, setExpiresAt] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");

  // The Admin API's root on this server (2026-11-07: the retired generation's
  // /api/functions/admin stood here, which answers nothing).
  const baseUrl = typeof window === "undefined"
    ? "/admin/v1"
    : serverUrl("/admin/v1");

  useEffect(() => {
    if (!organizationId || !canManageSecurity) return;
    let cancelled = false;
    setBusy("list");
    service.listTokens(organizationId)
      .then((next) => {
        if (!cancelled) setTokens(notionAdminBoundTokens(next, bounds));
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setMessage(notionAdminFailureMessage(
            error,
            "list",
          ));
        }
      })
      .finally(() => {
        if (!cancelled) setBusy("");
      });
    return () => {
      cancelled = true;
    };
  }, [bounds, canManageSecurity, organizationId, service, t]);

  function toggleCapability(capability: NotionAdminCapability) {
    setCapabilities((current) => current.includes(capability)
      ? current.filter((entry) => entry !== capability)
      : [...current, capability]);
  }

  async function createToken(event: FormEvent) {
    event.preventDefault();
    if (!canManageSecurity || !label.trim() || capabilities.length === 0) return;
    setBusy("create");
    setMessage("");
    try {
      const request = notionAdminCreateRequest({
        organizationId,
        label,
        capabilities,
        workspaceIds,
        legalHoldIds,
        expiresAt,
      }, bounds);
      const result = await service.createToken(request);
      if (result.adminToken) {
        setTokens(notionAdminBoundTokens([result.adminToken, ...tokens], bounds));
      }
      setSecret(notionAdminBoundSecret(result.adminTokenSecret ?? "", bounds));
      setLabel("");
    } catch (error) {
      setMessage(notionAdminFailureMessage(
        error,
        "create",
      ));
    } finally {
      setBusy("");
    }
  }

  async function revokeToken(token: OrganizationAdminToken) {
    if (!canManageSecurity) return;
    if (!await confirmAction({ message: t("tokenRevokeConfirm", { name: token.label }), detail: t("tokenRevokeConfirmDetail"),
      confirmLabel: t("enterpriseRevoke"), danger: true })) return;
    setBusy(`revoke:${token.id}`);
    setMessage("");
    try {
      const result = await service.revokeToken(
        notionAdminRevokeRequest(organizationId, token.id, bounds),
      );
      if (result.adminToken) {
        const next = tokens.map((entry) => (
          entry.id === result.adminToken!.id ? result.adminToken! : entry
        ));
        setTokens(notionAdminBoundTokens(next, bounds));
      }
    } catch (error) {
      setMessage(notionAdminFailureMessage(
        error,
        "revoke",
      ));
    } finally {
      setBusy("");
    }
  }

  async function copySecret() {
    try {
      await service.writeClipboard(secret);
    } catch (error) {
      setMessage(notionAdminFailureMessage(
        error,
        "clipboard",
      ));
    }
  }

  const liveTokens = tokens.filter((token) => (token.status ?? "active") === "active");
  const endedTokens = tokens.length - liveTokens.length;
  const shownTokens = showEnded ? tokens : liveTokens;
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseAdminApiTitle")} data-admin-api>
      <div className={styles.sectionTitle}>{t("enterpriseAdminApiTitle")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseAdminApiConnection")}</strong>
          <span>{t("enterpriseAdminApiDescription")}</span>
        </div>
      </div>
      <dl className={local.values}>
        <dt>{t("enterpriseAdminApiBaseUrl")}</dt><dd><CopyableValue value={baseUrl} /></dd>
        <dt>{t("enterpriseAdminApiVersion")}</dt><dd><CopyableValue value="2026-06-01" /></dd>
      </dl>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseAdminApiTokens")}</strong>
          <span>{t(canManageSecurity ? "enterpriseAdminApiScopeHelp" : "enterpriseAdminApiSecurityRoleRequired")}</span>
        </div>
        {canManageSecurity ? (
          <button type="button" className={local.addLink} data-admin-token-add onClick={() => setCreating(true)}>
            <Plus size="small" aria-hidden="true" />{t("enterpriseAdminApiCreateToken")}
          </button>
        ) : null}
      </div>
      <table className={local.table} aria-busy={busy === "list"}>
        <thead>
          <tr><th>{t("enterpriseAdminApiTokenLabel")}</th><th>{t("enterpriseTokenUsedColumn")}</th><th aria-label={t("domainActionsColumn")} /></tr>
        </thead>
        <tbody>
          {shownTokens.length ? shownTokens.map((token) => (
            <tr key={token.id}>
              <td>
                <strong>{token.label}</strong>
                {/* The prefix and how many permissions, the list on hover: twenty-odd scope names under each
                    token were most of the table (2026-11-05). */}
                <span className={local.cellNote} title={(token.scopes?.capabilities ?? []).join("\n") || undefined}>
                  {[token.tokenPrefix, token.scopes?.capabilities?.length
                    ? t("enterpriseAdminApiScopeCount", { count: token.scopes.capabilities.length }) : ""].filter(Boolean).join(" · ")}
                </span>
              </td>
              <td>{token.lastUsedAt ? displayDateTime(token.lastUsedAt) : t("enterpriseNeverUsed")}</td>
              <td className={local.actions}>
                {canManageSecurity && (token.status ?? "active") === "active" ? (
                  <SettingsRowMenu label={t("organizationMemberMenu", { name: token.label })} data={{ "data-admin-token-menu": token.id }} rows={[
                    { key: "revoke", label: t("enterpriseRevoke"), danger: true, disabled: busy === `revoke:${token.id}`, run: () => void revokeToken(token) },
                  ]} />
                ) : token.status === "revoked" ? t("tokenStatusRevoked") : token.status === "expired" ? t("tokenStatusExpired") : token.status}
              </td>
            </tr>
          )) : <tr><td colSpan={3} className={local.empty}>{t("enterpriseAdminApiNoTokens")}</td></tr>}
        </tbody>
      </table>
      {endedTokens ? (
        <button type="button" className={local.endedToggle} aria-expanded={showEnded} data-admin-token-ended
          onClick={() => setShowEnded((shown) => !shown)}>
          {showEnded ? t("enterpriseAdminApiHideEnded") : t("enterpriseAdminApiShowEnded", { count: endedTokens })}
        </button>
      ) : null}
      {message && !creating ? <div className={styles.notice} role="alert">{message}</div> : null}
      {creating ? (
        <SettingsFormDialog title={t("enterpriseAdminApiCreateToken")} busy={busy === "create"} wide initialFocus={labelRef}
          onClose={() => { setCreating(false); setSecret(""); setMessage(""); }}>
          {secret ? (
            <div className={dialogs.dialogBody}>
              <p className={dialogs.dialogNote}>{t("enterpriseAdminApiCopyNow")}</p>
              <code className={dialogs.secret} data-admin-token-secret>{secret}</code>
              <div className={dialogs.dialogActions}>
                <button type="button" className={dialogs.secondary} onClick={() => void copySecret()}>{t("enterpriseCopy")}</button>
                <button type="button" className={dialogs.primary} onClick={() => { setCreating(false); setSecret(""); }}>{t("common:actions.done")}</button>
              </div>
            </div>
          ) : (
            <form className={`${dialogs.dialogBody} ${local.rowSizedFields}`} onSubmit={(event) => void createToken(event)} data-admin-token-dialog>
              <label className={local.dialogField}>
                {t("enterpriseAdminApiTokenLabel")}
                <input ref={labelRef} required maxLength={bounds.maxScalarCharacters} value={label}
                  placeholder={t("enterpriseAdminApiTokenLabelPlaceholder")} onChange={(event) => setLabel(event.target.value)} />
              </label>
              {/* What the token may do and where sits behind one line, as long details are folded on a
                  first screen; all of it is chosen until someone narrows it. */}
              <details className={local.dialogMore} data-admin-token-scope>
                <summary>{t("enterpriseAdminApiScopeSummary", { count: capabilities.length })}</summary>
                <fieldset className={local.choices} aria-label={t("enterpriseAdminApiCapabilities")}>
                  <legend>{t("enterpriseAdminApiCapabilities")}</legend>
                  {NOTION_ADMIN_CAPABILITIES.map((capability) => (
                    <label key={capability}>
                      <input type="checkbox" checked={capabilities.includes(capability)} onChange={() => toggleCapability(capability)} />
                      {capability}
                    </label>
                  ))}
                </fieldset>
                <label className={local.dialogField}>
                  {t("enterpriseAdminApiWorkspaceIds")}
                  <textarea rows={2} maxLength={bounds.maxTotalCharacters} value={workspaceIds} onChange={(event) => setWorkspaceIds(event.target.value)} />
                </label>
                <label className={local.dialogField}>
                  {t("enterpriseAdminApiLegalHoldIds")}
                  <textarea rows={2} maxLength={bounds.maxTotalCharacters} value={legalHoldIds} onChange={(event) => setLegalHoldIds(event.target.value)} />
                </label>
              </details>
              <div className={local.switchRow}>
                {t("enterpriseAdminApiExpiresAt")}
                <AppDateTimeField label={t("enterpriseAdminApiExpiresDay")} timeLabel={t("enterpriseAdminApiExpiresTime")} value={expiresAt}
                  emptyLabel={t("enterpriseAdminApiNoExpiry")} clearLabel={t("enterpriseAdminApiNoExpiry")} onChange={setExpiresAt} />
              </div>
              {message ? <div className={styles.notice} role="alert">{message}</div> : null}
              <div className={dialogs.dialogActions}>
                <button type="button" className={dialogs.cancel} disabled={busy === "create"} onClick={() => setCreating(false)}>{t("common:actions.cancel")}</button>
                <button type="submit" className={dialogs.primary} disabled={busy === "create" || capabilities.length === 0}>{t("enterpriseAdminApiCreateToken")}</button>
              </div>
            </form>
          )}
        </SettingsFormDialog>
      ) : null}
    </section>
  );
}
