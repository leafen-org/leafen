// A line of Markdown as Leafen's runs: what every Markdown tool reads alike
// — code, bold, italics, strikes, web links and addresses, escapes — and,
// through `patterns` and `handle`, what a tool reads its own way: its links
// between notes, embeds, highlights, references and tags.

const COMMON = [
  ["code", /^`([^`]+)`/u], ["image", /^!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/u], ["link", /^\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/u],
  ["autolink", /^<(https?:\/\/[^>]+)>/u], ["url", /^https?:\/\/[^\s<>)\]]+/u], ["bold", /^(\*\*|__)(?=\S)([\s\S]+?\S)\1/u],
  ["strike", /^~~(?=\S)([\s\S]+?\S)~~/u], ["italic", /^([*_])(?=\S)([\s\S]+?\S)\1/u], ["escape", /^\\([\\`*_{}[\]()#+\-.!|=~$^])/u],
];

/**
 * The runs of a line. A tool's `patterns` ([name, regexp at the start]) are
 * tried before the common ones; `handle(name, found, marks, at)` takes what
 * they match — and a link to something that isn't a web address, and a
 * picture — with `at.add(text, marks)` and `at.walk(text, marks)`, and
 * answers true when it did. An underscore inside a word stays the word's.
 */
export function markdownRuns(source, { patterns = [], handle = () => false } = {}) {
  const runs = [];
  const add = (insert, attributes) => {
    if (!insert) return;
    const last = runs.at(-1);
    if (last && JSON.stringify(last.attributes ?? {}) === JSON.stringify(attributes ?? {})) last.insert += insert;
    else runs.push(attributes && Object.keys(attributes).length ? { insert, attributes } : { insert });
  };
  const all = [...patterns, ...COMMON];
  const walk = (text, marks) => {
    let at = 0;
    let plain = "";
    const flush = () => { add(plain, marks); plain = ""; };
    while (at < text.length) {
      const rest = text.slice(at);
      let matched = null;
      const inWord = rest[0] === "_" && /[\p{L}\p{N}]/u.test(text[at - 1] ?? "");
      if (!inWord && !/^[\p{L}\p{N}]/u.test(rest[0]) || rest.startsWith("http")) {
        for (const [name, pattern] of all) {
          const found = pattern.exec(rest);
          if (found) { matched = [name, found]; break; }
        }
      }
      if (!matched) { plain += rest[0]; at += 1; continue; }
      const [name, found] = matched;
      flush();
      if (!handle(name, found, marks, { add, walk })) {
        switch (name) {
          case "code": add(found[1], { ...marks, code: true }); break;
          case "image": add(found[1] || found[2], marks); break;
          case "link":
            if (/^(https?|mailto):/iu.test(found[2])) walk(found[1], { ...marks, link: found[2] });
            else walk(found[1], marks);
            break;
          case "autolink": add(found[1], { ...marks, link: found[1] }); break;
          case "url": add(found[0], { ...marks, link: found[0] }); break;
          case "bold": walk(found[2], { ...marks, bold: true }); break;
          case "strike": walk(found[1], { ...marks, strikethrough: true }); break;
          case "italic": walk(found[2], { ...marks, italic: true }); break;
          case "escape": add(found[1], marks); break;
          default: add(found[0], marks);
        }
      }
      at += found[0].length;
    }
    flush();
  };
  walk(source, {});
  if (runs.length) {
    runs[0].insert = runs[0].insert.replace(/^\s+/u, "");
    runs[runs.length - 1].insert = runs[runs.length - 1].insert.replace(/\s+$/u, "");
  }
  return runs.filter((run) => run.insert.length > 0);
}
