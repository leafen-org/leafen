// Team Quiz's screen: pick a table, sit with a name, and play the referee's
// game. A guest sits at the table their link names. The referee — the
// app's worker, one for each table — holds the game; this screen sends
// what the player does and draws what the table sees.
import { connect } from "./leafen-app-sdk.js";

const app = await connect();
const ko = String(app.context.locale || "").startsWith("ko");
const w = ko ? {
  title: "팀 퀴즈", lead: "팀이 넣은 문제로 함께 겨룹니다. 문제는 앱 워크스페이스의 “퀴즈 문제”에 더하세요.", example: "예시",
  table: (n) => `테이블 ${n}`, name: "이름", sit: "앉기", start: "시작", leave: "나가기", waiting: "시작을 기다리는 중",
  question: (n, total) => `문제 ${n} / ${total}`, answered: (n, total) => `${n}/${total}명 답함`, right: "맞았습니다!", wrong: "아쉬워요",
  ranking: "순위", points: (n) => `${n}점`, invite: "게스트 초대 링크", inviteFail: "게스트를 받으려면 관리자가 이 앱의 게스트를 켜야 합니다.",
  noQuestions: "문제가 없습니다. “퀴즈 문제”에 보기 넷과 정답 번호가 있는 문제를 더하세요.", lost: "연결이 끊겼습니다. 다시 앉으세요.", guest: "게스트",
  block: "내보내기", blockSure: "이 게스트를 내보내고 이 링크로 다시 못 들어오게 할까요?", blockYes: "내보내기", blockNo: "취소", blockReason: "퀴즈 테이블에서 내보냄",
  full: "이 테이블이 가득 찼습니다(사람 200명, 그중 게스트 100명까지). 다른 테이블에 앉거나 자리가 나면 다시 앉으세요.",
  nearlyFull: (n) => `테이블이 곧 가득 찹니다: ${n}/200명`,
} : {
  title: "Team Quiz", lead: "Play with the questions your team adds to “Quiz questions” in the app's workspace.", example: "Example",
  table: (n) => `Table ${n}`, name: "Name", sit: "Sit down", start: "Start", leave: "Leave", waiting: "Waiting to start",
  question: (n, total) => `Question ${n} of ${total}`, answered: (n, total) => `${n} of ${total} answered`, right: "Right!", wrong: "Not this time",
  ranking: "Ranking", points: (n) => `${n} points`, invite: "Guest link", inviteFail: "An administrator turns on guests for this app first.",
  noQuestions: "No questions yet: add some with four choices and the right number to “Quiz questions”.", lost: "Disconnected. Sit down again.", guest: "Guest",
  block: "Remove", blockSure: "Remove this guest and keep them from coming back by this link?", blockYes: "Remove", blockNo: "Cancel", blockReason: "Removed from a quiz table",
  full: "This table is full (200 people, up to 100 of them guests). Sit at another table, or again when a seat frees up.",
  nearlyFull: (n) => `The table is nearly full: ${n} of 200`,
};
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
document.title = w.title;

const root = document.getElementById("app");
// A table seats as many as the manifest's realtime.maxParticipants; Leafen
// refuses one more with LEAFEN_APP_ROOM_FULL.
const seats = 200;
const tables = ["table-1", "table-2", "table-3", "table-4"];
const me = Array.from(crypto.getRandomValues(new Uint8Array(12)), (byte) => byte.toString(16).padStart(2, "0")).join("");
const guestRoom = app.context.guest?.room;
let table = guestRoom || tables[0];
let name = "";
let room = null;
let state = null;
let chosen = 0;
let message = "";
let notice = "";
let invite = "";

function h(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attributes)) {
    if (key === "on") for (const [event, handler] of Object.entries(value)) node.addEventListener(event, handler);
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(key, value === true ? "" : String(value));
  }
  node.append(...children.flat().filter((child) => child !== null && child !== undefined && child !== false));
  return node;
}

function send(payload) {
  notice = "";
  room?.send(JSON.stringify({ id: me, ...payload }));
}

async function sit() {
  message = "";
  try {
    room = await app.joinRoom(table);
  } catch (error) {
    message = String(error?.message || error);
    if (message.includes("LEAFEN_APP_ROOM_FULL")) message = w.full;
    render();
    return;
  }
  room.onMessage((text) => {
    let next;
    try { next = JSON.parse(text); } catch { return; }
    if (next.t !== "state") return;
    if (state?.question?.n !== next.question?.n || next.phase === "lobby") chosen = 0;
    state = next;
    // A referee that started again doesn't know us yet.
    if (!next.players.some((player) => player.id === me)) send({ t: "hello", name });
    render();
  });
  room.onClose(() => { room = null; state = null; message = w.lost; render(); });
  send({ t: "hello", name });
  render();
}

function leave() {
  send({ t: "bye" });
  room?.close();
  room = null;
  state = null;
  render();
}

async function makeInvite() {
  try {
    const made = await app.guestLink(table, 120);
    invite = made.url;
  } catch {
    invite = "";
    notice = w.inviteFail;
  }
  render();
}

