// An address a built-in app's screen gives an outside program that takes
// only https — a Bitwarden app, Windows Explorer, a clipper — and the next
// step while Leafen has none: an https address comes from Settings › Server ›
// Access from outside (Tailscale's free public address needs no domain,
// certificate or port), and once it is on the app's addresses are at it (its
// homeUrl, which Leafen gives as the address from outside). Only the
// server's administrator turns it on, so they get the way there and anyone
// else hears who can; Leafen tells its own apps which the person is
// (context.administrator), never an app of someone else's.

const words = {
  ko: {
    administrator: "설정 › 서버 › 외부에서 접속에서 Tailscale(무료 공개 주소)을 켜면 리펜에 https 주소가 생기고, 이 주소도 그것으로 바뀝니다.",
    member: "서버 관리자가 설정 › 서버 › 외부에서 접속을 켜면 리펜에 https 주소가 생기고, 이 주소도 그것으로 바뀝니다. 서버 관리자에게 부탁하세요.",
    turnOn: "외부 접속 켜기",
  },
  en: {
    administrator: "Turn on Tailscale (free public address) under Settings › Server › Access from outside: Leafen gets an https address, and this one changes to it.",
    member: "Once a server administrator turns on Settings › Server › Access from outside, Leafen has an https address and this one changes to it. Ask them to.",
    turnOn: "Turn on outside access",
  },
};

/** Whether an address is https, all these programs take. */
export function httpsAddress(address) {
  try {
    return new URL(address).protocol === "https:";
  } catch {
    return false;
  }
}

/** Whether an address is https at a name the internet reaches: a clipper that sends only there refuses a private one. */
export function publicHttpsAddress(address) {
  if (!httpsAddress(address)) return false;
  const { hostname } = new URL(address);
  if (!hostname.includes(".")) return false;
  if (/\.(local|localhost|localdomain|internal|lan|home|corp)$/u.test(hostname)) return false;
  return !/^(10|127|0)\.|^192\.168\.|^172\.(1[6-9]|2\d|3[01])\.|^169\.254\.|^\[?(::1|f[cd]|fe80)/u.test(hostname);
}

/**
 * The note under such an address: what the program takes, in the app's own
 * words, then the next step — for the server's administrator the setting,
 * opened over the app and closed back to it.
 */
export function outsideAddressNote(app, h, takes, attributes = {}) {
  const w = String(app.context.locale || "").startsWith("ko") ? words.ko : words.en;
  const administrator = app.context.administrator === true;
  const origin = new URL(app.context.homeUrl).origin;
  const setting = `${origin}/admin/settings?section=tunnel&from=${encodeURIComponent(`/apps/${app.context.app.slug}`)}`;
  return h("p", { class: "warning", "data-outside-address-note": administrator ? "administrator" : "member", ...attributes },
    takes, " ", administrator ? w.administrator : w.member,
    administrator ? [" ", h("button", { class: "text", type: "button", "data-outside-access-turn-on": true,
      on: { click: () => void app.openLink(setting) } }, w.turnOn)] : null);
}
