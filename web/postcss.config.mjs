import { composedCopiesDropped } from "./scripts/composed-sheets-once.mjs";

// Hover styles apply where the pointer hovers: under (hover: none) a phone's
// tap counts as a hover, and iOS keeps a tap from pressing anything when the
// hover shows something new — a row's tools, a block's handle — so the first
// tap on an inbox row only showed its tools. Every rule's :hover selectors go
// under @media (hover: hover), next to the rule they came from; what a touch
// screen shows of such tools is the [data-touch="true"] rules'
// (src/lib/screenKind.ts). A rule already written under (hover: none) stays
// where it is: it is a touch screen's own, the grey cell a few controls keep
// after a tap as Notion's do (src/app/touchHoverCells.css) — a colour only,
// which iOS does not hold the tap for.
function hoverOnHoverDevices() {
  const moved = new WeakSet();
  const move = (rule, AtRule) => {
    if (moved.has(rule) || !rule.selector.includes(":hover")) return;
    for (let parent = rule.parent; parent; parent = parent.parent) {
      if (parent.type === "atrule" && /(?:^|[\s(])hover:\s*(?:hover|none)/u.test(parent.params)) return;
    }
    // ":not(:hover)" is what is not hovered, a touch screen's every element too.
    const hovering = rule.selectors.filter((selector) => selector.replace(/:not\([^()]*:hover[^()]*\)/gu, "").includes(":hover"));
    if (!hovering.length) return;
    const copy = rule.clone({ selectors: hovering });
    moved.add(copy);
    const media = new AtRule({ name: "media", params: "(hover: hover)" });
    media.append(copy);
    rule.after(media);
    const rest = rule.selectors.filter((selector) => !hovering.includes(selector));
    if (rest.length) rule.selectors = rest;
    else rule.remove();
  };
  return {
    postcssPlugin: "leafen-hover-on-hover-devices",
    // The whole sheet as it leaves, after the CSS modules step has made its
    // own changes, so nothing that step brings in ships untransformed.
    OnceExit(root, { AtRule }) {
      root.walkRules((rule) => move(rule, AtRule));
    },
  };
}
hoverOnHoverDevices.postcss = true;

// Heights follow the window a person sees: an iPhone home screen app measures
// 100vh as the whole screen, status bar too (874px for an 812px window, seen
// 2026-10-26), so the app ran past its window, the page scrolled, and the
// sidebar's top and the chat box's bottom were cut. Every vh length is a dvh
// one, which every browser the build targets knows and which is the window's
// height there; elsewhere the two are the same.
function heightsOfTheWindow() {
  return {
    postcssPlugin: "leafen-heights-of-the-window",
    // The whole sheet as it leaves (as above).
    OnceExit(root) {
      root.walkDecls((declaration) => {
        if (!/\dvh\b/u.test(declaration.value) || declaration.value.includes("url(")) return;
        declaration.value = declaration.value.replace(/(\d)vh\b/gu, "$1dvh");
      });
    },
  };
}
heightsOfTheWindow.postcss = true;

// The keyboard's ring has two marks: the browser's :focus-visible, and the
// keys' own, data-key-focus (src/lib/lastPress.ts), on the focused control
// where keys lead and the browser draws none — a control the pointer focused
// that a key then reaches (Escape after its menu), the opener a press outside
// gives the focus back to after typing, a focus code gives in WebKit. A
// control looks the same under either: every :focus-visible matches the keys'
// mark on the focused element too, at its own weight (:where), so a rule for
// the ring — its pointer ground, its own edge, the tools it shows — is
// written once (2026-11-24: 121 database sheets wrote their pointer ground
// under :focus-visible alone, and a ring the keys drew came without it, where
// Notion's draws both). No :has() asks for it: a row drawn while the keys'
// focus is in it asks its own mark, data-keys-within (lib/lastPress.ts
// keysFocusHolder), and the build refuses :focus-visible inside :has()
// (scripts/shipped-bundle-rules.mjs) — the keys' mark there, which moves with
// every focus the keys lead, had Chrome work out every :has() in the page at
// each (2026-11-15: 2.5 ms a focus move in a table inside a toggle, 0.4
// without), and the browser's alone missed the keys' focus WebKit gives no
// :focus-visible. One left there stays as written.
const KEYS_RING = ":is(:focus-visible, :where([data-key-focus]):focus)";
function keysRingAsFocusVisible() {
  const name = ":focus-visible";
  const rewrite = (selector) => {
    let out = "", depth = 0;
    const has = [];
    for (let at = 0; at < selector.length;) {
      if (selector.startsWith(":has(", at)) { has.push(depth++); out += ":has("; at += 5; continue; }
      const char = selector[at];
      if (char === "(") depth++;
      else if (char === ")" && --depth === has.at(-1)) has.pop();
      if (!has.length && selector.startsWith(name, at) && !/[\w-]/u.test(selector[at + name.length] ?? "")
        && !(out.endsWith(":is(") && selector.startsWith(KEYS_RING.slice(4 + name.length), at + name.length))) {
        out += KEYS_RING;
        at += name.length;
        continue;
      }
      out += char;
      at++;
    }
    return out;
  };
  return {
    postcssPlugin: "leafen-keys-ring-as-focus-visible",
    // The whole sheet as it leaves (as above).
    OnceExit(root) {
      root.walkRules((rule) => {
        if (rule.selector.includes(name)) rule.selector = rewrite(rule.selector);
      });
    },
  };
}
keysRingAsFocusVisible.postcss = true;

// First the copies of composed files leave the sheet (composed-sheets-once.mjs).
export default { plugins: [composedCopiesDropped(), keysRingAsFocusVisible(), hoverOnHoverDevices(), heightsOfTheWindow()] };
