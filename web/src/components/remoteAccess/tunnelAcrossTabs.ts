import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";

// What any of the person's tabs reads or sets of the address is told to the
// others, which follow it without asking (followAcrossTabs).
const tunnelTopic = (userId: string) => `admin-tunnel:${userId}`;

/** Tells the person's other tabs the address's state just read or set. */
export function tellTunnel(userId: string, status: unknown): void {
  tellTabs(tunnelTopic(userId), status);
}

/** Follows the address's state every `every` ms for all of the person's tabs: `read` asks, `heard` hears; until the stop. */
export function followTunnel(userId: string, every: number, read: () => Promise<unknown>, heard: (value: unknown) => void): () => void {
  const topic = tunnelTopic(userId);
  return followAcrossTabs(topic, every, () => read().then((next) => tellTabs(topic, next)), heard);
}
