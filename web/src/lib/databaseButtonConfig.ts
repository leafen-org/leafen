import type { GraphProperty } from "@/contentGraphWire.generated";
import type { AutomationAction } from "@/lib/types";

/** A database button's actions, as its config's document keeps them. */
export function databaseButtonActions(property: Pick<GraphProperty, "config">): AutomationAction[] {
  const document = property.config.button as { actions?: unknown } | undefined;
  return Array.isArray(document?.actions) ? document.actions as AutomationAction[] : [];
}

/**
 * A database button's config with its actions stored as the versioned
 * document the server runs. The document's label is the property's name, as
 * the button shows; no actions leave no document.
 */
export function databaseButtonConfig(
  property: Pick<GraphProperty, "id" | "name" | "config">,
  actions: AutomationAction[],
): Record<string, unknown> {
  const { button: _previous, actions: _actions, ...config } = property.config;
  return actions.length === 0
    ? config
    : { ...config, button: { version: 1, label: property.name.trim() || property.id, actions } };
}
