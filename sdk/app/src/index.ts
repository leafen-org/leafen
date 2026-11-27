// Leafen app SDK for an app's screen (Apache-2.0, see ../LICENSE).
//
// An app's screen runs in a frame with no origin. Leafen puts a one-time
// value in the frame's address (#leafen=…); `connect()` says hello with it
// and receives a private message port, through which every call goes.
// Build the screen as a single-page app: the channel belongs to one document
// and closes if the frame goes to another page.

export type Theme = "light" | "dark";

/** The credentials the person has given an app, by key. */
export interface CredentialList {
  /** Whether the administrator lets people give this app credentials. */
  enabled: boolean;
  credentials: Array<{ key: string; connected: boolean }>;
}

export interface LeafenContext {
  /** The person's language, such as "ko" or "en". */
  locale: string;
  theme: Theme;
  /**
   * The screen Leafen is on, by its own rule: a phone (a touch screen
   * narrower than 600px), a wider touch screen, or a desktop with a pointer.
   * The frame's own width doesn't say: it may stand beside Leafen's sidebar.
   */
  screen?: "phone" | "touch" | "desktop";
  /** The app's own place below /apps/<slug>/, without a leading slash, with the address's query and fragment. */
  path: string;
  /** The app's full public address, for links it gives people (mail, notifications); empty for guests. */
  homeUrl?: string;
  app: { slug: string; version: string };
  /** Present when a public guest opened the app through a guest link. */
  guest?: { room: string; expiresAt: number };
  /**
   * Where the app is drawn: its own page, one of its blocks in a page, beside
   * a page, a database cell it shows, the box where that cell is edited, a
   * file opened with it, or the import screen's window for one of its tools.
   */
  place?: "page" | "block" | "sidePanel" | "propertyCell" | "propertyEditor" | "fileOpener" | "importSource";
  /**
   * In the import screen's window: the tool's key in the manifest's
   * surfaces.importSources, and the sources the person moved from it into
   * this workspace before — each the source id a bundle's descriptor named
   * (source.workspaceId). A bundle naming one of them carries on that
   * migration; any other starts its own.
   */
  importSource?: { key: string; earlier: string[] };
  /**
   * The file a person opened with the app (surfaces.fileOpeners): its block
   * and page, its id — read its bytes with api("GET", `/files/${fileId}`) —
   * name, type and size, and whether the person may change the page. Nobody
   * holds a file: anyone who may change the page edits it, and Leafen shows
   * who else has it open.
   */
  file?: {
    pageId: string; blockId: string; fileId: string; name: string; contentType: string; size: number; canEdit: boolean;
  };
  /** The page a block is in or a panel is beside; read it with `api` within what the app may read. */
  pageId?: string;
  /** A block's own id and its key in the manifest's surfaces.blocks. */
  blockId?: string;
  block?: string;
  /**
   * A block's settings, kept in the block (JSON of up to 8 KB). Anyone who
   * may edit the page can change them, so treat them as input, not as trusted.
   */
  props?: Record<string, unknown>;
  /** Whether the person may edit the page a block is in, and so its settings; in a cell's editor, its value. */
  editable?: boolean;
  /** A cell's row and property, the property's type, its value in that type and, for a select, its options. */
  rowId?: string;
  propertyId?: string;
  type?: string;
  value?: unknown;
  options?: { id: string; name: string; color: string }[];
}

/**
 * What saving a file answered: the new version, or why it wasn't saved. When
 * someone saved first, Leafen asks the person, outside the app, whether to
 * save over it, keep theirs as a copy (`copied`: the new block, which later
 * saves go to) or open the latest (the screen opens again with it).
 */
export interface FileSaveResult {
  ok: boolean;
  version?: string;
  fileId?: string;
  copied?: { blockId: string };
  /** "LEAFEN_FILE_VERSION_CONFLICT" (the person kept the newer one) or another reason. */
  code?: string;
  conflict?: { fileId: string; name: string; size: number; version: string; savedBy?: string };
}

export interface ApiResponse<T = unknown> {
  status: number;
  /** JSON, or a file's bytes as an ArrayBuffer (such as GET /files/{id}), with their contentType. */
  body: T;
  contentType?: string;
}

