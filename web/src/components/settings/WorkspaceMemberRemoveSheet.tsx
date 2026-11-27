import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import styles from "./WorkspaceMemberRemoveSheet.module.css";

/**
 * Removing someone on a phone asks in a whole sheet from the bottom, as
 * Notion's phone (seen 2026-11-16 10:20 in iPhone Safari at 402, dark, from
 * a member's "역할 선택"): the grip and a round ✕ at its top right, their
 * face in the middle under it, the question bold and centred, the line
 * about what it does in grey under that, then a red filled button across the
 * sheet 20 in from either side over a grey outlined one that keeps them.
 * The words are Leafen's own, as its computer's question has them; a touch
 * screen wider than a phone keeps that question in the middle, as Notion's
 * iPad.
 */
export function WorkspaceMemberRemoveSheet({ userId, name, avatar, onConfirm, onClose }: {
  userId: string;
  name: string;
  avatar?: string | null;
  onConfirm(): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const question = t("memberRemoveConfirm", { name });
  return (
    <PhoneSheet title={question} head={false} onClose={onClose} bodyClassName={styles.body} data={{ "data-member-remove-sheet": "" }}>
      <button type="button" className={styles.close} aria-label={t("common:actions.close")} data-phone-sheet-close onClick={onClose}>
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
          <path d="M1 1 9 9M9 1 1 9" />
        </svg>
      </button>
      <PersonAvatar className={styles.face} userId={userId} avatar={avatar} name={name} size={46} />
      <h2 className={styles.question}>{question}</h2>
      <p className={styles.detail}>{t("memberRemoveConfirmDetail")}</p>
      <button type="button" className={styles.remove} data-member-remove-confirm onClick={() => { onClose(); onConfirm(); }}>
        {t("memberRemoveFromWorkspace")}
      </button>
      <button type="button" className={styles.keep} onClick={onClose}>{t("common:actions.cancel")}</button>
    </PhoneSheet>
  );
}
