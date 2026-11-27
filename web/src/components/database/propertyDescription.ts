// A property's description, as Notion lets each property have one (seen
// 2026-09-30): kept in its settings as config.description, where the public
// API, MCP and Notion import read and write it.

type DescribedProperty = { config?: unknown };

/** The description a property was given, or "". */
export function propertyDescription(property: DescribedProperty) {
  const description = (property.config as { description?: unknown } | null | undefined)?.description;
  return typeof description === "string" ? description.trim() : "";
}

/** A property's settings with a new description; none leaves no description. */
export function configWithDescription<T extends Record<string, unknown>>(config: T, description: string): T {
  const { description: _previous, ...rest } = config;
  const next = description.trim();
  return (next ? { ...rest, description: next } : rest) as T;
}