export interface RelayRequest {
  method?: string;
  /** An address the administrator approved in the manifest's network.relay. */
  url: string;
  headers?: Record<string, string>;
  /** Text, or base64 when bodyBase64 is true. */
  body?: string;
  bodyBase64?: boolean;
}

export interface RelayResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
  bodyBase64: boolean;
}

export interface ActionResult<T = unknown> {
  /** What the worker function set as its output, decoded when it was JSON. */
  output: T;
  runId: string;
}

export interface RoomTicket {
  serverUrl: string;
  roomName: string;
  ticket: string;
  expiresAt: number;
}

/**
 * A room joined for its messages: what the frame sends goes to the server —
 * the room's loop or its function — and what the server sends the room
 * comes back to everyone. (A shared document needs y-websocket with the
 * room's ticket.)
 */
export interface RoomConnection {
  send(message: string): void;
  onMessage(listener: (message: string) => void): () => void;
  onClose(listener: () => void): () => void;
  close(): void;
}

export interface GuestLink {
  url: string;
  expiresAt: number;
}

export interface ConnectedAccount {
  key: string;
  owner: string;
  connected: boolean;
  connectedAt: number;
}

export type KVCommand =
  | "GET" | "SET" | "INCR" | "DEL" | "EXPIRE" | "TTL"
  | "HGET" | "HGETALL" | "HSET" | "HDEL"
  | "LPUSH" | "RPUSH" | "LPOP" | "RPOP" | "LRANGE"
  | "ZADD" | "ZSCORE" | "ZREM" | "ZRANGE";

export interface KVRequest {
  command: KVCommand;
  key: string;
  field?: string;
  value?: string;
  member?: string;
  score?: number;
  start?: number;
  stop?: number;
  /** Seconds. */
  ttl?: number;
}

/** A call Leafen refused or that failed: `code` is Leafen's error code when it gave one. */
export class LeafenError extends Error {
  readonly code: string;

  constructor(text: string) {
    const [code, ...rest] = text.split(": ");
    super(rest.length ? rest.join(": ") || code : text);
    this.name = "LeafenError";
    this.code = rest.length ? code : text;
  }
}

/** A column of one of the app's tables. */
export interface TableColumn {
  id: string;
  name: string;
  type: string;
  position: string;
  config: Record<string, unknown>;
}

/** What rows to read and in what order, by column names. */
export interface TableQuery {
  /**
   * Conditions every row keeps: an operator the column's type takes
   * ("equals", "contains", "is_empty", "greater_than", "before", …) and an
   * option's name, a person's or row's ID, text, a number, true or false, a date.
   */
  where?: Array<{ column: string; operator: string; value?: unknown }>;
  /** Without it, rows keep the table's own order. */
  sort?: Array<{ column: string; descending?: boolean }>;
  /** Keeps the rows whose text contains it. */
  search?: string;
  /** Reads the archived rows instead of the others. */
  archived?: boolean;
  /** How many rows a page of `find` holds, 1 to 100 (100 by default). */
  limit?: number;
}

/** A table's row as it was read. */
export interface TableRow {
  id: string;
  position: string;
  createdAt: string;
  createdBy: string;
  updatedAt: string;
  lastEditedBy: string;
  /** A column's value as Leafen keeps it, or null. */
  value(column: string): unknown;
  /** A text, title, URL, email or phone column's value, or a formula's or rollup's as text. */
  text(column: string): string;
  number(column: string): number;
  checkbox(column: string): boolean;
  /** A select or status column's option name ("" when empty). */
  option(column: string): string;
  /** A multi-select column's option names. */
  options(column: string): string[];
  /** A person or relation column's people or rows. */
  ids(column: string): string[];
  date(column: string): { start: string; end: string };
}

/**
 * One of the app's databases, found by its data setup step's key, read and
 * written as the person within what the app may do: rows through Leafen's
 * database query, changes as the person's edit of a row.
 */
