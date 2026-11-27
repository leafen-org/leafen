import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Trash } from "@/icons/leafen";
import type { AutomationAction, DbProperty } from "@/lib/types";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { webhookUrlValid, type AutomationEditorSurface } from "./automationActionModel";
import editStyles from "./AutomationEditProperty.module.css";
import styles from "./AutomationWebhookVariables.module.css";

type WebhookAction = Extract<AutomationAction, { type: "send_webhook" }>;

/**
 * A webhook's fields, as Notion's card (seen 2026-10-24 at 1280px): "URL"
 * over its field and "유효하지 않은 URL" under one left so, or left empty; each custom header
 * a "키" and a "값" field and a trash button, "사용자 지정 헤더 추가" under
 * them; and from an automation, under a line, "콘텐츠": a box of checks —
 * "기존 속성 모두 선택" over a line, then each property by its icon and name.
 */
export function AutomationWebhookFields({ action, number, surface, properties, propertyLabels, saved = false, onChange }: {
  action: WebhookAction;
  number: number;
  surface: AutomationEditorSurface;
  properties: DbProperty[];
  propertyLabels: string[];
  /** Warned as an earlier action is: saved so, or another added after it (AutomationActionEditor). */
  saved?: boolean;
  onChange(action: WebhookAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const [left, setLeft] = useState(false);
  // Left empty, it is no address either (Notion, seen 2026-11-01).
  const invalid = (left || saved) && !webhookUrlValid(action.url);
  const header = (headerIndex: number, change: Partial<WebhookAction["headers"][number]>) => onChange({
    ...action,
    headers: action.headers.map((candidate, index) => index === headerIndex ? { ...candidate, ...change } : candidate),
  });
  // A deleted property stays chosen, red, until it is let go.
  const gone = action.propertyIds.filter((id) => !properties.some((property) => property.id === id));
  const all = properties.length > 0 && properties.every((property) => action.propertyIds.includes(property.id));
  const choose = (id: string, chosen: boolean) => onChange({
    ...action,
    propertyIds: chosen ? [...action.propertyIds.filter((candidate) => candidate !== id), id] : action.propertyIds.filter((candidate) => candidate !== id),
  });
  return (
    <div className={styles.webhook}>
      <p className={`${editStyles.fieldName} ${styles.urlName}`}>{t("webhookUrlLabel")}</p>
      <span className={styles.box} data-invalid={invalid || undefined}>
        <input className={styles.input} aria-label={t("urlForAction", { number })} aria-invalid={invalid || undefined} value={action.url}
          onChange={(event) => onChange({ ...action, url: event.target.value })} onBlur={() => setLeft(true)} />
      </span>
      {invalid ? <p className={styles.error} data-webhook-url-invalid>{t("webhookUrlInvalid")}</p> : null}
      {action.headers.map((value, headerIndex) => (
        <div key={`${action.id}-header-${headerIndex}`} className={styles.header} data-webhook-header={headerIndex}>
          <label className={styles.headerField}>
            <span>{t("webhookHeaderKey")}</span>
            <span className={styles.box}>
              <input className={styles.input} aria-label={t("webhookHeaderKeyForAction", { number, header: headerIndex + 1 })} value={value.name}
                onChange={(event) => header(headerIndex, { name: event.target.value })} />
            </span>
          </label>
          <label className={styles.headerField}>
            <span>{t("webhookHeaderValue")}</span>
            <span className={styles.box}>
              <input className={styles.input} aria-label={t("webhookHeaderValueForAction", { number, header: headerIndex + 1 })} value={value.value}
                onChange={(event) => header(headerIndex, { value: event.target.value })} />
            </span>
          </label>
          <button type="button" className={styles.headerRemove}
            aria-label={t("removeWebhookHeaderForAction", { number, header: headerIndex + 1 })}
            onClick={() => onChange({ ...action, headers: action.headers.filter((_, index) => index !== headerIndex) })}>
            <Trash size="regular" aria-hidden="true" />
          </button>
        </div>
      ))}
      <button type="button" className={styles.textButton} data-webhook-header-add
        onClick={() => onChange({ ...action, headers: [...action.headers, { name: "", value: "" }] })}>
        {t("addWebhookHeader")}
      </button>
      {surface === "event_automation" ? (
        <>
          <hr className={styles.split} />
          <p className={`${editStyles.fieldName} ${styles.contentName}`}>{t("webhookContent")}</p>
          <div className={styles.content} role="group" aria-label={t("webhookPropertiesForAction", { number })}>
            <label className={styles.check} data-webhook-all>
              <input className={styles.checkbox} type="checkbox" checked={all}
                onChange={(event) => onChange({ ...action, propertyIds: event.target.checked ? [...gone, ...properties.map((property) => property.id)] : gone })} />
              <span className={styles.checkLabel}>{t("webhookAllProperties")}</span>
            </label>
            <hr className={styles.rule} />
            {properties.map((property, propertyIndex) => (
              <label key={property.id} className={styles.check} data-webhook-property={property.id}>
                <input className={styles.checkbox} type="checkbox" checked={action.propertyIds.includes(property.id)} onChange={(event) => choose(property.id, event.target.checked)} />
                <span className={styles.checkLabel}>
                  <span className={styles.checkIcon}><PropertyIcon property={property} size="small" /></span>
                  <span className={styles.checkName}>{propertyLabels[propertyIndex]}</span>
                </span>
              </label>
            ))}
            {gone.map((id) => (
              <label key={id} className={styles.check} data-gone>
                <input className={styles.checkbox} type="checkbox" checked onChange={() => choose(id, false)} />
                <span className={`${styles.checkLabel} ${styles.checkName}`}>{t("automationGoneProperty")}</span>
              </label>
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
}
