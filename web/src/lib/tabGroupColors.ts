// The colors of a group of tabs in the Mac and Windows apps' strip and its
// menu (desktop/tab_strip_groups.js, tab_strip_group_menu.js),
// as Notion 7.44.1's tab bar takes them from its design tokens for each of a
// group's ten colors (its getTabGroupTheme and color menu, read 2026-11-24
// from its web client; its tokens read there the same day in both themes
// and its Windows app measured beside them: a blue group's ground #233850,
// its color circle #83abe1 in the dark). The page works them out and tells
// the strip with its other colors (desktopTabs.ts).

/** A group's colors, in the order its color menu shows them. */
export const TAB_GROUP_COLORS = ["default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"] as const;
export type TabGroupColor = (typeof TAB_GROUP_COLORS)[number];

// Each color's tokens, from the token sheet (app/tabGroupColors.css).
const TOKENS = ["tex-pri", "tex-sec", "tex-ter", "ico-pri", "ico-sec", "ico-ter", "bor-pri-tra", "bor-str", "bac-sec", "bac-ter",
  "bac-int-tra", "bac-sec-tra", "bac-ter-tra"] as const;

/** A group's colors in the strip and its menu. */
export type TabGroupLook = {
  /** Its ground while open, and folded (none for the plain color). */
  tray: string;
  chip: string;
  title: string;
  titleFolded: string;
  divider: string;
  chevron: string;
  tabText: string;
  tabTextActive: string;
  tabHover: string;
  tabActive: string;
  icon: string;
  close: string;
  /** Its circle in the color menu, its edge (the plain color's) and the ring of the chosen one. */
  swatch: string;
  swatchEdge: string;
  swatchRing: string;
};

const mix = (a: string, b: string, share: number) => `color-mix(in srgb, ${a} ${share}%, ${b})`;

/** A group's colors in the theme shown, as Notion's tab bar works them out from its tokens. */
export function tabGroupLook(color: string): TabGroupLook {
  const dark = document.documentElement.dataset.theme === "dark";
  const known = (TAB_GROUP_COLORS as readonly string[]).includes(color) ? color as TabGroupColor : "default";
  const style = getComputedStyle(document.documentElement);
  const [texPri, texSec, texTer, icoPri, icoSec, icoTer, borPriTra, borStr, bacSec, bacTer, bacIntTra, bacSecTra, bacTerTra] =
    TOKENS.map((name) => style.getPropertyValue(`--tab-group-${known}-${name}`).trim());
  const plain = known === "default";
  const title = dark ? mix(texPri, texTer, 70) : texSec;
  return {
    tray: bacSec, chip: plain ? "transparent" : bacSec, title, titleFolded: mix(title, "transparent", 80), divider: borPriTra,
    chevron: dark ? icoTer : icoSec, tabText: mix(texPri, "transparent", 80), tabTextActive: texPri, tabHover: bacIntTra,
    tabActive: plain ? bacTerTra : bacSecTra, icon: icoSec, close: icoPri,
    swatch: plain ? bacTer : icoSec, swatchEdge: plain ? borStr : "", swatchRing: plain ? icoTer : icoSec,
  };
}

/** Every color's look in the theme shown, for the strip (desktopTabs.ts). */
export function tabGroupLooks(): Record<string, TabGroupLook> {
  return Object.fromEntries(TAB_GROUP_COLORS.map((color) => [color, tabGroupLook(color)]));
}
