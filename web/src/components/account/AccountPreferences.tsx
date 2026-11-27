import { SwitchMark } from "@/components/Switch";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { AccountLanguagePreference } from "@/authWire.generated";
import { currentLanguagePreference, setLanguagePreference } from "@/i18n";
import { languageChoices } from "@/i18n/languages";
import { createAccountLanguageService } from "@/lib/accountLanguageService";
import { saveDisplayPreferences, useDisplayPreferences, type AccountDisplayPreferences } from "@/lib/displayPreferences";
import { useContrast, type ContrastPref } from "@/lib/contrast";
import { readStartPage, saveStartPage, START_PAGES, type StartPage } from "@/lib/startPage";
import { useTheme, type ThemePref } from "@/lib/theme";
import { DATE_MENTION_FORMATS, dateMentionLabel } from "@/components/editor/dateMentions";
import { deviceTimeZone } from "@/lib/dateReminders";
import { timeZoneSearchRank } from "@/lib/timeZones";
import { useTimeZoneChoices } from "@/lib/useTimeZoneChoices";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";
import { AppSelect } from "@/components/AppSelect";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { userErrorText, writeUncertainMark } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import { FirstRunSkipped } from "./FirstRunSkipped";
import { useSidebarLayout } from "@/components/shell/AppShell";

function todayIsoDate() {
  const today = new Date();
  return [today.getFullYear(), today.getMonth() + 1, today.getDate()].map((part) => String(part).padStart(2, "0")).join("-");
}

const START_PAGE_KEYS: Record<StartPage, string> = {
  last_visited_page: "startPageLastVisited",
  sidebar_top_page: "startPageSidebarTop",
  library: "startPageLibrary",
};

/**
 * The account's "기본 설정", in the order of Notion's (seen 2026-10-02): the
 * theme and contrast this device shows, whether Enter breaks the line in
 * comment boxes, the language the account reads, which reloads the product
 * in it, whether calendars start their weeks on Monday, the page this
 * device opens the workspace with, and
 * under privacy whether the open workspace's pages keep the person's views
 * by name. A choice shows as it is made and goes to the server behind it; a
 * refused one is put back with why by its control (lib/shownChanges.ts).
 */
// Notion sets the keys in the Enter row's description in bold (seen 2026-10-27).
function boldKeys(text: string) {
  return text.split(/(Cmd|Ctrl|Enter)/u).map((part, index) => index % 2 ? <b key={index}>{part}</b> : part);
}

