import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import styles from "@/components/AuthGate.module.css";
import { AddServerAccountDialog } from "@/components/sidebar/AddServerAccountDialog";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { endAddingAccount } from "@/lib/serverAccounts";
import type { Session } from "@/types";
import { AuthGate } from "./AuthGate";

/**
 * "계정 추가하기", as Notion's window over the page: another account signs in on
 * this server, this one staying signed in, or, by the link under it, one on
 * another server.
 */
export function AddAccountWindow({ onSession }: { onSession: (session: Session) => void }) {
  const { t } = useTranslation("sidebar");
  const [otherServer, setOtherServer] = useState(false);
  const dialog = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialog, modalOwned: () => [dialog.current?.parentElement],
    onEscape: endAddingAccount });
  return (
    <div className={styles.addAccountBackdrop} style={{ zIndex }} role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget) endAddingAccount();
    }}>
      <div ref={dialog} className={styles.addAccountWindow} role="dialog" aria-modal="true" aria-labelledby="add-account-title">
        <AuthGate addingAccount inWindow onSession={onSession} />
        <button className={`${styles.linkButton} ${styles.addAccountOtherServer}`} type="button" onClick={() => setOtherServer(true)}>
          {t("profiles.addTitle")}
        </button>
        <button className={styles.addAccountCancel} type="button" onClick={endAddingAccount}>{t("profiles.cancel")}</button>
      </div>
      {otherServer ? <AddServerAccountDialog onClose={() => setOtherServer(false)} /> : null}
    </div>
  );
}
