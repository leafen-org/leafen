/**
 * Fields drawn as their words before their editor is made — a table's title
 * or text until it is edited (RichTextCellShown), a page's title, values and
 * comment box until the frame they open in is painted (RichTextCellEditor's
 * afterPaint) — each with what makes its editor now.
 */
const sleeping = new WeakMap<HTMLElement, () => void>();

/** Keeps `wake` as what makes `element`'s editor now, until the returned stop. */
export function sleepUntilWoken(element: HTMLElement, wake: () => void): () => void {
  sleeping.set(element, wake);
  return () => {
    if (sleeping.get(element) === wake) sleeping.delete(element);
  };
}

/** Whether `element` is a field drawn before its editor is made. */
export function fieldAsleep(element: Element): boolean {
  return sleeping.has(element as HTMLElement);
}

/**
 * The editor a field drawn asleep turns into, made now — the element itself
 * where its editor is made in it, or the one drawn in its place — and the
 * element where it is awake already. The caller gives it the caret.
 */
export function liveField(element: HTMLElement): HTMLElement {
  const wake = sleeping.get(element), parent = element.parentElement;
  if (!wake || !parent) return element;
  wake();
  if (element.isConnected && element.getAttribute("contenteditable") === "true") return element;
  return parent.querySelector<HTMLElement>(":scope > [data-rich-cell-editor][contenteditable=\"true\"]") ?? element;
}
