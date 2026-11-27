package app

// A share link publishes its root page and every page under it, as Notion's
// public pages do, so its page graph follows the page tree: a page created,
// restored or moved under a published page joins the graph, and a page moved
// out, put in the trash or made an import draft leaves it. A page change
// queues the page for each graph it or its parent is in; a page leaving a
// graph leaves at once, and the pages under it follow through the queue.

const publicShareGraphJobsTable = "leafen_public_share_graph_jobs"
