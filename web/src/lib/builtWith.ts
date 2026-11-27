// The tools that helped build Leafen, thanked by name with a link to each
// one's own site. Product-authored and static: nothing fetched, nothing paid
// for, no product entitlement.
export interface BuiltWithEntry {
  name: string;
  url: string;
}

export const BUILT_WITH: readonly BuiltWithEntry[] = [
  { name: "Cloudflare", url: "https://www.cloudflare.com" },
  { name: "Claude", url: "https://claude.com" },
  { name: "ChatGPT", url: "https://openai.com" },
  { name: "GLM", url: "https://z.ai" },
  { name: "GitHub", url: "https://github.com" },
];
