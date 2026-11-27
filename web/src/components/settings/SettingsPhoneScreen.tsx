import { useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft } from "@/icons/leafen";
import { PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheet } from "@/components/shell/PhoneSheet";
import { SettingsSectionIcon, useSettingsLabels } from "./settingsWindowLabels";
import type { AccountSettingsSection } from "@/components/account/AccountSettingsPage";
import { AccountPhoneDangerZone } from "@/components/account/AccountSecurityShare";
import type { Session } from "@/types";
import type { SettingsGroup, SettingsSection } from "./settingsWindowModel";
import phoneStyles from "./SettingsPhone.module.css";
import styles from "./SettingsWindow.module.css";

/**
 * A phone's settings, as Notion's in iPhone Safari (seen 2026-11-14, 402pt):
 * a whole screen under a 44px head — "‹" at its left, which leaves it, and
 * "설정" on the head's top line in its middle, over a hairline — and under it
 * one long screen of the person's own settings, a section after another,
 * each under its 20px title, "알림" first, the password and passkeys at the
 * top of "기본 설정". Leafen's account and its servers follow them, then
 * "위험 구역" with the account's deletion, which ends Notion's; the
 * workspace's, the features', the administration's and the server's
 * sections, which Notion's phone doesn't hold, are rows at its end, each
 * opening a screen of its own whose "‹" comes back to them.
 */
export const PHONE_SETTINGS_SECTIONS: readonly AccountSettingsSection[] = ["notifications", "preferences", "account", "servers"];

/**
 * The phone settings' head; a screen that stands alone has "완료" at its
 * end in place of "‹", as Notion's phone "멤버" (seen 2026-11-15 at 402).
 */
export function SettingsPhoneHead({ title, titleId, onBack, onDone }: { title: string; titleId: string; onBack?(): void; onDone?(): void }) {
  const { t } = useTranslation("common");
  return (
    <div className={`${sheet.head} ${phoneStyles.phoneHead}`} data-settings-phone-head data-done={onDone ? "true" : undefined}>
      <span className={sheet.headSide}>
        {onBack ? <button type="button" className={`${sheet.headButton} ${sheet.headBack}`} aria-label={t("actions.back")} data-settings-phone-back onClick={onBack}>
          <ChevronLeft size="regular" aria-hidden="true" />
        </button> : null}
      </span>
      <strong id={titleId} className={sheet.title}>{title}</strong>
      <span className={`${sheet.headSide} ${sheet.headEnd}`}>
        {onDone ? <button type="button" className={sheet.headButton} data-settings-phone-done onClick={onDone}>{t("actions.done")}</button> : null}
      </span>
    </div>
  );
}

/**
 * Where the focus goes as the phone's settings put one screen in the place
 * of another (a row of the long screen opened, the email page): the row or
 * button pressed went with the screen before, and its place is the head of
 * the new one, whose "‹" leads back to it — so the focus goes to the head's
 * first button, as a computer's list keeps it on the item pressed. Left to
 * the window's own rule (keepModalFocus), it went to the screen's first
 * control that raises no keyboard, partway down (2026-11-16: 일반's icon
 * button), and before that to its first field, raising the keyboard over
 * the screen. A tap in iOS Safari gives the row no focus: the window keeps
 * it, and it stays there. Called on every screen; `screen` is null off a phone.
 */
export function useSettingsPhoneScreenFocus(panelRef: RefObject<HTMLElement | null>, screen: string | null) {
  const shown = useRef(screen);
  useLayoutEffect(() => {
    if (shown.current === screen) return;
    shown.current = screen;
    const active = document.activeElement;
    if (screen === null || (active && active !== document.body && active !== document.documentElement)) return;
    panelRef.current?.parentElement?.querySelector<HTMLElement>("[data-settings-phone-head] button")?.focus({ preventScroll: true });
  }, [panelRef, screen]);
}

/** One of the long screen's sections under its 20px title. */
export function SettingsPhoneSection({ part, title, children }: { part: string; title: string; children: ReactNode }) {
  return (
    <section className={phoneStyles.phoneSection} data-settings-part={part} aria-label={title}>
      <h2 className={phoneStyles.phoneSectionTitle}>{title}</h2>
      {children}
    </section>
  );
}

/** "위험 구역" after the person's own sections, as Notion's last (seen 2026-11-14): deleting the account. */
export function SettingsPhoneDangerZone({ session, onDeleted }: { session: Session; onDeleted: () => void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <SettingsPhoneSection part="section-danger" title={t("dangerZoneTitle")}>
      <AccountPhoneDangerZone session={session} onDeleted={onDeleted} />
    </SettingsPhoneSection>
  );
}

/** The sections past the person's own, as rows under their groups' names. */
export function SettingsPhoneMore({ groups, newVersion, onOpen }: {
  groups: SettingsGroup[];
  /** The server has a newer version: its updates' row is marked, as the list's item is. */
  newVersion: boolean;
  onOpen(section: SettingsSection): void;
}) {
  const labels = useSettingsLabels();
  if (groups.length === 0) return null;
  return (
    <div className={phoneStyles.phoneMore} data-settings-part="phone-more">
      {groups.map((group) => (
        <PhoneSheetGroup key={group.id} label={labels.group(group.id)}>
          {group.sections.map((item) => (
            <PhoneSheetRow key={item} icon={<SettingsSectionIcon section={item} />} label={labels.section(item)}
              value={item === "server-updates" && newVersion
                ? <span className={styles.updateMarker} data-update-available aria-label={labels.newVersion} /> : undefined}
              data={{ "data-settings-section": item }} onPress={() => onOpen(item)} />
          ))}
        </PhoneSheetGroup>
      ))}
    </div>
  );
}
