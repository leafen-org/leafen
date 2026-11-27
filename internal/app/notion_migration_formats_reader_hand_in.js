// How the reader hands what it read to Leafen, put into the reader where it
// says __HAND_IN__ (notion_migration_formats_read.go) and taken in
// notion_migration_formats_parts.go: a form from the tab — the one way out
// of Notion's page that goes anywhere — with the key, where to come back,
// the parts' sizes, then the parts: which blocks have comments, the
// settings, then the rows' orders, the largest, last, so that past Leafen's
// size for one reading what comes first still comes whole. The same settings go
// once: a view or a database like one before it names that one, and each
// rows' order goes once, by the views that have it (2026-11-23: 7,324 rows
// each holding the same linked views, their rows' order among them, came to
// more than 16 MiB, and Leafen took none of it; 10,000 rows with four each,
// a hundred rows in order, came to 204 MB). Each part goes as gzip in
// base64url where the browser can pack it.
  const handIn = async () => {
    const orders = { lists: [], of: {} }, listed = new Map();
    for (const [id, view] of Object.entries(out.views)) {
      if (!Array.isArray(view.rows)) continue;
      const shape = JSON.stringify(view.rows);
      if (!listed.has(shape)) listed.set(shape, orders.lists.push(view.rows) - 1);
      orders.of[id] = listed.get(shape);
      view.rows = null;
      view.ordered = true;
    }
    const once = (entries, own) => {
      const first = new Map();
      for (const [id, entry] of Object.entries(entries)) {
        const shape = JSON.stringify({ ...entry, [own]: undefined });
        if (first.has(shape)) entries[id] = { like: first.get(shape), [own]: entry[own] };
        else first.set(shape, id);
      }
    };
    once(out.views, "database");
    once(out.databases, "title");
    const pack = async (name, text) => {
      if (typeof CompressionStream !== "function") return [name, text, text.length];
      const blob = new Blob([text]);
      const packed = new Uint8Array(await new Response(blob.stream().pipeThrough(new CompressionStream("gzip"))).arrayBuffer());
      let binary = "";
      for (let at = 0; at < packed.length; at += 32768) binary += String.fromCharCode.apply(null, packed.subarray(at, at + 32768));
      return [name + ".gz", btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""), blob.size];
    };
    const discussions = out.discussions;
    delete out.discussions;
    const parts = [await pack("discussions", JSON.stringify(discussions)), await pack("formats", JSON.stringify(out)), await pack("orders", JSON.stringify(orders))];
    const sizes = parts.map(([name, text, size]) => name + ":" + size + ":" + text.length).join(",");
    const form = document.createElement("form");
    form.method = "POST";
    form.action = endpoint;
    form.style.display = "none";
    for (const [name, text] of [["key", key], ["back", location.href], ["sizes", sizes], ...parts]) {
      const field = document.createElement("textarea");
      field.name = name;
      field.value = text;
      form.appendChild(field);
    }
    document.body.appendChild(form);
    setTimeout(() => form.submit(), 50);
  };
