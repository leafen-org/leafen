import { PageLayoutIcon, PhotoIcon, SmileyIcon } from "@/icons/leafen";
import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseRowPagePresentationInput } from "@/databaseRowPageWire.generated";
import { nextCover } from "@/lib/covers";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import type { Page } from "@/types";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { PageCover } from "../page/PageCover";
import { randomPageIcon } from "../page/PageHeader";
import headerStyles from "../page/PageHeader.module.css";
import { hasExplicitPageIcon, isPagePictureIcon, PageIconGlyph } from "../page/PageIcon";
import { PageIconPicker } from "../page/PageIconPicker";
import type { PagePresentationChange } from "../page/pagePresentation";
import { useRowPageHeadAwayOnPhone } from "@/lib/emptyPageTitleCaret";

/** What of a database page its header shows: its icon and its cover. */
export interface RowPagePresentation {
  id: string;
  workspaceId: string;
  icon: string;
  iconType: string;
  cover: string;
  coverPosition: number;
}

/** The icon and cover of a presentation change, as a database page takes them. */
export function rowPagePresentationInput(
  change: PagePresentationChange,
): Omit<DatabaseRowPagePresentationInput, "expectedAuthorityRevision"> {
  return {
    ...(change.icon !== undefined ? { icon: change.icon } : {}),
    ...(change.iconType !== undefined ? { iconType: change.iconType as DatabaseRowPagePresentationInput["iconType"] } : {}),
    ...(change.cover !== undefined ? { cover: change.cover } : {}),
    ...(change.coverPosition !== undefined ? { coverPosition: change.coverPosition } : {}),
    ...(change.font !== undefined ? { font: change.font } : {}),
    ...(change.smallText !== undefined ? { smallText: change.smallText } : {}),
    ...(change.fullWidth !== undefined ? { fullWidth: change.fullWidth } : {}),
    ...(change.tableOfContentsDisplay !== undefined ? { tableOfContentsDisplay: change.tableOfContentsDisplay } : {}),
  };
}

/**
 * The box a database page's icon heads it in: a picture's 140, any other's
 * 78, a side peek's 36 — the layout builder's title card too (Notion's, seen
 * 2026-11-14: an emoji's 78 and a picture's 140 there as on the page).
 */
export function rowPageIconSize(icon: Pick<Page, "icon" | "iconType">, compact = false) {
  return compact ? 36 : isPagePictureIcon(icon) ? 140 : 78;
}

function pageOf(row: RowPagePresentation) {
  // The cover reads only these of a page.
  return { id: row.id, workspaceId: row.workspaceId, cover: row.cover, coverPosition: row.coverPosition } as Page;
}

/** A database page's cover, as a page's shows above its title. */
export function RowPageCover({
  row,
  compact,
  onChange,
}: {
  row: RowPagePresentation;
  compact: boolean;
  onChange?: (change: PagePresentationChange) => Promise<boolean>;
}) {
  if (!row.cover) return null;
  return (
    <PageCover page={pageOf(row)} compact={compact} readOnly={!onChange}
      onChange={(change) => onChange?.(change)} />
  );
}

/**
 * A database page's heading as a page's header is: its icon, "Add icon" and
 * "Add cover" shown while the heading is hovered or focused, and its title;
 * "Customize layout" follows them where the database may change, as
 * Notion's (seen 2026-10-12), or a wiki page's own controls. A phone that
 * opens it in the app scrolls them away (useRowPageHeadAwayOnPhone).
 */
