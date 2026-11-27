import { withLoadDeadline } from "@/lib/lazyScreen";
/**
 * "코드 포맷팅", as Notion's code block menu has it for JavaScript,
 * TypeScript, JSON, CSS and HTML only (seen 2026-10-13), with Prettier's
 * shape: Prettier and the parsers a language needs load when it is chosen,
 * not with the editor.
 */
const FORMATTABLE = new Set(["javascript", "typescript", "json", "css", "html"]);

export function codeFormattable(language: string) {
  return FORMATTABLE.has(language);
}

/** The code formatted, without the final line break Prettier adds; throws when it can't be read. */
export async function formatCode(language: string, code: string): Promise<string> {
  const { format } = await withLoadDeadline(import("prettier/standalone"));
  const estree = () => withLoadDeadline(import("prettier/plugins/estree"));
  const babel = () => withLoadDeadline(import("prettier/plugins/babel"));
  const options = async () => {
    switch (language) {
      case "javascript": return { parser: "babel", plugins: await Promise.all([babel(), estree()]) };
      case "json": return { parser: "json", plugins: await Promise.all([babel(), estree()]) };
      case "typescript": return { parser: "typescript", plugins: await Promise.all([withLoadDeadline(import("prettier/plugins/typescript")), estree()]) };
      case "css": return { parser: "css", plugins: [await withLoadDeadline(import("prettier/plugins/postcss"))] };
      case "html": return { parser: "html", plugins: await Promise.all([
        withLoadDeadline(import("prettier/plugins/html")), withLoadDeadline(import("prettier/plugins/postcss")), babel(), estree()]) };
      default: throw new Error(`No formatting for ${language}.`);
    }
  };
  const formatted = await format(code, await options());
  return formatted.replace(/\n$/u, "");
}
