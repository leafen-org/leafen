import { useLayoutEffect, useRef, type CSSProperties, type ReactNode, type RefObject } from "react";
import { FocusScope, useDialog } from "react-aria";
import { createFocusManager } from "@react-aria/focus";
import { useTranslation } from "react-i18next";
import styles from "./editorStyles.generated.module.css";

/** The overlay owns dismissal/return. Aria owns dialog entry and Tab;
 * the URL draft and its explicit save remain native toolbar actions. Once
 * `left` for the text, it keeps the focus no more until it is entered again.
 * Its field takes the focus as it is drawn, before it is placed, so keys
 * typed on the heels of ⌘K go there, not over the chosen words (2026-11-15:
 * placed a task later, a letter typed 6 ms after ⌘K took the words' place). */
export function SelectionToolbarLinkDialog({ dialogRef, inputRef, placed, left = false, onEnter, selectLinkOnOpen, className, style, children }: {
  dialogRef: RefObject<HTMLDivElement | null>; inputRef: RefObject<HTMLInputElement | null>;
  placed: boolean; left?: boolean; onEnter?: () => void; selectLinkOnOpen?: boolean;
  /** A linked picture's window is a text link's edit window (ImageLinkWindow). */
  className?: string; style: CSSProperties; children: ReactNode;
}) {
  const { t } = useTranslation("selectionToolbar");
  const { dialogProps } = useDialog({ "aria-label": t("linkEditor.ariaLabel") }, dialogRef);
  const entered = useRef(false);
  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (entered.current || !dialog) return;
    createFocusManager(dialogRef).focusFirst({ tabbable: true });
    // One hidden until it is placed (a picture's, ImageLinkWindow) takes it then.
    if (!dialog.contains(document.activeElement)) return;
    entered.current = true;
    if (selectLinkOnOpen) inputRef.current?.select();
  }, [placed, dialogRef, inputRef, selectLinkOnOpen]);
  return <FocusScope contain={placed && !left}><div {...dialogProps} ref={dialogRef} className={className ?? styles.tbLinkMenu}
    style={style} onMouseDown={event => event.stopPropagation()} onFocus={left ? onEnter : undefined}>{children}</div></FocusScope>;
}
