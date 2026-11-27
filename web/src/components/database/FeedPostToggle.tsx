import { type ReactNode, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ToggleCaretIcon } from "@/icons/leafen";
import { afterNextPaint } from "@/lib/afterPress";
import styles from "./FeedPostToggle.module.css";

/** Writes a toggle's shut (true) or open on its page, from its content as last known; answers the content written. */
export type FeedToggleSave = (collapsed: boolean, known: Record<string, unknown>) => Promise<Record<string, unknown>>;

/**
 * A toggle or a toggle heading in a feed post, as Notion's feed draws it
 * (seen 2026-11-17 in two tabs): open or shut as its page holds it, what is
 * inside it under it only while open, and opened or shut by its caret — on
 * its page too, as Notion's feed saves it (seen 2026-11-17 in two ways: open
 * after the feed was read again and on the row's page). The press draws it
 * at once; the page's write goes after the next frame, the last of quick
 * presses alone, and one the server refuses puts it back as the page holds
 * it. A reader who can't write the page (no `save`) opens it in the post only.
 * `line` is the toggle's own line, drawn as the post draws that kind.
 */
export function FeedPostToggle({ blockId, shut, content, level, line, save, children }: {
  blockId: string;
  /** Shut on its page. */
  shut: boolean;
  /** Its content as the post read it. */
  content: Record<string, unknown>;
  /** A toggle heading's level; none for a toggle. */
  level?: number;
  line: ReactNode;
  save?: FeedToggleSave;
  children: ReactNode;
}) {
  const { t } = useTranslation("blockItem");
  const [open, setOpen] = useState(!shut);
  // Its content and open as the page holds them, as last written here, and
  // the write waiting for the frame after the press.
  const known = useRef(content);
  const written = useRef(!shut);
  const waiting = useRef<(() => void) | null>(null);
  function press() {
    const next = !open;
    setOpen(next);
    if (!save) return;
    waiting.current?.();
    waiting.current = afterNextPaint(() => {
      waiting.current = null;
      if (next === written.current) return;
      const before = { content: known.current, open: written.current };
      written.current = next;
      known.current = { ...before.content, collapsed: !next };
      save(!next, before.content).then((stored) => {
        if (written.current === next) known.current = stored;
      }, () => {
        if (written.current !== next) return;
        known.current = before.content;
        written.current = before.open;
        setOpen(before.open);
      });
    });
  }
  return (
    <>
      <div className={styles.toggle} data-level={level} data-open={open && children ? "true" : undefined} data-feed-toggle={blockId}>
        <button type="button" className={styles.caret} aria-expanded={open}
          aria-label={t(open ? "toggle.close" : "toggle.open")} onClick={press}>
          <ToggleCaretIcon className={styles.caretIcon} size="tiny" aria-hidden="true" />
        </button>
        {line}
      </div>
      {open ? children : null}
    </>
  );
}
