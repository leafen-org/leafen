import { SwitchMark } from "@/components/Switch";
import { Fragment, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  COMMAND_SEARCH_KEY_TAKEN, commandSearchShortcutText, desktopBackground, desktopBackgroundAvailable,
  type DesktopBackgroundSettings, type DesktopBackgroundState,
} from "@/lib/desktopBackground";
import { hasDesktopMenuBar, packagedWindows } from "@/lib/desktopMenuCommands";
import { CommandSearchShortcutField } from "./CommandSearchShortcutField";
import { userErrorText } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import rows from "@/components/settings/SettingsItemRows.module.css";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { ListNotice } from "@/components/settings/SettingsRowNotice";
import { styles } from "./accountSettingsStyles";

// The desktop app's "This computer" settings, beside its server accounts:
// keeping on behind its last window once it is closed (Notion's app's
// "백그라운드에서 계속 실행", its Windows tray's 시작 설정), opening at login,
// keeping the computer awake while it serves (allowed at install), and the
// command search, as Notion's app's "명령어 검색 사용", its key ("명령어 검색
// 단축키") and on a Mac "메뉴 모음에 Notion 표시". Only in the desktop app. A
// switch turns as it is pressed and the app is told behind it; one it
// refuses is put back with why (lib/shownChanges.ts) — a key the system gave
// another app, under the key's field.

type Switch = Exclude<keyof DesktopBackgroundSettings, "commandSearchShortcut">;
const SWITCHES: Array<{ key: Switch; field: string; help: string }> = [
  { key: "keepRunning", field: "keepRunning", help: "keepRunningHelp" },
  { key: "openAtLogin", field: "openAtLogin", help: "openAtLoginHelp" },
  { key: "keepAwake", field: "keepAwake", help: "keepAwakeHelp" },
  { key: "awakeOnBattery", field: "awakeOnBattery", help: "awakeOnBatteryHelp" },
  { key: "commandSearch", field: "commandSearch", help: "commandSearchHelp" },
  { key: "menuBarIcon", field: "menuBarIcon", help: "menuBarIconHelp" },
];

/** `initial` is the state read with the page's other parts (ThisComputerSettings). */
export function DesktopBackground({ initial }: { initial?: DesktopBackgroundState | null } = {}) {
  const { t } = useTranslation("desktopBackground");
  const shown = desktopBackgroundAvailable();
  const [state, setState] = useState<DesktopBackgroundState | null>(initial ?? null);
  // The app's last answer, which the next change is sent over.
  const answered = useRef(state);
  answered.current = state;
  // The app answers from this computer, not the server: no server's words after ten seconds.
  const pending = useShownChanges<DesktopBackgroundSettings>("device");
  const [error, setError] = useState("");
  const [refusedKey, setRefusedKey] = useState("");
  const mac = hasDesktopMenuBar();
  const commandSearchHere = mac || packagedWindows();

  useEffect(() => {
    if (!shown || initial !== undefined) return;
    void desktopBackground.state().then(setState, () => setState(null));
  }, [initial, shown]);

  if (!shown || !state) return null;
  const now = { ...state, ...pending.shown };
  const apply = (next: Partial<DesktopBackgroundSettings>) => {
    setError("");
    setRefusedKey("");
    void pending.change(next, async () => {
      const { keepRunning, openAtLogin, keepAwake, awakeOnBattery, commandSearch, commandSearchShortcut, menuBarIcon } = answered.current ?? state;
      const answer = await desktopBackground.set({ keepRunning, openAtLogin, keepAwake, awakeOnBattery, commandSearch,
        commandSearchShortcut, menuBarIcon, ...next });
      answered.current = answer;
      setState(answer);
    }).catch((reason: unknown) => {
      if (String((reason as { message?: unknown } | null)?.message ?? reason).includes(COMMAND_SEARCH_KEY_TAKEN)) {
        setRefusedKey(next.commandSearchShortcut ?? commandSearchShortcutText(now, mac));
        return;
      }
      setError(userErrorText(reason, t("failed")));
    });
  };
  const shownRow = (key: Switch) => (key !== "awakeOnBattery" || now.keepAwake)
    && (key !== "commandSearch" || commandSearchHere) && (key !== "menuBarIcon" || (mac && now.commandSearch));
  // The key's row, under "명령어 검색 사용" while it is on (before "메뉴 모음에 리펜 표시" on a Mac).
  const shortcutRow = commandSearchHere && now.commandSearch ? (
    <div className={settingsStyles.securityRow} data-desktop-background-shortcut>
      <div className={settingsStyles.securityInstruction}>
        <strong>{t("commandSearchShortcut")}</strong>
        <span>{t("commandSearchShortcutHelp")}</span>
      </div>
      <CommandSearchShortcutField value={commandSearchShortcutText(now, mac)} mac={mac} refused={refusedKey}
        onChange={(text) => apply({ commandSearchShortcut: text })} />
    </div>
  ) : null;
  // Serving nothing, the line says closing the window ends the app only when it isn't kept in the background.
  const status = state.serving
    ? [t("statusServing"), state.awake ? t("statusAwake") : null].filter(Boolean).join(" · ")
    : t(now.keepRunning ? "statusNothingServed" : "statusIdle");
  return (
    <section className={styles.section} role="region" aria-label={t("title")} data-desktop-background data-settings-part="computer">
      <div className={styles.sectionHeader}>
        <div>
          <div className={styles.sectionTitle}>{t("title")}</div>
          <div className={styles.organizationMeta} data-desktop-background-status>{status}</div>
        </div>
      </div>
      {error ? <ListNotice role="alert">{error}</ListNotice> : null}
      <div className={rows.rowStack}>
        {SWITCHES.filter((item) => shownRow(item.key)).map((item) => (<Fragment key={item.key}>
          {item.key === "menuBarIcon" ? shortcutRow : null}
          <div className={settingsStyles.securityRow}>
            <div className={settingsStyles.securityInstruction}>
              <strong>{t(item.field)}</strong>
              <span>{t(item.help)}</span>
              {item.key === "openAtLogin" && state.openAtLogin && state.loginItem === "needs-approval" ? (
                <span className={settingsStyles.noticeText}>{t(/Windows/u.test(navigator.userAgent) ? "openAtLoginApprovalWindows" : "openAtLoginApproval")}</span>
              ) : null}
            </div>
            <button type="button" role="switch" aria-checked={now[item.key]} aria-label={t(item.field)}
              data-waiting-note={pending.note(item.key)} data-desktop-background-switch={item.key}
              onClick={() => apply({ [item.key]: !now[item.key] })}>
              <SwitchMark on={now[item.key]} />
            </button>
          </div>
          {item.key === "commandSearch" && !mac ? shortcutRow : null}
        </Fragment>))}
      </div>
    </section>
  );
}
