import type { Collection, Key, Node as CollectionNode } from "react-stately";

/**
 * A picker's rows as Aria's list collection, in place of the one Aria builds
 * from an `<Item>` for each row: its keys are read once for each list of
 * rows and a row's node is made the first time something reads it, so a
 * list of 100,000 rows (a relation to a large database, 2026-11-11) opens
 * and moves its highlight without an element and a node for every row.
 * Each row is an item of its own, named by its text, as `<Item key
 * textValue>{text}</Item>` makes it; a key given twice keeps its first
 * place and its last row, as Aria's list collection does.
 */
export class PickerCollection<T> implements Collection<CollectionNode<T>> {
  private readonly keys: string[] = [];
  private readonly rows: T[] = [];
  private readonly places = new Map<Key, number>();
  private readonly nodes: (CollectionNode<T> | undefined)[] = [];

  constructor(items: Iterable<T>, keyOf: (item: T) => string, private readonly textOf: (item: T) => string) {
    for (const item of items) {
      const key = keyOf(item), place = this.places.get(key);
      if (place !== undefined) { this.rows[place] = item; continue; }
      this.places.set(key, this.keys.length);
      this.keys.push(key);
      this.rows.push(item);
    }
  }

  get size() { return this.keys.length; }

  getKeys(): Iterable<Key> { return this.keys.values(); }

  *[Symbol.iterator](): IterableIterator<CollectionNode<T>> {
    for (let index = 0; index < this.keys.length; index++) yield this.at(index)!;
  }

  at(index: number): CollectionNode<T> | null {
    if (index < 0 || index >= this.keys.length) return null;
    const made = this.nodes[index];
    if (made) return made;
    const key = this.keys[index], value = this.rows[index], text = this.textOf(value);
    const node: CollectionNode<T> = {
      type: "item", key, value, index, level: 0, parentKey: null, hasChildNodes: false, childNodes: [],
      rendered: text, textValue: text, "aria-label": undefined, props: { textValue: text, children: text },
      prevKey: index > 0 ? this.keys[index - 1] : undefined, nextKey: this.keys[index + 1],
    };
    this.nodes[index] = node;
    return node;
  }

  /** A row's place in the list, or -1. */
  indexOf(key: Key | null | undefined): number {
    return key == null ? -1 : this.places.get(key) ?? -1;
  }

  getItem(key: Key): CollectionNode<T> | null { return this.at(this.indexOf(key)); }

  getKeyBefore(key: Key): Key | null {
    const place = this.indexOf(key);
    return place > 0 ? this.keys[place - 1] : null;
  }

  getKeyAfter(key: Key): Key | null {
    const place = this.indexOf(key);
    return place < 0 ? null : this.keys[place + 1] ?? null;
  }

  getFirstKey(): Key | null { return this.keys[0] ?? null; }

  getLastKey(): Key | null { return this.keys[this.keys.length - 1] ?? null; }

  getChildren(): Iterable<CollectionNode<T>> { return []; }

  getTextValue(key: Key): string { return this.getItem(key)?.textValue ?? ""; }
}
