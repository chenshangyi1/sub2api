import assert from "node:assert/strict";
import test from "node:test";

import { BoundedPromiseCache } from "@/server/promise-cache";

test("promise cache shares in-flight work and refreshes expired values", async () => {
  const cache = new BoundedPromiseCache<string, number>(100);
  let loads = 0;
  const load = async () => ++loads;

  const first = cache.getOrCreate("key", load, { now: 1_000 });
  const shared = cache.getOrCreate("key", load, { now: 1_050 });
  assert.equal(first, shared);
  assert.equal(await shared, 1);
  assert.equal(await cache.getOrCreate("key", load, { now: 1_100 }), 2);
});

test("promise cache keeps expired in-flight work shared", async () => {
  const cache = new BoundedPromiseCache<string, number>(100);
  let resolveLoad!: (value: number) => void;
  const pending = new Promise<number>((resolve) => {
    resolveLoad = resolve;
  });

  const first = cache.getOrCreate("key", () => pending, { now: 1_000 });
  const shared = cache.getOrCreate("key", async () => 2, { now: 1_100 });
  assert.equal(shared, first);

  resolveLoad(1);
  assert.equal(await first, 1);
  assert.equal(await cache.getOrCreate("key", async () => 2, { now: 1_101 }), 2);
});

test("promise cache evicts rejected work", async () => {
  const cache = new BoundedPromiseCache<string, number>(100);
  await assert.rejects(cache.getOrCreate("key", async () => {
    throw new Error("load failed");
  }, { now: 1_000 }), /load failed/);

  assert.equal(await cache.getOrCreate("key", async () => 2, { now: 1_001 }), 2);
});

test("promise cache bounds distinct keys and supports explicit refresh", async () => {
  const cache = new BoundedPromiseCache<string, number>(1_000, 2);
  let loads = 0;
  const load = async () => ++loads;

  await cache.getOrCreate("a", load, { now: 1_000 });
  await cache.getOrCreate("b", load, { now: 1_001 });
  await cache.getOrCreate("c", load, { now: 1_002 });
  assert.equal(await cache.getOrCreate("a", load, { now: 1_003 }), 4);
  assert.equal(await cache.getOrCreate("a", load, { now: 1_004, bypass: true }), 5);
});
