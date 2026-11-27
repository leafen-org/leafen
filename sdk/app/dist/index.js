// Leafen app SDK for an app's screen (Apache-2.0, see ../LICENSE).
//
// An app's screen runs in a frame with no origin. Leafen puts a one-time
// value in the frame's address (#leafen=…); `connect()` says hello with it
// and receives a private message port, through which every call goes.
// Build the screen as a single-page app: the channel belongs to one document
// and closes if the frame goes to another page.
/** A call Leafen refused or that failed: `code` is Leafen's error code when it gave one. */
export class LeafenError extends Error {
    constructor(text) {
        const [code, ...rest] = text.split(": ");
        super(rest.length ? rest.join(": ") || code : text);
        this.name = "LeafenError";
        this.code = rest.length ? code : text;
    }
}
/** Connects to Leafen from the app's frame. */
export function connect(options = {}) {
    const nonce = new URLSearchParams(window.location.hash.slice(1)).get("leafen");
    if (!nonce)
        return Promise.reject(new LeafenError("not_in_leafen: The page wasn't opened by Leafen."));
    return new Promise((resolve, reject) => {
        const timer = window.setTimeout(() => {
            window.removeEventListener("message", onMessage);
            reject(new LeafenError("no_answer: Leafen didn't answer."));
        }, options.timeoutMs ?? 10000);
        const onMessage = (event) => {
            const data = event.data;
            if (event.source !== window.parent || data?.type !== "leafen:ready" || !event.ports[0])
                return;
            window.clearTimeout(timer);
            window.removeEventListener("message", onMessage);
            const app = channel(event.ports[0], data.context);
            if (opening)
                sayWhenDrawn(opening, event.ports[0]);
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
function sayWhenDrawn(placeholder, port) {
    const first = placeholder.textContent;
    const drawn = () => !placeholder.isConnected || placeholder.textContent !== first;
    const say = () => port.postMessage({ type: "drawn" });
    if (drawn())
        return say();
    const observer = new MutationObserver(() => {
        if (!drawn())
            return;
        observer.disconnect();
        say();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true });
}
function channel(port, given) {
    const context = { ...given };
    const waiting = new Map();
    const themeListeners = new Set();
    const locationListeners = new Set();
    const propsListeners = new Set();
    const valueListeners = new Set();
    let next = 1;
    port.onmessage = (event) => {
        const message = event.data;
        if (!message)
            return;
        if (typeof message.id === "number") {
            const call = waiting.get(message.id);
            waiting.delete(message.id);
            if (message.error !== undefined)
                call?.reject(new LeafenError(String(message.error)));
            else
                call?.resolve(message.result);
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
            const props = message.props;
            context.props = props;
            propsListeners.forEach((listener) => listener(props));
        }
        if (message.type === "value") {
            context.value = message.value;
            context.options = Array.isArray(message.options) ? message.options : [];
            valueListeners.forEach((listener) => listener(context.value, context.options ?? []));
        }
    };
    const call = (method, params = {}) => new Promise((resolve, reject) => {
        const id = next++;
        waiting.set(id, { resolve: resolve, reject });
        port.postMessage({ id, method, params });
    });
    const listen = (set, listener) => {
        set.add(listener);
        return () => { set.delete(listener); };
    };
    const api = (method, path, body) => call("api", body === undefined ? { method, path } : { method, path, body });
    return {
        context,
        api,
        table: (key) => openTable(api, key),
        action: (name, input) => call("action", { name, input: input ?? {} }),
        relay: (request) => call("relay", { ...request }),
        kv: (request) => call("kv", { ...request }),
        storage: {
            get: async (key) => ((await call("storage", { command: "get", key })).value ?? null),
            set: async (key, value) => { await call("storage", { command: "set", key, value }); },
            remove: async (key) => { await call("storage", { command: "remove", key }); },
            keys: async () => (await call("storage", { command: "keys" })).keys ?? [],
        },
        room: (name) => call("room", { name }),
        joinRoom: async (name) => openRoom(await call("room", { name })),
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
        saveFile: async (data, options) => call("saveFile", {
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
function randomId() {
    return Array.from(crypto.getRandomValues(new Uint8Array(12)), (byte) => byte.toString(16).padStart(2, "0")).join("");
}
async function answered(api, method, path, body) {
    const response = await api(method, path, body);
    if (response.status >= 300) {
        const error = response.body?.error;
        throw new LeafenError(`${error?.code ?? `http_${response.status}`}: ${error?.message ?? ""}`);
    }
    return response.body;
}
async function openTable(api, key) {
    const data = await answered(api, "GET", "/app-data");
    const item = data.items.find((entry) => entry.key === key && entry.dataSourceId);
    if (!item)
        throw new LeafenError(`no_table: No "${key}" table is set up here yet.`);
    if (!item.pageId || !item.properties?.length)
        throw new LeafenError(`no_table: The "${key}" table can't be read here.`);
    const pageId = item.pageId, dataSourceId = item.dataSourceId ?? "", columns = item.properties;
    const named = (name) => {
        const column = columns.find((candidate) => candidate.name === name);
        if (!column)
            throw new LeafenError(`no_column: The "${key}" table has no "${name}" column.`);
        return column;
    };
    const optionsOf = (column) => (Array.isArray(column.config.options) ? column.config.options : []);
    const encode = (values) => Object.fromEntries(Object.entries(values).map(([name, given]) => {
        const column = named(name);
        let value = given;
        if (value !== null && ["select", "status", "multi_select"].includes(column.type)) {
            const find = (option) => {
                const found = optionsOf(column).find((candidate) => candidate.name === option);
                if (!found)
                    throw new LeafenError(`no_option: The "${column.name}" column has no "${String(option)}" option.`);
                return found.id;
            };
            value = Array.isArray(value) ? value.map(find) : value === "" ? null : find(value);
        }
        return [column.id, { type: column.type, value }];
    }));
    const row = (head, cells) => {
        const value = (name) => cells[named(name).id]?.value ?? null;
        const options = (name) => {
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
            ids: (name) => { const kept = value(name); return Array.isArray(kept) ? kept.filter((id) => typeof id === "string") : []; },
            date: (name) => { const kept = value(name); return { start: kept?.start ?? "", end: kept?.end ?? "" }; },
        };
    };
    const page = async (query = {}, cursor = "", countOnly = false) => {
        const filters = (query.where ?? []).map((where) => ({ propertyId: named(where.column).id, operator: where.operator, value: where.value ?? null }));
        const answer = await answered(api, "POST", `/pages/${pageId}/database-query`, {
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
    const find = async (query = {}, cursor = "") => {
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
                if (total !== null)
                    return total;
                if (!next)
                    throw new LeafenError("no_count: The query ended without a count.");
                cursor = next;
            }
        },
        row: async (id) => {
            const path = `/database-rows/${encodeURIComponent(id)}/page?limit=1`;
            const response = await api("GET", path);
            if (response.status === 404)
                return null;
            const found = (response.status >= 300 ? await answered(api, "GET", path) : response.body).row;
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
            const found = (await answered(api, "GET", `${path}?limit=1`)).row;
            if (!found || found.dataSourceId !== dataSourceId)
                throw new LeafenError("no_row: The table has no such row.");
            if (found.trashedAt)
                return;
            await answered(api, "POST", `${path}/trash`, { requestId: randomId(), expectedAuthorityRevision: found.authorityRevision ?? 0 });
        },
    };
}
// A room's messages to and from the server travel as y-websocket's
// stateless messages: the type 5, the text's length and the text.
const STATELESS = 5;
function varUint(value) {
    const bytes = [];
    while (value > 127) {
        bytes.push((value & 127) | 128);
        value >>>= 7;
    }
    bytes.push(value);
    return bytes;
}
function openRoom(ticket) {
    const socket = new WebSocket(`${ticket.serverUrl}/${ticket.roomName}?ticket=${encodeURIComponent(ticket.ticket)}`);
    socket.binaryType = "arraybuffer";
    const messageListeners = new Set();
    const closeListeners = new Set();
    const pending = [];
    const encoder = new TextEncoder(), decoder = new TextDecoder();
    const frame = (message) => {
        const text = encoder.encode(message);
        return new Uint8Array([...varUint(STATELESS), ...varUint(text.length), ...text]);
    };
    socket.onmessage = (event) => {
        if (!(event.data instanceof ArrayBuffer))
            return;
        const bytes = new Uint8Array(event.data);
        if (bytes[0] !== STATELESS)
            return;
        let index = 1, length = 0, shift = 0;
        for (;;) {
            const byte = bytes[index++];
            length |= (byte & 127) << shift;
            if (byte < 128)
                break;
            shift += 7;
        }
        const message = decoder.decode(bytes.slice(index, index + length));
        messageListeners.forEach((listener) => listener(message));
    };
    socket.onclose = () => closeListeners.forEach((listener) => listener());
    const listen = (set, listener) => {
        set.add(listener);
        return () => { set.delete(listener); };
    };
    return new Promise((resolve, reject) => {
        socket.onopen = () => {
            pending.splice(0).forEach((message) => socket.send(frame(message)));
            resolve({
                send: (message) => {
                    if (socket.readyState === WebSocket.OPEN)
                        socket.send(frame(message));
                    else
                        pending.push(message);
                },
                onMessage: (listener) => listen(messageListeners, listener),
                onClose: (listener) => listen(closeListeners, listener),
                close: () => socket.close(),
            });
        };
        socket.onerror = () => reject(new LeafenError("room_closed: The room can't be joined."));
    });
}
