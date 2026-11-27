import { useLayoutEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AccountLanguagePreference } from "@/authWire.generated";
import { LANGUAGE_OPTIONS } from "@/i18n/languages";
import { i18next, loadLanguageWords, resolvedLanguageForPreference, signedOutLanguage, switchLanguage } from "@/i18n";
import { keepFirstRunLanguage } from "@/lib/firstRunLanguage";
import { coverDocumentChrome } from "@/lib/documentChrome";
import { isRightToLeftLanguage } from "@/lib/textDirection";
import { AppSelect } from "@/components/AppSelect";
import styles from "../AuthGate.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * The language step: "언어", what it is for, the languages with the one
 * recommended (this device's) first and chosen to start, "시스템 설정", and
 * "계속". It is the first screen of the app's first install and of a
 * server's first setup, before any account (the multi-server decision of
 * 2026-10-19, 1), and the step an account without a language meets after
 * signing in (AuthenticatedAccountGates). It reads in the language being
 * chosen, at once, as soon as its words are here.
 */
export function LanguageStep({ value, recommended, busy, error, onChange, onSubmit, testId }: {
  value: AccountLanguagePreference;
  /** Shown first: the device's language, or the one chosen before the account. */
  recommended: string;
  busy: boolean;
  error: string;
  onChange(next: AccountLanguagePreference): void;
  onSubmit(): void;
  testId: string;
}) {
  const { t } = useTranslation(["authGate", "workspaceSettingsDialog"]);
  const [stepLanguage, setStepLanguage] = useState("");
  const stepT = useMemo(() => (stepLanguage ? i18next.getFixedT(stepLanguage) : t), [stepLanguage, t]);
  // The step names the window and its tab after its title, as the first
  // run's other steps do (coverDocumentChrome).
  const languageName = stepT("workspaceSettingsDialog:languageField");
  useLayoutEffect(() => coverDocumentChrome(languageName), [languageName]);
  const options = useMemo(() => {
    const first = LANGUAGE_OPTIONS.find((option) => option.value === recommended);
    const rest = LANGUAGE_OPTIONS.filter((option) => option.value !== recommended);
    return first ? [first, ...rest] : rest;
  }, [recommended]);

  function choose(next: AccountLanguagePreference) {
    onChange(next);
    const resolved = resolvedLanguageForPreference(next);
    void loadLanguageWords(resolved).then(() => setStepLanguage(resolved));
  }

  return (
    // Its frame reads the way the language being chosen does, as its words
    // already do (2026-11-09: Arabic words sat in a left-to-right frame).
    <main className={styles.screen} data-testid={testId} lang={stepLanguage || undefined}
      dir={stepLanguage ? (isRightToLeftLanguage(stepLanguage) ? "rtl" : "ltr") : undefined}>
      <section className={styles.panel} aria-busy={busy}>
        <LeafenBrand />
        {/* The step takes the focus itself, so no control shows a focus ring it didn't earn; Enter goes on,
            spent here: the next screen's field or button, focused as it opens, doesn't take it too. */}
        <form ref={focusOnce} tabIndex={-1} className={`${styles.form} ${styles.quietFocus}`}
          onSubmit={(event) => { event.preventDefault(); onSubmit(); }}
          onKeyDown={(event) => {
            if (!fieldEnter(event) || event.target !== event.currentTarget) return;
            event.preventDefault();
            event.currentTarget.requestSubmit();
          }}>
          <div className={styles.notice}>
            <strong>{stepT("workspaceSettingsDialog:languageField")}</strong>
            <p>{stepT("workspaceSettingsDialog:languageDescription")}</p>
          </div>
          <AppSelect aria-label={stepT("workspaceSettingsDialog:languageField")} className={styles.choice} data-account-language value={value}
            onChange={(event) => choose(event.currentTarget.value as AccountLanguagePreference)} disabled={busy}>
            {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            <option value="system">{stepT("workspaceSettingsDialog:languageSystem")}</option>
          </AppSelect>
          <button className={styles.primary} type="submit" disabled={busy}>{stepT("authGate:continue")}</button>
          {error ? <p className={styles.error} role="alert" aria-live="assertive">{error}</p> : null}
        </form>
      </section>
    </main>
  );
}

/**
 * The first screen of the app's first install and of a server's first setup,
 * before any account (user 2026-11-19 "맨처음에 언어 선택 먼저"): "계속" moves the
 * page to the language and keeps it for the screens before the account,
 * which takes it (lib/firstRunLanguage.ts). `onChosen` hears the language the
 * page read in before and the one it reads in now.
 */
export function FirstRunLanguageStep({ onChosen }: { onChosen(before: string, after: string): void }) {
  const [recommended] = useState(signedOutLanguage);
  const [value, setValue] = useState(() => recommended as AccountLanguagePreference);
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    const before = i18next.language;
    const after = resolvedLanguageForPreference(value);
    keepFirstRunLanguage(value);
    // Its words not here, the page goes on in the language it has.
    await switchLanguage(after).catch(() => undefined);
    onChosen(before, after);
  }
  return (
    <LanguageStep value={value} recommended={recommended} busy={busy} error="" testId="first-run-language"
      onChange={setValue} onSubmit={() => void submit()} />
  );
}

/** Outside the component so it runs as the form appears, not on every render, which would take the focus back. */
function focusOnce(form: HTMLFormElement | null) {
  form?.focus();
}

/** The full-window steps' head: the mark and "Leafen". */
export function LeafenBrand() {
  return (
    <div className={styles.brand}>
      <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
      <div><h1>Leafen</h1></div>
    </div>
  );
}
