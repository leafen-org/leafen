import { useTranslation } from "react-i18next";
import { NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { markGitHubSupportOpened, useDisplayPreferences } from "@/lib/displayPreferences";
import { DEFAULT_LEGAL_LINKS } from "@/lib/legalLinks";
import { sponsorThanksShown } from "@/lib/sponsorCredit";
import { useRuntimeConfig } from "@/lib/useRuntimeConfig";
import type { Session } from "@/types";
import styles from "./WorkspaceMenuGitHubLine.module.css";

/**
 * Whether the workspace menu shows its GitHub line to this person: read
 * before the menu opens so the row is there when it does — until they press
 * it once, on any device (the account keeps it with their display
 * preferences, read as the app starts), and never where the server's
 * LEAFEN_SPONSOR_CREDIT=off (sponsorThanksShown).
 */
export function useGitHubLineShown(): boolean {
  const config = useRuntimeConfig();
  const { githubSupportOpened } = useDisplayPreferences();
  return Boolean(config && sponsorThanksShown(config.sponsorCredits)) && !githubSupportOpened;
}

/**
 * "⭐ GitHub에서 리펜 응원하기" under the workspace menu's head, where Notion's
 * menu asks to try a plan (user decision 2026-11-25, in the sponsor credit's
 * place; Leafen asks for no plan): a row as the menu's others, 28px with its
 * mark in the 20px place and 14px words, muted, as Notion's row there is the
 * menu's others' size; at the top of the phone's sheet of the same menu, a
 * row as its foot's. It opens Leafen's public repository in a new tab with
 * no referrer, closes the menu, and goes for good for this person.
 */
export function WorkspaceMenuGitHubLine({ session, phone, onOpen }: { session: Session; phone?: boolean; onOpen(): void }) {
  const { t } = useTranslation("sidebar");
  return (
    <NativeActionMenuItem
      className={styles.line}
      href={DEFAULT_LEGAL_LINKS.sourceUrl}
      target="_blank"
      rel="noopener noreferrer"
      referrerPolicy="no-referrer"
      data-github-support=""
      data-phone={phone ? "true" : undefined}
      textValue={t("githubSupport")}
      onClick={() => {
        markGitHubSupportOpened(session);
        onOpen();
      }}
    >
      <span className={styles.mark} aria-hidden="true">⭐</span>
      <span className={styles.words}>{t("githubSupport")}</span>
    </NativeActionMenuItem>
  );
}
