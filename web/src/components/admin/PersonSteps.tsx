import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { LeafenApiError } from "../../apiTransport";
import { useCopyNotice } from "@/lib/copyNotice";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { cancelPersonStep, enterPersonStep, listPersonSteps, revealPersonStep, type PersonStep } from "../../personStepsApi";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import own from "./PersonSteps.module.css";
import { scheduleMessage } from "./scheduledBackupFolderWords";
import { serviceError } from "../settings/MCPServices";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * What an agent left to its person in this settings section (person_steps.go,
 * user decision 2026-11-22): the agent prepared the setting and Leafen holds
 * it until the person, here, types what only they can give — a key, a
 * password —, keeps the recovery code it shows, or opens what it made for
 * them alone. The same "여기에 직접 넣으세요" as a new server's restore left
 * to an agent (SetupRestoreAgent.tsx): the agent never sees it, and Leafen
 * runs the agent's request as this person with it. Leafen's own screen.
 */
export function PersonSteps({ session, section, onDone, outsideConsole = false }: {
  session: Session;
  section: string;
  onDone(): void;
  /** Drawn in the person's own settings, not the server's pages: it takes their measure here. */
  outsideConsole?: boolean;
}) {
  const [steps, setSteps] = useState<PersonStep[]>([]);
  const [turn, setTurn] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    listPersonSteps(session, section, controller.signal)
      .then((answer) => setSteps(answer.steps))
      // The section shows as it is without them; the next visit reads again.
      .catch(() => undefined);
    return () => controller.abort();
  }, [session, section, turn]);

  if (!steps.length) return null;
  const slots = steps.map((step) => (
    <PersonStepSlot key={step.id} session={session} step={step}
      onEnded={(done) => { setTurn((current) => current + 1); if (done) onDone(); }} />
  ));
  return outsideConsole ? <div className={`${styles.adminConsole} ${own.outside}`}>{slots}</div> : <>{slots}</>;
}

