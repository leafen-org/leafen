import { type ChangeEvent, useLayoutEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import styles from "../AuthGate.module.css";
import guide from "./SetupOutsideGuide.module.css";
import { SETUP_CODE_KEYBOARD } from "./codeKeyboard";
import { ServerLogPlaces } from "./ServerLogPlaces";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { useSeenSetupStatus } from "@/lib/useSeenSetupStatus";

/**
 * A server not set up yet, opened from the outside internet without its
 * setup link (user decision 2026-11-19): no code field, but what to do in
 * plain words — open it from the server's own network, where it asks no
 * code (internal/app/setup_same_network.go), at the address the server knows
 * there if it knows one; from outside, open the setup link the server left in
 * its log, and where that log is. Typing the code is one of the other ways.
 * Leafen's own screen: Notion has no server to set up.
 */
export function SetupOutsideGuide({ onEnterCode }: { onEnterCode(): void }) {
  const { t } = useTranslation("authGate");
  const home = useSeenSetupStatus()?.homeAddress ?? "";
  return (
    <div className={styles.form} data-setup-outside>
      <div className={styles.notice}>
        <strong>{t("setupOutsideTitle")}</strong>
        <p>{t("setupOutsideBody")}</p>
        {home ? <p data-setup-home-address>{t("setupOutsideHome")} <a href={home}>{home}</a></p> : null}
      </div>
      <section className={guide.away} aria-labelledby="setup-outside-away">
        <h2 id="setup-outside-away" className={guide.head}>{t("setupOutsideAwayTitle")}</h2>
        <p>{t("setupOutsideAwayBody")}</p>
        <ServerLogPlaces />
        <p>{t("setupOutsideAgent")}</p>
      </section>
      <details className={styles.moreHelp} data-setup-outside-other>
        <summary>{t("setupOutsideOther")}</summary>
        <p>{t("setupOutsideCodeNote")}</p>
        <button className={`${styles.outlined} ${guide.codeOpen}`} type="button" onClick={onEnterCode} data-setup-code-open>
          {t("setupOutsideCodeOpen")}
        </button>
      </details>
    </div>
  );
}

/**
 * The setup code's field: under the name where the setup link carried no
 * code — from outside, once the person chose to type it (it then takes the
 * focus), or after a code was refused.
 */
export function SetupCodeField({ value, onChange, disabled, chosen, rejection }: {
  value: string;
  onChange(value: string): void;
  disabled: boolean;
  /** Opened by "설정 코드 넣기": the field the person asked for takes the focus, but on a touch screen (screenKind.ts). */
  chosen: boolean;
  rejection: { "aria-invalid"?: true; "aria-describedby"?: string };
}) {
  const { t } = useTranslation("authGate");
  const field = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    if (chosen && fieldFocusOnOpen()) field.current?.focus();
  }, [chosen]);
  return (
    <>
      <label className={styles.label} htmlFor="setup-code">{t("setupCode")}</label>
      <input
        ref={field}
        id="setup-code"
        className={styles.input}
        type="text"
        autoComplete="one-time-code"
        {...SETUP_CODE_KEYBOARD}
        value={value}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
        disabled={disabled}
        required
        {...rejection}
      />
      <p id="setup-code-help" className={styles.fieldHelp}>{t("setupCodeHelp")}</p>
      {/* The variable is for the one who started the server: folded (2026-11-05). */}
      <details className={styles.moreHelp}>
        <summary>{t("setupCodeNoLink")}</summary>
        <p>{t("setupCodeEnvHint")}</p>
      </details>
    </>
  );
}
