/**
 * Reads a list a window at a time to its end, for a screen that needs all of
 * it at once (to match or search it); each request stays one window.
 */
export async function readEveryWindow<T>(
  read: (cursor?: string) => Promise<{ items: T[]; nextCursor?: string }>,
  signal?: AbortSignal,
): Promise<T[]> {
  let page = await read();
  const items = [...page.items];
  while (page.nextCursor && !signal?.aborted) {
    page = await read(page.nextCursor);
    items.push(...page.items);
  }
  return items;
}