function PersonStepSlot({ session, step, onEnded }: { session: Session; step: PersonStep; onEnded(done: boolean): void }) {
  const { t } = useTranslation("personSteps");
  const { t: tServer } = useTranslation("serverConsole");
  const { t: tWorkspace } = useTranslation("workspaceSettingsDialog");
  const words = { t, tServer, tWorkspace };
  const copy = useCopyNotice();
  const [values, setValues] = useState<Record<string, string>>({});
  const [confirm, setConfirm] = useState("");
  const [code, setCode] = useState("");
  const [shown, setShown] = useState<Record<string, string> | null>(null);
  const [kept, setKept] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const mounted = useRef(true);
  const keeping = step.keep === "recovery-code" && !step.kept;
  const keepOnly = keeping && !step.fields.length;
  // A backup password the person makes up is typed twice, as on 백업 보호.
  const twice = step.fields.includes("password") && step.operation.includes("/admin/backups/");

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  // The recovery code to keep shows as the slot does.
  useEffect(() => {
    if (!keeping) return;
    revealPersonStep(session, step.id)
      .then((answer) => { if (mounted.current) setCode(answer.shown.recoveryCode ?? ""); })
      .catch((cause) => { if (mounted.current) setError(stepMessage(cause, step, words)); });
    // The words follow the language, which the code needn't be made again for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keeping, session, step]);

  async function act(action: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (cause) {
      if (mounted.current) setError(stepMessage(cause, step, words));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (twice && values.password !== confirm) {
      setError(t("serverConsole:protection.passwordMismatch"));
      return;
    }
    void act(async () => {
      await enterPersonStep(session, step.id, { values, kept: keeping ? kept || keepOnly : false });
      onEnded(true);
    });
  }

  const filled = step.fields.every((field) => values[field]?.trim()) && (!twice || confirm);
  const ready = filled && (!keeping || kept || keepOnly) && (!keeping || code);
  return (
    <section className={styles.section} aria-labelledby={`person-step-${step.id}`} data-person-step={step.id}>
      <div id={`person-step-${step.id}`} className={styles.sectionTitle}>{t("title", { title: step.title })}</div>
      <form className={styles.settingRows} noValidate onSubmit={submit}>
        <p className={styles.settingNote}>
          {step.agent ? t("preparedBy", { agent: step.agent }) : t("preparedByAgent")}
          {step.place ? <> · <span className={own.place}>{step.place}</span></> : null}
        </p>
        {keeping ? (
          <div className={own.block} data-person-step-code>
            <strong className={own.head}>{t("codeTitle")}</strong>
            <p className={styles.emailProviderMeta}>{t("serverConsole:protection.codeShownOnce")}</p>
            {code ? <code className={styles.recoveryCode} data-person-step-code-value>{code}</code>
              : <p className={styles.settingNote} role="status">{t("codeLoading")}</p>}
            <div className={dataStyles.actions}>
              <button type="button" className={styles.secondaryButton} disabled={!code} onClick={() => void copy(code)}>
                {t("serverConsole:protection.copy")}
              </button>
              {keepOnly ? (
                <button type="submit" className={styles.primaryButton} disabled={busy || !code} data-person-step-code-saved>
                  {t("serverConsole:protection.saved")}
                </button>
              ) : null}
            </div>
            {keepOnly ? null : (
              <label className={own.check}>
                <input type="checkbox" checked={kept} disabled={busy || !code} onChange={(event) => setKept(event.target.checked)}
                  data-person-step-code-kept />
                <span>{t("codeKept")}</span>
              </label>
            )}
            <p className={styles.emailProviderMeta}>{t("serverConsole:protection.codeHelp")}</p>
          </div>
        ) : null}
        {step.fields.length ? (
          <div className={styles.tunnelFields} data-person-step-fields>
            <strong className={own.head}>{t("fieldsTitle")}</strong>
            <p className={styles.emailProviderMeta}>{t("fieldsNote")}</p>
            {step.fields.map((field) => (
              <label key={field}>
                <span>{t(fieldLabel(step.operation, field))}</span>
                <input type="password" autoComplete="off" spellCheck={false} disabled={busy} value={values[field] ?? ""}
                  onChange={(event) => setValues((current) => ({ ...current, [field]: event.target.value }))}
                  data-person-step-field={field} />
              </label>
            ))}
            {twice ? (
              <label>
                <span>{t("serverConsole:protection.confirmPassword")}</span>
                <input type="password" autoComplete="off" spellCheck={false} disabled={busy} value={confirm}
                  onChange={(event) => setConfirm(event.target.value)} data-person-step-confirm />
              </label>
            ) : null}
          </div>
        ) : null}
        {step.shows ? (
          <div className={own.block} data-person-step-shows>
            <p className={styles.emailProviderMeta}>{t("showNote")}</p>
            {/* Showing it resets the account's password, as the users' own reset does. */}
            {step.operation === "PUT /admin/accounts/{accountId}/password" && !shown ? <p className={styles.emailProviderMeta}>{t("serverConsole:users.confirmResetDetail")}</p> : null}
            {shown ? Object.entries(shown).map(([name, value]) => (
              <div key={name} className={styles.tunnelFields}>
                <label>
                  <span>{t(fieldLabel(step.operation, name))}</span>
                  <textarea readOnly rows={3} value={value} onFocus={(event) => event.currentTarget.select()} data-person-step-shown />
                </label>
                <div className={dataStyles.actions}>
                  <button type="button" className={styles.secondaryButton} onClick={() => void copy(value)}>
                    {t("serverConsole:protection.copy")}
                  </button>
                </div>
                {step.operation.includes("/standby/code") ? <p className={styles.emailProviderMeta}>{t("serverConsole:standby.codeHelp")}</p> : null}
              </div>
            )) : null}
          </div>
        ) : null}
        <div className={dataStyles.actions}>
          {shown ? (
            <button type="button" className={styles.secondaryButton} onClick={() => onEnded(true)} data-person-step-close>
              {t("close")}
            </button>
          ) : (
            <button type="button" className={styles.secondaryButton} disabled={busy}
              onClick={() => void act(async () => { await cancelPersonStep(session, step.id); onEnded(false); })} data-person-step-cancel>
              {step.shows ? t("skip") : t("cancel")}
            </button>
          )}
          {step.shows && !shown ? (
            <button type="button" className={styles.primaryButton} disabled={busy}
              onClick={() => void act(async () => { setShown((await revealPersonStep(session, step.id)).shown); })} data-person-step-show>
              {t("show")}
            </button>
          ) : null}
          {step.fields.length ? (
            <button type="submit" className={styles.primaryButton} disabled={busy || !ready} data-person-step-save>
              {busy ? t("saving") : t("save")}
            </button>
          ) : null}
        </div>
        {error ? <p className={styles.errorText} role="alert" data-error>{error}</p> : null}
      </form>
    </section>
  );
}

// The field's name as the person's own settings screen says it.
const FIELD_LABELS: Record<string, string> = {
  s3AccessKey: "fields.s3AccessKey", s3SecretKey: "fields.s3SecretKey", webdavPassword: "fields.webdavPassword",
  "smtp.password": "fields.smtpPassword", "providers.google.clientSecret": "fields.googleClientSecret",
  "providers.oidc.clientSecret": "fields.oidcClientSecret", "placeSearch.apiKey": "fields.placeSearchApiKey",
  "placeSearch.apiKeyId": "fields.placeSearchApiKeyId", value: "fields.appSecret", code: "fields.standbyCode",
  secret: "fields.backupOpener", "auth.password": "fields.servicePassword", "auth.value": "fields.serviceKeyValue",
  temporaryPassword: "fields.temporaryPassword",
};

function fieldLabel(operation: string, field: string) {
  if (field === "password") {
    if (operation.includes("/network-proxy")) return "fields.proxyPassword";
    return operation.includes("/admin/accounts") ? "fields.accountPassword" : "fields.backupPassword";
  }
  if (field === "token" || field === "auth.token") return operation.includes("/mcp-services") ? "fields.serviceToken" : "fields.tunnelToken";
  return FIELD_LABELS[field] ?? "fields.secret";
}

/** A refused try in the words of the setting's own screen. */
function stepMessage(error: unknown, step: PersonStep, { t, tServer, tWorkspace }: { t: Translate; tServer: Translate; tWorkspace: TFunction }) {
  if (error instanceof LeafenApiError && error.code === "LEAFEN_PERSON_STEP_ENDED") return t("ended");
  if (step.operation === "PUT /admin/backups/schedule") return scheduleMessage(error, tServer);
  if (step.operation.includes("/mcp-services")) return serviceError(tWorkspace, error, tWorkspace("mcpService.errorFailed"));
  if (step.operation.includes("/admin/backups/")) return productErrorMessage(error, "administratorScheduledBackup.save");
  if (step.operation.includes("/network-proxy")) return productErrorMessage(error, "administratorNetworkProxy.save");
  return productErrorMessage(error, "administratorSettings.save");
}
