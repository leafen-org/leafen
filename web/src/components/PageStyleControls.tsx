import { SwitchMark } from "@/components/Switch";
import { NativeActionMenuItem, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { useTranslation } from "react-i18next";
import { FullWidthIcon, SmallTextIcon, ThemeLayoutIcon } from "@/icons/leafen";
import { pageMenuSearchWords } from "@/lib/pageMenuSearchWords";
import { menuSwitch, pageMenuItem, type PageMenuEntry, type PageMenuShow, type PageMenuSubmenu } from "./PageMenu";
import fontRowStyles from "./PageFontRow.module.css";
import styles from "./RowMenu.module.css";

const PAGE_FONTS = [
  { value: "default", sample: "Ag", labelKey: "fonts.default" },
  { value: "serif", sample: "Ag", labelKey: "fonts.serif" },
  { value: "mono", sample: "Ag", labelKey: "fonts.mono" },
] as const;
// Each font's own words for a search (pageMenuSearchWords).
const FONT_WORDS = { default: "fontDefault", serif: "fontSerif", mono: "fontMono" } as const;

export type PageStyle = { font: string; smallText: boolean; fullWidth: boolean; tableOfContentsDisplay?: string };
export type PageStyleChange = Partial<{ font: "default" | "serif" | "mono"; smallText: boolean; fullWidth: boolean; tableOfContentsDisplay: "default" | "off" }>;

/** The page's three fonts as tiles, as Notion's page "…" menu draws them (seen 2026-10-07). */
function PageFontTiles({ font: current, disabled, onChange, header }: {
  font: string;
  disabled: boolean;
  onChange(change: PageStyleChange): void;
  /** The tiles' name, over them. */
  header?: string;
}) {
  const { t } = useTranslation("rowMenu");
  return (
    <NativeMenuSection className={styles.fontPicker} aria-label={header ?? t("rowMenu:aria.pageFont")}>
      {header ? <NativeMenuHeader className={styles.sectionLabel}>{header}</NativeMenuHeader> : null}
      {PAGE_FONTS.map((font) => {
        const active = current === font.value;
        return (
          <NativeActionMenuItem key={font.value} type="button" className={styles.fontOption} data-page-font={font.value} data-font={font.value} data-active={active ? "true" : undefined} data-menu-item role="menuitemradio" aria-checked={active} disabled={disabled} onClick={() => onChange({ font: font.value })}>
            <span className={styles.fontSample}>{font.sample}</span>
            <span>{t(`rowMenu:${font.labelKey}`)}</span>
          </NativeActionMenuItem>
        );
      })}
    </NativeMenuSection>
  );
}

/** "작은 텍스트" and "전체 너비", each with its switch. */
function PageStyleSwitches({ style, disabled, onChange, visible = () => true }: {
  style: PageStyle;
  disabled: boolean;
  onChange(change: PageStyleChange): void;
  /** Whether a row's label is shown, as the menu's search leaves it. */
  visible?: (label: string) => boolean;
}) {
  const { t } = useTranslation("rowMenu");
  const smallText = t("rowMenu:style.smallText");
  const fullWidth = t("rowMenu:style.fullWidth");
  return (
    <>
      {visible(smallText) ? (
        <NativeActionMenuItem type="button" className={styles.item} data-menu-item role="menuitemcheckbox" aria-checked={style.smallText} disabled={disabled} onClick={() => onChange({ smallText: !style.smallText })}>
          <SmallTextIcon size="regular" aria-hidden="true" />
          <span>{smallText}</span>
          <SwitchMark on={style.smallText} className={styles.menuSwitch} />
        </NativeActionMenuItem>
      ) : null}
      {visible(fullWidth) ? (
        <NativeActionMenuItem type="button" className={styles.item} data-menu-item role="menuitemcheckbox" aria-checked={style.fullWidth} disabled={disabled} onClick={() => onChange({ fullWidth: !style.fullWidth })}>
          <FullWidthIcon size="regular" aria-hidden="true" />
          <span>{fullWidth}</span>
          <SwitchMark on={style.fullWidth} className={styles.menuSwitch} />
        </NativeActionMenuItem>
      ) : null}
    </>
  );
}

/** A page's style in its … menu, as Notion's: the font, small text and full width. */
export function PageStyleControls({ style, disabled, onChange }: {
  style: PageStyle;
  disabled: boolean;
  onChange(change: PageStyleChange): void;
}) {
  const { t } = useTranslation("rowMenu");
  return (
    <NativeMenuSection data-page-style-controls aria-label={t("rowMenu:sections.style")}>
      <NativeMenuHeader className={styles.sectionLabel}>{t("rowMenu:sections.style")}</NativeMenuHeader>
      <PageFontTiles font={style.font} disabled={disabled} onChange={onChange} />
      <PageStyleSwitches style={style} disabled={disabled} onChange={onChange} />
    </NativeMenuSection>
  );
}

/**
 * "테마와 레이아웃 ›", as Notion's page menu (seen 2026-10-17): the fonts under
 * "글꼴 스타일", then under "테마" small text, full width and the rows the
 * page adds — 목차, 페이지 댓글 and 인라인 댓글 › on a page, 목차 on a
 * database's page, 인라인 댓글 › alone on a wiki — and last, after a line and
 * under no name, the rows `apart`: a database's page's 레이아웃 사용자 지정,
 * which a search lists under "테마와 레이아웃" after the theme's rows (Notion's,
 * seen 2026-11-21 twice, Chrome 1440: "테마", "레이아웃", "사용자 지정"). A search
 * lists the fonts as rows, each with its "Ag" in that font and no mark of
 * the one chosen, as Notion's (seen 2026-11-16, Chrome 1440: "테마"). The
 * fonts and the small text are found by their own words too
 * (pageMenuSearchWords: "글꼴", "font"), as typing showed in Notion's (2026-11-21).
 */
export function usePageThemeSubmenu({ busy, style, disabled, onChange, fullWidth = true, styled = true, themeHead = false, rows, apart }: {
  busy: boolean;
  style: PageStyle;
  disabled: boolean;
  onChange(change: PageStyleChange): void;
  fullWidth?: boolean;
  /** False lists the page's own rows only, as a locked database page's menu does (seen 2026-11-14) —
   *  under the "테마" head with `themeHead`, as a page suggested in (seen 2026-11-18 twice). */
  styled?: boolean;
  themeHead?: boolean;
  /** The page's own rows after small text and full width. */
  rows?: (show: PageMenuShow, searching: boolean) => PageMenuEntry[];
  /** The page's rows in a section of their own after the theme's, with no name; with `styled` false, after `rows`. */
  apart?: (show: PageMenuShow) => PageMenuEntry[];
}): PageMenuSubmenu {
  const { t } = useTranslation("rowMenu");
  const item = pageMenuItem(busy);
  const fontHeader = t("rowMenu:sections.fontStyle");
  return {
    submenu: "theme", label: t("rowMenu:themeAndLayout"), icon: <ThemeLayoutIcon size="regular" aria-hidden="true" />, width: 300,
    data: { "data-page-theme-menu": "" },
    sections: (show, searching) => !styled ? [{ header: themeHead ? t("rowMenu:sections.theme") : undefined,
      rows: [...(rows?.(show, searching) ?? []), ...(apart?.(show) ?? [])] }] : [
      searching ? {
        header: fontHeader,
        rows: PAGE_FONTS.map((font) => item(show, `font-${font.value}`, t(`rowMenu:${font.labelKey}`),
          <span className={fontRowStyles.sample} data-font-sample data-font={font.value} aria-hidden="true">{font.sample}</span>, () => onChange({ font: font.value }),
          { disabled, role: "menuitemradio", checked: style.font === font.value, search: pageMenuSearchWords(FONT_WORDS[font.value]),
            data: { "data-page-font": font.value } })),
      } : {
        header: fontHeader, own: true,
        rows: [<PageFontTiles key="fonts" header={fontHeader} font={style.font} disabled={disabled} onChange={onChange} />],
      },
      {
        header: t("rowMenu:sections.theme"),
        rows: [
          item(show, "small-text", t("rowMenu:style.smallText"), <SmallTextIcon size="regular" aria-hidden="true" />,
            () => onChange({ smallText: !style.smallText }),
            { disabled, role: "menuitemcheckbox", checked: style.smallText, trailing: menuSwitch(style.smallText), data: { "data-page-small-text": "" },
              search: pageMenuSearchWords("smallText") }),
          fullWidth ? item(show, "full-width", t("rowMenu:style.fullWidth"), <FullWidthIcon size="regular" aria-hidden="true" />,
            () => onChange({ fullWidth: !style.fullWidth }),
            { disabled, role: "menuitemcheckbox", checked: style.fullWidth, trailing: menuSwitch(style.fullWidth), data: { "data-page-full-width": "" } }) : null,
          ...(rows?.(show, searching) ?? []),
        ],
      },
      { rows: apart?.(show) ?? [] },
    ],
  };
}
