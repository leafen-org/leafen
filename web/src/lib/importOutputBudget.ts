import { workspaceImportBounds } from "@/workspaceImportWire.generated";

/** A document larger than Leafen imports, as it is or once converted. */
export class FileImportTooLargeError extends Error {}

/**
 * Counts a conversion's output as it grows: past what Leafen imports at
 * once, the conversion stops with FileImportTooLargeError — never with a
 * result cut short. It counts characters, of which the bytes the importer
 * measures are at least as many.
 */
export class ImportOutputBudget {
  private used = 0;

  constructor(private readonly limit: number = workspaceImportBounds.rawTextBytes) {}

  add(characters: number) {
    this.used += characters;
    if (this.used > this.limit) throw new FileImportTooLargeError("The converted document is larger than Leafen imports at once.");
  }
}

/** Refuses a place (a row, a column) past what the text Leafen imports
 * could reach: each before it takes at least one character. */
export function assertImportReach(position: number) {
  if (position > workspaceImportBounds.rawTextBytes) {
    throw new FileImportTooLargeError("The converted document is larger than Leafen imports at once.");
  }
}
