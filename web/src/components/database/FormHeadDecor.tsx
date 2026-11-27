import { PhotoIcon, SmileyIcon } from "@/icons/leafen";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { nextCover } from "@/lib/covers";
import type { FormViewConfig } from "@/lib/types";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { storedIconType } from "../page/nativeIcons";
import { PageCover } from "../page/PageCover";
import { randomPageIcon } from "../page/PageHeader";
import headerStyles from "../page/PageHeader.module.css";
import { PageIconGlyph } from "../page/PageIcon";
import { PageIconPicker } from "../page/PageIconPicker";
import type { PagePresentationChange } from "../page/pagePresentation";
import { formCoverPage, formIconSource } from "../forms/FormSurface";
import styles from "./FormView.module.css";

export type FormDecorChange = Partial<Pick<FormViewConfig, "icon" | "cover" | "coverPosition">>;

/** A form builder's cover, 160 high across the builder, which changes, moves and goes as a page's (Notion's, seen 2026-11-08). */
export function FormHeadCover({ form, pageId, workspaceId, onChange }: {
  form: Pick<FormViewConfig, "cover" | "coverPosition">;
  pageId: string;
  workspaceId: string;
  onChange: (change: FormDecorChange) => void;
}) {
  if (!form.cover) return null;
  return (
    <div className={styles.formCover} data-form-cover>
      <PageCover page={formCoverPage(form, pageId, workspaceId)} onChange={(change: PagePresentationChange) => {
        onChange({
          ...(change.cover !== undefined ? { cover: change.cover } : {}),
          ...(change.coverPosition !== undefined ? { coverPosition: change.coverPosition } : {}),
        });
        return true;
      }} />
    </div>
  );
}

/**
 * A form builder's head over its title, made of a page head's parts, as
 * Notion's (seen 2026-11-08): "☺ 양식 아이콘 · 🖼 양식 커버" over the title;
 * "양식 아이콘" sets a random emoji, 78 square, and opens the icon picker
 * under it; "양식 커버" sets a cover (FormHeadCover). The icon and the
 * buttons are the column's first children, so the title's hover shows the
 * buttons (FormView.module.css .formDecor); with both set there are none.
 */
export function FormHeadDecor({ form, onChange }: {
  form: Pick<FormViewConfig, "icon" | "cover">;
  onChange: (change: FormDecorChange) => void;
}) {
  const { t } = useTranslation(["formView", "pageHeader"]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const iconRef = useRef<HTMLButtonElement>(null);
  const icon = formIconSource(form.icon);
  const hasIcon = icon.iconType !== "none";
  const pick = (next: string) => { onChange({ icon: next }); setPickerOpen(false); };
  // A form's icon is a bare string, so the picker offers what one holds:
  // an emoji, a built-in icon and a picture's address.
  const picker = {
    returnFocusFromKeysOnly: true, current: form.icon,
    onPick: pick,
    onShuffle: (value: string) => onChange({ icon: value }),
    onPickBuiltInIcon: pick,
    onPickImage: (url: string) => { if (storedIconType(url) === "image") pick(url); },
    onRemove: hasIcon ? () => pick("") : undefined,
  };
  const anchor = usePageIconPickerAnchor(pickerOpen && hasIcon, iconRef);
  const open = pickerOpen ? hasIcon
    ? anchor && <AnchoredIconPicker anchor={anchor} onClose={() => setPickerOpen(false)} picker={picker} />
    : <PageIconPicker placement="inline" {...picker} onClose={() => setPickerOpen(false)} />
    : null;
  return (
    <>
      {hasIcon ? (
        <div className={styles.formIcon} data-form-icon>
          <button ref={iconRef} type="button" className={headerStyles.bigIcon} aria-label={t("pageHeader:changeIcon")}
            aria-haspopup="dialog" aria-expanded={pickerOpen} onClick={() => setPickerOpen(true)}>
            <PageIconGlyph page={icon} size={78} box={78} fallback="none" />
          </button>
          {open}
        </div>
      ) : null}
      {hasIcon && form.cover ? null : (
        <div className={styles.formDecor} data-form-decor>
          {hasIcon ? null : (
            <span className={headerStyles.addIconWrap}>
              <button type="button" className={headerStyles.ctrlBtn} aria-haspopup="dialog" aria-expanded={pickerOpen}
                onClick={() => { onChange({ icon: randomPageIcon() }); setPickerOpen(true); }}>
                <SmileyIcon size="tiny" weight="fill" aria-hidden="true" /><span>{t("formView:formIcon")}</span>
              </button>
              {open}
            </span>
          )}
          {form.cover ? null : (
            <button type="button" className={headerStyles.ctrlBtn} onClick={() => onChange({ cover: nextCover(form.cover), coverPosition: 50 })}>
              <PhotoIcon size="tiny" weight="fill" aria-hidden="true" /><span>{t("formView:formCover")}</span>
            </button>
          )}
        </div>
      )}
    </>
  );
}
