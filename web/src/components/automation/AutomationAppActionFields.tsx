import { useTranslation } from "react-i18next";
import type { UsableApp, UsableAppInputField } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import type { RunAppActionAutomationAction } from "@/lib/types";
import styles from "./automationActionEditor.module.css";
import { AppSelect } from "@/components/AppSelect";

/**
 * "앱 동작 실행" (Leafen): one of an installed app's actions that
 * don't ask for approval, with its input — text, number and yes/no fields
 * as the app names them, anything else as JSON.
 */
export function AutomationAppActionFields({ action, apps, number, onChange }: {
  action: RunAppActionAutomationAction;
  apps: UsableApp[];
  number: number;
  onChange: (action: RunAppActionAutomationAction) => void;
}) {
  const { t, i18n } = useTranslation(["databaseView"]);
  const app = apps.find((candidate) => candidate.installId === action.appInstallId);
  const automation = app?.automations?.find((candidate) => candidate.key === action.appAction);
  const label = (field: UsableAppInputField) => `${appText(field.labels ?? {}, i18n.language) || field.label}${field.required ? " *" : ""}`;
  const set = (key: string, value: unknown) => {
    const appInput = { ...action.appInput };
    if (value === "" || value === undefined) delete appInput[key];
    else appInput[key] = value;
    onChange({ ...action, appInput });
  };
  const choose = (installId: string) => {
    const next = apps.find((candidate) => candidate.installId === installId);
    onChange({ ...action, appInstallId: installId, appAction: next?.automations?.[0]?.key ?? "", appInput: {} });
  };
  return (
    <>
      <AppSelect aria-label={t("databaseView:appForAction", { number })} value={action.appInstallId} data-automation-app
        onChange={(event) => choose(event.target.value)}>
        {app ? null : <option value="">{t("databaseView:chooseApp")}</option>}
        {apps.map((candidate) => <option key={candidate.installId} value={candidate.installId}>{appText(candidate.name, i18n.language)}</option>)}
      </AppSelect>
      {app ? (
        <AppSelect aria-label={t("databaseView:appActionForAction", { number })} value={action.appAction} data-automation-app-action
          onChange={(event) => onChange({ ...action, appAction: event.target.value, appInput: {} })}>
          {(app.automations ?? []).map((candidate) => (
            <option key={candidate.key} value={candidate.key}>{appText(candidate.name, i18n.language) || candidate.key}</option>
          ))}
        </AppSelect>
      ) : null}
      {automation?.externalEffect ? <p>{t("databaseView:appActionExternal")}</p> : null}
      {automation?.fields.map((field) => {
        const value = action.appInput[field.key];
        if (field.type === "boolean") {
          return (
            <label key={field.key} className={styles.checkLabel}>
              <input type="checkbox" checked={value === true} data-automation-app-field={field.key}
                onChange={(event) => set(field.key, event.target.checked)} />
              {label(field)}
            </label>
          );
        }
        if (field.type === "number" || field.type === "integer") {
          return (
            <label key={field.key} className={styles.fieldLabel}>
              <span aria-hidden="true">{label(field)}</span>
              <input type="number" step={field.type === "integer" ? 1 : "any"} aria-label={label(field)}
                data-automation-app-field={field.key} value={typeof value === "number" ? value : ""}
                onChange={(event) => set(field.key, event.target.value === "" ? undefined : Number(event.target.value))} />
            </label>
          );
        }
        if (field.type === "string") {
          return (
            <label key={field.key} className={styles.fieldLabel}>
              <span aria-hidden="true">{label(field)}</span>
              <input aria-label={label(field)} data-automation-app-field={field.key}
                value={typeof value === "string" ? value : ""} onChange={(event) => set(field.key, event.target.value)} />
            </label>
          );
        }
        return (
          <label key={field.key} className={styles.fieldLabel}>
            <span aria-hidden="true">{`${label(field)} (JSON)`}</span>
            <textarea aria-label={label(field)} data-automation-app-field={field.key}
              defaultValue={value === undefined ? "" : JSON.stringify(value)}
              onChange={(event) => {
                try {
                  set(field.key, event.target.value.trim() ? JSON.parse(event.target.value) : undefined);
                } catch {
                  // Kept as typed until it reads as JSON.
                }
              }} />
          </label>
        );
      })}
    </>
  );
}