function lobby() {
  const input = h("input", { name: "name", value: name, required: true, autocomplete: "nickname" });
  input.addEventListener("input", () => { name = input.value; });
  return [
    guestRoom ? null : h("div", { class: "tables", role: "group", "aria-label": w.title }, tables.map((key, index) => h("button", {
      type: "button", "aria-pressed": key === table ? "true" : "false", "data-table": key,
      on: { click: () => { table = key; render(); } },
    }, w.table(index + 1)))),
    h("label", {}, w.name, input),
    h("div", { class: "row" }, h("button", { class: "primary", type: "button", "data-sit": true, on: { click: () => {
      name = input.value.trim();
      if (name) void sit();
      else input.focus();
    } } }, w.sit)),
  ];
}

// A signed-in person may remove a guest from the table; Leafen keeps the
// guest out of the link and tells the administrator.
let removing = "";
async function removeGuest(guest) {
  removing = "";
  try {
    await app.reportGuest(guest, { reason: w.blockReason, block: true });
    send({ t: "remove", guest });
  } catch (error) {
    message = String(error?.message || error);
  }
  render();
}

function players() {
  return h("ul", { class: "players", "data-players": true }, (state?.players ?? []).map((player) => h("li", {
    "data-answered": player.answered ? "true" : "false",
  }, `${player.name}${state.phase === "lobby" ? "" : ` · ${w.points(player.score)}`}`,
  !guestRoom && player.guest ? (removing === player.guest
    ? h("span", { class: "remove", role: "group" }, ` ${w.blockSure} `,
      h("button", { class: "secondary", type: "button", "data-guest-remove-yes": player.guest, on: { click: () => void removeGuest(player.guest) } }, w.blockYes),
      h("button", { class: "secondary", type: "button", on: { click: () => { removing = ""; render(); } } }, w.blockNo))
    : h("button", { class: "secondary small", type: "button", "data-guest-remove": player.guest,
      on: { click: () => { removing = player.guest; render(); } } }, w.block)) : null)));
}

function seated() {
  const at = guestRoom ? "" : ` · ${w.table(tables.indexOf(table) + 1)}`;
  if (!state || state.phase === "lobby") {
    return [
      h("p", { class: "meta" }, `${w.waiting}${at}`),
      (state?.players.length ?? 0) >= seats * 0.9 ? h("p", { class: "meta", "data-nearly-full": true }, w.nearlyFull(state.players.length)) : null,
      players(),
      state?.notice === "noQuestions" ? h("p", { class: "error", role: "alert" }, w.noQuestions) : null,
      h("div", { class: "row" },
        h("button", { class: "primary", type: "button", "data-start": true, on: { click: () => send({ t: "start" }) } }, w.start),
        guestRoom ? null : h("button", { class: "secondary", type: "button", "data-invite": true, on: { click: () => void makeInvite() } }, w.invite),
        h("button", { class: "secondary", type: "button", on: { click: leave } }, w.leave)),
      invite ? h("p", { class: "link", "data-invite-link": true }, invite) : null,
    ];
  }
  if (state.phase === "end") {
    return [h("h2", {}, w.ranking), h("ol", { class: "ranking", "data-ranking": true },
      (state.ranking ?? []).map((entry) => h("li", {}, `${entry.name} · ${w.points(entry.score)}`)))];
  }
  const question = state.question;
  const reveal = state.phase === "reveal";
  const mine = state.players.find((player) => player.id === me);
  const answered = state.players.filter((player) => player.answered).length;
  const left = Math.max(0, Math.min(100, (state.remainingMs / 20000) * 100));
  return [
    h("p", { class: "meta" }, `${w.question(question.n + 1, question.total)} · ${w.answered(answered, state.players.length)}`),
    h("div", { class: "timer", "aria-hidden": "true" }, h("span", { style: `width:${reveal ? 0 : left}%` })),
    h("p", { class: "question", "data-question": question.n }, question.text),
    h("div", { class: "choices" }, question.choices.map((choice, index) => h("button", {
      type: "button", "data-choice": index + 1, "aria-pressed": chosen === index + 1 ? "true" : "false",
      "data-right": reveal && state.answer === index + 1 ? "true" : false,
      "data-wrong": reveal && chosen === index + 1 && state.answer !== index + 1 ? "true" : false,
      disabled: reveal || chosen !== 0,
      on: { click: () => { chosen = index + 1; send({ t: "answer", q: question.n, choice: chosen }); render(); } },
    }, `${index + 1}. ${choice}`))),
    reveal ? h("p", { class: "meta", role: "status", "data-verdict": mine?.correct ? "right" : "wrong" }, mine?.correct ? w.right : w.wrong) : null,
    players(),
  ];
}

function render() {
  root.replaceChildren(...[
    h("h1", {}, w.title, h("span", { class: "badge" }, w.example)),
    h("p", { class: "lead" }, guestRoom ? `${w.guest} · ${w.table(tables.indexOf(guestRoom) + 1)}` : w.lead),
    message ? h("p", { class: "error", role: "alert" }, message) : null,
    ...(room ? seated() : lobby()),
    notice ? h("p", { class: "status", role: "status" }, notice) : null,
  ].filter(Boolean));
}

// The person's own name when they are signed in: who they are from the
// smallest window, then they alone by id, wherever they come by name.
if (!guestRoom) {
  try {
    const first = await app.api("GET", "/app-people?limit=1");
    const you = first.body?.you ?? "";
    const people = you ? await app.api("GET", `/app-people?ids=${encodeURIComponent(you)}`) : null;
    name = people?.body?.people?.find((person) => person.id === you)?.name ?? "";
  } catch {
    name = "";
  }
}
render();
