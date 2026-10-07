/**
 * Node >= 22 defines a `localStorage` global that stays `undefined` unless the
 * process is started with `--localstorage-file`. Vitest's jsdom environment only
 * copies browser globals that globalThis does not already own, so that shadowed
 * getter wins and `localStorage` ends up undefined inside tests.
 * Install a minimal Storage implementation when that happens.
 */
class MemoryStorage implements Storage {
  private store = new Map<string, string>();

  get length() {
    return this.store.size;
  }

  clear() {
    this.store.clear();
  }

  getItem(key: string) {
    return this.store.has(key) ? (this.store.get(key) as string) : null;
  }

  key(index: number) {
    return [...this.store.keys()][index] ?? null;
  }

  removeItem(key: string) {
    this.store.delete(key);
  }

  setItem(key: string, value: string) {
    this.store.set(key, String(value));
  }
}

if (typeof globalThis.localStorage === 'undefined') {
  Object.defineProperty(globalThis, 'localStorage', {
    value: new MemoryStorage(),
    configurable: true,
    writable: true,
  });
}
