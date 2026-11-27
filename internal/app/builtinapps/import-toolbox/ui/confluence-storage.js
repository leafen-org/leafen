// A Confluence page's storage format (the XHTML its REST API gives, with
// `ac:` macros and `ri:` resources) made into the HTML a space export has,
// so one page converter reads both: panels, code, expands, status, task
// lists, pictures and attachments, page links, user mentions, emoticons,
// layouts and the new editor's panels; other macros stay named so the
// converter counts them.

const PANEL_KINDS = { info: "information", tip: "tip", note: "note", warning: "warning" };
const ADF_PANELS = { info: "information", note: "note", success: "tip", warning: "warning", error: "warning" };
const EMOTICONS = {
  smile: "🙂", sad: "🙁", cheeky: "😛", laugh: "😄", wink: "😉", "thumbs-up": "👍", "thumbs-down": "👎",
  information: "ℹ️", tick: "✅", cross: "❌", warning: "⚠️", plus: "➕", minus: "➖", question: "❓",
  "light-on": "💡", "light-off": "💡", "yellow-star": "⭐", "red-star": "⭐", "green-star": "⭐", "blue-star": "⭐", heart: "❤️",
};

const VOID = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"]);
const escape = (text) => text.replace(/&/gu, "&amp;").replace(/</gu, "&lt;").replace(/>/gu, "&gt;");

/** The people a storage-format page mentions, by account id. */
export function mentionedAccounts(storage) {
  return [...storage.matchAll(/ri:account-id="([^"]+)"/gu)].map((match) => match[1]);
}

/**
 * The export-shaped HTML of a page's storage format. `links` resolves what it
 * points at: `page(title, spaceKey)` to a page's file name in the space or
 * "", `attachment(filename)` to an attachment's path or "", `user(accountId)`
 * to a person's name or "".
 */
export function storageToExportHtml(storage, links) {
  // The HTML parser keeps neither CDATA nor the "/>" of an element that
  // isn't void (<time/>, <ri:page/>), which would take in all that follows.
  const source = storage
    .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/gu, (_, text) => escape(text))
    .replace(/<([a-zA-Z][\w:-]*)((?:\s+[^<>]*?)?)\s*\/>/gu, (_, tag, attributes) =>
      VOID.has(tag.toLowerCase()) ? `<${tag}${attributes}>` : `<${tag}${attributes}></${tag}>`);
  const doc = new DOMParser().parseFromString(`<!doctype html><html><body>${source}</body></html>`, "text/html");
  const made = new Converter(doc, links);
  made.children(doc.body);
  return doc.body.innerHTML;
}

class Converter {
  constructor(doc, links) {
    this.doc = doc;
    this.links = links;
  }

  element(tag, attributes = {}, children = []) {
    const element = this.doc.createElement(tag);
    for (const [name, value] of Object.entries(attributes)) element.setAttribute(name, value);
    for (const child of children) element.append(child);
    return element;
  }

  own(element, tag) {
    return [...element.children].find((child) => child.tagName.toLowerCase() === tag) ?? null;
  }

  parameters(element) {
    const found = {};
    for (const child of element.children) {
      if (child.tagName.toLowerCase() === "ac:parameter") found[child.getAttribute("ac:name") ?? ""] = (child.textContent ?? "").trim();
    }
    return found;
  }

  /** The element's children made over, in place. */
  children(parent) {
    for (const child of [...parent.children]) this.node(child);
  }

  /** Moves an element's (made-over) children into `into`. */
  contents(element, into) {
    if (!element) return into;
    this.children(element);
    into.append(...element.childNodes);
    return into;
  }

  node(element) {
    const tag = element.tagName.toLowerCase();
    if (!tag.includes(":") && tag !== "time") {
      this.children(element);
      return;
    }
    const replacement = this.replacement(element, tag);
    if (replacement === undefined) {
      // Unknown resources and wrappers: their content stays where they were.
      this.children(element);
      element.replaceWith(...element.childNodes);
    } else if (replacement === null) {
      element.remove();
    } else {
      element.replaceWith(replacement);
    }
  }

  replacement(element, tag) {
    switch (tag) {
      case "ac:structured-macro": return this.macro(element);
      case "ac:task-list": return this.tasks(element);
      case "ac:image": return this.image(element);
      case "ac:link": return this.link(element);
      case "ac:emoticon": {
        const fallback = element.getAttribute("ac:emoji-fallback");
        return this.doc.createTextNode(fallback && !fallback.startsWith(":") ? fallback : EMOTICONS[element.getAttribute("ac:name") ?? ""] ?? "");
      }
      case "ac:layout": return this.layout(element);
      case "ac:adf-extension": return this.adf(element);
      case "ac:placeholder": case "ac:parameter": return null;
      case "time": return this.doc.createTextNode(element.getAttribute("datetime") ?? element.textContent ?? "");
    }
    return undefined;
  }

