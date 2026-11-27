"use client";

import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { userErrorText } from "@/lib/settingsErrors";

import type {
  EnterpriseActionRunner,
  EnterpriseOperationContext,
  EnterpriseSettingsService,
  EnterpriseSettingsSnapshot,
} from "@/lib/enterpriseSettingsAdapter";
import {
  createEnterpriseSingleFlight,
  enterpriseOperationContext,
  runEnterpriseOperation,
} from "@/lib/enterpriseSettingsAdapter";

import type { EnterpriseAreas } from "@/lib/organizationSpecialistRolePolicy";
import { useOptionalProductToast } from "@/lib/productToast";
import { EnterpriseAuditExports, EnterpriseDiscovery } from "./enterprise/EnterpriseAuditDiscovery";
import { EnterpriseDataLossPrevention } from "./enterprise/EnterpriseDataLossPrevention";
import { EnterpriseIdentityProvisioning } from "./enterprise/EnterpriseIdentityProvisioning";
import { EnterpriseLegalHolds } from "./enterprise/EnterpriseLegalHolds";
import { EnterpriseMcpConnections } from "./enterprise/EnterpriseMcpConnections";
import chromeStyles from "@/components/WorkspaceSettingsChrome.module.css";
import enterpriseStyles from "@/components/WorkspaceSettingsEnterprise.module.css";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";

export type EnterpriseSettingsPanelProps = {
  /** The areas the person's admin roles cover; the others aren't shown. */
  areas: EnterpriseAreas;
  isCurrent: (context: EnterpriseOperationContext) => boolean;
  /** The organization's Admin API tokens, which are the core's. */
  adminPanel: ReactNode;
  organizationId: string;
  requestGeneration: number;
  service: EnterpriseSettingsService;
  sessionIdentity: string;
  signal: AbortSignal;
  snapshot: EnterpriseSettingsSnapshot;
  /** Opens the organization's audit log, where its records are exported. */
  onOpenAuditLog?(): void;
  /**
   * "identity": SAML SSO, the enterprise-managed MCP connections and SCIM, in
   * the order of Notion's "신원"; "governance": the rest (DLP, audit, discovery,
   * legal holds, the Admin API).
   */
  view?: "identity" | "governance";
};

type EnterpriseFeedback = {
  contextIdentity: string;
  busy: string;
  // The action the error belongs to, said under its own form.
  action: string;
  error: string;
};

function errorMessage(error: unknown) {
  return userErrorText(error, i18next.t("settingsErrors:requestFailed"));
}

export function EnterpriseSettingsPanel({
  areas,
  isCurrent,
  adminPanel,
  organizationId,
  requestGeneration,
  service,
  sessionIdentity,
  signal,
  snapshot,
  view,
  onOpenAuditLog,
}: EnterpriseSettingsPanelProps) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [feedback, setFeedback] = useState<EnterpriseFeedback>({
    contextIdentity: "",
    busy: "",
    action: "",
    error: "",
  });
  const singleFlight = useRef(createEnterpriseSingleFlight()).current;
  const context = useMemo(() => enterpriseOperationContext({
    sessionIdentity,
    organizationId,
    requestGeneration,
    snapshotIdentity: snapshot.snapshotIdentity,
    signal,
  }), [organizationId, requestGeneration, sessionIdentity, signal, snapshot.snapshotIdentity]);

  const contextIdentity = JSON.stringify([
    context.sessionIdentity,
    context.organizationId,
    context.requestGeneration,
    context.snapshotIdentity,
  ]);
  const currentFeedback = feedback.contextIdentity === contextIdentity
    ? feedback
    : { contextIdentity, busy: "", action: "", error: "" };

  const toast = useOptionalProductToast();
  const setNotice = useCallback((notice: string) => {
    if (isCurrent(context)) toast?.notify(notice);
  }, [context, isCurrent, toast]);

  const runAction = useCallback<EnterpriseActionRunner>(async <T,>(
    key: string,
    operation: (operationContext: EnterpriseOperationContext) => Promise<T>,
  ) => {
    if (context.signal.aborted || !isCurrent(context)) return undefined;
    const flightKey = `${contextIdentity}:${key}`;
    return singleFlight(flightKey, async () => {
      setFeedback({ contextIdentity, busy: key, action: key, error: "" });
      try {
        return await runEnterpriseOperation(context, isCurrent, operation);
      } catch (caught) {
        if (caught instanceof Error && caught.name === "AbortError") return undefined;
        if (isCurrent(context)) {
          setFeedback({
            contextIdentity,
            busy: "",
            action: key,
            error: errorMessage(caught),
          });
        }
        return undefined;
      } finally {
        if (isCurrent(context)) {
          setFeedback((current) => (
            current.contextIdentity === contextIdentity && current.busy === key
              ? { ...current, busy: "" }
              : current
          ));
        }
      }
    });
  }, [context, contextIdentity, isCurrent, singleFlight]);

  if (!organizationId) {
    return (
      <section className={chromeStyles.section}>
        <div className={enterpriseStyles.enterpriseCallout}>{t("enterpriseOrganizationRequired")}</div>
      </section>
    );
  }

  // Each save's refusal shows under its own form (EnterpriseActionFeedback).
  const result = { action: currentFeedback.action, error: currentFeedback.error };
  const shared = { busy: currentFeedback.busy, result, runAction, service, snapshot };
  if (view === "identity") {
    return (
      <div className={enterpriseStyles.enterpriseSections} data-enterprise-identity>
        {areas.identity ? <EnterpriseIdentityProvisioning {...shared} canManageSecurity={areas.identity} organizationId={organizationId} part="sso" /> : null}
        {areas.integrations ? <EnterpriseMcpConnections {...shared} /> : null}
        {areas.identity ? <EnterpriseIdentityProvisioning {...shared} canManageSecurity={areas.identity} organizationId={organizationId} part="scim" /> : null}
      </div>
    );
  }

  return (
    <>
      <section id="enterprise" className={chromeStyles.section}>
        <SettingsSectionTitle className={chromeStyles.sectionTitle} restates>{t("enterpriseTitle")}</SettingsSectionTitle>
      </section>
      {areas.security ? <EnterpriseDataLossPrevention {...shared} policy={snapshot.dlp} /> : null}
      {areas.auditLog ? <EnterpriseAuditExports {...shared} setNotice={setNotice} onOpenAuditLog={onOpenAuditLog} /> : null}
      {areas.contentSearch ? <EnterpriseDiscovery {...shared} setNotice={setNotice} organizationId={organizationId} /> : null}
      {areas.legalHolds ? <EnterpriseLegalHolds {...shared} organizationId={organizationId} /> : null}
      {areas.adminTokens ? adminPanel : null}
    </>
  );
}
