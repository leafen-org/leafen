import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, CommentBubbleIcon, CountChevronDown, LockIcon, UnlockIcon } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, PhoneSheetSwitch } from "./shell/PhoneSheet";
import type { PagePresentationChange } from "./page/pagePresentation";
import type { RowMenuPhoneStyle } from "./RowMenuPhoneSheet";
import styles from "./RowMenuPhoneCustomize.module.css";

const INLINE_COMMENTS = ["default", "minimal"] as const;

/** A locked page's lock for the reader alone: unlocked for them, or locked again. */
export type RowMenuPhoneLockForMe = { unlockedForMe: boolean; change(unlocked: boolean): unknown };

/**
 * The phone "작업" sheet's "페이지 사용자 지정", as Notion's (seen 2026-11-16
 * on the A90's Chrome at 411, a page's and a locked database's page, each
 * locked and not): in the "작업" sheet's place, a half sheet of two rows,
 * their comment marks in the words' colour (the settings tone) — page
 * comments' switch, inline comments' "기본 ⌄" opening an untitled half sheet
 * of "기본 ✓ · 최소화". The page locked, a 12px line across the sheet under
 * its head reads "🔒 페이지 잠김" with "잠금 해제", which unlocks it for the
 * reader alone, then "🔓 페이지 잠김" with "다시 잠금"; while it holds, the
 * switch is faint and neither row changes, their words as they are.
 */
export function RowMenuPhoneCustomize({ kind, style, onStyle, locked, lockForMe, onClose }: {
  kind: "page" | "wiki" | "database" | "row";
  style: RowMenuPhoneStyle;
  /** Changes the style; without it the rows show it and change nothing. */
  onStyle?: (change: PagePresentationChange) => void;
  /** Whether the page's lock holds for the reader now. */
  locked: boolean;
  /** The page is locked: its line, with the reader's own unlock or lock again. */
  lockForMe?: RowMenuPhoneLockForMe;
  onClose(): void;
}) {
  const { t } = useTranslation(["rowMenu", "topBar"]);
  const [inlineOpen, setInlineOpen] = useState(false);
  const held = locked || !onStyle;
  const change = (next: PagePresentationChange) => { if (!held) onStyle?.(next); };
  const inlineValue: typeof INLINE_COMMENTS[number] = style.inlineCommentsDisplay === "minimal" ? "minimal" : "default";
  const inlineLabel = (value: typeof INLINE_COMMENTS[number]) => t(value === "minimal" ? "rowMenu:customize.inlineMinimal" : "rowMenu:customize.inlineDefault");
  const unlocked = Boolean(lockForMe?.unlockedForMe);
  return (
    <PhoneSheet size="half" tone="settings" title={t("rowMenu:phone.customize")} onClose={onClose} data={{ "data-page-phone-customize": "" }}>
      {lockForMe ? (
        <div className={styles.lockLine} data-page-phone-lock-line={unlocked ? "unlocked" : "locked"}>
          {unlocked ? <UnlockIcon size="page" className={styles.lockMark} aria-hidden="true" /> : <LockIcon size="page" className={styles.lockMark} aria-hidden="true" />}
          <span>{t("rowMenu:phone.pageLocked")}</span>
          <button type="button" className={styles.lockButton} onClick={() => lockForMe.change(!unlocked)}>
            {t(unlocked ? "topBar:lockAgain" : "rowMenu:actions.unlockForMe")}
          </button>
        </div>
      ) : null}
      <PhoneSheetGroup className={held ? `${styles.rows} ${styles.held}` : styles.rows}>
        {/* A wiki's own page keeps no page comments here, as before. */}
        {kind === "wiki" ? null : (
          <PhoneSheetRow more={false} icon={<CommentBubbleIcon size="regular" />} label={t("rowMenu:customize.pageComments")}
            value={<PhoneSheetSwitch on={style.pageCommentsDisplay !== "off"} />} disabled={held}
            onPress={() => change({ pageCommentsDisplay: style.pageCommentsDisplay === "off" ? "default" : "off" })} />
        )}
        <PhoneSheetRow more={false} icon={<CommentBubbleIcon size="regular" />} label={t("rowMenu:customize.inlineComments")}
          value={<span className={styles.choice}>{inlineLabel(inlineValue)}<CountChevronDown size="small" trim className={styles.choiceMark} aria-hidden="true" /></span>}
          disabled={held} onPress={() => setInlineOpen(true)} />
      </PhoneSheetGroup>
      {inlineOpen ? (
        <PhoneSheet size="half" title="" label={t("rowMenu:customize.inlineComments")} onClose={() => setInlineOpen(false)}>
          <PhoneSheetGroup>
            {INLINE_COMMENTS.map((value) => (
              <PhoneSheetRow key={value} more={false} label={inlineLabel(value)} value={inlineValue === value ? <CheckIcon size="regular" /> : undefined}
                onPress={() => { change({ inlineCommentsDisplay: value }); setInlineOpen(false); }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </PhoneSheet>
  );
}
