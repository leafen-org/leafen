import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { BuiltInIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import { FileText, iconPixels, type LeafenIconSize } from "@/icons/leafen";
import type { PageLayoutContentTab } from "@/lib/databasePageLayout";
import { AnchoredIconPicker } from "../page/AnchoredEmojiPicker";
import { databaseViewLabels } from "./databaseViewLabels";
import { DescribeButton } from "./describeButton";
import { menuIconButtonProps } from "./menuIconButton";
import viewStyles from "./DatabaseViewSettingsPanel.module.css";
import phoneStyles from "./PhonePageLayoutBuilder.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/** The content tab's icon: one of Notion's icons it was given, else a page. */
export function ContentTabIcon({ tab, size }: { tab: PageLayoutContentTab | undefined; size: LeafenIconSize }) {
  const own = tab?.icon ? parseNativeIconValue(tab.icon) : undefined;
  return own ? <span aria-hidden="true" style={{ display: "inline-flex" }}><BuiltInIconGlyph icon={own} size={iconPixels(size)} /></span>
    : <FileText size={size} aria-hidden="true" />;
}

/**
 * The content tab's settings line, as Notion's "설정 보기" for it (seen
 * 2026-11-01), the same as a view's: its icon (Notion's icons, "제거" back
 * to a page), its name, kept on Enter or leaving it, and an "i" that opens
 * a line for its description under it. An emptied name is "콘텐츠" again.
 */
export function PageLayoutContentTabFields({ tab, phone = false, onChange }: {
  tab: PageLayoutContentTab | undefined;
  /** In the phone's sheet: its 45px row with the 30px tile, the name chosen as it opens, as Notion's (seen 2026-10-25 at 375px). */
  phone?: boolean;
  onChange(next: PageLayoutContentTab): void;
}) {
  const { t } = useTranslation("pageLayout");
  const labels = databaseViewLabels();
  const fallback = t("contentTab");
  const [name, setName] = useState(tab?.name || fallback);
  useEffect(() => setName(tab?.name || fallback), [tab?.name, fallback]);
  const description = tab?.description ?? "";
  const [describing, setDescribing] = useState(false);
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const [iconAnchor, setIconAnchor] = useState<{ top: number; bottom: number; left: number; right: number } | null>(null);
  useEffect(() => { if (phone) nameRef.current?.select(); }, [phone]);
  function change(patch: PageLayoutContentTab) {
    const next = { ...tab, ...patch };
    onChange(Object.fromEntries(Object.entries(next).filter(([, value]) => value)) as PageLayoutContentTab);
  }
  function rename(value: string) {
    const next = value.trim();
    const kept = next && next !== fallback ? next : undefined;
    if (kept !== tab?.name) change({ name: kept });
    if (!next) setName(fallback);
  }
  function describe(value: string) {
    const next = value.trim();
    if (next !== description) change({ description: next || undefined });
  }
  return (
    <>
      <div className={phone ? phoneStyles.tabSettingsRow : viewStyles.viewName} data-content-tab-row>
        <button type="button" className={phone ? phoneStyles.tabSettingsMark : viewStyles.viewIconButton}
          {...menuIconButtonProps(labels.changeIcon, iconAnchor !== null)} data-content-tab-icon onClick={(event) => {
            const rect = event.currentTarget.getBoundingClientRect();
            setIconAnchor({ top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right });
          }}>
          <ContentTabIcon tab={tab} size="regular" />
        </button>
        <span className={phone ? phoneStyles.tabSettingsNameBox : viewStyles.viewNameBox}>
          <input ref={nameRef} value={name} aria-label={labels.viewName} placeholder={fallback} data-content-tab-name
            onChange={(event) => setName(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return;
              event.preventDefault();
              rename(event.currentTarget.value);
            }}
            onBlur={(event) => rename(event.target.value)} />
          <DescribeButton className={phone ? undefined : viewStyles.describe}
            name={labels.addViewDescription} open={describing || description !== ""} data-content-tab-describe onClick={() => {
              setDescribing(true);
              focusOpenedField(() => descriptionRef.current);
            }} />
        </span>
      </div>
      {describing || description ? (
        <textarea ref={descriptionRef} className={viewStyles.viewDescription} rows={1} maxLength={2000} defaultValue={description}
          aria-label={labels.viewDescription} placeholder={labels.viewDescriptionPlaceholder} data-content-tab-description
          onKeyDown={(event) => {
            if (!fieldEnter(event) || event.shiftKey) return;
            event.preventDefault();
            describe(event.currentTarget.value);
          }}
          onBlur={(event) => describe(event.target.value)} />
      ) : null}
      {iconAnchor ? (
        <AnchoredIconPicker anchor={iconAnchor} marker="content-tab-icon-picker" onClose={() => setIconAnchor(null)}
          picker={{
            label: labels.viewIcon,
            onPickBuiltInIcon: (icon) => { change({ icon }); setIconAnchor(null); },
            onRemove: () => { if (tab?.icon) change({ icon: undefined }); setIconAnchor(null); },
          }} />
      ) : null}
    </>
  );
}
