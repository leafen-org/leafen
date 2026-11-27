// What the server sends to (outbound_public_network_policy.go
// normalizeOutboundPublicURL): an address on the open internet, not this
// computer or a private network, with no name and password and no "#".

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

function publicIPv4(parts: number[]) {
  const [a, b] = parts as [number, number, number, number];
  if (parts.some((part) => part > 255)) return false;
  if (parts.every((part) => part === 0) || parts.every((part) => part === 255)) return false;
  return !(a === 10 || a === 127 || a >= 224
    || (a === 100 && b >= 64 && b < 128)
    || (a === 169 && b === 254)
    || (a === 172 && b >= 16 && b < 32)
    || (a === 192 && b === 168));
}

function publicIPv6(address: string) {
  const lower = address.toLowerCase();
  const mapped = /^::ffff:(\d{1,3}(?:\.\d{1,3}){3})$/.exec(lower);
  if (mapped) return publicHost(mapped[1]!);
  if (lower === "::" || lower === "::1") return false;
  const head = parseInt(lower.split(":")[0] || "0", 16);
  // fc00::/7 private, fe80::/10 link-local, ff00::/8 multicast.
  return !((head & 0xfe00) === 0xfc00 || (head & 0xffc0) === 0xfe80 || (head & 0xff00) === 0xff00);
}

function publicHost(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/\.$/, "");
  if (!host || host === "localhost" || host.endsWith(".localhost")) return false;
  if (host.startsWith("[") && host.endsWith("]")) return publicIPv6(host.slice(1, -1));
  const v4 = IPV4.exec(host);
  return v4 ? publicIPv4(v4.slice(1).map(Number)) : true;
}

/** Whether the server would send to this http or https address. */
export function publicWebAddress(value: string) {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    return false;
  }
  return (url.protocol === "https:" || url.protocol === "http:") && !url.username && !url.password && !url.hash
    && publicHost(url.hostname);
}
