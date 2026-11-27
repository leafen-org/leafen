import { useId, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { AppApprovalField } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { closedMentionLabel, isClosedMentionText, titleWithClosedMentions } from "@/lib/closedMentions";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./AppApprovalDialog.module.css";

export type AppApprovalAnswer = "decline" | "once" | "hour";

let host: HTMLDivElement | null = null;
let root: Root | null = null;
let answerOpen: ((answer: AppApprovalAnswer) => void) | null = null;

/**
 * Leafen's window — never the app's frame — asking the person to allow
 * an app's action (docs/app-platform.md 5): what the app wants to do, its
 * input as lines they read, and allowing it just once or, where the
 * organization isn't overseen, for an hour without asking again.
 */
export function askAppApproval(request: {
  appName: string;
  actionName: string;
  risk: string;
  fields: AppApprovalField[];
  allowHour: boolean;
  externalEffect?: boolean;
}): Promise<AppApprovalAnswer> {
  if (typeof document === "undefined") return Promise.resolve("decline");
  answerOpen?.("decline");
  if (!host || !host.isConnected) {
    host = document.createElement("div");
    host.dataset.appApprovalHost = "";
    // Drawn over everything: nothing that hides the page outside a window keeps it hidden (ConfirmActionDialog.tsx).
    host.dataset.reactAriaTopLayer = "";
    document.body.append(host);
    root = createRoot(host);
  }
  return new Promise((resolve) => {
    const answer = (choice: AppApprovalAnswer) => {
      if (answerOpen !== answer) return;
      answerOpen = null;
      root?.render(null);
      resolve(choice);
    };
    answerOpen = answer;
    root?.render(<AppApprovalDialog {...request} onAnswer={answer} />);
  });
}

/** One field's value as a person reads it. */
export function approvalFieldText(field: AppApprovalField, t: TFunction<"common">, language?: string) {
  switch (field.kind) {
    case "number": {
      // Grouped as the person reads numbers ("45,000"), as the app's own rows show it.
      const number = Number(field.value);
      return Number.isFinite(number) ? number.toLocaleString(language) : field.value;
    }
    case "boolean":
      return field.value === "true" ? t("appHost.approvalYes") : t("appHost.approvalNo");
    case "empty":
      return t("appHost.approvalEmpty");
    case "page":
      // A page or row with no title is named so in the reader's language; one
      // closed to the reader comes as a closed mention's mark and says so, a
      // mention in a title they can't open as a plain title says it
      // (app_platform_approval_page_title.go).
      if (isClosedMentionText(field.value)) return closedMentionLabel(field.value);
      return titleWithClosedMentions(field.value) || t("labels.untitled");
  }
  return field.value;
}

/** A field's name in the person's language, as the app gives it. */
export function approvalFieldLabel(field: AppApprovalField, language: string) {
  return appText(field.labels ?? {}, language) || field.label;
}

/** An approval's input, field by field. */
export function AppApprovalFields({ fields }: { fields: AppApprovalField[] }) {
  const { t, i18n } = useTranslation("common");
  if (!fields.length) return null;
  const shown = (field: AppApprovalField) => approvalFieldText(field, t, i18n.language);
  return (
    <dl className={styles.fields} data-app-approval-fields>
      {fields.map((field, index) => (
        <div key={`${field.label}-${index}`} className={styles.field} data-app-approval-field={field.kind}>
          <dt>{approvalFieldLabel(field, i18n.language)}</dt>
          <dd>{shown(field)}</dd>
        </div>
      ))}
    </dl>
  );
}

function AppApprovalDialog({ appName, actionName, risk, fields, allowHour, onAnswer, externalEffect}: {
  appName: string;
  actionName: string;
  risk: string;
  fields: AppApprovalField[];
  allowHour: boolean;
  externalEffect?: boolean;
  onAnswer(answer: AppApprovalAnswer): void;
}) {
  const { t } = useTranslation("common");
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const declineRef = useRef<HTMLButtonElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "alertdialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: true,
    onEscape: () => onAnswer("decline"), initialFocus: declineRef,
  });
  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("appHost.approvalDecline")} onClick={() => onAnswer("decline")} />
      <section ref={dialogRef} className={styles.dialog} role="alertdialog" aria-modal="true" aria-labelledby={titleId} data-app-approval-dialog>
        <h2 id={titleId} className={styles.message}>{t("appHost.approval", { app: appName, action: actionName })}</h2>
        <AppApprovalFields fields={fields} />
        {risk === "high" ? <p className={styles.important} data-app-approval-important>{t("appHost.approvalImportant")}</p> : null}
        {/* The same lines in the same order as the approval page an agent's call links to. */}
        {externalEffect ? <p className={styles.important} data-app-approval-external>{t("appHost.approvalExternal")}</p> : null}
        <button type="button" className={styles.action} data-app-approval-once onClick={() => onAnswer("once")}>
          {t("appHost.approvalOnce")}</button>
        {allowHour ? (
          <button type="button" className={styles.action} data-app-approval-hour onClick={() => onAnswer("hour")}>{t("appHost.approvalHour")}</button>
        ) : null}
        <button ref={declineRef} type="button" className={styles.action} data-app-approval-decline onClick={() => onAnswer("decline")}>
          {t("appHost.approvalDecline")}</button>
      </section>
    </div>
  );
}
