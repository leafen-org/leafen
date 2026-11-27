// Whether a Claude connector can connect to an address — the one rule every
// place that guides a person to add Leafen as a custom connector follows
// (2026-11-22: a server whose public address carried :25781 got "the server
// didn't respond" at Claude's check). Claude connects to a connector from
// its own cloud, and only on the standard HTTPS port: "Claude can't connect
// to a URL that specifies another port" (support.claude.com, article
// 11175166). Only the connector is stopped there — the browser, Leafen's apps
// and agents such as Claude Code work at the address as it is — so a place
// says so beside its connector guidance alone, never over the whole address.
// OpenAI's own pages for ChatGPT's custom MCP servers name no such limit, so
// nothing here is said of ChatGPT. Leafen says the same
// (internal/app/connector_reach.go).

/**
 * The port that keeps a Claude connector from the address: "" when it can
 * connect (https, with no port or 443), and "" for an address that isn't
 * https, which the https rule turns away first (agentServerAddress).
 */
export function claudeConnectorBlockingPort(origin: string): string {
  try {
    const parsed = new URL(origin);
    // The URL leaves out a scheme's own port, 443 for https.
    return parsed.protocol === "https:" ? parsed.port : "";
  } catch {
    return "";
  }
}
