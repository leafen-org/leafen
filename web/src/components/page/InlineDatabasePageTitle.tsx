import { type ComponentProps, type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { i18next } from "@/i18n";
import { PhotoIcon, SmileyIcon } from "@/icons/leafen";
import type { DataSourceIcon as DatabaseIcon } from "@/lib/nativeDatabaseViewProductAdapter";
import type { WorkspaceFileTarget } from "@/lib/storage";
import type { TextSpan } from "@/lib/types";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import type { DatabaseDescriptionParts } from "./DatabaseDescription";
import headerStyles from "./PageHeader.module.css";
import { randomPageIcon } from "./PageHeader";
import { hasExplicitPageIcon, PageIcon } from "./PageIcon";
import { PageIconPicker } from "./PageIconPicker";
import { PageRenamePopover } from "./PageRenamePopover";
import { onPageRenameRequest } from "./pageRenameRequest";
import { DatabaseTitleField, DatabaseTitleText } from "../database/DatabaseTitleField";
import { DatabaseTitleIcon } from "../database/DatabaseTitleIcon";
import styles from "./InlineDatabasePageTitle.module.css";

/**
 * An inline database opened alone as a page: its icon and name as the
 * page's title, as Notion's (seen 2026-10-01). Renaming it renames the
 * database. Over its title "아이콘 추가" gives the database an icon, which
 * the database's title shows wherever it is; the icon opens its picker.
 * The database's description sits under it, its control beside "아이콘 추가".
 * The top bar's last piece renames it from the rename popover (seen 2026-10-27).
 */
export function InlineDatabasePageTitle({ title, titleSpans, icon, name = "shown", placeholder: givenPlaceholder, readOnly, onTitleChange, onIconChange, uploadTarget, onAddCover, description, verifyControl, verifiedBadge }: {
  title: string;
  /** The name's formatting, which it shows and edits, as Notion's (seen 2026-11-07). */
  titleSpans?: readonly TextSpan[];
  /**
   * Whether its large name shows: a linked database's only once it has a
   * name of its own, as Notion's (seen 2026-10-15) — "↗ 〈source〉" under it
   * names it meanwhile; a view block's — a dashboard's, a link's still
   * waiting for its source — never, its icon standing alone over its views,
   * which may show their own title row (seen 2026-11-09).
   */
  name?: "shown" | "onceNamed" | "none";
  /** What it shows while untitled: "새 데이터베이스", or its sources' names (untitledDatabaseSourcesName). */
  placeholder?: string;
  icon: ComponentProps<typeof PageIcon>["page"];
  readOnly: boolean;
  onTitleChange: (title: string, spans?: TextSpan[]) => void;
  onIconChange?: (icon: DatabaseIcon) => void;
  uploadTarget?: WorkspaceFileTarget;
  /** "커버 추가", while the database has no cover. */
  onAddCover?: () => void;
  description?: DatabaseDescriptionParts;
  /** "인증 추가", as a page's header has it. */
  verifyControl?: ReactNode;
  /** "인증됨" above the title, once verified. */
  verifiedBadge?: ReactNode;
}) {
  const placeholder = givenPlaceholder || inlineDatabasePlaceholderTitle();
  const nameShown = name === "shown" || (name === "onceNamed" && Boolean(title.trim()));
  const { t } = useTranslation("pageHeader");
  const [pickerOpen, setPickerOpen] = useState(false);
  const iconButtonRef = useRef<HTMLButtonElement>(null);
  const hasIcon = hasExplicitPageIcon(icon);
  const editableIcon = !readOnly && onIconChange ? onIconChange : undefined;
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  const pageId = uploadTarget?.pageId;
  useEffect(() => readOnly || !pageId ? undefined : onPageRenameRequest((request) => {
    if (request.pageId === pageId) setRenameAt(request.at);
  }), [pageId, readOnly]);

  function change(next: DatabaseIcon) {
    editableIcon?.(next);
    setPickerOpen(false);
    window.requestAnimationFrame(() => iconButtonRef.current?.focus());
  }

  // As a page's, "아이콘 추가" gives a random emoji and opens the picker on it.
  function addIcon() {
    editableIcon?.({ icon: randomPageIcon(), iconType: "emoji" });
    setPickerOpen(true);
  }

  const picker = pickerOpen && editableIcon ? (
    <PageIconPicker
      pageIcon current={icon}
      placement={hasIcon ? "anchored" : "inline"}
      uploadTarget={uploadTarget}
      onPick={(emoji) => change({ icon: emoji, iconType: "emoji" })}
      onPickImage={(url) => change({ icon: url, iconType: "image" })}
      onPickCustomEmoji={(emoji) => change({ icon: emoji.id, iconType: "custom_emoji" })}
      onPickBuiltInIcon={(value) => change({ icon: value, iconType: "icon" })}
      onRemove={() => change({ icon: "", iconType: "none" })}
      onClose={() => setPickerOpen(false)}
    />
  ) : null;

  return (
    <div className={styles.title} data-inline-database-page-title data-nameless={nameShown ? undefined : "true"}>
      {(editableIcon && !hasIcon) || (!readOnly && onAddCover) || description?.control || verifyControl ? (
        <div className={styles.controls} role="toolbar" aria-label={t("pageOptions")} data-inline-database-page-controls>
          {editableIcon && !hasIcon ? (
            <div className={styles.addIconWrap}>
              <button ref={iconButtonRef} type="button" className={headerStyles.ctrlBtn} aria-haspopup="dialog"
                aria-expanded={pickerOpen} data-inline-database-add-icon onClick={addIcon}>
                <SmileyIcon size="tiny" weight="fill" aria-hidden="true" />
                <span>{t("addIcon")}</span>
              </button>
              {picker}
            </div>
          ) : null}
          {!readOnly && onAddCover ? (
            <button type="button" className={headerStyles.ctrlBtn} aria-label={t("addPageCover")}
              data-inline-database-add-cover onClick={onAddCover}>
              <PhotoIcon size="tiny" weight="fill" aria-hidden="true" />
              <span>{t("addCover")}</span>
            </button>
          ) : null}
          {/* Verification before the description, as Notion's (seen 2026-10-01). */}
          {verifyControl}
          {description?.control}
        </div>
      ) : null}
      {verifiedBadge}
      <div className={styles.row}>
        {hasIcon ? (
          editableIcon ? (
            <span className={styles.iconWrap}>
              <DatabaseTitleIcon className={styles.icon} icon={icon} size={36} buttonRef={iconButtonRef} open={pickerOpen}
                onOpenChange={setPickerOpen} onIconChange={editableIcon} uploadTarget={uploadTarget} dataAttributes={{ "data-inline-database-icon": "" }} />
            </span>
          ) : <PageIcon className={styles.glyph} page={icon} size={36} fallback="none" />
        ) : null}
        {!nameShown ? null : readOnly ? (
          <h1 className={styles.name} data-inline-database-page-name>
            <DatabaseTitleText title={title} spans={titleSpans} fallback={placeholder} />
          </h1>
        ) : (
          <DatabaseTitleField element="h1" className={styles.name} dataAttributes={{ "data-inline-database-page-name": "" }}
            value={title} spans={titleSpans} placeholder={placeholder} ariaLabel={i18next.t("databaseView:databaseTitle")}
            onTitleChange={onTitleChange} />
        )}
      </div>
      {description?.field}
      {renameAt && pageId ? (
        <PageRenamePopover page={{ id: pageId, workspaceId: uploadTarget?.workspaceId ?? "", icon: icon.icon, iconType: icon.iconType, kind: "database" }}
          iconFallback="database" title={title} titleSpans={titleSpans} untitled={placeholder} at={renameAt} onTitleChange={onTitleChange}
          onPresentationChange={editableIcon ? (next) => editableIcon({ icon: next.icon ?? "", iconType: (next.iconType ?? "none") as DatabaseIcon["iconType"] }) : undefined}
          onClose={() => setRenameAt(null)} />
      ) : null}
    </div>
  );
}
