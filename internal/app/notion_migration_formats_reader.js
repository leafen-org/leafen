// Leafen's reader of how a Notion workspace looks where Notion's API doesn't
// say (notion_migration_formats_read.go). Run in a tab where the person is
// signed in to Notion, it reads with Notion's own web API, read-only and only
// these settings — no page text: each page's, row's and template's full
// width, small text and font; each database's page layout (its properties'
// order and visibility) and the title its block keeps apart from its
// sources' names; each view's groups as set by hand (their order,
// which are hidden, whether empty ones hide, a board's column colors), its
// wrap, the order people put its rows in, its own default template and its
// conditions on deleted properties; each form's title, description,
// questions (a checkbox's label among them) and who may answer; and which
// blocks have comments, with how many of their discussions are resolved —
// Notion's API never gives those, so the report counts them. It hands them
// straight to Leafen, which matches them in the migration's run and lists
// what it couldn't (notion_migration_formats_left.go); the tab comes back
// to Notion.
(async () => {
  const endpoint = __ENDPOINT__, key = __KEY__, roots = __ROOTS__, until = __UNTIL__;
  const user = (document.cookie.match(/(?:^|; )notion_user_id=([^;]+)/) || [])[1];
  const headers = { "content-type": "application/json" };
  if (user) headers["x-notion-active-user-header"] = decodeURIComponent(user);
  // Notion limits how fast one person asks it (seen 2026-11-18: three
  // readers asking without a pause had some 380 asks in 30 seconds answered,
  // then every ask refused with 429 for about ten minutes, the person's own
  // Notion tabs included). So the three readers take turns, about three asks
  // a second, and when Notion refuses one with 429 they all wait — as long as
  // it says, else 30 seconds, twice as long each time it refuses again, up
  // to five minutes — then go on where they were at half the pace. By
  // `until`, before the key ends, they stop and hand in what they have; what
  // they didn't reach is listed as not read.
  const pause = (ms) => new Promise((done) => setTimeout(done, ms));
  let gap = 340, nextAsk = 0, waitUntil = 0, waits = 0;
  const turn = async () => {
    for (;;) {
      const at = Math.max(Date.now(), nextAsk, waitUntil);
      if (at >= until) throw new Error(waits ? "out of time: Notion kept asking to wait" : "out of time");
      nextAsk = at + gap;
      if (at > Date.now()) await pause(at - Date.now());
      if (Date.now() >= waitUntil) return waitUntil;
    }
  };
  const api = async (name, body) => {
    for (let failed = 0; ; ) {
      const asked = await turn();
      const response = await fetch("/api/v3/" + name, { method: "POST", headers, credentials: "include", body: JSON.stringify(body) });
      // A wait set after this ask went says nothing of this answer.
      const first = waitUntil === asked;
      if (response.ok) {
        if (first) waits = 0;
        return response.json();
      }
      if (response.status === 429) {
        if (first) {
          const said = Number(response.headers.get("retry-after"));
          waitUntil = Date.now() + (said > 0 ? Math.min(said * 1000, 900000) : Math.min(30000 * 2 ** waits, 300000));
          // The pace halves once a refusal, not for each ask it refuses meanwhile.
          if (!waits++) gap = Math.min(gap * 2, 3000);
        }
        continue;
      }
      if (response.status >= 500 && ++failed < 5) {
        await pause(1000 * failed);
        continue;
      }
      throw new Error(name + " " + response.status);
    }
  };
  const value = (record) => record && record.value && (record.value.value || record.value);
  const style = (format) => [!!(format && format.page_full_width), !!(format && format.page_small_text), (format && format.page_font) || ""];
  const plain = (format) => !format || (!format.page_full_width && !format.page_small_text && !format.page_font);
  const out = { format: 1, pages: {}, databases: {}, views: {}, unread: [], discussions: { bodies: [], blocks: [], resolved: {}, unknown: 0 } };
  const seen = new Set();
  const queue = [];
  const push = (kind, id, block, of) => {
    if (!id || seen.has(kind + id)) return;
    seen.add(kind + id);
    queue.push({ kind, id, block, of });
  };
  // A body's blocks — a page's, a row's or a template's —, its closed
  // toggles' too; `known` is its own record when the chunks leave it out.
  const loadBody = async (pageId, known) => {
    const blocks = {};
    let cursor = { stack: [] }, chunk = 0;
    do {
      const result = await api("loadPageChunk", { pageId, limit: 100, cursor, chunkNumber: chunk++, verticalColumns: false });
      for (const [id, record] of Object.entries((result.recordMap && result.recordMap.block) || {})) {
        const block = value(record);
        if (block) blocks[id] = block;
      }
      keepRecords(result.recordMap);
      cursor = result.cursor;
    } while (cursor && cursor.stack && cursor.stack.length && chunk < 500);
    if (known && !blocks[pageId]) blocks[pageId] = known;
    await loadClosed(pageId, blocks);
    return blocks;
  };
  // What a body holds: which of its blocks have comments, and its sub-pages
  // and databases — inline or linked —, each read on its own, a row's and a
  // template's as a page's (2026-11-23: a row's body was read for comments
  // alone, so the views linked in each row made from a template, 2,032 of
  // them, kept Leafen's wrap and default template).
  const readInside = (pageId, blocks) => {
    noteDiscussions(pageId, blocks);
    const walk = (ids) => {
      for (const id of ids || []) {
        const block = blocks[id];
        if (!block || block.alive === false) continue;
        if (block.type === "page" || block.type === "collection_view_page") push("page", id);
        else if (block.type === "collection_view") push("database", id, block);
        else walk(block.content);
      }
    };
    walk(blocks[pageId] && blocks[pageId].content);
  };
  const readPage = async (pageId) => {
    const blocks = await loadBody(pageId);
    const page = blocks[pageId];
    if (!page || page.alive === false) {
      out.unread.push({ kind: "page", id: pageId });
      return;
    }
    if (page.type === "collection_view_page" || page.type === "collection_view") {
      if (page.type === "collection_view_page") out.pages[pageId] = style(page.format);
      push("database", pageId, page);
      return;
    }
    out.pages[pageId] = style(page.format);
    readInside(pageId, blocks);
  };
  // A database's templates, which its rows' query leaves out: each template
  // page's width, small text and font when any is set, and what its body
  // holds (notion_migration_formats_templates.go).
  const readTemplates = async (collection, space, of) => {
    const ids = collection.template_pages || [];
    if (!ids.length) return;
    const records = await api("syncRecordValuesMain", { requests: ids.map((id) => ({ pointer: { table: "block", id, spaceId: space }, version: -1 })) });
    for (const id of ids) {
      const template = value(((records.recordMap && records.recordMap.block) || {})[id]);
      if (template && template.alive !== false && !plain(template.format)) out.pages[id] = style(template.format);
      // What its body holds, as a row's: the views a row made from it gets.
      if (template && template.alive !== false) push("body", id, template, of);
    }
  };
  // A form's title, description and questions, which Notion's API doesn't
  // give: the view's form block, that block's layout and the layout's
  // questions in order (seen 2026-11-16) — each one's name, whether it must
  // be answered, its description and a checkbox's label, by the property it
  // asks. The title is the block's collaborative text.
  const text = (rich) => Array.isArray(rich) ? rich.map((part) => (Array.isArray(part) ? part[0] : "") || "").join("") : "";
  const shared = (crdt) => {
    const node = crdt && crdt.n && crdt.n[crdt.r];
    const items = (node && node.s && node.s.i) || [];
    return items.filter((item) => item && item.t === "t" && typeof item.c === "string").map((item) => item.c).join("");
  };
  const readForm = async (viewId, view, space) => {
    const pointer = view.format && view.format.form_block_pointer;
    if (!pointer || !pointer.id) return;
    const blocks = await api("syncRecordValuesMain", { requests: [{ pointer: { table: "block", id: pointer.id, spaceId: pointer.spaceId || space }, version: -1 }] });
    const form = value(((blocks.recordMap && blocks.recordMap.block) || {})[pointer.id]);
    const layoutPointer = form && form.format && form.format.form_layout_pointer;
    if (!layoutPointer || !layoutPointer.id) return;
    const at = layoutPointer.spaceId || space;
    const layouts = await api("syncRecordValuesMain", { requests: [{ pointer: { table: "layout", id: layoutPointer.id, spaceId: at }, version: -1 }] });
    const layout = value(((layouts.recordMap && layouts.recordMap.layout) || {})[layoutPointer.id]);
    const ids = ((layout && layout.modules && layout.modules.form_layout_schema) || [])
      .filter((module) => module && module.type === "formQuestion" && module.formQuestionId).map((module) => module.formQuestionId);
    const asked = ids.length ? await api("syncRecordValuesMain", {
      requests: ids.map((id) => ({ pointer: { table: "form_question", id, spaceId: at }, version: -1 })),
    }) : {};
    const questions = [];
    for (const id of ids) {
      const question = value(((asked.recordMap && asked.recordMap.form_question) || {})[id]);
      const config = question && question.alive !== false && question.config;
      if (!config || !config.propertyId) continue;
      const typed = config.propertyTypeSpecificConfig || {};
      questions.push({
        property: config.propertyId, name: text(config.name), required: !!config.required,
        description: config.showDescription === false ? "" : text(config.description),
        sync: !!config.shouldSyncQuestionNameToPropertyName, checkboxLabel: typed.checkbox ? text(typed.checkbox.label) : "",
      });
    }
    const formConfig = (form.format && form.format.form_config) || {};
    // Who may answer, by the form block's sharing (seen 2026-11-16: a form
    // open to the workspace's members has [{role: "reader", type:
    // "space_permission"}]): anyone with its link, the members, or no one;
    // without sharing to read, nothing is said.
    const granted = Array.isArray(form.permissions) ? form.permissions.filter((entry) => entry && entry.role && entry.role !== "none") : null;
    const audience = !granted ? "" : granted.some((entry) => entry.type === "public_permission") ? "web"
      : granted.some((entry) => entry.type === "space_permission") ? "workspace" : "none";
    out.views[viewId].form = {
      title: shared(form.crdt_data && form.crdt_data.title) || text(form.properties && form.properties.title),
      description: text(formConfig.description), questions, audience,
    };
  };
  // The conditions a view's filter keeps on properties people deleted, which
  // Notion's API leaves out of the view's filter: the view's own query keeps
  // them, each naming the property, and the database keeps a deleted
  // property's name among its deleted ones (seen 2026-11-16).
  const deletedFilters = (view, collection) => {
    const live = collection.schema || {}, deleted = collection.deleted_schema || {}, found = [];
    const walk = (node) => {
      if (!node || typeof node !== "object") return;
      if (Array.isArray(node.filters)) {
        for (const child of node.filters) walk(child);
        return;
      }
      if (typeof node.property === "string" && !live[node.property]) {
        const gone = deleted[node.property];
        found.push({ property: node.property, name: (gone && typeof gone.name === "string" && gone.name) || "" });
      }
    };
    walk(view.query2 && view.query2.filter);
    return found.length ? found : null;
  };
  const pointed = (format) => format && format.collection_pointer && format.collection_pointer.id;
  const readDatabase = async (block) => {
    const viewIds = block.view_ids || [];
    const space = block.space_id;
    // A database linked from many rows is asked once: what a chunk or an
    // earlier answer carried isn't asked again (keepRecords). A linked view's
    // block names no collection — its views do (seen 2026-11-23 on a row made
    // from a template holding one) —, so its views come first.
    const ask = async (pointers) => {
      const asked = pointers.filter(([table, id]) => id && !known[table][id]);
      if (asked.length) {
        keepRecords((await api("syncRecordValuesMain", { requests: asked.map(([table, id]) => ({ pointer: { table, id, spaceId: space }, version: -1 })) })).recordMap);
      }
    };
    const named = block.collection_id || pointed(block.format);
    await ask([["collection", named], ...viewIds.map((id) => ["collection_view", id])]);
    const collectionId = named || viewIds.map((id) => pointed((value(known.collection_view[id]) || {}).format)).find(Boolean);
    if (!collectionId) {
      out.unread.push({ kind: "database", id: block.id });
      return;
    }
    await ask([["collection", collectionId]]);
    const collection = value(known.collection[collectionId]);
    if (!collection) {
      out.unread.push({ kind: "database", id: block.id });
      return;
    }
    const format = collection.format || {};
    out.databases[block.id] = {
      order: (format.collection_page_properties || []).map((entry) => entry.property),
      visibility: Object.fromEntries((format.property_visibility || []).map((entry) => [entry.property, entry.visibility])),
      // The title its block keeps of its own, apart from its sources' names
      // (seen 2026-11-21: none for one made inline, the name one made as a
      // full page was made with) — its collaborative text, or its title.
      title: shared(block.crdt_data && block.crdt_data.title) || text(block.properties && block.properties.title),
    };
    for (const viewId of viewIds) {
      const view = value(known.collection_view[viewId]);
      if (!view) {
        out.unread.push({ kind: "view", id: viewId, database: block.id });
        continue;
      }
      const viewFormat = view.format || {};
      const columns = view.type === "board" ? viewFormat.board_columns : viewFormat.collection_groups;
      const by = (view.type === "board" ? viewFormat.board_columns_by : viewFormat.collection_group_by) || null;
      out.views[viewId] = {
        database: block.id,
        type: view.type,
        groups: Array.isArray(columns) ? columns.map((column) => ({
          property: column.property,
          type: column.value && column.value.type,
          value: column.value && "value" in column.value ? column.value.value : null,
          hidden: !!column.hidden,
        })) : null,
        hideEmpty: by && "hideEmptyGroups" in by ? !!by.hideEmptyGroups : null,
        colors: view.type === "board" && by ? !by.disableBoardColorColumns : null,
        wrap: "table_wrap" in viewFormat ? !!viewFormat.table_wrap : null,
        rows: Array.isArray(view.page_sort) ? view.page_sort : null,
        deletedFilters: deletedFilters(view, collection),
      };
      // Its own default template, a linked view's too.
      const chosen = viewFormat.collection_view_default_template;
      if (chosen && chosen.template_page_id) out.views[viewId].defaultTemplate = chosen.template_page_id;
      if (view.type === "form_editor") {
        await readForm(viewId, view, space).catch((error) =>
          out.unread.push({ kind: "view", id: viewId, database: block.id, reason: "form " + String(error && error.message || error).slice(0, 60) }));
      }
    }
    if (collection.parent_id === block.id) {
      await readTemplates(collection, space, block.id);
    }
    // A database's own rows, read once: a row's width, small text and font,
    // when any is set.
    if (collection.parent_id === block.id && viewIds.length) {
      const rows = await api("queryCollection", {
        source: { type: "collection", id: collectionId, spaceId: space },
        collectionView: { id: viewIds[0], spaceId: space },
        loader: { type: "reducer", reducers: { results: { type: "results", limit: 50000 } }, sort: [], searchQuery: "", userTimeZone: "UTC" },
      });
      for (const record of Object.values((rows.recordMap && rows.recordMap.block) || {})) {
        const row = value(record);
        if (row && row.parent_table === "collection" && !plain(row.format)) out.pages[row.id] = style(row.format);
        if (row && row.parent_table === "collection") push("body", row.id, row, block.id);
      }
    }
  };
  // A closed toggle's blocks aren't in its page's chunks (seen 2026-11-16: a
  // page with one closed toggle never counted as read whole, so the run asked
  // every block of it for comments, and a sub-page in the toggle went
  // unread): they're asked by id, a level at a time, until none is missing or
  // Notion gives no more.
  const loadClosed = async (pageId, blocks) => {
    const space = blocks[pageId] && blocks[pageId].space_id;
    for (let level = 0; space && level < 50; level++) {
      const missing = [];
      const walk = (ids) => {
        for (const id of ids || []) {
          const block = blocks[id];
          if (!block) missing.push(id);
          else if (block.alive !== false && block.type !== "page" && block.type !== "collection_view_page" && block.type !== "collection_view") walk(block.content);
        }
      };
      walk(blocks[pageId].content);
      let found = 0;
      for (let at = 0; at < missing.length; at += 100) {
        let records;
        try {
          records = await api("syncRecordValuesMain", { requests: missing.slice(at, at + 100).map((id) => ({ pointer: { table: "block", id, spaceId: space }, version: -1 })) });
        } catch (error) {
          return; // The body stays not read whole: the run asks all of it.
        }
        for (const [id, record] of Object.entries((records.recordMap && records.recordMap.block) || {})) {
          const block = value(record);
          if (block && !blocks[id]) {
            blocks[id] = block;
            found++;
          }
        }
        keepRecords(records.recordMap);
      }
      if (!found) return;
    }
  };
  // Which blocks have comments, so the run asks only those (Notion's API
  // lists a block's comments by the block alone; settleResolved keeps those
  // with a discussion still open): a page's or row's body counts as read
  // only when every block in it was loaded.
  const noteDiscussions = (pageId, blocks) => {
    let whole = true;
    const walk = (ids) => {
      for (const id of ids || []) {
        const block = blocks[id];
        if (!block) { whole = false; continue; }
        if (block.alive === false) continue;
        if (Array.isArray(block.discussions) && block.discussions.length) out.discussions.blocks.push(id);
        if (block.type !== "page" && block.type !== "collection_view_page" && block.type !== "collection_view") walk(block.content);
      }
    };
    walk(blocks[pageId] && blocks[pageId].content);
    if (whole) out.discussions.bodies.push(pageId);
    countResolved(pageId, blocks);
  };
  // How many of a body's discussions are resolved, by the block, row or page
  // they are on: a discussion's own record says it (resolved), which a page's
  // chunk carries and syncRecordValuesMain gives for the rest (seen
  // 2026-11-16 — a block keeps its resolved discussions' ids). A chunk also
  // carries the page's parents, so each discussion counts once, and only
  // those on the body itself: the page or row and the blocks in it.
  const discussionRecords = {}, counted = new Set(), pending = [];
  // The collections and views the chunks and answers carried, as they are
  // (a chunk carries those of the databases in it, seen 2026-11-23).
  const known = { collection: {}, collection_view: {} };
  const keepRecords = (recordMap) => {
    keepDiscussionRecords(recordMap);
    for (const table of ["collection", "collection_view"]) {
      for (const [id, record] of Object.entries((recordMap && recordMap[table]) || {})) {
        if (value(record)) known[table][id] = record;
      }
    }
  };
  const keepDiscussionRecords = (recordMap) => {
    for (const [id, record] of Object.entries((recordMap && recordMap.discussion) || {})) {
      const discussion = value(record);
      if (discussion) discussionRecords[id] = discussion;
    }
  };
  const countResolved = (pageId, blocks) => {
    const note = (block) => {
      for (const id of (block && Array.isArray(block.discussions) && block.discussions) || []) {
        if (counted.has(id)) continue;
        counted.add(id);
        pending.push({ id, block: block.id, space: block.space_id });
      }
    };
    const walk = (ids) => {
      for (const id of ids || []) {
        const block = blocks[id];
        if (!block || block.alive === false || block.type === "page" || block.type === "collection_view_page" || block.type === "collection_view") continue;
        note(block);
        walk(block.content);
      }
    };
    note(blocks[pageId]);
    walk(blocks[pageId] && blocks[pageId].content);
  };
  const settleResolved = async () => {
    const asked = pending.filter((item) => !discussionRecords[item.id]);
    for (let at = 0; at < asked.length; at += 100) {
      const part = asked.slice(at, at + 100);
      try {
        const records = await api("syncRecordValuesMain", { requests: part.map((item) => ({ pointer: { table: "discussion", id: item.id, spaceId: item.space }, version: -1 })) });
        keepDiscussionRecords(records.recordMap);
      } catch (error) {
        // Left unknown below.
      }
    }
    // The run asks only the blocks with a discussion still open, or one it
    // couldn't tell: Notion's API gives no resolved or deleted discussion, so
    // asking a block that has only those came back empty.
    const bare = (id) => String(id).replace(/-/g, "").toLowerCase();
    const open = new Set(), seen = new Set();
    for (const item of pending) {
      const discussion = discussionRecords[item.id];
      seen.add(bare(item.block));
      if (!discussion || discussion.alive === false) {
        if (!discussion) {
          out.discussions.unknown++;
          open.add(bare(item.block));
        }
        continue;
      }
      if (discussion.resolved === true) out.discussions.resolved[item.block] = (out.discussions.resolved[item.block] || 0) + 1;
      else open.add(bare(item.block));
    }
    out.discussions.blocks = out.discussions.blocks.filter((id) => open.has(bare(id)) || !seen.has(bare(id)));
  };
  const readRowBody = async (rowId, row) => {
    if (!row.content || !row.content.length) {
      out.discussions.bodies.push(rowId);
      countResolved(rowId, { [rowId]: row });
      return;
    }
    readInside(rowId, await loadBody(rowId, row));
  };
  for (const root of roots) push("page", root.id);
  // How far it has read: window.leafenReading in this tab, and to Leafen about
  // once a second for its import window (notion_migration_formats_progress.go),
  // by Notion IDs and the key's hash — a message to Leafen's page that opened
  // this tab, which answers the first (an opener that doesn't in five seconds
  // is another page's), else an image's address, which Notion's pages load from https.
  const done = { pages: 0, databases: 0, rowBodies: 0 };
  let hash = "", told = 0, last = "", asked = 0, heard = false;
  const leafen = new URL(endpoint).origin;
  window.addEventListener("message", (event) => { heard ||= event.origin === leafen && !!(event.data && event.data.leafenHeard); });
  if (globalThis.crypto && crypto.subtle) {
    crypto.subtle.digest("SHA-256", new TextEncoder().encode(key)).then((digest) => {
      hash = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
    }, () => {});
  }
  const tell = (finished) => {
    const wait = Math.max(0, Math.ceil((waitUntil - Date.now()) / 1000));
    window.leafenReading = { ...done, left: queue.length, notionAskedToWaitSeconds: wait, finished };
    if (!hash || Date.now() - told < 1000) return;
    told = Date.now();
    const query = "h=" + hash + "&p=" + done.pages + "&d=" + done.databases + "&b=" + done.rowBodies +
      "&l=" + queue.length + "&w=" + wait + "&i=" + encodeURIComponent(last);
    const opener = window.opener && !window.opener.closed && window.opener;
    if (opener && (heard || Date.now() - (asked ||= Date.now()) < 5000)) return opener.postMessage({ leafenReading: query }, leafen);
    new Image().src = endpoint + "/progress?" + query;
  };
  tell(false);
  const work = async () => {
    while (queue.length) {
      const item = queue.shift();
      try {
        if (item.kind === "page") await readPage(item.id);
        else if (item.kind === "body") await readRowBody(item.id, item.block);
        else await readDatabase(item.block);
      } catch (error) {
        out.unread.push({ kind: item.kind, id: item.id, reason: String(error && error.message || error).slice(0, 80) });
      }
      done[item.kind === "page" ? "pages" : item.kind === "body" ? "rowBodies" : "databases"]++;
      last = item.kind === "body" ? "r" + item.id + "." + (item.of || "") : (item.kind === "page" ? "p" : "d") + item.id;
      tell(false);
    }
  };
  /* __HAND_IN__ */
  // Three at a time, as the next ones are found.
  while (queue.length) await Promise.all([work(), work(), work()]);
  await settleResolved();
  tell(true);
  await handIn();
  return "Read " + Object.keys(out.pages).length + " pages, " + Object.keys(out.databases).length + " databases and " +
    Object.keys(out.views).length + " views (" + out.unread.length + " not read); handing them to Leafen.";
})();
