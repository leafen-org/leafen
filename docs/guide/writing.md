# Writing the guide

These are the rules for every chapter in this folder, and the template each
chapter starts from. The [guide map](README.md) lists the chapters.

## Rules

1. **Write from the code, not from memory.** Read the source a chapter
   describes and use Leafen running with new, made-up data to walk each step.
   A name, limit or default you could not find in the source does not go in.
2. **Use the names on screen.** Write a button, menu item or setting exactly
   as the English screen shows it. Where the tools or the API use another word
   for the same thing, say so once, where it matters.
3. **Exact, not approximate.** Give limits and defaults as numbers, with the
   unit. No "about", "usually", "should" or "may" for behaviour the code
   decides, no options that do not exist, no marketing.
4. **Backticks mean a real name.** Put an MCP tool, an API path, a `leafen`
   command, an environment variable or a type name in backticks only when it
   exists in Leafen as written. Release checks compare them with the code.
5. **One place for each fact.** Installing, running a server, the API, apps
   and the MCP reference have their own documents. Link to the section there
   instead of repeating it; repeat only a short sentence a reader needs to
   follow along.
6. **Every example is made up.** Use `example.com`, `example.org` or a
   `.example` or `.test` name for addresses and mail, and plain role words
   ("a member", "a guest", "the owner") or obviously invented names for
   people, companies and pages. If a name came from somewhere real, change it.
7. **Differences from Notion are stated, not hidden.** A chapter names where
   Leafen works differently from Notion on purpose, in its "Differences from
   Notion" section, and the [differences chapter](README.md#leafen-and-connections)
   lists them all. Do not copy Notion's help text.
8. **Pictures show made-up data only.** They are taken by the screenshot tool
   from a test server with made-up data and saved in
   `img/` next to the chapters. Each picture has alt text that says what it
   shows. Do not add a picture taken by hand.
9. **Plain English.** Short sentences, the second person ("you"), present
   tense. A Korean edition is not part of the 0.1 pre-release.

## Types get their own sections

A chapter that covers a set of types gives each type its own `###` section,
whose heading ends with the type's name in backticks, for example
``### Status (`status`)``. This holds for editor blocks, properties and views.
Release checks fail when a type has no section.

## Chapter template

Copy this into a new file named as the guide map names it, then fill it in.
Leave out a section only when it has nothing to say, and keep the order.

````markdown
# <Chapter title>

<!-- guide-sources:
<product source file the chapter describes>
<another source file>
-->

<One paragraph: what this chapter covers, who it is for, and links to the
chapters that cover what it does not.>

## <The first thing a person does here>

<What it is, then the steps, numbered, with the exact names on screen.>

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |

## For agents and the API

<The MCP tools and API paths that do the same, in backticks, and what differs
from doing it on screen. Link to the MCP reference or Building on Leafen for
details.>

## Differences from Notion

<Only the differences Leafen makes on purpose, in the reader's words.>

## Related chapters

- <links>
````

The `guide-sources` comment lists the product files, relative to the
repository root, whose behaviour the chapter describes. When one of them
changes after the chapter was last changed, release checks list the chapter
as one to read again.
