import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DocPlainTextIcon, InlineDatabaseIcon } from "@/icons/leafen";
import { prefersReducedMotion } from "@/lib/motion";
import type { NativeNotionMigrationLive, NativeNotionMigrationLiveItem } from "../../notionImportWire.generated";
import styles from "./NotionMoveLive.module.css";

// A Notion move's stage as it goes, as a copy window shows a copy (user
// decision 2026-11-23 "묶어서 올려" — a screen of Leafen's own, where
// Notion's import window shows a bar alone): the name of what is read or
// made now passes about a second apart, leaving Notion while it is read,
// coming into Leafen while it is made, going between the two while it is
// checked. The names are the server's, as read or made
// (notion_migration_live.go): none is made up, and one that stops stays.
// With motion reduced, the name stands alone.

/** The pace the names pass at. */
const nameEveryMs = 1000;

/** Which way a stage's names go: out of Notion, into Leafen, or between them. */
export type NotionMoveWay = "out" | "in" | "between";

export function notionMoveWay(stage: NativeNotionMigrationLive["stage"]): NotionMoveWay {
  if (stage === "creating" || stage === "comments") return "in";
  return stage === "verifying" ? "between" : "out";
}

/** The name showing now: the next one the server noted, one a second, or the last. */
function usePassingName(items: readonly NativeNotionMigrationLiveItem[]) {
  const [shown, setShown] = useState<NativeNotionMigrationLiveItem | null>(items[items.length - 1] ?? null);
  const shownAt = useRef(shown?.at ?? 0);
  const itemsRef = useRef(items);
  itemsRef.current = items;
  useEffect(() => {
    const timer = window.setInterval(() => {
      const next = itemsRef.current.find((item) => item.at > shownAt.current);
      if (!next) return;
      shownAt.current = next.at;
      setShown(next);
    }, nameEveryMs);
    return () => window.clearInterval(timer);
  }, []);
  // Before any name passed, the newest stands.
  if (!shown && items.length) return items[items.length - 1];
  return shown;
}

export function NotionMoveLive({ live, way }: { live: NativeNotionMigrationLive; way: NotionMoveWay }) {
  const { t } = useTranslation(["importDialog"]);
  const item = usePassingName(live.items);
  const still = prefersReducedMotion();
  const glyph = item?.kind === "database" ? <InlineDatabaseIcon aria-hidden /> : <DocPlainTextIcon aria-hidden />;
  return (
    <div className={styles.live} data-notion-move-live={live.stage} data-notion-move-way={way}>
      {still ? null : (
        <div className={styles.track} aria-hidden>
          <span className={styles.end}>{t("importDialog:migration.liveFrom")}</span>
          <span className={styles.lane}>
            {item ? <span key={item.at} className={`${styles.chip} ${styles[way]}`}>{glyph}<span>{item.name}</span></span> : null}
          </span>
          <img className={styles.mark} src="/mark-128.png" alt="" width={20} height={20} />
        </div>
      )}
      {item ? (
        <span className={styles.name} data-notion-move-name>{glyph}<span>{item.name}</span></span>
      ) : null}
    </div>
  );
}

/** The settings reading in the person's Notion, as it reads: its names, and how far it is. */
export function NotionMoveFormats({ live }: { live: NativeNotionMigrationLive }) {
  const { t } = useTranslation(["importDialog"]);
  return (
    <div className={styles.live} data-notion-move-formats>
      <strong>{t("importDialog:migration.liveFormats")}</strong>
      <NotionMoveLive live={live} way="out" />
      <small>{[t("importDialog:migration.liveFormatsCount", { done: live.done, left: live.left }),
        live.waitSeconds ? t("importDialog:migration.liveWaiting", { seconds: live.waitSeconds }) : ""].filter(Boolean).join(" · ")}</small>
    </div>
  );
}
