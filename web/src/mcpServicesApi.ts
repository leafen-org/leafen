import { request } from "@/apiTransport";
import type { Session } from "@/types";

/** An outside service the person connected to Leafen by its remote MCP address. */
export type MCPService = {
  id: string;
  url: string;
  name: string;
  auth: "none" | "token" | "oauth";
  status: "signing_in" | "connected" | "sign_in_again" | "unreachable";
  failure?: string;
  /** Notion's "항상 허용": every tool runs without asking. */
  alwaysAllow?: boolean;
  /** How many tools the service has on for agents; they read them a window at a time. */
  toolCount: number;
  connectedAt: number;
  /**
   * After the server's address changed, a client the person made on the
   * service: the return address it has, the one to add, and whether they
   * said it is.
   */
  earlierCallback?: string;
  callback?: string;
  callbackAdded?: boolean;
};

/** A connection's answer: the service, and its sign-in page when it asks the person to sign in. */
export type MCPServiceAnswer = { service: MCPService; authorizeUrl?: string };

const servicePath = (serviceId: string) => `/mcp-services/${encodeURIComponent(serviceId)}`;

/** A window of the person's services, the latest connected first; nextCursor reads on. */
export function listMCPServices(session: Session, cursor?: string, signal?: AbortSignal) {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return request<{ services: MCPService[]; nextCursor?: string }>(`/mcp-services${query}`, { method: "GET", signal }, session);
}

export function readMCPService(session: Session, serviceId: string, signal?: AbortSignal) {
  return request<{ service: MCPService }>(servicePath(serviceId), { method: "GET", signal }, session);
}

/** An OAuth client the person made on a service for Leafen, for a service that lets no client register itself. */
export type MCPServiceClient = { clientId: string; clientSecret?: string };

/** How the service at an address signs in: its sign-in page (and the scopes it asks for), or headers the person gives. */
export type MCPServiceProbe = {
  url: string;
  signIn: "oauth" | "headers";
  /** The server answered with no sign-in at all. */
  open: boolean;
  name?: string;
  scopes: string[];
  callbackUrl?: string;
  /**
   * The person makes a client on the service for Leafen before signing in,
   * as the refusal's code says: the service registers no clients, or reads
   * Leafen at an address it can't reach (publicUrl: Leafen's public address,
   * where it has one). `note` says why, in the words an agent is told.
   */
  clientNeeded?: "LEAFEN_MCP_SERVICE_CLIENT_NEEDED" | "LEAFEN_MCP_SERVICE_ADDRESS_UNREACHABLE";
  publicUrl?: string;
  note?: string;
  /** The sign-in server the service named isn't answering now: its sign-in page all the same, said with a note. */
  signInUnavailable?: boolean;
};

export function probeMCPService(session: Session, url: string, signal?: AbortSignal) {
  return request<MCPServiceProbe>("/mcp-services/probe", { method: "POST", body: JSON.stringify({ url }), signal }, session);
}

/** How a service without a sign-in page signs in, as Notion's form gives it. */
export type MCPServiceHeaderAuth =
  | { type: "bearer"; prefix: "Bearer" | "Token"; token: string }
  | { type: "basic"; username: string; password: string }
  | { type: "api_key"; key: string; value: string };

/** What the person's form gives: a name, and the headers or the sign-in's scopes and own client. */
export type MCPServiceAddition = {
  name?: string;
  token?: string;
  auth?: MCPServiceHeaderAuth;
  headers?: { name: string; value: string }[];
  scopes?: string[];
  client?: MCPServiceClient;
};

export function addMCPService(session: Session, url: string, given: MCPServiceAddition = {}) {
  const { client, ...rest } = given;
  return request<MCPServiceAnswer>("/mcp-services", {
    method: "POST", body: JSON.stringify({ url, ...rest,
      ...(client?.clientId ? { clientId: client.clientId, ...(client.clientSecret ? { clientSecret: client.clientSecret } : {}) } : {}) }),
  }, session);
}

export function signInMCPService(session: Session, serviceId: string) {
  return request<MCPServiceAnswer>(`${servicePath(serviceId)}/sign-in`, { method: "POST" }, session);
}

/** Says the person's client on the service has the server's new return address, or takes it back. */
export function setMCPServiceReturn(session: Session, serviceId: string, added: boolean) {
  return request<{ service: MCPService }>(`${servicePath(serviceId)}/return`, { method: "POST", body: JSON.stringify({ added }) }, session);
}

export function checkMCPService(session: Session, serviceId: string) {
  return request<MCPServiceAnswer>(`${servicePath(serviceId)}/check`, { method: "POST" }, session);
}

/** One of a service's tools as its person sets it: on or off, and run at once or asked first. */
export type MCPServiceToolSetting = {
  name: string;
  title?: string;
  description?: string;
  readOnly: boolean;
  destructive: boolean;
  enabled: boolean;
  run: "auto" | "ask";
};

/** A window of the service's tools; nextCursor reads on. */
export function listMCPServiceTools(session: Session, serviceId: string, cursor?: string, signal?: AbortSignal) {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return request<{ service: MCPService; tools: MCPServiceToolSetting[]; nextCursor?: string }>(
    `/mcp-services/tools/${encodeURIComponent(serviceId)}${query}`, { method: "GET", signal }, session);
}

export function setMCPServiceTool(session: Session, serviceId: string, tool: string, change: { enabled?: boolean; run?: "auto" | "ask" }) {
  return request<MCPServiceToolSetting>(`/mcp-services/tools/${encodeURIComponent(serviceId)}/${encodeURIComponent(tool)}`, {
    method: "PATCH", body: JSON.stringify(change),
  }, session);
}

export function setMCPServiceAlwaysAllow(session: Session, serviceId: string, alwaysAllow: boolean) {
  return request<MCPServiceAnswer>(servicePath(serviceId), { method: "PATCH", body: JSON.stringify({ alwaysAllow }) }, session);
}

export function removeMCPService(session: Session, serviceId: string) {
  return request<void>(servicePath(serviceId), { method: "DELETE" }, session);
}

/** Which of the person's services an agent connection uses: a window of them, nextCursor reading on. */
export type MCPConnectionServices = {
  mode: "all" | "chosen";
  services: { id: string; name: string; address: string; status: string; tools: number; chosen: boolean }[];
  nextCursor?: string;
};

const connectionServicesPath = (connectionId: string) => `/mcp/connections/${encodeURIComponent(connectionId)}/services`;

export function readConnectionServices(session: Session, connectionId: string, cursor?: string) {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return request<MCPConnectionServices>(`${connectionServicesPath(connectionId)}${query}`, { method: "GET" }, session);
}

/** Sets all services or the chosen ones, and with a service, whether it is chosen. */
export function setConnectionServices(session: Session, connectionId: string, mode: "all" | "chosen",
  change?: { serviceId: string; chosen: boolean }) {
  return request<{ mode: "all" | "chosen" }>(connectionServicesPath(connectionId), {
    method: "PUT", body: JSON.stringify({ mode, ...change }),
  }, session);
}