export interface Table {
  readonly key: string;
  readonly pageId: string;
  readonly dataSourceId: string;
  readonly columns: TableColumn[];
  /**
   * A page of the rows the query keeps from where `cursor` (none at first)
   * says, and the next page's cursor ("" after the last). Leafen works through
   * a large table a part at a time, so a page may be empty before the last.
   */
  find(query?: TableQuery, cursor?: string): Promise<{ rows: TableRow[]; next: string }>;
  /** Every row the query keeps, to the last. */
  rows(query?: TableQuery): AsyncGenerator<TableRow>;
  /** How many rows the query keeps. */
  count(query?: TableQuery): Promise<number>;
  /** One row by its ID, with its kept values (formulas and rollups come with `find`); null when it isn't in the table. */
  row(id: string): Promise<TableRow | null>;
  /** Makes a row after the ones the app added before and gives its ID. */
  add(values: Record<string, unknown>): Promise<string>;
  /**
   * Changes the values named by column and leaves the rest of the row: text;
   * a number; true or false; an option's name (an array of names for a
   * multi-select); people's or rows' IDs; a date as "2006-01-02", "start/end"
   * or `{start, end}`; null clears it. An option the column lacks is refused.
   */
  update(id: string, values: Record<string, unknown>): Promise<void>;
  /** Changes many rows, their values by row ID as `update` takes them: a hundred rows to a request. */
  updateMany(rows: Record<string, Record<string, unknown>>): Promise<void>;
  /** Moves a row to the trash, where people can restore it; a row already there stays. */
  remove(id: string): Promise<void>;
}

/** The person's own store for this app. */
export interface PersonStorage {
  /** The value kept under a key, or null. */
  get<T = unknown>(key: string): Promise<T | null>;
  /** Keeps any JSON value but null under a key (1 to 100 characters). */
  set(key: string, value: unknown): Promise<void>;
  remove(key: string): Promise<void>;
  /** Every key kept, in order. */
  keys(): Promise<string[]>;
}