export function RowPageHeading({
  row,
  onChange,
  onCustomizeLayout,
  verifyControl,
  controls,
  backlinks,
  titleEmpty,
  compact = false,
  layoutHeading = false,
  children,
}: {
  row: RowPagePresentation;
  onChange?: (change: PagePresentationChange) => Promise<boolean>;
  onCustomizeLayout?: () => void;
  /** "인증 추가" after "Add cover", before the layout, as Notion's (seen 2026-10-17). */
  verifyControl?: ReactNode;
  /** Controls after "Add cover", as a wiki page's "인증 추가" and "댓글 추가". */
  controls?: ReactNode;
  /** "N개의 백링크", first among the options, as Notion's (seen 2026-10-29). */
  backlinks?: ReactNode;
  titleEmpty: boolean;
  /** A side peek's compact head, as a page's there. */
  compact?: boolean;
  /** A page layout's heading in a peek shows its buttons only over their own
   *  row, not over the title or its pinned properties, as Notion's (seen
   *  2026-11-08, 2026-11-14); opened whole, over the title too, never over
   *  the pinned properties (PageHeader.module.css). */
  layoutHeading?: boolean;
  /** The title. */
  children: ReactNode;
}) {
  const { t } = useTranslation("pageHeader");
  const [pickerOpen, setPickerOpen] = useState(false);
  const iconButtonRef = useRef<HTMLButtonElement>(null);
  const headRef = useRef<HTMLDivElement>(null);
  useRowPageHeadAwayOnPhone(row.id, headRef);
  const icon = { icon: row.icon, iconType: row.iconType as Page["iconType"] };
  const hasIcon = hasExplicitPageIcon(icon);
  const change = (next: PagePresentationChange) => {
    void onChange?.(next);
    setPickerOpen(false);
  };
  const pickerProps = {
    returnFocusFromKeysOnly: true, current: icon,
    uploadTarget: { workspaceId: row.workspaceId, pageId: row.id },
    onPick: (emoji: string) => change({ icon: emoji, iconType: "emoji" }),
    // A random pick leaves the picker to say whether it stays open, as a page's (PageIconPicker).
    onShuffle: (icon: string, iconType: "emoji" | "icon") => void onChange?.({ icon, iconType }),
    onPickImage: (url: string) => change({ icon: url, iconType: "image" }),
    onPickCustomEmoji: (emoji: WorkspaceCustomEmoji) => change({ icon: emoji.id, iconType: "custom_emoji" }),
    onPickBuiltInIcon: (value: string) => change({ icon: value, iconType: "icon" }),
    onRemove: hasIcon ? () => change({ icon: "", iconType: "none" }) : undefined,
  };
  const closePicker = () => {
    setPickerOpen(false);
  };
  // Under the icon the picker centers on it, over the column (as a page's).
  const iconPickerAnchor = usePageIconPickerAnchor(pickerOpen && hasIcon && Boolean(onChange), iconButtonRef);
  const picker = pickerOpen && onChange ? hasIcon
    ? iconPickerAnchor && <AnchoredIconPicker anchor={iconPickerAnchor} onClose={closePicker} picker={pickerProps} />
    : <PageIconPicker placement="inline" {...pickerProps} onClose={closePicker} />
    : null;
  // A picture heads the row page in a 140px box, as a page's does.
  const picture = !compact && isPagePictureIcon(icon);
  const iconSize = rowPageIconSize(icon, compact);
  return (
    <div ref={headRef} className={headerStyles.header} data-has-cover={row.cover ? "true" : "false"}
      data-has-icon={hasIcon ? "true" : "false"} data-title-empty={titleEmpty ? "true" : "false"} data-compact={compact ? "true" : undefined} data-row-page-header
      data-layout-heading={layoutHeading ? "true" : undefined}>
      {hasIcon ? (
        <div className={headerStyles.iconWrap}>
          {onChange ? (
            <button ref={iconButtonRef} type="button" className={headerStyles.bigIcon} aria-label={t("changeIcon")}
              aria-haspopup="dialog" aria-expanded={pickerOpen} data-row-page-icon onClick={() => setPickerOpen(true)}
              data-icon-picture={picture ? "true" : undefined}>
              <PageIconGlyph page={icon} size={iconSize} box={iconSize} fallback="none" />
            </button>
          ) : (
            <span className={headerStyles.bigIcon} data-row-page-icon data-icon-picture={picture ? "true" : undefined}>
              <PageIconGlyph page={icon} size={iconSize} box={iconSize} fallback="none" />
            </span>
          )}
          {picker}
        </div>
      ) : null}
      {/* The options under the icon, over the title, as a page's; their row is
          there with none in it, keeping its band (PageHeader.module.css). */}
      <div className={headerStyles.controls} data-wrap="true" data-row-page-header-controls
        {...(onChange && (!hasIcon || !row.cover) || onCustomizeLayout || controls || verifyControl || backlinks
          ? { role: "toolbar", "aria-label": t("pageOptions") } : { "aria-hidden": true })}>
        {backlinks}
        {!hasIcon && onChange ? (
          <div className={headerStyles.addIconWrap}>
            <button ref={iconButtonRef} type="button" className={headerStyles.ctrlBtn} aria-haspopup="dialog"
              aria-expanded={pickerOpen} data-page-header-control
              onClick={() => {
                void onChange({ icon: randomPageIcon(), iconType: "emoji" });
                setPickerOpen(true);
              }}>
              <SmileyIcon size="tiny" weight="fill" aria-hidden="true" />
              <span>{t("addIcon")}</span>
            </button>
            {picker}
          </div>
        ) : null}
        {!row.cover && onChange ? (
          <button type="button" className={headerStyles.ctrlBtn} aria-label={t("addPageCover")} data-page-header-control
            onClick={() => void onChange({ cover: nextCover(row.cover), coverPosition: 50 })}>
            <PhotoIcon size="tiny" weight="fill" aria-hidden="true" />
            <span>{t("addCover")}</span>
          </button>
        ) : null}
        {verifyControl}
        {onCustomizeLayout ? (
          <button type="button" className={headerStyles.ctrlBtn} data-page-header-control data-row-page-customize-layout
            onClick={onCustomizeLayout}>
            <PageLayoutIcon size="tiny" weight="fill" aria-hidden="true" />
            <span>{t("customizeLayout")}</span>
          </button>
        ) : null}
        {controls}
      </div>
      {children}
    </div>
  );
}
