import { Fragment, type ReactNode } from "react";
import { i18next } from "@/i18n";
import { isBlockType } from "@/lib/blockSemantics";
import { blockTypeLabel } from "../editor/blockItemLabels";
import { dateMentionDisplayText } from "../editor/dateMentions";
import { markdownInlineTokens } from "../editor/markdownLexer";
import own from "./ReadOnlyMarkdown.module.css";

/** An inline Markdown token as the lexer gives it (MarkedJS). */
type Token = { type: string; raw?: string; text?: string; tokens?: Token[] };

// The page's Markdown is Notion's enhanced Markdown: a tag reads as the words
// it holds, and one that holds none as what it stands for (2026-10-16: a date
// mention, a self-closing tag, was dropped whole) — a date, an untitled page,
// a person's "@", a file or a block this text can't show.
type Tag = { name: string; closing: boolean; selfClosing: boolean; attributes: Record<string, string> };

const TAG = /^<(\/?)([a-zA-Z][\w-]*)((?:\s+[\w-]+(?:="[^"]*")?)*)\s*(\/?)>$/u;
const FILE_TAGS = new Set(["file", "image", "video", "audio", "pdf"]);

function readTag(raw: string): Tag | null {
  const match = TAG.exec(raw.trim());
  if (!match) return null;
  const attributes: Record<string, string> = {};
  for (const [, name, value = ""] of match[3].matchAll(/([\w-]+)(?:="([^"]*)")?/gu)) {
    attributes[name] = value.replace(/&quot;/gu, '"').replace(/&lt;/gu, "<").replace(/&gt;/gu, ">").replace(/&amp;/gu, "&");
  }
  return { name: match[2].toLowerCase(), closing: match[1] === "/", selfClosing: match[4] === "/", attributes };
}

function blockKind(type: string) {
  return isBlockType(type) ? blockTypeLabel(type) : "";
}

// A block with an address (a web bookmark, an embed) shows it as text after
// its kind, so the administrator can judge it; nothing opens it.
function unshown(type: string, key: number | string, source = "") {
  const kind = blockKind(type);
  return (
    <span key={key} className={own.unshown} data-unshown-block={type}>
      {kind ? i18next.t("serverConsole:workspaceContent.pages.unshownBlock", { kind })
        : i18next.t("serverConsole:workspaceContent.pages.unshownBlockAny")}
      {source ? <span className={own.source} data-unshown-source> · {source}</span> : null}
    </span>
  );
}

function tagNode(tag: Tag, key: number | string): ReactNode {
  switch (tag.name) {
    case "br": return <br key={key} />;
    case "mention-date": {
      const { start = "", startTime, end = "", endTime } = tag.attributes;
      if (!start) return null;
      return (
        <span key={key} className={own.date} data-mention-date={start}>
          {dateMentionDisplayText(startTime ? `${start}T${startTime}` : start, "@", undefined, end && endTime ? `${end}T${endTime}` : end)}
        </span>
      );
    }
    case "mention-user": return tag.closing ? null : "@";
    case "mention-page":
    case "mention-database":
      return tag.selfClosing ? i18next.t("serverConsole:workspaceContent.pages.untitled") : null;
    case "unknown": return unshown(tag.attributes.alt ?? "", key, tag.attributes.source ?? "");
    default:
      if (FILE_TAGS.has(tag.name) && !tag.closing) {
        return <span key={key} className={own.kind}>{blockKind(tag.name)}</span>;
      }
      return null;
  }
}

function inline(tokens: Token[] | undefined, fallback = ""): ReactNode {
  if (!tokens?.length) return fallback;
  return tokens.map((token, index) => {
    switch (token.type) {
      case "strong": return <strong key={index}>{inline(token.tokens, token.text)}</strong>;
      case "em": return <em key={index}>{inline(token.tokens, token.text)}</em>;
      case "del": return <del key={index}>{inline(token.tokens, token.text)}</del>;
      case "codespan": return <code key={index}>{token.text}</code>;
      case "br": return <br key={index} />;
      case "html": {
        const tag = readTag(token.raw ?? token.text ?? "");
        return tag ? <Fragment key={index}>{tagNode(tag, index)}</Fragment> : null;
      }
      // A link reads as its words, an image as its kind and caption:
      // nothing here opens or loads.
      case "link":
        return <Fragment key={index}>{inline(token.tokens, token.text)}</Fragment>;
      case "image":
        return <Fragment key={index}><span className={own.kind}>{blockKind("image")}</span>{token.text ?? ""}</Fragment>;
      default:
        return <Fragment key={index}>{token.tokens ? inline(token.tokens, token.text) : token.text ?? token.raw ?? ""}</Fragment>;
    }
  });
}

/**
 * A line of the Markdown: a block, and how far in it sits — a tab a level as
 * the page's Markdown wrote it until 2026-10-29, a list marker's width of
 * spaces as it writes it now; only deeper or as deep matters.
 */
type Line = { depth: number; text: string; raw: string };

function readLines(markdown: string): Line[] {
  return markdown.replace(/\r\n?/gu, "\n").split("\n").map((raw) => {
    const text = raw.replace(/^[\t ]+/u, "");
    return { depth: raw.length - text.length, text, raw };
  });
}

/** A line written `depth` in, as it was written. */
function outdented(line: Line, depth: number) {
  return line.raw.replace(new RegExp(`^[\\t ]{0,${depth}}`, "u"), "");
}

function words(text: string): ReactNode {
  return inline(markdownInlineTokens(text) as unknown as Token[], text);
}

/** Where the lines deeper than `depth` from `start` end: a block's children. */
function childrenEnd(lines: Line[], start: number, depth: number) {
  let end = start;
  while (end < lines.length && (lines[end].depth > depth || lines[end].text === "" && end + 1 < lines.length && lines[end + 1].depth > depth)) end += 1;
  return end;
}

const LIST_ITEM = /^(?:- \[( |x)\] |(- )|(\d+)\. )/u;
const UNKNOWN = /^<unknown\b/u;

function listKind(text: string) {
  const match = LIST_ITEM.exec(text);
  if (!match) return null;
  return match[1] !== undefined ? "todo" : match[2] ? "bullet" : "number";
}

/** Where a block written over several lines closes: its closing tag's line at its depth. */
function closingLine(lines: Line[], start: number, depth: number, closing: string) {
  let end = start;
  while (end < lines.length && !(lines[end].depth === depth && lines[end].text === closing)) end += 1;
  return end;
}

const TABLE = /^<table\b[^>]*>$/u;
const CELL = /^<td>(.*)<\/td>$/u;

/**
 * A table's rows as Notion writes them: a <tr> line for each and a cell a
 * <td> line, its words in it or, as the page's Markdown writes it now, on
 * the lines up to its </td>.
 */
function tableRows(lines: Line[]): string[][] {
  const rows: string[][] = [];
  let cell: string[] | null = null;
  for (const { text } of lines) {
    if (cell) {
      if (text === "</td>") { rows[rows.length - 1]?.push(cell.join(" ")); cell = null; }
      else if (text !== "<p />") cell.push(text);
      continue;
    }
    if (text === "<tr>") rows.push([]);
    else if (text === "<td>") cell = [];
    else {
      const whole = CELL.exec(text);
      if (whole && rows.length) rows[rows.length - 1].push(whole[1]);
    }
  }
  return rows;
}

/** A pipe table's cells, a "\|" being a bar in a cell. */
function pipeCells(text: string) {
  return text.replace(/^\|/u, "").replace(/\|$/u, "").split(/(?<!\\)\|/u).map((cell) => cell.trim().replace(/\\\|/gu, "|"));
}

const PIPE_RULE = /^\|(?:\s*:?-{3,}:?\s*\|)+$/u;

function tableNode(key: number, rows: string[][], headerRow: boolean, headerColumn: boolean) {
  return (
    <table key={key} className={own.table} data-read-only-table>
      <tbody>
        {rows.map((cells, row) => (
          <tr key={row}>
            {cells.map((cell, column) => {
              const Cell = headerRow && row === 0 || headerColumn && column === 0 ? "th" : "td";
              return <Cell key={column}>{words(cell)}</Cell>;
            })}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// A block written as a tag around its words and the blocks it holds, as the
// page's Markdown writes it now (2026-10-29): its words on the line after the
// opening tag ("<p />" for none), then after a blank line what it holds, up
// to its closing tag as far in.
const HELD = /^<(toggle|p|quote|h[1-4]|todo|li|tab)\b([^>]*)>$/u;
const WRAPPER = /^<(ul|ol|tabs)\b[^>]*>$/u;
const LINK_BLOCK = /^<(bookmark|embed|table-of-contents)\b([^>]*?)\s*(?:\/>|>(.*?)<\/(?:bookmark|embed|table-of-contents)>)$/u;

function heldBlock(name: string, attributes: Record<string, string>, title: string, inner: ReactNode[], key: number): ReactNode {
  const own_ = title === "<p />" ? "" : title;
  switch (name) {
    case "toggle": {
      const level = /^h([1-4])$/u.exec(attributes.heading ?? "")?.[1];
      const Title = level ? `h${Math.min(Number(level) + 1, 6)}` as "h2" : "p";
      return (
        <div key={key} className={own.toggle} data-read-only-toggle>
          <Title className={own.toggleTitle}><span aria-hidden="true">▾ </span>{words(own_)}</Title>
          <div className={own.children}>{inner}</div>
        </div>
      );
    }
    case "quote": return <blockquote key={key}>{words(own_)}{inner.length ? <div className={own.children}>{inner}</div> : null}</blockquote>;
    case "todo": case "li": {
      const checked = name === "todo" ? attributes.checked === "true" : undefined;
      const item = (
        <li key={key} data-checked={checked === undefined ? undefined : String(checked)}>
          {checked === undefined ? null : <span className={own.check} aria-hidden="true">{checked ? "☑" : "☐"}</span>}
          {words(own_)}{inner}
        </li>
      );
      return name === "todo" ? <ul key={key} className={own.todo}>{item}</ul> : item;
    }
    default: {
      // A heading, a paragraph, or a tab under its name.
      const heading = /^h([1-4])$/u.exec(name)?.[1];
      const Own = heading ? `h${Math.min(Number(heading) + 1, 6)}` as "h2" : "p";
      return (
        <Fragment key={key}>
          <Own className={name === "tab" ? own.toggleTitle : undefined}>{words(own_)}</Own>
          {inner.length ? <div className={own.children}>{inner}</div> : null}
        </Fragment>
      );
    }
  }
}

/**
 * Notion's enhanced Markdown is a block a line, its children a tab deeper
 * (2026-10-16: read as CommonMark, a callout's or an unshown block's tag took
 * the lines after it as raw text, and a to-do showed "[x]" apart from its
 * words). A code fence and a callout run over several lines.
 */
function blocks(lines: Line[]): ReactNode[] {
  const shown: ReactNode[] = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    const { depth, text } = line;
    const key = index;
    if (text === "" || text === "<empty-block/>") { index += 1; continue; }
    if (text.startsWith("```")) {
      const fence = /^`+/u.exec(text)?.[0] ?? "```";
      const body: string[] = [];
      index += 1;
      while (index < lines.length && lines[index].text !== fence) {
        body.push(outdented(lines[index], depth));
        index += 1;
      }
      index += 1;
      shown.push(<pre key={key}><code>{body.join("\n")}</code></pre>);
      continue;
    }
    // A toggle shows its title and, a level in, what it holds; a table its
    // rows; columns side by side; an equation its formula as written — all
    // as Notion's page Markdown writes them (2026-10-17: each was a block
    // this view couldn't show).
    if (text === "<p />") { index += 1; continue; }
    const held = HELD.exec(text);
    if (held) {
      const end = closingLine(lines, index + 1, depth, `</${held[1]}>`);
      const title = lines[index + 1] && index + 1 < end ? lines[index + 1].text : "";
      const attributes = readTag(text)?.attributes ?? {};
      shown.push(heldBlock(held[1], attributes, title, blocks(lines.slice(index + 2, end)), key));
      index = end + 1;
      continue;
    }
    const empty = /^<toggle\b[^>]*><\/toggle>$/u.exec(text);
    if (empty) {
      shown.push(heldBlock("toggle", readTag(text.replace(/<\/toggle>$/u, ""))?.attributes ?? {}, "", [], key));
      index += 1;
      continue;
    }
    const wrapper = WRAPPER.exec(text);
    if (wrapper) {
      const end = closingLine(lines, index + 1, depth, `</${wrapper[1]}>`);
      const inner = blocks(lines.slice(index + 1, end));
      shown.push(wrapper[1] === "ol" ? <ol key={key}>{inner}</ol> : wrapper[1] === "ul" ? <ul key={key}>{inner}</ul> : <div key={key}>{inner}</div>);
      index = end + 1;
      continue;
    }
    // A web bookmark or embed shows its address, a table of contents its kind.
    const link = LINK_BLOCK.exec(text);
    if (link) {
      const source = readTag(`<${link[1]}${link[2]} />`)?.attributes.source ?? "";
      shown.push(<p key={key}>{unshown(link[1].replace(/-/gu, "_"), "kind", source)}{link[3] ? <> {words(link[3])}</> : null}</p>);
      index += 1;
      continue;
    }
    if (text.startsWith("|") && PIPE_RULE.test(lines[index + 1]?.text ?? "")) {
      let end = index;
      while (end < lines.length && lines[end].depth === depth && lines[end].text.startsWith("|")) end += 1;
      const rows = [lines[index], ...lines.slice(index + 2, end)].map((row) => pipeCells(row.text));
      shown.push(tableNode(key, rows, true, false));
      index = end;
      continue;
    }
    if (text === "<details>") {
      const end = closingLine(lines, index + 1, depth, "</details>");
      const title = /^<summary>(.*)<\/summary>$/u.exec(lines[index + 1]?.text ?? "");
      shown.push(
        <div key={key} className={own.toggle} data-read-only-toggle>
          <p className={own.toggleTitle}><span aria-hidden="true">▾ </span>{words(title?.[1] ?? "")}</p>
          <div className={own.children}>{blocks(lines.slice(title ? index + 2 : index + 1, end))}</div>
        </div>,
      );
      index = end + 1;
      continue;
    }
    if (text === "$$") {
      const end = closingLine(lines, index + 1, depth, "$$");
      const formula = lines.slice(index + 1, end).map((part) => outdented(part, depth));
      shown.push(<pre key={key} className={own.equation} data-read-only-equation><code>{formula.join("\n")}</code></pre>);
      index = end + 1;
      continue;
    }
    if (TABLE.test(text)) {
      const end = closingLine(lines, index + 1, depth, "</table>");
      const attributes = readTag(text)?.attributes ?? {};
      shown.push(tableNode(key, tableRows(lines.slice(index + 1, end)), attributes["header-row"] === "true", attributes["header-column"] === "true"));
      index = end + 1;
      continue;
    }
    if (text === "<columns>") {
      const end = closingLine(lines, index + 1, depth, "</columns>");
      const columns: Line[][] = [];
      // Each <column> a tab in, as written until 2026-10-29, or as far in as
      // its <columns>, as written now, with its share of the row.
      for (let at = index + 1; at < end; at += 1) {
        if (lines[at].depth < depth || !/^<column\b[^>]*>$/u.test(lines[at].text)) continue;
        const close = closingLine(lines, at + 1, lines[at].depth, "</column>");
        columns.push(lines.slice(at + 1, close));
        at = close;
      }
      shown.push(
        <div key={key} className={own.columns} data-read-only-columns>
          {columns.map((column, at) => <div key={at} className={own.column}>{blocks(column)}</div>)}
        </div>,
      );
      index = end + 1;
      continue;
    }
    if (/^<callout\b/u.test(text)) {
      let end = index + 1;
      while (end < lines.length && !(lines[end].depth === depth && lines[end].text === "</callout>")) end += 1;
      const icon = readTag(text)?.attributes.icon;
      shown.push(
        <div key={key} className={own.callout}>
          {icon ? <span className={own.calloutIcon} aria-hidden="true">{icon}</span> : null}
          <div>{blocks(lines.slice(index + 1, end))}</div>
        </div>,
      );
      index = end + 1;
      continue;
    }
    const kind = listKind(text);
    if (kind) {
      const items: ReactNode[] = [];
      while (index < lines.length && lines[index].depth === depth && listKind(lines[index].text) === kind) {
        const itemText = lines[index].text;
        const match = LIST_ITEM.exec(itemText)!;
        const end = childrenEnd(lines, index + 1, depth);
        items.push(
          <li key={index} data-checked={kind === "todo" ? String(match[1] === "x") : undefined}>
            {kind === "todo" ? <span className={own.check} aria-hidden="true">{match[1] === "x" ? "☑" : "☐"}</span> : null}
            {words(itemText.slice(match[0].length))}
            {end > index + 1 ? blocks(lines.slice(index + 1, end)) : null}
          </li>,
        );
        index = end;
      }
      shown.push(kind === "number" ? <ol key={key}>{items}</ol> : <ul key={key} className={kind === "todo" ? own.todo : undefined}>{items}</ul>);
      continue;
    }
    const end = childrenEnd(lines, index + 1, depth);
    // Under an unshown block, an unshown child (a table's row) says nothing
    // more; what the children hold is still shown.
    const children = lines.slice(index + 1, end).filter((child) => !UNKNOWN.test(text) || !UNKNOWN.test(child.text));
    const heading = /^(#{1,4}) (.*?)(?: \{toggle="true"\})?$/u.exec(text);
    if (heading) {
      const Heading = `h${Math.min(heading[1].length + 1, 6)}` as "h2";
      shown.push(<Heading key={key}>{words(heading[2])}</Heading>);
    } else if (text === "---") {
      shown.push(<hr key={key} />);
    } else if (text.startsWith("> ")) {
      shown.push(<blockquote key={key}>{words(text.slice(2))}</blockquote>);
    } else {
      shown.push(<p key={key}>{words(text)}</p>);
    }
    if (children.length > 0) shown.push(<div key={`${key}-children`} className={own.children}>{blocks(children)}</div>);
    index = end;
  }
  return shown;
}

/**
 * A page's Markdown shown as read only text — headings, lists, to-dos,
 * quotes, callouts, code, toggles, tables, columns and equations, with
 * mentions and the blocks it can't show named — with nothing in it run,
 * opened or loaded.
 */
export function ReadOnlyMarkdown({ markdown, className }: { markdown: string; className?: string }) {
  return <div className={className}>{blocks(readLines(markdown))}</div>;
}