export interface LeafenApp {
  /** What Leafen told the app when it connected; theme and path stay current. */
  readonly context: LeafenContext;
  /** Leafen's native API as the person, within what the administrator approved. */
  api<T = unknown>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>>;
  /** One of the app's actions, run on the server; Leafen asks the person first when it needs approval. */
  action<T = unknown>(name: string, input?: unknown): Promise<ActionResult<T>>;
  /** An approved outside address, through Leafen, which fills in secrets. */
  relay(request: RelayRequest): Promise<RelayResponse>;
  /** The app's temporary store in the organization it was opened for. */
  kv<T = unknown>(request: KVRequest): Promise<T>;
  /** One of the app's databases by its data setup step's key. */
  table(key: string): Promise<Table>;
  /**
   * The person's own small store for this app, kept on the server: what
   * browser storage would hold, which a frame without an origin can't have.
   * Up to 200 keys, each value JSON of up to 16 KB, 256 KB in all.
   */
  storage: PersonStorage;
  /** A ticket to one of the app's realtime rooms (join it with y-websocket). */
  room(name: string): Promise<RoomTicket>;
  /** Joins one of the app's rooms for its messages to and from the server. */
  joinRoom(name: string): Promise<RoomConnection>;
  /** A public link to a guest room, when the administrator lets the app take guests. */
  guestLink(room: string, minutes?: number): Promise<GuestLink>;
  /**
   * Reports a guest — by the `guest` ID Leafen gives the app with the
   * guest's room messages — to the administrator; `block` also ends the
   * guest's session and keeps its address out of that link.
   */
  reportGuest(guest: string, options?: { reason?: string; block?: boolean }): Promise<{ blocked: boolean }>;
  /** Opens the provider's sign-in; call it from a click. */
  connectAccount(provider: string): Promise<{ accounts: ConnectedAccount[] }>;
  connectedAccounts(): Promise<{ accounts: ConnectedAccount[] }>;
  disconnectAccount(provider: string): Promise<unknown>;
  /**
   * Asks the person, in Leafen's own window, for a credential the manifest's
   * `credentials` declares — a mail account. The app never sees it: a worker
   * run for the person writes `{{credential:KEY.FIELD}}` to a socket or relay
   * header whose entry names it, and Leafen fills it in.
   */
  connectCredential(key: string): Promise<CredentialList>;
  /** Which credentials the person has given (never their values). */
  credentials(): Promise<CredentialList>;
  /** Removes the person's credential. */
  disconnectCredential(key: string): Promise<CredentialList>;
  /** Moves within the app: Leafen's address bar follows. */
  navigate(path: string): Promise<void>;
  /** The full address of a place in the app, for links outside Leafen's window. */
  link(path?: string): string;
  /** Opens one of Leafen's pages in Leafen itself, leaving the app's screen. */
  openPage(pageId: string): Promise<void>;
  /**
   * Opens an outside page in a new tab after Leafen asks the person; true when
   * they went. An address on the Leafen server itself opens without asking: one
   * of Leafen's screens in place, anything else it serves in a new tab.
   */
  openLink(url: string): Promise<boolean>;
  /** A short message Leafen shows outside the app's frame. */
  notice(message: string): Promise<void>;
  /**
   * Saves the file a person opened with the app (an opener with `edit`) as a
   * new version of its block, while they are the one editing it
   * (`context.file.editing`). If someone saved it since it was opened, it
   * isn't saved: `conflict` names what is there now.
   */
  saveFile(data: Blob | ArrayBuffer | Uint8Array, options?: { name?: string; contentType?: string }): Promise<FileSaveResult>;
  /**
   * Tells Leafen whether the opened file has changes not saved yet. While it
   * has, Leafen asks the person before the app's window or the browser tab
   * closes, so the changes aren't lost without a word.
   */
  setUnsaved(unsaved: boolean): Promise<void>;
  /**
   * Prints a PDF the app made, in Leafen's page (an app's frame can't open
   * a print dialog), after the person has used the page. Up to 96 MB.
   */
  printDocument(pdf: Blob | ArrayBuffer | Uint8Array): Promise<void>;
  /**
   * In the import screen's window: hands Leafen the tool's content as a
   * workspace bundle (docs/bundle.md), up to 5 GB. Leafen checks it, puts it
   * in a workspace of its own and moves it with the person through the
   * migration's plan, run and report; the promise settles once Leafen has
   * taken it. Its losses, in the bundle's descriptor, go to the report.
   */
  importBundle(bundle: Blob): Promise<void>;
  /** Called when Leafen's theme changes; returns a function that stops listening. */
  onTheme(listener: (theme: Theme) => void): () => void;
  /** Called when the person goes back or forward to another place in the app. */
  onLocation(listener: (path: string) => void): () => void;
  /** In a block: keeps new settings in the block, when the person may edit the page. */
  setBlockProps(props: Record<string, unknown>): Promise<void>;
  /** In a block: the frame's height in pixels, 48 to 2000. */
  resize(height: number): Promise<void>;
  /** In a block: called when its settings change, here or by someone else. */
  onProps(listener: (props: Record<string, unknown>) => void): () => void;
  /**
   * In a cell's editor: writes the value in the property's type — text, a
   * number or null, true or false, an option's id or null, or a list of
   * option ids.
   */
  setValue(value: unknown): Promise<void>;
  /** In a cell's editor: closes it. */
  closeEditor(): Promise<void>;
  /** In a cell or its editor: called when the value or options change. */
  onValue(listener: (value: unknown, options: { id: string; name: string; color: string }[]) => void): () => void;
  /** Ends the channel. */
  close(): void;
}

export interface ConnectOptions {
  /** How long to wait for Leafen's answer to the hello. Default 10 seconds. */
  timeoutMs?: number;
}

/** Connects to Leafen from the app's frame. */
export function connect(options: ConnectOptions = {}): Promise<LeafenApp> {
  const nonce = new URLSearchParams(window.location.hash.slice(1)).get("leafen");
  if (!nonce) return Promise.reject(new LeafenError("not_in_leafen: The page wasn't opened by Leafen."));
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      window.removeEventListener("message", onMessage);
      reject(new LeafenError("no_answer: Leafen didn't answer."));
    }, options.timeoutMs ?? 10_000);
    const onMessage = (event: MessageEvent) => {
      const data = event.data as { type?: unknown; context?: unknown } | null;
      if (event.source !== window.parent || data?.type !== "leafen:ready" || !event.ports[0]) return;
      window.clearTimeout(timer);
      window.removeEventListener("message", onMessage);
      const app = channel(event.ports[0], data.context as LeafenContext);
      if (opening) sayWhenDrawn(opening, event.ports[0]);
      resolve(app);
    };
    window.addEventListener("message", onMessage);
    // A page whose first screen is a placeholder marks it data-leafen-opening:
    // Leafen keeps saying the app is opening, where the frame will be, until
    // the app draws over it, instead of showing the placeholder (2026-11-06).
    const opening = document.querySelector("[data-leafen-opening]");
    window.parent.postMessage({ type: "leafen:hello", nonce, opening: Boolean(opening) }, "*");
  });
}

