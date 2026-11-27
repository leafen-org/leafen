// A screen that follows a state on the server something else changes — an
// agent's commands from the terminal, the outside address opening — asks
// once for all of the person's tabs, not once per tab (2026-11-13: thirteen
// tabs in the first run asked about eleven times a second, hidden ones as
// often as the shown one). What a tab learns of such a state, read or
// written, it tells the others on this server (BroadcastChannel), under the
// state's topic; a tab that knows an answer newer than its own wait asks
// nothing. Only a tab in sight asks; one behind follows what it hears and,
// coming back in sight, asks once at once if it knows nothing fresh. Of two
// tabs in sight the one that read last goes on, the other waits half a
// wait longer, so a tab that closes or goes behind is taken over. The app's
// repeated asks of the server all follow this way (2026-11-13: the band
// about where the server's data is, the other workspaces' unread dots, the
// backup band, moves and replicas under way, import runs, a page held or
// copied, a device joining), but for those only the tab can use: its own
// job, upload, socket or open file, or a wait that ends with an outage or
// a restart.

type Heard = (value: unknown, here: boolean) => void;
type Message = { topic: string; at: number; value?: unknown };
type Known = { at: number; here: boolean; answer?: { value: unknown; at: number } };

const CHANNEL = "leafen:follow";

let channel: BroadcastChannel | null | undefined;
const listeners = new Map<string, Set<Heard>>();
// When each topic's latest answer, or a tab's start to ask for one, was
// known, whether this tab made it, and the latest answer itself.
const latest = new Map<string, Known>();

function know(topic: string, at: number, here: boolean, answer?: { value: unknown }) {
  const known = latest.get(topic);
  const kept = answer ? { value: answer.value, at } : known?.answer;
  if (!known || at >= known.at) latest.set(topic, { at, here, answer: kept });
  else if (answer && (!known.answer || at >= known.answer.at)) known.answer = kept;
}

function open(): BroadcastChannel | null {
  if (channel !== undefined) return channel;
  channel = typeof BroadcastChannel === "undefined" ? null : new BroadcastChannel(CHANNEL);
  channel?.addEventListener("message", (event: MessageEvent<Message>) => {
    const message = event.data;
    if (!message || typeof message.topic !== "string" || typeof message.at !== "number") return;
    const answered = "value" in message;
    know(message.topic, message.at, false, answered ? { value: message.value } : undefined);
    if (answered) for (const heard of [...listeners.get(message.topic) ?? []]) heard(message.value, false);
  });
  return channel;
}

function post(message: Message) {
  try {
    open()?.postMessage(message);
  } catch {
    // A value the channel can't carry stays with this tab; the others read it themselves.
  }
}

/** What this tab read or wrote of a followed state: its own followers and the other tabs hear it. */
export function tellTabs(topic: string, value: unknown): void {
  const at = Date.now();
  know(topic, at, true, { value });
  post({ topic, at, value });
  for (const heard of [...listeners.get(topic) ?? []]) heard(value, true);
}

/** Hears a followed state's answers, this tab's (here) and the others', until the returned stop. */
export function hearTabs(topic: string, heard: Heard): () => void {
  open();
  const set = listeners.get(topic) ?? new Set<Heard>();
  set.add(heard);
  listeners.set(topic, set);
  return () => {
    set.delete(heard);
    if (set.size === 0) listeners.delete(topic);
  };
}

const inSight = () => document.visibilityState === "visible";

/**
 * Reads a followed state once now, for this tab and the others, and while
 * `following` goes on as followAcrossTabs; `heard` hears every answer any of
 * the person's tabs reads of it. Until the returned stop.
 */
export function readAndFollow(topic: string, every: number, following: boolean, read: () => Promise<unknown>,
  heard: Heard): () => void {
  const ask = () => read().then((value) => tellTabs(topic, value));
  const stop = following ? followAcrossTabs(topic, every, ask, heard) : hearTabs(topic, heard);
  void ask().catch(() => undefined);
  return stop;
}

/**
 * Follows a state every `every` ms for all of the person's tabs: `read` asks
 * the server and tells the answer with tellTabs, which `heard` hears in
 * whichever tab follows. With `now` it starts from an answer at once — one
 * told within the wait, else its own read; without, the first ask is a wait
 * from now. With `behind` a tab behind asks too, still one for all: a state
 * the app needs while none of its tabs is in sight (the app's notifications).
 * Until the returned stop.
 */
export function followAcrossTabs(topic: string, every: number, read: () => Promise<unknown>,
  heard: Heard, { now = false, behind = false }: { now?: boolean; behind?: boolean } = {}): () => void {
  let live = true;
  let timer = 0;
  let asking = false;
  const notBefore = now ? 0 : Date.now() + every;
  const stopHearing = hearTabs(topic, heard);
  // When this tab asks next: a wait after its own answer, a wait and a half after another tab's.
  const due = () => {
    const known = latest.get(topic);
    return Math.max(notBefore, known ? known.at + (known.here ? every : every * 1.5) : 0);
  };
  const look = () => {
    window.clearTimeout(timer);
    if (!live || asking || (!behind && !inSight())) return;
    const wait = due() - Date.now();
    if (wait > 0) {
      timer = window.setTimeout(look, wait);
      return;
    }
    asking = true;
    const at = Date.now();
    know(topic, at, true);
    post({ topic, at });
    void read().catch(() => undefined).finally(() => {
      asking = false;
      look();
    });
  };
  const comeBack = () => {
    if (behind || inSight()) look();
    else window.clearTimeout(timer);
  };
  const answer = latest.get(topic)?.answer;
  if (now && answer && Date.now() - answer.at < every) heard(answer.value, false);
  look();
  document.addEventListener("visibilitychange", comeBack);
  return () => {
    live = false;
    window.clearTimeout(timer);
    document.removeEventListener("visibilitychange", comeBack);
    stopHearing();
  };
}
