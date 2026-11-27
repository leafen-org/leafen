import type { DatabaseViewSettingsScreen } from "./DatabaseViewSettingsPanel";
import "./settingsPanelWidth.css";

/**
 * Whether a view's settings, docked beside a database's page or a popover
 * under an inline database's button, are as wide as what their screen shows
 * — 290px at least, 24px short of the window at most — or stay 290px.
 *
 * Notion's sorts screen fits its rules, each property and direction up to
 * 180 (seen 2026-11-18 at 1440, dpr 1, in two tabs: docked, a select's sort
 * 320.08 and a number's 364.88, its "낮은 항목 → 높은 항목으로" cut at 180,
 * the panel's end kept where it was; an inline database's popover 320.08 for
 * the same sort; 290 again once the sorts are removed). Its filters and
 * layout screens are drawn the same way but their rows are cut to the panel
 * (a filter "아주 긴 이름의 우선순위 속성: 높음, 낮음" cut at 232.6 in a 290
 * one), so they stay 290 as here, where their cards and rules are laid out
 * for the panel's width; its first screen and every list it picks from ("새
 * 정렬", "필터 추가", "그룹화 기준", the properties, conditional color, the
 * database's sources, its properties, data source management) are 290 at
 * most. A list a screen shows to pick from heads the panel while it shows
 * (settingsScreenHead.ts).
 */
export function settingsPanelFitsContent(screen: DatabaseViewSettingsScreen | null, pickingInScreen: boolean) {
  return screen === "sort" && !pickingInScreen;
}
