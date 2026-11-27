/**
 * The separators the account's "숫자 형식" chooses, as Notion's (seen
 * 2026-10-02): numbers, currencies and chart labels keep the language's form
 * but take "1,000,000.00"'s or "1.000.000,00"'s separators; the default
 * keeps the language's. The account's display preferences set it
 * (displayPreferences.ts), and a change restarts the app, as Notion's does.
 */
export type NumberSeparatorChoice = "default" | "comma_period" | "period_comma";

let choice: NumberSeparatorChoice = "default";

export function setNumberSeparatorChoice(next: NumberSeparatorChoice) {
  choice = next;
}

/** The grouping and decimal separators chosen, or null for the language's. */
export function numberSeparators(): { group: string; decimal: string } | null {
  switch (choice) {
    case "comma_period": return { group: ",", decimal: "." };
    case "period_comma": return { group: ".", decimal: "," };
    default: return null;
  }
}
