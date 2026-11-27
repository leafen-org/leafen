import { SwitchInput } from "@/components/Switch";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { storePersonValue } from "@/lib/browserStorage";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { NotionSelect } from "./NotionSelect";
import { PhonePropertySection, usePhonePropertySettings } from "./PhonePropertySettings";
import phoneStyles from "./PhonePropertySettings.module.css";
import {
  NUMBER_DECIMAL_PLACES,
  NUMBER_DISPLAY_COLORS,
  LAST_CURRENCY_KEY,
  numberDisplayForProperty,
  numberFormatCode,
  numberFormatMatches,
  numberFormatsLastCurrencyFirst,
  type NumberDisplayKind,
  type NumberFormat,
} from "./numberFormat";
import sheet from "./databaseSheet";
import styles from "./NumberPropertyConfig.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const KINDS: readonly NumberDisplayKind[] = ["number", "bar", "ring"];

/**
 * A number property's settings, as Notion's "속성 편집" (seen 2026-09-30): its
 * format and decimal places, then how its values show — a number, a bar or a
 * ring, on three preview cards — and for a bar or a ring its color, the value
 * a full one stands for ("나누기", 100) and whether the number shows beside
 * it. They belong to the property, so every view showing it follows them.
 * The format list has a search, names each currency with its code, "원
 * (KRW)", and marks none, showing the name alone once chosen (seen
 * 2026-10-01). In a phone's "속성 편집" its format and places are rows of
 * the type's group and "표시 옵션" a section under it, as Notion's there
 * (seen 2026-11-11).
 */
