import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { NotionSelect } from "@/components/database/NotionSelect";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { switchLanguage } from "@/i18n";
import { LANGUAGE_OPTIONS, languageChoices } from "@/i18n/languages";
import { GlobeIcon } from "@/icons/leafen";
import { keepFirstRunLanguage } from "@/lib/firstRunLanguage";
import type { AuthGateStep } from "./AuthGateScreen";
import styles from "./SignInLanguage.module.css";

// The steps of the sign-in screen proper, as Notion's login page keeps its
// language at the foot through its own (its email and SSO steps, seen
// 2026-11-19). Sign-up has none, as Notion's sign-up page; a first setup asks
// its language first (LanguageStep.tsx); an invitation's and a mailed link's
// screens are not the sign-in screen.
const SIGN_IN_STEPS = new Set<AuthGateStep>(["signin", "email", "device-code", "agent", "company", "mfa", "password-reset-request"]);

/**
 * The sign-in screen's language, at its foot, as Notion's signed-out login
 * page (seen 2026-11-19 in a new browser profile, 1440 and 390 wide, 1024,
 * 900 and 600 high): "🌐 언어: 〈언어〉 ⌄" centred, chosen ahead from this
 * device's language. Its list is Notion's language list, the settings' own
 * (each language in its own words over its name in the screen's language,
 * unmarked, 240 wide whatever the scrollbar), standing over it with its foot
 * on its top, up to 70% of the window high. A choice asks first whether to
 * change the language, as Notion's does, then the screen reads in it at once and keeps it
 * with the first screen's choice (lib/firstRunLanguage.ts): this browser's
 * next sign-in screens read in it, and the language step after signing in
 * offers it first. Notion's login page on a touch screen has none (seen on
 * an iPhone's twice, an iPad's and a touch screen's), nor does this.
 *
 * At a page's address (PageLoginWall.tsx) it stands under the card, its row
 * 44px from the card and the window's foot, as the row Notion's wall keeps
 * there ("Notion 소개 · 🌐 언어: 한국어 ⌄ · 지원팀에 메시지 보내기", the
 * language alone here).
 */
export function SignInLanguage({ step, inWindow, signedIn, wall }: {
  step: AuthGateStep;
  inWindow?: boolean;
  signedIn?: boolean;
  wall: boolean;
}) {
  const { t, i18n } = useTranslation(["authGate", "workspaceSettingsDialog", "common"]);
  const touch = useSidebarLayout(true).touch;
  // A choice reads in the control at once; the screen follows as its words come.
  const [choosing, setChoosing] = useState("");
  const options = useMemo(() => languageChoices(i18n.language).map((option) => ({
    value: option.value, label: option.label, description: option.name,
    triggerLabel: t("authGate:languageChoice", { language: option.label }),
  })), [i18n.language, t]);
  if (touch || inWindow || signedIn || !SIGN_IN_STEPS.has(step)) return null;
  const value = choosing || i18n.language;
  const shown = options.find((option) => option.value === value)?.triggerLabel
    ?? t("authGate:languageChoice", { language: LANGUAGE_OPTIONS.find((option) => option.value === value)?.label ?? value });

  async function choose(next: string) {
    if (next === value) return;
    const name = LANGUAGE_OPTIONS.find((option) => option.value === next)?.label ?? next;
    const confirmed = await confirmAction({
      message: t("workspaceSettingsDialog:languageChangeConfirm", { language: name }),
      confirmLabel: t("workspaceSettingsDialog:languageChangeAction"),
      cancelLabel: t("common:actions.cancel"),
      danger: true,
    });
    if (!confirmed) return;
    setChoosing(next);
    keepFirstRunLanguage(next);
    // Its words not here, the screen goes on in the language it has.
    await switchLanguage(next).catch(() => undefined);
    setChoosing("");
  }

  return (
    <div className={wall ? styles.wallFoot : styles.foot} data-sign-in-language>
      <NotionSelect ariaLabel={shown} value={value} options={options} onChange={(next) => void choose(next)}
        className={styles.select} buttonClassName={styles.control} settingsRow unmarked menuAlign="start"
        menuSize={{ width: 240, gap: 0, maxHeight: Math.round(window.innerHeight * 0.7) }}
        buttonContent={<><GlobeIcon className={styles.globe} aria-hidden="true" /><span>{shown}</span></>} />
    </div>
  );
}
