import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AdministratorWorkspacePublicItem } from "@/administratorWire.generated";
import { administratorWorkspaceContent } from "@/administratorWorkspaceContentApi";
import { userErrorText } from "@/lib/settingsErrors";
import { ReadFailed } from "../ReadFailed";
import type { Session } from "../../types";
import { confirmAction } from "../ConfirmActionDialog";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorWorkspaceContent.module.css";
import { displayDate } from "@/lib/displayDate";
import { listedPageNameKind, untitledPageDisplayTitle } from "../../lib/pageTitle";
import { titleWithClosedMentions } from "../../lib/closedMentions";

const GROUPS = [["site", "sites"], ["form", "forms"], ["link", "links"]] as const;

/**
 * What a workspace made public, for a server administrator to turn off
 *: its sites, forms and
 * links, as its settings' "공개 페이지" lists them.
 */
export function AdministratorWorkspacePublic({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const { t } = useTranslation("serverConsole");
  const [items, setItems] = useState<AdministratorWorkspacePublicItem[]>();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // A failed read of the list, said where it shows with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    administratorWorkspaceContent.publicItems(session, workspaceId, controller.signal).then((list) => setItems(list.items), (cause) => {
      if (!controller.signal.aborted) setReadFailed(userErrorText(cause, t("workspaceContent.loadFailed")));
    });
    return () => controller.abort();
    // The list loads once for the workspace shown, and again when asked.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readTurn, session, workspaceId]);

  async function disable(item: AdministratorWorkspacePublicItem) {
    const confirmed = await confirmAction({
      message: t("workspaceContent.public.disableMessage", { title: titleWithClosedMentions(item.title, item.titleSpans) || untitledPageDisplayTitle(listedPageNameKind(item)) }),
      detail: t("workspaceContent.public.disableDetail"),
      confirmLabel: t("workspaceContent.public.disable"),
      danger: true,
    });
    if (!confirmed) return;
    setBusy(item.id);
    setError("");
    try {
      await administratorWorkspaceContent.disablePublic(session, workspaceId, item.kind, item.id);
      setItems((current) => current?.filter((other) => other.id !== item.id));
    } catch (cause) {
      setError(userErrorText(cause, t("workspaceContent.failed")));
    } finally {
      setBusy("");
    }
  }

  const date = (value: string) => value ? displayDate(value) : "";
  return (
    <div>
      {GROUPS.map(([kind, label]) => {
        const group = (items ?? []).filter((item) => item.kind === kind);
        if (!group.length) return null;
        return (
          <div key={kind}>
            <div className={own.group}>{t(`workspaceContent.public.${label}`)}</div>
            <div className={own.list}>
              {group.map((item) => (
                <div key={item.id} className={own.item} data-admin-public={item.id}>
                  <span className={own.itemText}>
                    <strong>{titleWithClosedMentions(item.title, item.titleSpans) || untitledPageDisplayTitle(listedPageNameKind(item))}</strong>
                    <span>{[item.path, item.publishedByName, date(item.publishedAt)].filter(Boolean).join(" · ")}</span>
                  </span>
                  <button type="button" className={styles.secondaryButton} disabled={busy === item.id} onClick={() => void disable(item)}>
                    {t("workspaceContent.public.disable")}
                  </button>
                </div>
              ))}
            </div>
          </div>
        );
      })}
      {items && !items.length ? <div className={own.hint}>{t("workspaceContent.public.empty")}</div> : null}
      {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
      {error ? <div className={own.error} role="alert">{error}</div> : null}
    </div>
  );
}
