// What an exported page's HTML draws apart from its body: a row page's
// properties, and the page's comments.

/** An exported row page's properties table, which its database's CSV gives the row (withoutRowProperties). */
export function withoutRowPropertyTable(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const table = document.querySelector("header table.properties");
  if (!table) return html;
  table.remove();
  return document.documentElement.outerHTML;
}

/**
 * Notion's HTML export with its comments ("댓글 내보내기", seen 2026-11-22)
 * draws them after the page's article, in an inline element of their own: a
 * divider, a <details> of the page's comments and one of its inline comments,
 * each discussion a list of comments — the writer's initial, name and time,
 * then the words — an inline one after the words it was left on ("블록 텍스트:
 * <mark>…</mark>"), a divider after each; its Markdown export has none.
 * Notion's own import keeps them in the page, as no discussion (seen
 * 2026-11-22 by the command session through its MCP fetch, the same export):
 * under a divider at the body's end, the two toggles, their titles bold; each
 * comment an item of a bulleted list, its line the initial as a letter, the
 * name bold and the time, its words a paragraph under it, formatted as
 * written; an inline discussion after its "**블록 텍스트**: words" line, the
 * mark's color gone; a divider after each discussion. So the section comes
 * into the page's body as those blocks — where before, inline, it read as
 * one long last line.
 */
export function withExportedCommentsAsBlocks(html: string) {
  if (!html.includes("</article>") || !html.includes("<details")) return html;
  const document = new DOMParser().parseFromString(html, "text/html");
  const body = document.querySelector("body > article.page > div.page-body");
  let moved = false;
  for (let element = body?.parentElement?.nextElementSibling; element; ) {
    const after = element.nextElementSibling;
    const comments = Array.from(element.querySelectorAll("details ul.toggle > li"))
      .filter((comment) => comment.querySelector(".user") && comment.querySelector("time"));
    if (comments.length > 0) {
      for (const comment of comments) commentItem(comment);
      for (const summary of Array.from(element.querySelectorAll("details > summary"))) {
        const title = document.createElement("b");
        title.append(...Array.from(summary.childNodes));
        summary.replaceChildren(title);
      }
      element.querySelectorAll("details mark").forEach((mark) => mark.replaceWith(...Array.from(mark.childNodes)));
      const section = document.createElement("div");
      section.append(...Array.from(element.childNodes));
      body!.append(section);
      element.remove();
      moved = true;
    }
    element = after;
  }
  return moved ? document.documentElement.outerHTML : html;
}

/** A comment's item as Notion's import reads it: the writer's initial, name and time, its words a paragraph under them. */
function commentItem(comment: Element) {
  const [writer, words] = Array.from(comment.children);
  const document = comment.ownerDocument;
  writer?.querySelectorAll(".user-icon").forEach((icon) => icon.replaceWith(document.createTextNode(icon.textContent ?? "")));
  const user = writer?.querySelector(".user");
  const line = Array.from(user?.childNodes ?? writer?.childNodes ?? [])
    .flatMap((part) => part instanceof Element && part.tagName === "SPAN" ? Array.from(part.childNodes) : [part]);
  const paragraph = document.createElement("p");
  paragraph.append(...Array.from(words?.childNodes ?? []));
  comment.replaceChildren(...line, paragraph);
}