/** Tells Leafen once the page's placeholder has left or changed: the app has drawn its first screen. */
function sayWhenDrawn(placeholder: Element, port: MessagePort) {
  const first = placeholder.textContent;
  const drawn = () => !placeholder.isConnected || placeholder.textContent !== first;
  const say = () => port.postMessage({ type: "drawn" });
  if (drawn()) return say();
  const observer = new MutationObserver(() => {
    if (!drawn()) return;
    observer.disconnect();
    say();
  });
  observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true });
}

function channel(port: MessagePort, given: LeafenContext): LeafenApp {
  const context: LeafenContext = { ...given };
  const waiting = new Map<number, { resolve: (value: unknown) => void; reject: (reason: unknown) => void }>();
  const themeListeners = new Set<(theme: Theme) => void>();
  const locationListeners = new Set<(path: string) => void>();
  const propsListeners = new Set<(props: Record<string, unknown>) => void>();
  const valueListeners = new Set<(value: unknown, options: { id: string; name: string; color: string }[]) => void>();
  let next = 1;
  port.onmessage = (event: MessageEvent) => {
    const message = event.data as {
      id?: unknown; result?: unknown; error?: unknown; type?: unknown; theme?: unknown; path?: unknown; props?: unknown;
      value?: unknown; options?: unknown;
    } | null;
    if (!message) return;
    if (typeof message.id === "number") {
      const call = waiting.get(message.id);
      waiting.delete(message.id);
      if (message.error !== undefined) call?.reject(new LeafenError(String(message.error)));
      else call?.resolve(message.result);
      return;
    }
    if (message.type === "theme" && (message.theme === "light" || message.theme === "dark")) {
      context.theme = message.theme;
      themeListeners.forEach((listener) => listener(context.theme));
    }
    if (message.type === "location" && typeof message.path === "string") {
      context.path = message.path;
      locationListeners.forEach((listener) => listener(context.path));
    }
    if (message.type === "props" && message.props && typeof message.props === "object") {
      const props = message.props as Record<string, unknown>;
      context.props = props;
      propsListeners.forEach((listener) => listener(props));
    }
    if (message.type === "value") {
      context.value = message.value;
      context.options = Array.isArray(message.options) ? message.options as { id: string; name: string; color: string }[] : [];
      valueListeners.forEach((listener) => listener(context.value, context.options ?? []));
    }
  };
  const call = <T>(method: string, params: Record<string, unknown> = {}) => new Promise<T>((resolve, reject) => {
    const id = next++;
    waiting.set(id, { resolve: resolve as (value: unknown) => void, reject });
    port.postMessage({ id, method, params });
  });
  const listen = <L>(set: Set<L>, listener: L) => {
    set.add(listener);
    return () => { set.delete(listener); };
  };
  const api = <T>(method: string, path: string, body?: unknown) =>
    call<ApiResponse<T>>("api", body === undefined ? { method, path } : { method, path, body });
  return {
    context,
    api,
    table: (key) => openTable(api, key),
    action: (name, input) => call("action", { name, input: input ?? {} }),
    relay: (request) => call("relay", { ...request }),
    kv: (request) => call("kv", { ...request }),
    storage: {
      get: async <T>(key: string) => ((await call<{ value?: T }>("storage", { command: "get", key })).value ?? null),
      set: async (key, value) => { await call("storage", { command: "set", key, value }); },
      remove: async (key) => { await call("storage", { command: "remove", key }); },
      keys: async () => (await call<{ keys?: string[] }>("storage", { command: "keys" })).keys ?? [],
    },
    room: (name) => call("room", { name }),
    joinRoom: async (name) => openRoom(await call<RoomTicket>("room", { name })),
    guestLink: (room, minutes) => call("guestLink", minutes === undefined ? { room } : { room, minutes }),
    reportGuest: (guest, options) => call("reportGuest", { guest, reason: options?.reason ?? "", block: options?.block ?? false }),
    connectAccount: (provider) => call("connectAccount", { provider }),
    connectedAccounts: () => call("connectedAccounts"),
    disconnectAccount: (provider) => call("disconnectAccount", { provider }),
    connectCredential: (key) => call("connectCredential", { key }),
    credentials: () => call("credentials"),
    disconnectCredential: (key) => call("disconnectCredential", { key }),
    navigate: async (path) => {
      await call("navigate", { path });
      context.path = path.replace(/^\/+/u, "");
    },
    link: (path = "") => {
      const place = path.replace(/^\/+/u, "");
      return `${context.homeUrl ?? ""}${place ? `/${place}` : ""}`;
    },
    openPage: async (pageId) => { await call("openPage", { pageId }); },
    openLink: (url) => call("openLink", { url }),
    notice: async (message) => { await call("notice", { message }); },
    saveFile: async (data, options) => call<FileSaveResult>("saveFile", {
      bytes: data instanceof Blob ? await data.arrayBuffer() : data instanceof Uint8Array ? data.slice().buffer : data, ...options,
    }),
    setUnsaved: async (unsaved) => { await call("setUnsaved", { unsaved: unsaved === true }); },
    printDocument: async (pdf) => {
      await call("printDocument", { bytes: pdf instanceof Blob ? await pdf.arrayBuffer() : pdf instanceof Uint8Array ? pdf.slice().buffer : pdf });
    },
    importBundle: async (bundle) => { await call("importBundle", { bundle }); },
    onTheme: (listener) => listen(themeListeners, listener),
    onLocation: (listener) => listen(locationListeners, listener),
    setBlockProps: async (props) => {
      await call("blockProps", { props });
      context.props = props;
    },
    resize: async (height) => { await call("resize", { height }); },
    onProps: (listener) => listen(propsListeners, listener),
    setValue: async (value) => {
      await call("setValue", { value });
      context.value = value;
    },
    closeEditor: async () => { await call("closeEditor"); },
    onValue: (listener) => listen(valueListeners, listener),
    close: () => {
      port.close();
      waiting.forEach((pending) => pending.reject(new LeafenError("closed: The channel was closed.")));
      waiting.clear();
    },
  };
}