export function NumberPropertyConfig({ prop, onConfig, shows = "all" }: {
  prop: DbProperty;
  onConfig(config: PropertyConfig): void;
  /** A count or percentage rollup sets only how its numbers show; a
   * percentage measures its bar and ring out of 1; a formula's list of
   * numbers takes no decimal places. */
  shows?: "all" | "format display" | "display" | "percent display";
}) {
  const { t } = useTranslation(["propertyTypeConfig"]);
  const choicesRef = useRef<HTMLDivElement>(null);
  const display = numberDisplayForProperty({ config: (prop.config ?? {}) as Record<string, unknown> });
  const [divideBy, setDivideBy] = useState(String(display.divideBy));
  useEffect(() => setDivideBy(String(display.divideBy)), [display.divideBy]);
  function update(patch: Partial<PropertyConfig>) {
    const next: Record<string, unknown> = { ...prop.config, ...patch };
    for (const [key, value] of Object.entries(patch)) if (value === undefined) delete next[key];
    onConfig(next as PropertyConfig);
  }
  function keepDivideBy() {
    const value = Number(divideBy.trim());
    if (!Number.isFinite(value) || value <= 0) setDivideBy(String(display.divideBy));
    else if (value !== display.divideBy) update({ numberDivideBy: value === 100 ? undefined : value });
  }
  const formatRows = shows === "all" || shows === "format display";
  const percent = shows === "percent display" || display.format === "percent";
  const phone = usePhonePropertySettings();
  const kinds = (
    <div>
    <NativeChoiceGrid items={KINDS} keyOf={kind => kind} textOf={kind => t(`numberDisplays.${kind}`)}
      checkedKey={display.kind} listRef={choicesRef} autoFocus={false} label={t("numberDisplay")}
      className={phone ? `${styles.kinds} ${phoneStyles.numberKinds}` : styles.kinds}
      itemClassName={phone ? `${styles.kind} ${phoneStyles.numberKind}` : styles.kind} marker={kind => ({ "data-number-display-kind": kind })}
      onAction={kind => update({ numberDisplay: kind === "number" ? undefined : kind })}>
      {kind => <>
          <span className={styles.preview} aria-hidden="true">
            {kind === "number" && <span className={styles.previewNumber}>42</span>}
            {kind === "bar" && <span className={styles.previewBar}><span /></span>}
            {kind === "ring" && (
              <svg width={21} height={21} viewBox="0 0 14 14">
                <path className={styles.previewTrack} d="M 7 0 A 7 7 0 1 1 7 14 A 7 7 0 1 1 7 0 M 7 2 A 5 5 0 1 0 7 12 A 5 5 0 1 0 7 2 Z" />
                <circle className={styles.previewFill} cx={7} cy={7} r={6} fill="none" strokeWidth={2} strokeLinecap="round"
                  strokeDasharray={2 * Math.PI * 6} strokeDashoffset={2 * Math.PI * 6 * 0.58} />
              </svg>
            )}
          </span>
          <span>{t(`numberDisplays.${kind}`)}</span>
      </>}
    </NativeChoiceGrid>
    </div>
  );
  const displayOptions = (
    <>
      {display.kind !== "number" && (
        <>
          <div>
            <NotionSelect
              ariaLabel={t("numberColor")} settingLabel={t("numberColor")} settingCaret="down" settingMenuWidth={150}
              value={display.color}
              options={NUMBER_DISPLAY_COLORS.map((name) => ({
                value: name,
                label: t(`numberColors.${name}`),
                icon: <span className={styles.swatch} style={{ background: `var(--c-accent-${name})` }} />,
              }))}
              onChange={(value) => update({ numberDisplayColor: value === "green" ? undefined : value })}
            />
          </div>
          {!percent && <label className={styles.divideRow}>
            <span>{t("numberDivideBy")}</span>
            <input type="text" inputMode="decimal" value={divideBy} aria-label={t("numberDivideBy")}
              data-number-divide-by
              onChange={(event) => setDivideBy(event.target.value)}
              onBlur={keepDivideBy}
              onKeyDown={(event) => {
                if (imeComposing(event)) return;
                if (fieldEnter(event)) {
                  event.preventDefault();
                  keepDivideBy();
                }
              }} />
          </label>}
          <label className={`${styles.switchRow} ${styles.displaySwitch}`}>
            <span>{t("numberShowValue")}</span>
            <SwitchInput checked={display.showValue}
              onChange={(event) => update({ numberShowValue: event.target.checked ? undefined : false })} />
          </label>
        </>
      )}
      <p className={phone ? `${styles.note} ${phoneStyles.numberNote}` : styles.note}>{t("numberDisplayNote")}</p>
    </>
  );
  return (
    <>
      {formatRows && <>
      <div>
        <NotionSelect
          ariaLabel={t("numberFormat")} settingLabel={t("numberFormat")} settingMenuWidth={192}
          value={display.format}
          options={numberFormatsLastCurrencyFirst().map((format) => {
            const name = t(`numberFormats.${format}`);
            const code = numberFormatCode(format);
            return { value: format, label: code ? t("numberFormatWithCode", { name, code }) : name, triggerLabel: name };
          })}
          search={{
            ariaLabel: t("numberFormatSearch"),
            placeholder: t("numberFormatSearch"),
            match: (option, query) => numberFormatMatches(option.value as NumberFormat, query),
          }}
          showCheck={false}
          onChange={(value) => {
            if (prop.type === "number" && numberFormatCode(value as NumberFormat)) storePersonValue(LAST_CURRENCY_KEY, value);
            update({ numberFormat: value });
          }}
        />
      </div>
      {shows === "all" && <div>
        <NotionSelect
          ariaLabel={t("decimalPlaces")} settingLabel={t("decimalPlaces")}
          value={display.decimals === undefined ? "" : String(display.decimals)}
          options={[
            { value: "", label: t("decimalPlacesDefault") },
            ...NUMBER_DECIMAL_PLACES.map((places) => ({ value: String(places), label: String(places) })),
          ]}
          onChange={(value) => update({ decimalPlaces: value === "" ? undefined : Number(value) })}
        />
      </div>}
      </>}
      {phone ? null : (
        <div className={`${sheet.propertyHeaderField} ${styles.displayField}`}>
          <span className={styles.displayHead}>{t("numberDisplay")}</span>
          {kinds}
        </div>
      )}
      {phone ? (
        <PhonePropertySection plain label={t("numberDisplay")} data={{ "data-number-display-section": "" }}>
          <div className={phoneStyles.numberDisplay}>{kinds}{displayOptions}</div>
        </PhonePropertySection>
      ) : displayOptions}
    </>
  );
}
