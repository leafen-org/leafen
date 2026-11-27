import { type FormEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { SAMLConfigurationInput } from "@/enterpriseSAMLWire.generated";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SwitchMark } from "@/components/Switch";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import local from "@/components/settings/SettingsRows.module.css";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";
import { useCopyNotice } from "@/lib/copyNotice";

/**
 * "SAML SSO 구성 편집": what the identity provider gave, and whether it may
 * start a sign-in. Saving with the switch just turned on turns SSO on.
 */
export function SamlConfigDialog({ initial, certificateKept, busy, result, onSave, onClose }: {
  initial: SAMLConfigurationInput;
  certificateKept: boolean;
  busy: boolean;
  result: EnterpriseActionResult;
  onSave: (draft: SAMLConfigurationInput) => Promise<boolean>;
  onClose: () => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [draft, setDraft] = useState(initial);
  const issuerRef = useRef<HTMLInputElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (await onSave(draft)) onClose();
  };
  return (
    <SettingsFormDialog title={t("enterpriseSsoEdit")} busy={busy} wide initialFocus={issuerRef} onClose={onClose}>
      <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)} data-saml-config-dialog>
        <p className={dialogs.dialogNote}>{t("enterpriseSsoDialogNote")}</p>
        <label className={local.dialogField}>
          {t("enterpriseSamlIdpEntityId")}
          <input ref={issuerRef} value={draft.issuer} onChange={(event) => setDraft({ ...draft, issuer: event.target.value })} />
        </label>
        <label className={local.dialogField}>
          {t("enterpriseSamlLoginUrl")}
          <input type="url" placeholder="https://idp.example.com/sso" value={draft.loginUrl}
            onChange={(event) => setDraft({ ...draft, loginUrl: event.target.value })} />
        </label>
        <label className={local.dialogField}>
          {t("enterpriseSamlCertificate")}
          <textarea rows={5} value={draft.certificate} placeholder={certificateKept ? t("enterpriseSamlCertificateKept") : undefined}
            onChange={(event) => setDraft({ ...draft, certificate: event.target.value })} />
        </label>
        <label className={local.switchRow}>
          {t("enterpriseSamlAllowIdpInitiated")}
          <button type="button" role="switch" aria-checked={draft.allowIdpInitiated} aria-label={t("enterpriseSamlAllowIdpInitiated")}
            onClick={() => setDraft({ ...draft, allowIdpInitiated: !draft.allowIdpInitiated })}>
            <SwitchMark on={draft.allowIdpInitiated} />
          </button>
        </label>
        <EnterpriseActionFeedback result={result} actions={["sso"]} />
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={dialogs.primary} disabled={busy}>{t("enterpriseSaveSso")}</button>
        </div>
      </form>
    </SettingsFormDialog>
  );
}

/** "새 SCIM 토큰": a name, then the secret for the one time it can be copied, as a personal token's. */
export function ScimTokenDialog({ busy, result, onCreate, onClose }: {
  busy: boolean;
  result: EnterpriseActionResult;
  onCreate: (label: string) => Promise<string | null>;
  onClose: () => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [label, setLabel] = useState("");
  const [secret, setSecret] = useState("");
  const copy = useCopyNotice();
  const nameRef = useRef<HTMLInputElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const created = await onCreate(label.trim());
    if (created) setSecret(created);
  };
  return (
    <SettingsFormDialog title={t("enterpriseScimTokenDialogTitle")} busy={busy} initialFocus={nameRef} onClose={onClose}>
      {secret ? (
        <div className={dialogs.dialogBody}>
          <p className={dialogs.dialogNote}>{t("enterpriseCopyTokenNow")}</p>
          <code className={dialogs.secret} data-scim-token-secret>{secret}</code>
          <div className={dialogs.dialogActions}>
            <button type="button" className={dialogs.secondary}
              onClick={() => void copy(secret)}>
              {t("personalAccessTokenCopy")}
            </button>
            <button type="button" className={dialogs.primary} onClick={onClose}>{t("common:actions.done")}</button>
          </div>
        </div>
      ) : (
        <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)}>
          <label className={local.dialogField}>
            {t("enterpriseTokenLabel")}
            <input ref={nameRef} value={label} placeholder={t("enterpriseDefaultTokenLabel")}
              onChange={(event) => setLabel(event.target.value)} />
          </label>
          <EnterpriseActionFeedback result={result} actions={["token:create"]} />
          <div className={dialogs.dialogActions}>
            <button type="button" className={dialogs.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
            <button type="submit" className={dialogs.primary} disabled={busy}>{t("enterpriseCreateToken")}</button>
          </div>
        </form>
      )}
    </SettingsFormDialog>
  );
}
