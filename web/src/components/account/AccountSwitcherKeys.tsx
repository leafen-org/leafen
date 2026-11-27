import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import type { SwitcherKeyList } from "@/authWire.generated";
import { loadSession } from "@/lib/authSessionStore";
import { styles } from "./accountSettingsStyles";
import { displayDate } from "@/lib/displayDate";

function hostOf(origin: string): string {
  try {
    return new URL(origin).host;
  } catch {
    return origin;
  }
}

/**
 * The other servers' pages that list this account's workspaces here (the
 * multi-server decision's 2026-10-07 addition), each with when it was
 * allowed and last asked, and taken back with one button.
 */
export function AccountSwitcherKeys({ onSettled }: { onSettled?: () => void } = {}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [list, setList] = useState<SwitcherKeyList | null>(null);
  const [busy, setBusy] = useState("");
  const refresh = useCallback(() => {
    const signedIn = loadSession();
    if (!signedIn) {
      onSettled?.();
      return;
    }
    void request<SwitcherKeyList>("/account/switcher-keys", {}, signedIn).then(setList, () => setList(null))
      .finally(() => onSettled?.());
  }, [onSettled]);
  useEffect(refresh, [refresh]);
  if (!list || (list.keys.length === 0 && list.enabled)) return null;
  const when = (time: number) => displayDate(time);
  return (
    <div className={styles.securityPanel} data-account-switcher-keys>
      <div className={styles.organizationMeta}>{t("switcherKeysTitle")}</div>
      {list.enabled ? null : <div className={styles.organizationMeta}>{t("switcherKeysOff")}</div>}
      {list.keys.map((key) => (
        <div key={key.id} className={styles.securityRow} data-switcher-key={key.holder}>
          <div>
            <strong>{hostOf(key.holder)}</strong>
            <span>{key.usedAt ? t("switcherKeysUsed", { allowed: when(key.createdAt), used: when(key.usedAt) }) : t("switcherKeysAllowed", { allowed: when(key.createdAt) })}</span>
          </div>
          <div className={styles.sectionActions}>
            <button type="button" className={styles.secondaryButton} disabled={busy === key.id} onClick={() => {
              const signedIn = loadSession();
              if (!signedIn) return;
              setBusy(key.id);
              void request(`/account/switcher-keys/${encodeURIComponent(key.id)}`, { method: "DELETE" }, signedIn)
                .finally(() => { setBusy(""); refresh(); });
            }}>
              {t("switcherKeysRevoke")}
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
