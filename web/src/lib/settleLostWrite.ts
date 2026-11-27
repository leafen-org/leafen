/**
 * A versioned change sent just after one whose answer was lost: the one
 * before may have been taken, moving the version this one was based on, and
 * the server then refuses it as made on an old version (2026-11-14: a
 * landing page chosen just after one whose answer was lost was refused, and
 * the one before came back). Refused so, the one before is sent again as it
 * was: taken, it answers with its receipt, which names the version this one
 * is built again on and sent; refused as made on an old version too, it
 * wasn't taken and can't be (`forget`), and the refusal is someone else's
 * change, told as it came. Its answer lost again, the refusal is told and
 * the one before kept.
 */
export async function sendAfterLostWrite<T>({ send, lost, conflicted, forget, again }: {
  /** This change, as built. */
  send: () => Promise<T>;
  /** The change before it, sent again as it was; none when there is none or it is this one. */
  lost: (() => Promise<T>) | null;
  /** Whether the server refused a change as made on an old version. */
  conflicted: (error: unknown) => boolean;
  /** The change before, known not taken. */
  forget: () => void;
  /** This change built again on the version the change before's answer names, and sent. */
  again: (settled: T) => Promise<T>;
}): Promise<T> {
  try {
    return await send();
  } catch (error) {
    if (!lost || !conflicted(error)) throw error;
    const settled = await lost().then((answer) => ({ answer }), (cause: unknown) => {
      if (conflicted(cause)) forget();
      return null;
    });
    if (!settled) throw error;
    return again(settled.answer);
  }
}
