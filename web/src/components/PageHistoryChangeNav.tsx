import { useEffect, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronUp } from "@/icons/leafen";
import { ownDigits } from "@/lib/i18n";
import styles from "./PageHistoryChangeNav.module.css";

// A box of deleted content is one change, as Notion counts it (seen 2026-11-07).
const CHANGE_SELECTOR = '[data-history-changed="true"], tr[data-history-row-change], [data-history-removed]';

/**
 * The pill at the foot of a version's preview that steps through what the
 * version changed, as Notion's (seen 2026-11-05): "2 / 3개 변경 사항", up and
 * down round and round, and only the change it is on outlined.
 */
export function PageHistoryChangeNav({ canvasRef, versionKey }: {
  canvasRef: RefObject<HTMLElement | null>;
  /** Changes when another version's preview shows. */
  versionKey: string;
}) {
  const { t } = useTranslation("updatesPanel");
  const [count, setCount] = useState(0);
  const [index, setIndex] = useState(0);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const read = () => setCount(canvas.querySelectorAll(CHANGE_SELECTOR).length);
    read();
    // A mark comes and goes with its element or its attribute: the same
    // version shown again from its bookmark keeps its rows, unmarked.
    const observer = new MutationObserver(read);
    observer.observe(canvas, {
      childList: true, subtree: true, attributes: true, attributeFilter: ["data-history-changed", "data-history-row-change", "data-history-removed"],
    });
    return () => observer.disconnect();
  }, [canvasRef, versionKey]);

  useEffect(() => setIndex(0), [versionKey, count]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    canvas.querySelectorAll("[data-history-current-change]").forEach((held) => held.removeAttribute("data-history-current-change"));
    const changes = [...canvas.querySelectorAll<HTMLElement>(CHANGE_SELECTOR)];
    changes.forEach((change, position) => {
      change.toggleAttribute("data-history-current-change", position === index);
    });
    changes[index]?.scrollIntoView({ block: "nearest" });
  }, [canvasRef, count, index, versionKey]);

  if (count === 0) return null;
  const step = (by: number) => setIndex((current) => (current + by + count) % count);
  // The count in the language's own digits and the change it is on in Western
  // ones, as Notion's pill writes them (Arabic "1 / ١ من التغييرات", seen 2026-11-19).
  const label = ownDigits(t("versions.changeCount", { current: "\uE000", count })).replace("\uE000", String(index + 1));
  return (
    <div className={styles.nav} data-history-change-nav>
      <span className={styles.label}>{label}</span>
      <button type="button" className={styles.step} aria-label={t("versions.previousChange")} onClick={() => step(-1)}>
        <ChevronUp size="tiny" aria-hidden="true" />
      </button>
      <button type="button" className={styles.step} aria-label={t("versions.nextChange")} onClick={() => step(1)}>
        <ChevronDown size="tiny" aria-hidden="true" />
      </button>
      <span className={styles.announce} aria-live="polite">{t("versions.changeOf", { current: index + 1, count })}</span>
    </div>
  );
}
