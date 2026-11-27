// Unlocking a locked page "for me" lasts as long as the tab that did it, as
// Notion's does (seen 2026-10-31: kept while going to another page and back,
// gone after a reload or in another tab). The server keeps one unlock per
// person; the tabs that unlocked a page say so here, so a tab opening a page
// no tab of its person holds open locks it for them again.

const unlockedHere = new Set<string>();
const CHANNEL = "leafen-page-unlocks";
const ANSWER_WITHIN_MS = 150;

type Message = { kind: "ask"; pageId: string; question: string } | { kind: "held"; question: string };

let channel: BroadcastChannel | null = null;
function openChannel() {
  if (channel || typeof BroadcastChannel === "undefined") return channel;
  channel = new BroadcastChannel(CHANNEL);
  channel.addEventListener("message", (event: MessageEvent<Message>) => {
    const message = event.data;
    if (message?.kind === "ask" && unlockedHere.has(message.pageId)) channel?.postMessage({ kind: "held", question: message.question } satisfies Message);
  });
  return channel;
}

/** Whether this tab unlocked the page for its person. */
export function unlockedInThisTab(pageId: string) {
  return unlockedHere.has(pageId);
}

/** Notes this tab's own unlock or lock again of a page. */
export function noteUnlockedInThisTab(pageId: string, unlocked: boolean) {
  openChannel();
  if (unlocked) unlockedHere.add(pageId);
  else unlockedHere.delete(pageId);
}

/** Whether another open tab holds the page unlocked for this person. */
export function heldInAnotherTab(pageId: string): Promise<boolean> {
  const open = openChannel();
  if (!open) return Promise.resolve(false);
  const question = `${pageId}:${Math.random().toString(36).slice(2)}`;
  return new Promise((resolve) => {
    const listen = (event: MessageEvent<Message>) => {
      if (event.data?.kind !== "held" || event.data.question !== question) return;
      open.removeEventListener("message", listen);
      window.clearTimeout(timer);
      resolve(true);
    };
    const timer = window.setTimeout(() => { open.removeEventListener("message", listen); resolve(false); }, ANSWER_WITHIN_MS);
    open.addEventListener("message", listen);
    open.postMessage({ kind: "ask", pageId, question } satisfies Message);
  });
}