type Api = <T>(method: string, path: string, body?: unknown) => Promise<ApiResponse<T>>;
type Cell = { value?: unknown; text?: string };
type RowHead = { id: string; position?: string; createdAt?: string; createdBy?: string; updatedAt?: string; lastEditedBy?: string };

function randomId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(12)), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function answered<T>(api: Api, method: string, path: string, body?: unknown): Promise<T> {
  const response = await api<T & { error?: { code?: string; message?: string } }>(method, path, body);
  if (response.status >= 300) {
    const error = response.body?.error;
    throw new LeafenError(`${error?.code ?? `http_${response.status}`}: ${error?.message ?? ""}`);
  }
  return response.body;
}

async function openTable(api: Api, key: string): Promise<Table> {
  const data = await answered<{ items: Array<{ key: string; pageId?: string; dataSourceId?: string; properties?: TableColumn[] }> }>(api, "GET", "/app-data");
  const item = data.items.find((entry) => entry.key === key && entry.dataSourceId);
  if (!item) throw new LeafenError(`no_table: No "${key}" table is set up here yet.`);
  if (!item.pageId || !item.properties?.length) throw new LeafenError(`no_table: The "${key}" table can't be read here.`);
  const pageId = item.pageId, dataSourceId = item.dataSourceId ?? "", columns = item.properties;
  const named = (name: string) => {
    const column = columns.find((candidate) => candidate.name === name);
    if (!column) throw new LeafenError(`no_column: The "${key}" table has no "${name}" column.`);
    return column;
  };
  const optionsOf = (column: TableColumn) => (Array.isArray(column.config.options) ? column.config.options : []) as Array<{ id: string; name: string }>;
  const encode = (values: Record<string, unknown>) => Object.fromEntries(Object.entries(values).map(([name, given]) => {
    const column = named(name);
    let value = given;
    if (value !== null && ["select", "status", "multi_select"].includes(column.type)) {
      const find = (option: unknown) => {
        const found = optionsOf(column).find((candidate) => candidate.name === option);
        if (!found) throw new LeafenError(`no_option: The "${column.name}" column has no "${String(option)}" option.`);
        return found.id;
      };
      value = Array.isArray(value) ? value.map(find) : value === "" ? null : find(value);
    }
    return [column.id, { type: column.type, value }];
  }));
  const row = (head: RowHead, cells: Record<string, Cell>): TableRow => {
    const value = (name: string) => cells[named(name).id]?.value ?? null;
    const options = (name: string) => {
      const kept = value(name);
      const ids = Array.isArray(kept) ? kept : kept === null ? [] : [kept];
      return ids.flatMap((id) => optionsOf(named(name)).filter((option) => option.id === id).map((option) => option.name));
    };
    return {
      id: head.id, position: head.position ?? "", createdAt: head.createdAt ?? "", createdBy: head.createdBy ?? "",
      updatedAt: head.updatedAt ?? "", lastEditedBy: head.lastEditedBy ?? "",
      value,
      text: (name) => {
        const shown = cells[named(name).id]?.text, kept = value(name);
        return typeof shown === "string" ? shown : typeof kept === "string" ? kept : "";
      },
      number: (name) => { const kept = value(name); return typeof kept === "number" ? kept : 0; },
      checkbox: (name) => value(name) === true,
      option: (name) => options(name)[0] ?? "",
      options,
      ids: (name) => { const kept = value(name); return Array.isArray(kept) ? kept.filter((id): id is string => typeof id === "string") : []; },
      date: (name) => { const kept = value(name) as { start?: string; end?: string } | null; return { start: kept?.start ?? "", end: kept?.end ?? "" }; },
    };
  };
  const page = async (query: TableQuery = {}, cursor = "", countOnly = false) => {
    const filters = (query.where ?? []).map((where) => ({ propertyId: named(where.column).id, operator: where.operator, value: where.value ?? null }));
    const answer = await answered<{ rows: Array<RowHead & { values?: Record<string, Cell>; computed?: Record<string, Cell> }>; nextCursor?: string | null; hasMore?: boolean; total?: number | null }>(
      api, "POST", `/pages/${pageId}/database-query`, {
        dataSourceId, limit: query.limit ?? 100, countOnly,
        sorts: (query.sort ?? []).map((sort) => ({ propertyId: named(sort.column).id, direction: sort.descending ? "descending" : "ascending" })),
        ...(filters.length ? { filterGroup: { conjunction: "and", filters, groups: [] } } : {}),
        ...(query.archived ? { archiveScope: "archived" } : {}),
        ...(query.search ? { search: query.search } : {}),
        ...(cursor ? { cursor } : {}),
      });
    return {
      rows: answer.rows.map((item) => row(item, { ...item.values, ...item.computed })),
      next: answer.hasMore ? answer.nextCursor ?? "" : "",
      total: answer.total ?? null,
    };
  };
  const find = async (query: TableQuery = {}, cursor = "") => {
    const { rows, next } = await page(query, cursor);
    return { rows, next };
  };
  return {
    key, pageId, dataSourceId, columns,
    find,
    rows: async function* (query = {}) {
      let cursor = "";
      do {
        const page = await find(query, cursor);
        yield* page.rows;
        cursor = page.next;
      } while (cursor);
    },
    count: async (query = {}) => {
      let cursor = "";
      for (;;) {
        const { next, total } = await page({ ...query, limit: 1 }, cursor, true);
        if (total !== null) return total;
        if (!next) throw new LeafenError("no_count: The query ended without a count.");
        cursor = next;
      }
    },
    row: async (id) => {
      const path = `/database-rows/${encodeURIComponent(id)}/page?limit=1`;
      const response = await api<{ row?: RowHead & { dataSourceId?: string; trashedAt?: string; values?: Record<string, Cell> } }>("GET", path);
      if (response.status === 404) return null;
      const found = (response.status >= 300 ? await answered<typeof response.body>(api, "GET", path) : response.body).row;
      return found && found.dataSourceId === dataSourceId && !found.trashedAt ? row(found, found.values ?? {}) : null;
    },
    add: async (values) => {
      const id = randomId();
      await answered(api, "POST", `/pages/${pageId}/content-graph/mutations`, {
        mutationId: randomId(),
        operations: [{ kind: "upsertRow", row: { id, dataSourceId, position: `t${String(Date.now()).padStart(13, "0")}`, values: encode(values) } }],
      });
      return id;
    },
    update: async (id, values) => {
      await answered(api, "POST", `/database-rows/${encodeURIComponent(id)}/page/values`, { mutationId: randomId(), values: encode(values) });
    },
    updateMany: async (rows) => {
      const ids = Object.keys(rows).sort();
      for (let start = 0; start < ids.length; start += 100) {
        await answered(api, "POST", `/pages/${pageId}/database-row-values`, {
          mutationId: randomId(), rows: ids.slice(start, start + 100).map((rowId) => ({ rowId, values: encode(rows[rowId]) })),
        });
      }
    },
    remove: async (id) => {
      const path = `/database-rows/${encodeURIComponent(id)}/page`;
      const found = (await answered<{ row?: { dataSourceId?: string; trashedAt?: string; authorityRevision?: number } }>(api, "GET", `${path}?limit=1`)).row;
      if (!found || found.dataSourceId !== dataSourceId) throw new LeafenError("no_row: The table has no such row.");
      if (found.trashedAt) return;
      await answered(api, "POST", `${path}/trash`, { requestId: randomId(), expectedAuthorityRevision: found.authorityRevision ?? 0 });
    },
  };
}

