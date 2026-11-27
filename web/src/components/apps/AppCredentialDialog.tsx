import { type FormEvent, useId, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { createRoot, type Root } from "react-dom/client";
import { useTranslation } from "react-i18next";
import type { AppCredential } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./AppCredentialDialog.module.css";

let host: HTMLDivElement | null = null;
let root: Root | null = null;
let answerOpen: ((saved: boolean) => void) | null = null;

/**
 * Leafen's window — never the app's frame — where a person types what an
 * app asks for, such as their mail account (the mail decision of
 * 2026-10-07). It says the app never sees it and suggests an app password;
 * `save` keeps it on the server, and the window answers whether it did.
 */
export function enterAppCredential(request: {
  appName: string;
  credential: AppCredential;
  language: string;
  save(values: Record<string, string>): Promise<void>;
}): Promise<boolean> {
  if (typeof document === "undefined") return Promise.resolve(false);
  answerOpen?.(false);
  if (!host || !host.isConnected) {
    host = document.createElement("div");
    host.dataset.appCredentialHost = "";
    // Drawn over everything: nothing that hides the page outside a window keeps it hidden (ConfirmActionDialog.tsx).
    host.dataset.reactAriaTopLayer = "";
    document.body.append(host);
    root = createRoot(host);
  }
  return new Promise((resolve) => {
    const answer = (saved: boolean) => {
      if (answerOpen !== answer) return;
      answerOpen = null;
      root?.render(null);
      resolve(saved);
    };
    answerOpen = answer;
    root?.render(<AppCredentialDialog {...request} onAnswer={answer} />);
  });
}

function AppCredentialDialog({ appName, credential, language, save, onAnswer }: {
  appName: string;
  credential: AppCredential;
  language: string;
  save(values: Record<string, string>): Promise<void>;
  onAnswer(saved: boolean): void;
}) {
  const { t } = useTranslation("common");
  const titleId = useId();
  const dialogRef = useRef<HTMLFormElement>(null);
  const firstRef = useRef<HTMLInputElement>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(values);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: true,
    onEscape: () => { if (!busy) onAnswer(false); }, initialFocus: firstRef,
  });
  const name = appText(credential.name, language);
  const complete = credential.fields.every((field) => (values[field.key] ?? "") !== "");

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!complete || busy) return;
    setBusy(true);
    setError("");
    try {
      await save(values);
      onAnswer(true);
    } catch (failure) {
      setBusy(false);
      setError(String(failure instanceof Error ? failure.message : failure).includes("credentials")
        ? t("appHost.credential.off") : t("appHost.credential.failed"));
    }
  }

  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("appHost.credential.cancel")} disabled={busy}
        onClick={() => onAnswer(false)} />
      <form ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        data-app-credential-dialog={credential.key} onSubmit={(event) => void submit(event)}>
        <h2 id={titleId} className={styles.title}>{t("appHost.credential.title", { app: appName, name })}</h2>
        <p className={styles.note}>{t("appHost.credential.sealed", { app: appName })}</p>
        {credential.help ? <p className={styles.note}>{appText(credential.help, language)}</p> : null}
        <p className={styles.note}>{t("appHost.credential.appPassword")}</p>
        {credential.fields.map((field, index) => (
          <label key={field.key} className={styles.field}>
            {appText(field.name, language)}
            <input ref={index === 0 ? firstRef : undefined} type={field.secret ? "password" : "text"}
              autoComplete={field.secret ? "new-password" : "off"} spellCheck={false} disabled={busy}
              value={values[field.key] ?? ""} data-app-credential-field={field.key}
              onChange={(event) => setValues((current) => ({ ...current, [field.key]: event.target.value }))} />
          </label>
        ))}
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
        <div className={styles.actions}>
          <button type="button" className={styles.cancel} disabled={busy} onClick={() => onAnswer(false)}>{t("appHost.credential.cancel")}</button>
          <button type="submit" className={styles.save} disabled={busy || !complete} data-app-credential-save>{t("appHost.credential.save")}</button>
        </div>
      </form>
    </div>
  );
}
