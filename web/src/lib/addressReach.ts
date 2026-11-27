import { networkReach } from "./networkReach.generated";

/**
 * Who can open an address (2026-10-20): "loopback" only this computer
 * (localhost, 127.x, ::1), "home" the same network (a home or office IP or
 * name, a tailnet address, a name of one label), "" anyone it is published to.
 */
export function addressReach(origin: string): "loopback" | "home" | "" {
  try {
    return hostReach(new URL(origin).hostname);
  } catch {
    return "";
  }
}

type Range = { bytes: number[]; bits: number };

function ipv4Bytes(text: string): number[] | null {
  const parts = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/u.exec(text);
  const bytes = parts ? parts.slice(1).map(Number) : [];
  return parts && bytes.every((byte) => byte <= 255) ? bytes : null;
}

function ipv6Words(text: string): number[] | null {
  if (text === "") return [];
  const pieces = text.split(":");
  const words: number[] = [];
  for (const [index, piece] of pieces.entries()) {
    const ipv4 = index === pieces.length - 1 ? ipv4Bytes(piece) : null;
    if (ipv4) words.push((ipv4[0] << 8) | ipv4[1], (ipv4[2] << 8) | ipv4[3]);
    else if (/^[0-9a-f]{1,4}$/u.test(piece)) words.push(parseInt(piece, 16));
    else return null;
  }
  return words;
}

// An IP address's bytes, an IPv4 one written as IPv6 (::ffff:a.b.c.d) as
// IPv4, as the server reads it; null for a name.
function addressBytes(host: string): number[] | null {
  const ipv4 = ipv4Bytes(host);
  if (ipv4 || !host.includes(":")) return ipv4;
  const halves = host.replace(/%.*$/u, "").split("::");
  const head = ipv6Words(halves[0]);
  const tail = halves.length === 2 ? ipv6Words(halves[1]) : [];
  if (halves.length > 2 || !head || !tail) return null;
  const fill = 8 - head.length - tail.length;
  if (halves.length === 2 ? fill < 1 : fill !== 0) return null;
  const bytes = [...head, ...new Array<number>(fill).fill(0), ...tail].flatMap((word) => [word >> 8, word & 0xff]);
  return bytes.slice(0, 10).every((byte) => byte === 0) && bytes[10] === 0xff && bytes[11] === 0xff ? bytes.slice(12) : bytes;
}

function range(text: string): Range {
  const [address, bits] = text.split("/");
  return { bytes: addressBytes(address) ?? [], bits: Number(bits ?? 128) };
}

function inRange(bytes: number[], { bytes: start, bits }: Range): boolean {
  if (bytes.length !== start.length) return false;
  for (let index = 0, left = bits; left > 0; index += 1, left -= 8) {
    const mask = left >= 8 ? 0xff : (0xff << (8 - left)) & 0xff;
    if ((bytes[index] & mask) !== (start[index] & mask)) return false;
  }
  return true;
}

const THIS_COMPUTER = networkReach.thisComputer.ranges.map(range);
const UNSPECIFIED = networkReach.thisComputer.unspecified.map((address) => range(`${address}/${address.includes(":") ? 128 : 32}`));
const NETWORK = networkReach.network.ranges.map(range);

/**
 * addressReach of a host name alone, by the one table the server judges
 * with too (lib/networkReach.generated.ts, from contracts/network-reach.json):
 * the unspecified address names this computer, and a name of one label is a
 * machine on this network (a compose service's, such as "leafen").
 */
export function hostReach(hostname: string): "loopback" | "home" | "" {
  const host = hostname.trim().toLowerCase().replace(/^\[|\]$/gu, "").replace(/\.$/u, "");
  if (host === "") return "";
  const bytes = addressBytes(host);
  if (bytes) {
    if ([...UNSPECIFIED, ...THIS_COMPUTER].some((each) => inRange(bytes, each))) return "loopback";
    return NETWORK.some((each) => inRange(bytes, each)) ? "home" : "";
  }
  const { thisComputer, network } = networkReach;
  if (thisComputer.names.some((name) => host === name) || thisComputer.suffixes.some((suffix) => host.endsWith(suffix))) return "loopback";
  if (network.oneLabelNames && !host.includes(".")) return "home";
  return network.suffixes.some((suffix) => host.endsWith(suffix)) ? "home" : "";
}

/**
 * Whether the server opens from outside at an address — on a phone or
 * another device anywhere, and to agents on the internet (Claude, ChatGPT),
 * which take only HTTPS: an https address at a host outside this computer
 * and its network. Every place that asks whether the server has its outside
 * address asks it here (2026-11-22: a place that asked only whether the
 * runtime settings named a public address took this computer's own,
 * LEAFEN_PUBLIC_URL=http://127.0.0.1…, for one); the server's outsideOrigin
 * says the same from the same table.
 */
export function outsideAddress(address: string): boolean {
  try {
    const url = new URL(address);
    return url.protocol === "https:" && hostReach(url.hostname) === "";
  } catch {
    return false;
  }
}
