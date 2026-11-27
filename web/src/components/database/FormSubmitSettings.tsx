import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SwitchMark } from "@/components/Switch";
import { ChevronRight, PaintBrushIcon, TitlePropertyIcon } from "@/icons/leafen";
import { useFormSettings } from "@/lib/formSettingsBridge";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { FormButtonColor, FormViewSubmitConfig } from "@/lib/types";
import styles from "./FormSubmitSettings.module.css";

// The colors as Notion's swatches run (seen 2026-11-04), the ones a form's button takes.
// Notion's ten, five to a row (seen 2026-11-04).
const COLORS: readonly FormButtonColor[] = ["blue", "gray", "brown", "yellow", "orange", "green", "azure", "purple", "pink", "red"];

/**
 * "폼 설정" › "제출 화면", as Notion's (seen 2026-11-04): the submit button's
 * color and text, the thanks' title and body, and which of its buttons show.
 */
export function FormSubmitSettings({ viewId, locked }: { viewId: string; locked: boolean }) {
  const { t } = useTranslation(["formView", "blockHandle"]);
  const source = useFormSettings(viewId);
  const [colorsOpen, setColorsOpen] = useState(false);
  const colorRow = useRef<HTMLButtonElement>(null), swatches = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(swatches, colorsOpen ? (size, viewport) => placeAround(
    popoverRectOf(colorRow.current)!, size, viewport, { gap: 4, align: "end" }) : null,
  [colorsOpen], undefined, { kind: "picker", restoreFocus: false, onEscape: () => setColorsOpen(false), onOutsidePress: () => setColorsOpen(false), inside: () => [colorRow.current] });
  if (!source) return <p className={styles.note}>{t("formView:openFormToEdit")}</p>;
  const submit = source.form.submit;
  const off = locked;
  const change = (patch: Partial<FormViewSubmitConfig>) => source.change((form) => ({ ...form, submit: { ...form.submit, ...patch } }));
  const colorName = (color: FormButtonColor) => color === "blue" ? t("formView:buttonColorDefault") : t(`formView:buttonColors.${color}`);
  const toggle = (label: string, key: "showViewResponse" | "showResponseCopy" | "showSubmitAnother") => (
    <button type="button" role="switch" aria-checked={submit[key] !== false} className={styles.toggle} disabled={off}
      data-form-submit-toggle={key} onClick={() => change({ [key]: submit[key] === false })}>
      <span>{label}</span><SwitchMark on={submit[key] !== false} />
    </button>
  );
  return (
    <div className={styles.screen} data-form-submit-settings>
      <div className={styles.section}>{t("formView:submitButton")}</div>
      <button ref={colorRow} type="button" className={styles.row} disabled={off} aria-expanded={colorsOpen} data-form-submit-color
        onClick={() => setColorsOpen((open) => !open)}>
        <PaintBrushIcon size="regular" aria-hidden="true" /><span className={styles.rowLabel}>{t("formView:buttonColorRow")}</span>
        <span className={styles.rowMeta}><span className={styles.dot} data-color={submit.buttonColor} aria-hidden="true" />{colorName(submit.buttonColor)}</span>
        <ChevronRight size="small" aria-hidden="true" />
      </button>
      {colorsOpen ? (
        <div ref={swatches} className={styles.swatches} role="radiogroup" aria-label={t("formView:buttonColorRow")} style={popoverPlacementStyle(place)}>
          {COLORS.map((color) => (
            <button key={color} type="button" role="radio" aria-checked={submit.buttonColor === color} className={styles.swatch}
              aria-label={t("formView:setButtonColor", { color: colorName(color) })} data-color={color}
              onClick={() => { change({ buttonColor: color }); setColorsOpen(false); }} />
          ))}
        </div>
      ) : null}
      {/* "Aa 텍스트", empty on a new form with the default word as its hint (Notion's, seen 2026-11-08). */}
      <label className={styles.row}>
        <TitlePropertyIcon size="regular" aria-hidden="true" /><span className={styles.rowLabel}>{t("formView:buttonTextRow")}</span>
        <input className={styles.input} value={submit.buttonLabel} placeholder={t("formView:defaults.submit")} disabled={off} maxLength={512}
          onChange={(event) => change({ buttonLabel: event.target.value })} />
      </label>
      <div className={styles.section}>{t("formView:confirmationTitleRow")}</div>
      <input className={styles.field} value={submit.confirmationTitle} disabled={off} aria-label={t("formView:confirmationTitleRow")}
        maxLength={512} onChange={(event) => change({ confirmationTitle: event.target.value })} />
      <div className={styles.section}>{t("formView:confirmationBodyRow")}</div>
      <textarea className={styles.field} rows={3} value={submit.confirmationBody} disabled={off} aria-label={t("formView:confirmationBodyRow")}
        maxLength={2048} onChange={(event) => change({ confirmationBody: event.target.value })} />
      <div className={styles.section}>{t("formView:moreButtons")}</div>
      {toggle(t("formView:viewResponse"), "showViewResponse")}
      {toggle(t("formView:responseCopy"), "showResponseCopy")}
      {toggle(t("formView:submitAnother"), "showSubmitAnother")}
    </div>
  );
}
