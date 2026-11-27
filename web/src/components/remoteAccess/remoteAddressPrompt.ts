import type { TFunction } from "i18next";
import { LeafenApiError } from "@/apiTransport";
import { settingsErrorMessage } from "@/lib/settingsErrors";

/**
 * Why a server's address or tunnel didn't change: what was given says what it
 * must be (2026-11-05: "couldn't change the tunnel" for a plain http address,
 * which has no tunnel to change).
 */
export function tunnelChangeMessage(failure: unknown, t: TFunction, given: "address" | "token" | "other"): string {
  if (failure instanceof LeafenApiError && failure.code === "LEAFEN_SERVER_TUNNEL_INVALID" && given !== "other") {
    return t(given === "address" ? "serverConsole:tunnel.addressInvalid" : "serverConsole:tunnel.tokenInvalid");
  }
  return settingsErrorMessage(failure, t("serverConsole:tunnel.applyFailed"));
}

/** What the server measured of its network (GET /admin/tunnel/reach). */
export type ServerReach = {
  os: string;
  arch: string;
  container: boolean;
  internet: boolean;
  publicIPv4?: string;
  ipv6?: string;
  tailscale: boolean;
  tailscaleProblem?: string;
  natpmp: boolean;
  upnp: boolean;
  situation: "closed" | "tailscale" | "ok";
  /** The server looks like a company's, which is offered its own address first (server_tunnel_company.go). */
  company?: boolean;
};

const REASONS = ["dns", "timeout", "intercepted", "refused", "answer", "unreachable"];

/** Why Tailscale can't be used, in the reader's words. */
export function tailscaleReason(t: TFunction, reach: ServerReach): string {
  const problem = reach.tailscaleProblem ?? "unreachable";
  return REASONS.includes(problem)
    ? t(`serverConsole:remoteChoice.reasons.${problem}`)
    : t("serverConsole:remoteChoice.reasons.failed", { failure: problem });
}

/**
 * The text an agent is given when Tailscale can't be used (the built-in
 * tunnel decision's changes of 2026-10-19): the facts the server measured,
 * that it works with the person — explaining the ways and asking before each
 * change to the network, security or money — the safety rules, and the last
 * step, the settings tool. A closed network turns it to a company address and
 * certificate. It closes the connection text (agentConnectionPrompt's
 * startWith), so the agent connects first and can reach the tool.
 */
export function remoteAddressPrompt(t: TFunction, reach: ServerReach): string {
  const yes = t("serverConsole:remoteChoice.prompt.yes");
  const no = t("serverConsole:remoteChoice.prompt.no");
  const unknown = t("serverConsole:remoteChoice.prompt.unknown");
  const closed = reach.situation === "closed";
  const company = reach.situation === "ok" && reach.company === true;
  const lines = [
    t(closed ? "serverConsole:remoteChoice.prompt.closedIntro" : company ? "serverConsole:remoteChoice.prompt.companyIntro" : "serverConsole:remoteChoice.prompt.ownIntro"),
    "",
    t("serverConsole:remoteChoice.prompt.factsTitle"),
    t("serverConsole:remoteChoice.prompt.factSystem", {
      system: `${reach.os}/${reach.arch}`, container: reach.container ? t("serverConsole:remoteChoice.prompt.container") : "",
    }),
    t("serverConsole:remoteChoice.prompt.factInternet", { value: reach.internet ? yes : no }),
    t("serverConsole:remoteChoice.prompt.factTailscale", { value: reach.tailscale ? yes : tailscaleReason(t, reach) }),
    t("serverConsole:remoteChoice.prompt.factIPv4", { value: reach.publicIPv4 || unknown }),
    t("serverConsole:remoteChoice.prompt.factIPv6", { value: reach.ipv6 || no }),
    t("serverConsole:remoteChoice.prompt.factPortMapping", { natpmp: reach.natpmp ? yes : no, upnp: reach.upnp ? yes : no }),
    "",
    t(closed ? "serverConsole:remoteChoice.prompt.closedWays" : "serverConsole:remoteChoice.prompt.ownWays"),
    "",
    t("serverConsole:remoteChoice.prompt.consult"),
    "",
    t("serverConsole:remoteChoice.prompt.safety"),
    "",
    t("serverConsole:remoteChoice.prompt.finish"),
  ];
  return lines.join("\n");
}
