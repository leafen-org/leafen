import { createContext, useContext } from "react";

/** The open settings page's own title. */
export const SettingsPageTitle = createContext("");

/**
 * A settings section's title. One that only repeats the page's title above
 * it, as a page's first and only section does, stays for screen readers
 * alone, as Notion's settings name a page once (2026-10-16: "서버 계정",
 * "에이전트" and "로그인 제공자" named twice). `restates` marks one that says
 * the page's name in other words ("사용자" › "서버 사용자").
 */
export function SettingsSectionTitle({ id, className, restates = false, children }: {
  id?: string; className?: string; restates?: boolean; children: string;
}) {
  const pageTitle = useContext(SettingsPageTitle);
  const hidden = children === pageTitle || restates && pageTitle !== "";
  return <div id={id} className={hidden ? "visually-hidden" : className} data-settings-section-title>{children}</div>;
}