  macro(element) {
    const name = element.getAttribute("ac:name") ?? "";
    const parameters = this.parameters(element);
    const rich = this.own(element, "ac:rich-text-body");
    const plain = this.own(element, "ac:plain-text-body");
    if (PANEL_KINDS[name] || name === "panel") return this.panel(PANEL_KINDS[name] ?? "", parameters.title, rich);
    switch (name) {
      case "code": case "noformat": {
        const pre = this.element("pre", {
          class: "syntaxhighlighter-pre", "data-syntaxhighlighter-params": `brush: ${parameters.language || "plain"}; gutter: false`,
        });
        pre.textContent = plain?.textContent ?? "";
        return this.element("div", { class: "code panel pdl" }, [pre]);
      }
      case "expand": {
        const title = this.element("span", { class: "expand-control-text" });
        title.textContent = parameters.title ?? "";
        return this.element("div", { class: "expand-container" }, [
          this.element("div", { class: "expand-control" }, [title]),
          this.contents(rich, this.element("div", { class: "expand-content" })),
        ]);
      }
      case "status": {
        const lozenge = this.element("span", { class: "status-macro aui-lozenge" });
        lozenge.textContent = parameters.title || parameters.colour || "";
        return lozenge;
      }
      case "anchor": return null;
      case "jira": {
        // An issue link keeps its key.
        const issue = this.element("span", { class: "conf-macro", "data-macro-name": name });
        issue.textContent = parameters.key ? `[${parameters.key}]` : "";
        return issue;
      }
    }
    // Another macro keeps its name, for the converter to count, and its body.
    return this.contents(rich, this.element("div", { class: "conf-macro output-block", "data-macro-name": name }));
  }

  panel(kind, title, rich) {
    const parts = [];
    if (title) {
      const heading = this.element("p", { class: "title" });
      heading.textContent = title;
      parts.push(heading);
    }
    if (!kind) {
      parts.push(this.contents(rich, this.element("div", { class: "panelContent" })));
      return this.element("div", { class: "panel" }, parts);
    }
    parts.push(this.contents(rich, this.element("div", { class: "confluence-information-macro-body" })));
    return this.element("div", { class: `confluence-information-macro confluence-information-macro-${kind}` }, parts);
  }

  tasks(element) {
    const items = [...element.children].filter((child) => child.tagName.toLowerCase() === "ac:task").map((task) => {
      const done = (this.own(task, "ac:task-status")?.textContent ?? "").trim() === "complete";
      return this.contents(this.own(task, "ac:task-body"), this.element("li", done ? { class: "checked" } : {}));
    });
    return this.element("ul", { class: "inline-task-list" }, items);
  }

  image(element) {
    const attachment = this.own(element, "ri:attachment");
    const address = this.own(element, "ri:url")?.getAttribute("ri:value");
    if (address) return this.element("img", { src: address, class: "confluence-embedded-image confluence-external-resource" });
    const filename = attachment?.getAttribute("ri:filename") ?? "";
    // A picture attached to another page is out of reach.
    const path = attachment && !this.own(attachment, "ri:page") ? this.links.attachment(filename) : "";
    return this.element("img", {
      class: "confluence-embedded-image", src: path || `attachments/missing/${encodeURIComponent(filename)}`,
      "data-linked-resource-default-alias": filename,
    });
  }

  link(element) {
    const label = (this.own(element, "ac:plain-text-link-body")?.textContent ?? "").trim();
    const body = this.own(element, "ac:link-body");
    const text = (fallback) => {
      const anchor = this.element("span");
      if (body) this.contents(body, anchor);
      else anchor.textContent = label || fallback;
      return [...anchor.childNodes];
    };
    const page = this.own(element, "ri:page");
    if (page) {
      const title = page.getAttribute("ri:content-title") ?? "";
      const target = this.links.page(title, page.getAttribute("ri:space-key") ?? "");
      return this.element("a", target ? { href: target } : { class: "createlink", href: "#" }, text(title));
    }
    const user = this.own(element, "ri:user");
    if (user) {
      const mention = this.element("a", { class: "confluence-userlink user-mention" });
      mention.textContent = label || (body?.textContent ?? "").trim() || this.links.user?.(user.getAttribute("ri:account-id") ?? "") || "user";
      return mention;
    }
    const attachment = this.own(element, "ri:attachment");
    if (attachment) return this.element("span", {}, text(attachment.getAttribute("ri:filename") ?? ""));
    const address = this.own(element, "ri:url")?.getAttribute("ri:value");
    if (address) return this.element("a", { href: address }, text(address));
    return this.element("span", {}, text(""));
  }

  layout(element) {
    const sections = [...element.children].filter((child) => child.tagName.toLowerCase() === "ac:layout-section").map((section) => {
      const cells = [...section.children].filter((child) => child.tagName.toLowerCase() === "ac:layout-cell")
        .map((cell) => this.element("div", { class: "cell normal" }, [this.contents(cell, this.element("div", { class: "innerCell" }))]));
      return this.element("div", { class: "columnLayout" }, cells);
    });
    return this.element("div", { class: "contentLayout2" }, sections);
  }

  adf(element) {
    const node = this.own(element, "ac:adf-node");
    if (node?.getAttribute("type") === "panel") {
      const kind = [...node.children].find((child) => child.tagName.toLowerCase() === "ac:adf-attribute"
        && child.getAttribute("key") === "panel-type")?.textContent?.trim() ?? "info";
      return this.panel(ADF_PANELS[kind] ?? "information", "", this.own(node, "ac:adf-content"));
    }
    const fallback = this.own(element, "ac:adf-fallback");
    return this.contents(fallback, this.element("div"));
  }
}
