import { useEffect, useLayoutEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCopyNotice } from "@/lib/copyNotice";
import {
  isDeveloperBarShortcut,
  readDeveloperIds,
  shortDeveloperId,
  toggleDeveloperBar,
  useDeveloperBarHidden,
  useDeveloperMode,
  type DeveloperIds,
} from "@/lib/developerMode";
import type { Session } from "@/types";
import styles from "./DeveloperBar.module.css";

const KINDS = ["user", "workspace", "page"] as const;

/**
 * The developer bar of "개발자 기능 활성화", as Notion's (seen 2026-10-02):
 * a 28px bar along the window's bottom with the person's, the workspace's
 * and the open page's API IDs, each shortened and copied whole when pressed,
 * and ⌘⇧. to hide it, which also brings it back. The app's height leaves
 * room for it (--developer-bar-height).
 */
export function DeveloperBar({ session, workspaceId, pageId }: {
  session: Session;
  workspaceId?: string | null;
  pageId?: string | null;
}) {
  const on = useDeveloperMode();
  const hidden = useDeveloperBarHidden();
  const shown = on && !hidden;

  useEffect(() => {
    if (!on) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isDeveloperBarShortcut(event)) return;
      event.preventDefault();
      toggleDeveloperBar();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [on]);

  useLayoutEffect(() => {
    if (!shown) return;
    const root = document.documentElement;
    root.style.setProperty("--developer-bar-height", "28px");
    return () => {
      root.style.removeProperty("--developer-bar-height");
    };
  }, [shown]);

  return shown ? <DeveloperBarIds session={session} workspaceId={workspaceId} pageId={pageId} /> : null;
}

function DeveloperBarIds({ session, workspaceId, pageId }: {
  session: Session;
  workspaceId?: string | null;
  pageId?: string | null;
}) {
  const { t } = useTranslation("appShell");
  const [ids, setIds] = useState<DeveloperIds | null>(null);
  // Each chip keeps its ID while a toast says it was copied.
  const notice = useCopyNotice();

  useEffect(() => {
    const controller = new AbortController();
    readDeveloperIds(session, { workspaceId, pageId }, controller.signal)
      .then(setIds)
      .catch(() => {
        if (!controller.signal.aborted) setIds(null);
      });
    return () => controller.abort();
  }, [pageId, session, workspaceId]);

  function copy(id: string) {
    return notice(id);
  }

  return (
    <div className={styles.bar} role="toolbar" aria-label={t("developerBar.label")} data-developer-bar>
      {KINDS.map((kind) => {
        const id = ids?.[kind];
        if (!id) return null;
        return (
          <button
            key={kind}
            type="button"
            className={styles.chip}
            data-developer-id={kind}
            title={t("developerBar.copy", { id })}
            onClick={() => void copy(id)}
          >
            <span className={styles.label}>{t(`developerBar.${kind}`)}</span>
            <span className={styles.value}>{shortDeveloperId(id)}</span>
          </button>
        );
      })}
      <button
        type="button"
        className={styles.hide}
        data-developer-bar-hide
        aria-label={t("developerBar.hide")}
        title={t("developerBar.hide")}
        onClick={toggleDeveloperBar}
      >
        ⌘⇧.
      </button>
    </div>
  );
}
