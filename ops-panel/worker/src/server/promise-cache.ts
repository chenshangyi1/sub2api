type PromiseCacheEntry<T> = {
  expiresAt: number;
  pending: boolean;
  promise: Promise<T>;
};

type PromiseCacheOptions = {
  bypass?: boolean;
  now?: number;
};

export class BoundedPromiseCache<K, V> {
  private readonly entries = new Map<K, PromiseCacheEntry<V>>();

  constructor(
    private readonly ttlMs: number,
    private readonly maxEntries = 128,
  ) {
    if (ttlMs <= 0 || maxEntries <= 0) {
      throw new Error("promise cache limits must be positive");
    }
  }

  getOrCreate(key: K, load: () => Promise<V>, options: PromiseCacheOptions = {}) {
    const now = options.now ?? Date.now();
    this.prune(now);

    const cached = this.entries.get(key);
    if (!options.bypass && cached && (cached.pending || cached.expiresAt > now)) {
      return cached.promise;
    }

    const promise = load();
    this.entries.delete(key);
    this.entries.set(key, { expiresAt: now + this.ttlMs, pending: true, promise });
    this.prune(now);

    void promise.then(
      () => {
        const entry = this.entries.get(key);
        if (entry?.promise === promise) entry.pending = false;
      },
      () => {
        if (this.entries.get(key)?.promise === promise) {
          this.entries.delete(key);
        }
      },
    );
    return promise;
  }

  private prune(now: number) {
    for (const [key, entry] of this.entries) {
      if (!entry.pending && entry.expiresAt <= now) this.entries.delete(key);
    }
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value as K | undefined;
      if (oldest === undefined) break;
      this.entries.delete(oldest);
    }
  }
}