export function AccountPreferences({ session, workspaceId, signIn }: {
  session: Session;
  workspaceId: string | null;
  /** A phone's password and passkey rows, over the theme (AccountSecurityShare.tsx); nothing elsewhere. */
  signIn?: ReactNode;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "blockItem"]);
  const { phone: phoneScreen, touch } = useSidebarLayout(true);
  const [themePref, setThemePref] = useTheme();
  const [contrastPref, setContrastPref] = useContrast();
  const [language, setLanguage] = useState(() => currentLanguagePreference(session.user.id));
  const [error, setError] = useState("");
  // Which part of the page the failure is told in: by the control, not at its end.
  const [failedIn, setFailedIn] = useState<"input" | "languageTime" | "viewHistory">("languageTime");
  const [viewHistory, setViewHistory] = useState<boolean | null>(null);
  // Where the organization shows everyone or no one, it decides, not the person.
  const [viewDisplay, setViewDisplay] = useState<"person" | "shown" | "hidden">("person");
  const pending = useShownChanges<AccountDisplayPreferences & { language: string; viewHistory: boolean }>();
  const saved = useDisplayPreferences();
  const display = { ...saved, ...pending.shown };
  const shownLanguage = pending.shown.language ?? language;
  const shownViewHistory = pending.shown.viewHistory ?? viewHistory;
  // While automatic the zone shown is the device's, as Notion's locked row shows it.
  const shownTimeZone = display.timeZoneAuto ? deviceTimeZone() : display.timeZone || deviceTimeZone();
  // The zone list is made once for what it shows, not at each press (2026-11-13:
  // a switch's press redrew it, 40–50 ms before its paint), and holds the shown
  // zone alone until the frame after the section's (useTimeZoneChoices).
  const zoneChoices = useTimeZoneChoices(shownTimeZone, !display.timeZoneAuto);
  const zoneOptions = useMemo(() => zoneChoices.map(({ zone, place, offset, label }, index) => (
    <option key={zone} value={zone} data-description={offset} data-trigger-label={label}
      data-group={index === 0 ? t("timeZoneCurrent") : t("timeZoneChoose")}>{place}</option>
  )), [zoneChoices, t]);
  const languages = useMemo(() => languageChoices(i18n.language), [i18n.language]);
  const [startPage, setStartPage] = useState<StartPage>(() => readStartPage(session.user.id, workspaceId ?? ""));
  useEffect(() => {
    setStartPage(readStartPage(session.user.id, workspaceId ?? ""));
  }, [session.user.id, workspaceId]);

  useEffect(() => {
    setViewHistory(null);
    if (!workspaceId) return;
    const controller = new AbortController();
    api.getPageAnalyticsViewHistory(session, workspaceId, controller.signal)
      .then((state) => { setViewHistory(state.identifiedHistoryEnabled); setViewDisplay(state.viewDisplay); })
      .catch(() => undefined);
    return () => controller.abort();
  }, [session, workspaceId]);

  function refused(place: typeof failedIn) {
    return (error: unknown) => {
      setFailedIn(place);
      setError(userErrorText(error, t("settingsErrors:requestFailed")));
    };
  }

  function changeViewHistory(next: boolean) {
    if (!workspaceId) return;
    setError("");
    void pending.change({ viewHistory: next }, async () => {
      await api.updatePageAnalyticsViewHistory(session, workspaceId, next);
      setViewHistory(next);
    }, "viewHistory").catch(refused("viewHistory"));
  }

  function changeDisplay(change: Partial<AccountDisplayPreferences>) {
    setError("");
    void pending.change(change, () => saveDisplayPreferences(session, change), "display")
      .catch(refused("enterAddsNewLine" in change ? "input" : "languageTime"));
  }

  function changeNumberFormat(numberFormat: AccountDisplayPreferences["numberFormat"]) {
    setError("");
    void pending.change({ numberFormat }, () => saveDisplayPreferences(session, { numberFormat }), "display")
      // Numbers everywhere show it, so the app starts again with it, as Notion's does.
      .then(() => window.location.reload(), refused("languageTime"));
  }

  async function changeLanguage(next: string) {
    const name = next === "system" ? t("languageSystem") : languages.find((option) => option.value === next)?.label ?? next;
    // Asks first, as Notion's does: the whole app starts again in it. Its
    // Korean writes Notion's "…로" after a language's name; the system
    // option, Leafen's own, has a line of its own ("시스템 설정으로").
    const confirmed = await confirmAction({
      message: t("languageChangeConfirm", { language: name, context: next === "system" ? "system" : undefined }),
      confirmLabel: t("languageChangeAction"),
      cancelLabel: t("common:actions.cancel"),
      danger: true,
    });
    if (!confirmed) return;
    setError("");
    void pending.change({ language: next }, async () => {
      await createAccountLanguageService(session).save(next as AccountLanguagePreference);
      setLanguage(next);
      setLanguagePreference(next, session.user.id);
    }, "language").catch(refused("languageTime"));
  }

  // A phone lays the start page under the theme and the privacy last, under its own
  // heading, as Notion's in iPhone Safari (seen 2026-11-14); the other groups' headings
  // go there (AccountSettingsPhone.module.css).
  // Its heading is the desktop's "데스크톱 앱", a touch screen's "시작", as Notion's iPad (seen 2026-11-13).
  // A plain browser shows it too, as Notion's web does (seen 2026-10-02 and 2026-11-19 in Chrome): the start
  // page is what a tab opened at the server's address opens, not the desktop app's alone.
  const startPageHeading = t(touch ? "startSection" : "desktopAppSection");
  const startPageSection = workspaceId ? (
    <section className={styles.section} role="region" aria-label={startPageHeading}>
      <div className={styles.sectionTitle}>{startPageHeading}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("startPageField")}</strong>
          <span>{t("startPageDescription")}</span>
        </div>
        <AppSelect look="setting"
          aria-label={t("startPageField")}
          value={startPage}
          onChange={(event) => {
            const next = event.target.value as StartPage;
            saveStartPage(session.user.id, workspaceId, next);
            setStartPage(next);
          }}
        >
          {START_PAGES.map((value) => <option key={value} value={value}>{t(START_PAGE_KEYS[value])}</option>)}
        </AppSelect>
      </div>
    </section>
  ) : null;
  const privacySection = workspaceId ? (
    <section className={styles.section} role="region" aria-label={t("privacySection")} data-preferences-group="privacy">
      <div className={styles.sectionTitle}>{t("privacySection")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("viewHistoryField")}</strong>
          <span>{viewDisplay === "person" ? t("viewHistoryDescription") : t(`viewHistoryOrganization.${viewDisplay}`)}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={shownViewHistory === true}
          aria-label={t("viewHistoryField")}
          disabled={viewHistory === null || viewDisplay !== "person"}
          data-waiting-note={pending.note("viewHistory")}
          data-view-display={viewDisplay}
          onClick={() => changeViewHistory(!shownViewHistory)}
        >
          <SwitchMark on={shownViewHistory} />
        </button>
      </div>
      {error && failedIn === "viewHistory" ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
    </section>
  ) : null;

  return (
    <div data-account-preferences>
      {signIn}
      <section className={styles.section} role="region" aria-label={t("themeField")}>
        <div className={styles.sectionTitle}>{t("themeField")}</div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("themeField")}</strong>
            <span>{t("themeDescription")}</span>
          </div>
          <AppSelect look="setting"
            aria-label={t("themeField")}
            value={themePref}
            onChange={(event) => setThemePref(event.target.value as ThemePref)}
          >
            <option value="system">{t("themeSystemOption")}</option>
            <option value="light">{t("themeLightOption")}</option>
            <option value="dark">{t("themeDarkOption")}</option>
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("contrastField")}</strong>
            <span>{t("contrastDescription")}</span>
          </div>
          <AppSelect look="setting"
            aria-label={t("contrastField")}
            value={contrastPref}
            onChange={(event) => setContrastPref(event.target.value as ContrastPref)}
          >
            <option value="system">{t("contrastSystemOption")}</option>
            <option value="standard">{t("contrastStandardOption")}</option>
            <option value="high">{t("contrastHighOption")}</option>
          </AppSelect>
        </div>
      </section>
      {phoneScreen ? startPageSection : null}
      <section className={styles.section} role="region" aria-label={t("inputOptionsSection")}>
        <div className={styles.sectionTitle}>{t("inputOptionsSection")}</div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterNewLineField")}</strong>
            <span>{boldKeys(t("enterNewLineDescription"))}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={display.enterAddsNewLine}
            aria-label={t("enterNewLineField")}
            data-waiting-note={pending.note("enterAddsNewLine")}
            onClick={() => changeDisplay({ enterAddsNewLine: !display.enterAddsNewLine })}
          >
            <SwitchMark on={display.enterAddsNewLine} />
          </button>
        </div>
        {error && failedIn === "input" ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
      </section>
      <section className={styles.section} role="region" aria-label={t("languageAndTimeSection")}>
        <div className={styles.sectionTitle}>{t("languageAndTimeSection")}</div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("languageField")}</strong>
            <span>{t("languageDescription")}</span>
          </div>
          {/* Notion's language list: 240px from the select's left edge, a
              scrollbar that takes room inside it (NotionSelect's settings
              list), past the settings window's right one where it runs out
              (measured 2026-11-19, 1440 window: 1163.5–1403.5 beside a window
              ending at 1368), right on the button's edge (2026-11-14: 178..684
              under 150..178). */}
          <AppSelect look="setting" menuWidth={240} menuAlign="start" menuGap={0} unmarked
            aria-label={t("languageField")}
            value={shownLanguage}
            data-waiting-note={pending.note("language")}
            onChange={(event) => void changeLanguage(event.target.value)}
          >
            {languages.map((option) => (
              <option key={option.value} value={option.value} data-description={option.name}>{option.label}</option>
            ))}
            <option value="system">{t("languageSystem")}</option>
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("numberFormatField")}</strong>
            <span>{t("numberFormatDescription")}</span>
          </div>
          <AppSelect look="setting"
            aria-label={t("numberFormatField")}
            value={display.numberFormat}
            data-waiting-note={pending.note("numberFormat")}
            data-number-format-default
            onChange={(event) => changeNumberFormat(event.target.value as AccountDisplayPreferences["numberFormat"])}
          >
            <option value="default">{t("numberFormatDefault")}</option>
            <option value="comma_period">1,000,000.00</option>
            <option value="period_comma">1.000.000,00</option>
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("textDirectionField")}</strong>
            <span>{t("textDirectionDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={display.alwaysShowTextDirection}
            aria-label={t("textDirectionField")}
            data-waiting-note={pending.note("alwaysShowTextDirection")}
            data-always-show-text-direction
            onClick={() => changeDisplay({ alwaysShowTextDirection: !display.alwaysShowTextDirection })}
          >
            <SwitchMark on={display.alwaysShowTextDirection} />
          </button>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("weekStartMondayField")}</strong>
            <span>{t("weekStartMondayDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={display.weekStartsOnMonday}
            aria-label={t("weekStartMondayField")}
            data-waiting-note={pending.note("weekStartsOnMonday")}
            onClick={() => changeDisplay({ weekStartsOnMonday: !display.weekStartsOnMonday })}
          >
            <SwitchMark on={display.weekStartsOnMonday} />
          </button>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("dateMentionFormatField")}</strong>
            <span>{t("dateMentionFormatDescription")}</span>
          </div>
          {/* The closed choice names the format at the row's end, as the other rows' do (2026-10-27: it filled
              its row); the list shows each one's example under its name, as Notion's (seen 2026-10-17). */}
          <AppSelect look="setting" data-date-mention-format-default aria-label={t("dateMentionFormatField")}
            value={display.dateMentionFormat} data-waiting-note={pending.note("dateMentionFormat")}
            onChange={(event) => changeDisplay({ dateMentionFormat: event.target.value as typeof display.dateMentionFormat })}>
            {DATE_MENTION_FORMATS.map((format) => (
              <option key={format} value={format} data-description={`@${dateMentionLabel(todayIsoDate(), undefined, format)}`}>
                {t(`blockItem:dateMention.formats.${format}`)}
              </option>
            ))}
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("timeZoneAutoField")}</strong>
            <span>{t("timeZoneAutoDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={display.timeZoneAuto}
            aria-label={t("timeZoneAutoField")}
            data-waiting-note={pending.note("timeZoneAuto")}
            data-time-zone-auto
            onClick={() => changeDisplay(display.timeZoneAuto
              ? { timeZoneAuto: false, timeZone: shownTimeZone }
              : { timeZoneAuto: true, timeZone: deviceTimeZone() })}
          >
            <SwitchMark on={display.timeZoneAuto} />
          </button>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("timeZoneField")}</strong>
            <span>{t("timeZoneDescription")}</span>
          </div>
          <AppSelect look="setting"
            aria-label={t("timeZoneField")}
            value={shownTimeZone}
            disabled={display.timeZoneAuto}
            data-waiting-note={pending.note("timeZone")}
            data-time-zone
            onChange={(event) => changeDisplay({ timeZone: event.target.value })}
            menuWidth={342}
            menuAlign="start"
            // Notion's time zone list is at most 128px short of the window
            // and right on its button's edge, so low in a 723 window it turns
            // over it whole (seen 2026-11-14: 595 high, 12..607 over a button
            // at 607..635, moved up to 116..711 over one at 400).
            menuHeight={typeof window === "undefined" ? undefined : window.innerHeight - 128}
            menuGap={0}
            // A search nothing answers keeps the zones' head over "결과 없음", as the date window's (DateZoneChoices).
            search={{ ariaLabel: t("timeZoneSearch"), placeholder: t("timeZoneSearch"), noResultsLabel: t("timeZoneNone"), noResultsGroup: t("timeZoneChoose"),
              rank: (option, query) => timeZoneSearchRank({ offset: option.description ?? "", place: option.label, zone: option.value }, query) }}
          >
            {zoneOptions}
          </AppSelect>
        </div>
        {error && failedIn === "languageTime" ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
      </section>
      {phoneScreen ? null : startPageSection}
      {phoneScreen ? null : privacySection}
      {/* After the rows, a section of its own as they are (FirstRunSkipped). */}
      <FirstRunSkipped session={session} />
      {phoneScreen ? privacySection : null}
    </div>
  );
}
