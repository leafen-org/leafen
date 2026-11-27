import { useEffect, useRef, useState, type ReactNode } from "react";
import { setInteractionModality } from "react-aria";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { FileBlank, TableIcon } from "@/icons/leafen";
import { isNewPageShortcut } from "@/lib/keyboard";
import { windowOverProduct } from "@/lib/productWindows";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useMenuEscape } from "@/components/sidebar/sidebarMenuEscape";
import styles from "./NewShortcutMenu.module.css";
import { focusedOpener } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { imeComposing } from "@/lib/fieldKeyBoundary";

type Choice = { key: string; label: string; icon: ReactNode; run(): void };

/**
 * ⌘N's menu, as Notion's (its create menu, its source read 2026-11-13, seen
 * in its Mac app 2026-11-12): a 300px window in the middle of the page with
 * "새 페이지" and "새 데이터베이스" — Notion's "새 채팅" and "새 회의록" are
 * its AI's — the first lit as it opens, each marked "Press 1", "Press 2"…,
 * which that number key chooses as Enter does the lit one. Escape or a press
 * outside closes it, giving the focus back to where it was. Where the person
 * can't make a page (no workspace yet, a guest's), ⌘N does what `unable` says.
 */
export function useNewShortcutMenu({ blocked, canChoose, unable, onPage, onDatabase }: {
  blocked: boolean; canChoose: boolean; unable(): void; onPage(): void; onDatabase(): void;
}) {
  const [open, setOpen] = useState(false);
  const latest = useRef({ blocked, canChoose, unable });
  latest.current = { blocked, canChoose, unable };
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      // A window over the product holds it back, search too (lib/productWindows.ts).
      if (latest.current.blocked || windowOverProduct() || !isNewPageShortcut(event)) return;
      event.preventDefault();
      if (!latest.current.canChoose) {
        latest.current.unable();
        return;
      }
      // Its first row lit as it opens, unringed though ⌘N is a key, as
      // Notion's; a row the arrows then move to is ringed.
      setInteractionModality("pointer");
      setOpen(true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return open ? <NewShortcutMenu onPage={onPage} onDatabase={onDatabase} close={() => setOpen(false)} /> : null;
}

function NewShortcutMenu({ onPage, onDatabase, close }: { onPage(): void; onDatabase(): void; close(): void }) {
  const { t } = useTranslation("sidebar");
  const menu = useRef<HTMLDivElement>(null);
  // Where the focus was, given back when nothing was chosen.
  const before = useRef(focusedOpener());
  const shut = (restore: boolean) => {
    close();
    const back = before.current;
    if (restore && back?.isConnected) giveFocusBack({ to: back, keepScroll: true });
  };
  const choose = (run: () => void) => () => { shut(false); run(); };
  const choices: Choice[] = [
    { key: "page", label: t("newPage"), icon: <FileBlank size="regular" aria-hidden="true" />, run: choose(onPage) },
    { key: "database", label: t("newDatabase"), icon: <TableIcon size="regular" aria-hidden="true" />, run: choose(onDatabase) },
  ];
  const picker = useNativeMenu({ items: choices, open: true, menuRef: menu, label: t("newShortcutMenu"),
    focusOnOpen: true, highlightOnOpen: true, keyOf: (choice) => choice.key, textOf: (choice) => choice.label,
    onAction: (choice) => choice.run() });
  useMenuEscape(true, () => shut(true));
  // A number key chooses its row, as Notion's ("Press 1"…), unless typed
  // into a field.
  const latest = useRef(choices);
  latest.current = choices;
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || imeComposing(event)) return;
      const target = event.target;
      if (target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/u.test(target.tagName))) return;
      const choice = /^[1-9]$/u.test(event.key) ? latest.current[Number(event.key) - 1] : undefined;
      if (!choice) return;
      event.preventDefault();
      event.stopPropagation();
      choice.run();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);
  return createPortal(
    <>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("closeNewMenu")} onClick={() => shut(true)} />
      <div {...picker.menuProps} ref={menu} className={styles.menu} data-new-shortcut-menu>
        {choices.map((choice, index) => (
          <NativeMenuItem key={choice.key} state={picker.state} itemKey={choice.key} className={styles.row} data-new-shortcut-choice={choice.key}>
            {({ labelProps }) => <>
              {choice.icon}
              <span {...labelProps} className={styles.label}>{choice.label}</span>
              <span className={styles.key} aria-hidden="true">{`Press ${index + 1}`}</span>
            </>}
          </NativeMenuItem>
        ))}
      </div>
    </>,
    document.body,
  );
}
