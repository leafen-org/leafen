"use client";

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { SAMLConfigurationInput } from "@/enterpriseSAMLWire.generated";
import type { SCIMConfiguration } from "@/enterpriseSCIMWire.generated";
import type {
  EnterpriseActionRunner,
  EnterpriseSettingsService,
  EnterpriseSettingsSnapshot,
} from "@/lib/enterpriseSettingsAdapter";

import { confirmAction } from "@/components/ConfirmActionDialog";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { SwitchMark } from "@/components/Switch";
import { ArrowDown, Plus } from "@/icons/leafen";
import { serverUrl } from "@/lib/serverOrigin";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";
import { AppSelect } from "@/components/AppSelect";
import { CopyableValue } from "@/components/settings/CopyableValue";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";
import { displayDate, displayDateTime } from "@/lib/displayDate";
import { SamlConfigDialog, ScimTokenDialog } from "./EnterpriseIdentityDialogs";

type SsoDraft = SAMLConfigurationInput;

type EnterpriseIdentityProvisioningProps = {
  busy: string;
  canManageSecurity: boolean;
  organizationId: string;
  runAction: EnterpriseActionRunner;
  service: EnterpriseSettingsService;
  snapshot: EnterpriseSettingsSnapshot;
  /** The last save's outcome, said under the form it came from. */
  result: EnterpriseActionResult;
  /** Only SAML SSO or only SCIM, where the settings' "신원" places each. */
  part?: "sso" | "scim";
};

function initialSso(snapshot: EnterpriseSettingsSnapshot): SsoDraft {
  const provider = snapshot.saml.provider;
  return {
    enabled: provider?.enabled ?? false,
    issuer: provider?.issuer ?? "",
    loginUrl: provider?.loginUrl ?? "",
    certificate: "",
    allowIdpInitiated: provider?.allowIdpInitiated ?? false,
    enforcement: provider?.enforcement ?? "optional",
    emailAttribute: provider?.emailAttribute ?? "email",
    linkedWorkspaceId: provider?.linkedWorkspaceId ?? "",
    accountCreation: provider?.accountCreation ?? "off",
  };
}