// A room's messages to and from the server travel as y-websocket's
// stateless messages: the type 5, the text's length and the text.
const STATELESS = 5;

function varUint(value: number): number[] {
  const bytes: number[] = [];
  while (value > 127) {
    bytes.push((value & 127) | 128);
    value >>>= 7;
  }
  bytes.push(value);
  return bytes;
}

function openRoom(ticket: RoomTicket): Promise<RoomConnection> {
  const socket = new WebSocket(`${ticket.serverUrl}/${ticket.roomName}?ticket=${encodeURIComponent(ticket.ticket)}`);
  socket.binaryType = "arraybuffer";
  const messageListeners = new Set<(message: string) => void>();
  const closeListeners = new Set<() => void>();
  const pending: string[] = [];
  const encoder = new TextEncoder(), decoder = new TextDecoder();
  const frame = (message: string) => {
    const text = encoder.encode(message);
    return new Uint8Array([...varUint(STATELESS), ...varUint(text.length), ...text]);
  };
  socket.onmessage = (event: MessageEvent) => {
    if (!(event.data instanceof ArrayBuffer)) return;
    const bytes = new Uint8Array(event.data);
    if (bytes[0] !== STATELESS) return;
    let index = 1, length = 0, shift = 0;
    for (;;) {
      const byte = bytes[index++];
      length |= (byte & 127) << shift;
      if (byte < 128) break;
      shift += 7;
    }
    const message = decoder.decode(bytes.slice(index, index + length));
    messageListeners.forEach((listener) => listener(message));
  };
  socket.onclose = () => closeListeners.forEach((listener) => listener());
  const listen = <L>(set: Set<L>, listener: L) => {
    set.add(listener);
    return () => { set.delete(listener); };
  };
  return new Promise((resolve, reject) => {
    socket.onopen = () => {
      pending.splice(0).forEach((message) => socket.send(frame(message)));
      resolve({
        send: (message) => {
          if (socket.readyState === WebSocket.OPEN) socket.send(frame(message));
          else pending.push(message);
        },
        onMessage: (listener) => listen(messageListeners, listener),
        onClose: (listener) => listen(closeListeners, listener),
        close: () => socket.close(),
      });
    };
    socket.onerror = () => reject(new LeafenError("room_closed: The room can't be joined."));
  });
}