export function EnterpriseIdentityProvisioning({
  busy,
  canManageSecurity,
  organizationId,
  runAction,
  service,
  snapshot,
  result,
  part,
}: EnterpriseIdentityProvisioningProps) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [sso, setSso] = useState(() => initialSso(snapshot));
  const [editingSso, setEditingSso] = useState<SsoDraft | null>(null);
  const [creatingToken, setCreatingToken] = useState(false);
  // The provider as last read or saved — the one current provider every part
  // reads: where its values are, after an address change the ones the identity
  // provider still has, the id in the shown addresses and whether a
  // certificate is kept (2026-11-13: the editor reopened after the first save
  // read the section's opening snapshot and showed no "저장되어 있습니다").
  const [samlProvider, setSamlProvider] = useState(() => snapshot.saml.provider);
  const [scim, setScim] = useState<SCIMConfiguration>(snapshot.scim);

  useEffect(() => {
    setSso(initialSso(snapshot));
    setSamlProvider(snapshot.saml.provider);
    setScim(snapshot.scim);
  }, [snapshot, snapshot.snapshotIdentity]);

  // The server serves these under its native API root on its published origin,
  // and its metadata, ACS and sign-in routes take the SAML provider's id (the
  // organization's id resolves to it too). Build the shown addresses from the
  // same facts so the IdP reaches what the server actually serves.
  const samlApiRoot = "/api/leafen/v1";
  const samlEntry = encodeURIComponent(samlProvider?.id ?? organizationId);
  // The address given to the identity provider is the server's outward one,
  // which the server says; this window's address only without it.
  const samlEndpointUrl = (path: string) => (samlProvider?.serviceOrigin ? samlProvider.serviceOrigin + path
    : typeof window === "undefined" ? path : serverUrl(path));
  const endpoint = samlEndpointUrl(`${samlApiRoot}/scim/v2`);
  const samlMetadataUrl = samlEndpointUrl(`${samlApiRoot}/auth/saml/${samlEntry}/metadata`);
  // The server names itself to the identity provider by its metadata URL
  // (newSAMLServiceProvider), as Notion's setup lists its "SAML SSO Entity
  // ID" beside the ACS URL; a first provider takes the organization's id, so
  // the value shown before the first save is the one the server checks.
  const samlEntityId = samlMetadataUrl;
  const samlAcsUrl = samlEndpointUrl(`${samlApiRoot}/auth/saml/acs`);
  const samlSignInUrl = samlEndpointUrl(`${samlApiRoot}/auth/saml/${samlEntry}/start`);

  // Each switch and choice saves at once, as Notion's rows do, and what shows
  // is what was saved: a refused save doesn't read as on (2026-10-16).
  async function saveSso(draft: SsoDraft) {
    const next = await runAction("sso", (context) => service.updateSAML(context, {
      ...draft,
      issuer: draft.issuer.trim(),
      loginUrl: draft.loginUrl.trim(),
      certificate: draft.certificate.trim(),
      emailAttribute: draft.emailAttribute?.trim() || "email",
    }));
    if (!next) return false;
    setSso(initialSso({ ...snapshot, saml: next }));
    setSamlProvider(next.provider);
    return true;
  }

  // Turning it on asks for what the identity provider gave when none is saved;
  // turning it off also ends requiring it, which only holds while it is on.
  function setSsoEnabled(enabled: boolean) {
    if (enabled && !samlProvider) setEditingSso({ ...sso, enabled: true });
    else void saveSso(enabled ? { ...sso, enabled } : { ...sso, enabled, enforcement: "optional" });
  }

  // 계정 자동 생성, as Notion's switch: on, new people join the linked
  // workspace, the one whose settings are open when none is linked yet.
  const linkedWorkspaces = snapshot.saml.workspaces;
  const autoCreate = sso.accountCreation === "automatic";
  // The workspace new accounts join, deleted since: they would wait for an
  // invitation, so the table says so and any workspace left can take them.
  const joinsGone = autoCreate && !linkedWorkspaces.some((workspace) => workspace.id === sso.linkedWorkspaceId);
  const choosesJoin = linkedWorkspaces.length > 1 || joinsGone;
  function setAutoCreate(on: boolean) {
    const linked = linkedWorkspaces.some((workspace) => workspace.id === sso.linkedWorkspaceId) ? sso.linkedWorkspaceId
      : linkedWorkspaces.find((workspace) => workspace.id === snapshot.currentWorkspaceId)?.id ?? linkedWorkspaces[0]?.id ?? "";
    void saveSso({ ...sso, accountCreation: on ? "automatic" : "off", linkedWorkspaceId: on ? linked : sso.linkedWorkspaceId });
  }

  async function setServiceReturn(added: boolean) {
    const next = await runAction("sso:return", (context) => service.setSAMLServiceReturn(context, added));
    if (!next) return;
    setSamlProvider(next.provider);
  }

  async function saveScim(enabled: boolean) {
    const next = await runAction("scim", (context) => service.updateSCIM(context, {
      enabled,
      provisioningMode: "scim_v2",
      deprovisionAction: "deactivate",
    }));
    if (!next) return;
    setScim(next);
  }

  async function createToken(label: string) {
    const next = await runAction("token:create", (context) => service.createSCIMToken(context, {
      label: label || t("enterpriseDefaultTokenLabel"),
    }));
    if (!next) return null;
    setScim(next.configuration);
    return next.token.token;
  }

  async function revokeToken(id: string, label: string) {
    // Ending a token's sync at once asks first, as removing a member does.
    if (!await confirmAction({ message: t("tokenRevokeConfirm", { name: label }), detail: t("tokenRevokeConfirmDetail"),
      confirmLabel: t("enterpriseRevoke"), danger: true })) return;
    const next = await runAction(`token:${id}`, (context) => service.revokeSCIMToken(context, id));
    if (!next) return;
    setScim(next);
  }

  return (
    <>
      {part === "scim" ? null : (
      <section className={styles.section} role="region" aria-label={t("enterpriseSsoTitle")} data-saml-section>
        <div className={styles.sectionTitle}>{t("enterpriseSsoTitle")}</div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseEnableSso")}</strong>
            <span>{t("enterpriseSsoDescription")}</span>
            {canManageSecurity ? (
              <button type="button" className={local.rowButton} disabled={busy === "sso"} data-saml-edit
                onClick={() => setEditingSso({ ...sso })}>
                {t("enterpriseSsoEdit")}
              </button>
            ) : null}
          </div>
          <button type="button" role="switch" aria-checked={sso.enabled} aria-label={t("enterpriseEnableSso")}
            disabled={!canManageSecurity || busy === "sso"} data-saml-enabled onClick={() => setSsoEnabled(!sso.enabled)}>
            <SwitchMark on={sso.enabled} />
          </button>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseEnforcement")}</strong>
            <span>{t("enterpriseEnforcementDescription")}</span>
          </div>
          <AppSelect aria-label={t("enterpriseEnforcement")} look="setting" menuWidth={280} value={sso.enforcement} data-saml-enforcement
            disabled={!canManageSecurity || !sso.enabled || busy === "sso"}
            onChange={(event) => void saveSso({ ...sso, enforcement: event.target.value as SsoDraft["enforcement"] })}>
            <option value="optional">{t("enterpriseOptional")}</option>
            <option value="required_for_verified_domains">{t("enterpriseVerifiedDomains")}</option>
            <option value="required_for_all_members">{t("enterpriseAllMembers")}</option>
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseAutoCreate")}</strong>
            <span>{t("enterpriseAutoCreateDescription")}</span>
          </div>
          <button type="button" role="switch" aria-checked={autoCreate} aria-label={t("enterpriseAutoCreate")}
            disabled={!canManageSecurity || !samlProvider || !sso.enabled || busy === "sso"} data-saml-account-creation
            onClick={() => setAutoCreate(!autoCreate)}>
            <SwitchMark on={autoCreate} />
          </button>
        </div>
        {/* With SAML SSO off, what it covers is dimmed, as Notion's. */}
        <div className={local.dimmable} data-dimmed={sso.enabled ? undefined : ""}>
          <div className={styles.securityInstruction} data-saml-linked-workspaces>
            <strong>{t("enterpriseLinkedWorkspace")}</strong>
            <span>{t("enterpriseLinkedWorkspacesDescription")}</span>
          </div>
          {joinsGone ? <p className={local.linkedGone} role="status" data-saml-new-accounts-gone>{t("enterpriseLinkedNewAccountsGone")}</p> : null}
          <table className={`${local.table} ${local.linkedTable}`}>
            <thead>
              <tr>
                <th aria-sort="ascending">
                  <span className={local.sorted}>{t("enterpriseLinkedColumnName")}<ArrowDown pixels={12} aria-hidden="true" /></span>
                </th>
                <th>{t("enterpriseLinkedColumnMembers")}</th>
                <th>{t("enterpriseLinkedColumnCreated")}</th>
                {choosesJoin ? <th aria-label={t("domainActionsColumn")} /> : null}
              </tr>
            </thead>
            <tbody>
              {linkedWorkspaces.map((workspace) => {
                // Where new accounts land is said only where there is a choice.
                const joins = linkedWorkspaces.length > 1 && autoCreate && workspace.id === sso.linkedWorkspaceId;
                return (
                  <tr key={workspace.id} data-saml-linked-workspace={workspace.id}>
                    <td>
                      <span className={local.linkedName}>
                        {workspace.name}
                        {workspace.id === snapshot.currentWorkspaceId ? <span className={local.tag}>{t("current")}</span> : null}
                        {joins ? <span className={local.tag} data-saml-new-accounts>{t("enterpriseLinkedNewAccounts")}</span> : null}
                      </span>
                    </td>
                    <td>{t("enterpriseLinkedMemberCount", { count: workspace.memberCount })}</td>
                    <td>{displayDate(workspace.createdAt)}</td>
                    {choosesJoin ? (
                      <td className={local.actions}>
                        {canManageSecurity && sso.enabled && autoCreate && !joins ? (
                          <SettingsRowMenu label={t("organizationMemberMenu", { name: workspace.name })} rows={[
                            { key: "join", label: t("enterpriseLinkedNewAccountsHere"), disabled: busy === "sso",
                              run: () => void saveSso({ ...sso, linkedWorkspaceId: workspace.id }) },
                          ]} />
                        ) : null}
                      </td>
                    ) : null}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseSamlValuesTitle")}</strong>
            <span>{t("enterpriseSamlValuesDescription")}</span>
          </div>
        </div>
        <dl className={local.values}>
          <dt>{t("enterpriseSamlMetadataUrl")}</dt><dd><CopyableValue value={samlMetadataUrl} /></dd>
          <dt>{t("enterpriseSamlAcsUrl")}</dt><dd><CopyableValue value={samlAcsUrl} /></dd>
          <dt>{t("enterpriseSamlEntityId")}</dt><dd data-saml-entity-id><CopyableValue value={samlEntityId} /></dd>
          <dt>{t("enterpriseSamlSignInUrl")}</dt><dd><CopyableValue value={samlSignInUrl} /></dd>
        </dl>
        <EnterpriseActionFeedback result={editingSso ? { ...result, action: "" } : result} actions={["sso"]} />
        {samlProvider?.earlierServiceOrigin ? (
          <div className={local.moved} data-saml-return-moved={samlProvider.serviceOriginAdded ? "added" : "earlier"}>
            <p>{t(samlProvider.serviceOriginAdded ? "enterpriseSamlReturnAdded" : "enterpriseSamlReturnMoved", { earlier: samlProvider.earlierServiceOrigin })}</p>
            <button type="button" className={local.rowButton} disabled={!canManageSecurity || busy !== ""}
              onClick={() => void setServiceReturn(!samlProvider.serviceOriginAdded)}>
              {t(samlProvider.serviceOriginAdded ? "enterpriseSamlReturnUndo" : "enterpriseSamlReturnAdd")}
            </button>
          </div>
        ) : null}
        {editingSso ? (
          <SamlConfigDialog initial={editingSso} certificateKept={Boolean(samlProvider)} busy={busy === "sso"}
            result={result} onSave={saveSso} onClose={() => setEditingSso(null)} />
        ) : null}
      </section>
      )}

      {part === "sso" ? null : (
      <section className={styles.section} role="region" aria-label={t("enterpriseScimTitle")} data-scim-section>
        <div className={styles.sectionTitle}>{t("enterpriseScimTitle")}</div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseEnableScim")}</strong>
            <span>{t("enterpriseScimDescription")}</span>
          </div>
          <button type="button" role="switch" aria-checked={scim.enabled} aria-label={t("enterpriseEnableScim")}
            disabled={!canManageSecurity || busy === "scim"} data-scim-enabled onClick={() => void saveScim(!scim.enabled)}>
            <SwitchMark on={scim.enabled} />
          </button>
        </div>
        <dl className={local.values}>
          <dt>{t("enterpriseScimEndpoint")}</dt><dd><CopyableValue value={endpoint} /></dd>
        </dl>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseScimTokensTitle")}</strong>
            <span>{t("enterpriseScimTokensDescription")}</span>
          </div>
          {canManageSecurity ? (
            <button type="button" className={local.addLink} disabled={!scim.enabled} data-scim-token-add onClick={() => setCreatingToken(true)}>
              <Plus size="small" aria-hidden="true" />{t("enterpriseCreateToken")}
            </button>
          ) : null}
        </div>
        <table className={local.table}>
          <thead>
            <tr><th>{t("enterpriseTokenLabel")}</th><th>{t("enterpriseTokenUsedColumn")}</th><th aria-label={t("domainActionsColumn")} /></tr>
          </thead>
          <tbody>
            {scim.tokens.length ? scim.tokens.map((token) => (
              <tr key={token.id}>
                <td><strong>{token.label}</strong></td>
                <td>
                  {token.lastUsedAt > 0 ? displayDateTime(token.lastUsedAt) : t("enterpriseNeverUsed")}
                </td>
                <td className={local.actions}>
                  {canManageSecurity && token.status === "active" ? (
                    <SettingsRowMenu label={t("organizationMemberMenu", { name: token.label })} data={{ "data-scim-token-menu": token.id }} rows={[
                      { key: "revoke", label: t("enterpriseRevoke"), danger: true, disabled: busy === `token:${token.id}`,
                        run: () => void revokeToken(token.id, token.label) },
                    ]} />
                  ) : token.status === "revoked" ? t("tokenStatusRevoked") : token.status === "expired" ? t("tokenStatusExpired") : token.status}
                </td>
              </tr>
            )) : <tr><td colSpan={3} className={local.empty}>{t("enterpriseScimNoTokens")}</td></tr>}
          </tbody>
        </table>
        <EnterpriseActionFeedback result={creatingToken ? { ...result, action: "" } : result} actions={["scim", "token:"]} />
        {creatingToken ? (
          <ScimTokenDialog busy={busy === "token:create"} result={result} onCreate={createToken} onClose={() => setCreatingToken(false)} />
        ) : null}
      </section>
      )}
    </>
  );
}
