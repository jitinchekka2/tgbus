// Runs the journey planner's WebAssembly build in Node, the way the page's
// Web Worker uses it. Needs a built site, so it is skipped unless TGBUS_DIST
// names a directory containing planner.wasm, wasm_exec.js and data/timetable.bin:
//
//   make site FEED=path/to/feed && TGBUS_DIST=dist node --test web/test/wasm.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const dist = process.env.TGBUS_DIST ? resolve(process.env.TGBUS_DIST) : "";
const ready = dist && existsSync(join(dist, "planner.wasm"));

test("planner WebAssembly", { skip: ready ? false : "set TGBUS_DIST to a built site (make site) to run" }, async (t) => {
  createRequire(import.meta.url)(join(dist, "wasm_exec.js")); // defines globalThis.Go
  const go = new globalThis.Go();
  const { instance } = await WebAssembly.instantiate(readFileSync(join(dist, "planner.wasm")), go.importObject);
  go.run(instance); // never resolves: the Go program waits to serve calls
  const api = globalThis.tgbusPlanner;
  const plan = (req) => JSON.parse(api.plan(JSON.stringify(req)));

  await t.test("refuses to plan before a timetable is loaded", () => {
    assert.equal(plan({}).code, "not_loaded");
  });

  await t.test("load reports corrupt data as a message, not an exception", () => {
    assert.match(api.load(new Uint8Array([1, 2, 3, 4, 5, 6])), /bad header|timetable/);
  });

  await t.test("loads the real timetable", () => {
    const err = api.load(new Uint8Array(readFileSync(join(dist, "data", "timetable.bin"))));
    assert.equal(err, "");
    const [stops, routes] = api.counts().split(",").map(Number);
    assert.ok(stops >= 4 && routes >= 2, `counts: ${api.counts()}`);
  });

  // Tiny fixture feed (4 stops) or the full Telangana feed; pick queries to match.
  const fixture = api.counts() === "4,2";
  const q = fixture
    ? { fromLat: 17.385, fromLon: 78.486, toLat: 17.4, toLon: 78.56, depart: 7 * 3600 + 50 * 60, arrive: 8 * 3600 + 30 * 60 }
    : { fromLat: 17.38495, fromLon: 78.48848, toLat: 17.19144, toLon: 78.64767, depart: 6 * 3600 + 50 * 60, arrive: 7 * 3600 + 59 * 60 };

  await t.test("plans a journey with legs and times", () => {
    const r = plan(q);
    assert.equal(r.code, undefined, r.error);
    assert.ok(r.journeys.length >= 1);
    const j = r.journeys[0];
    // Walking times are rounded up to whole seconds, so compare to the minute.
    assert.equal(Math.floor(j.arrive / 60), Math.floor(q.arrive / 60));
    assert.equal(j.depart, q.depart);
    assert.equal(j.legs[0].mode, "walk");
    assert.equal(j.legs[0].from, null);
    assert.equal(j.legs.at(-1).to, null);
    const ride = j.legs.find((l) => l.mode === "ride");
    assert.ok(ride.route.id && ride.stops.length >= 2);
    assert.equal(ride.stops[0].id, ride.from.id);
  });

  await t.test("reports an unreachable place with a code", () => {
    const r = plan({ ...q, fromLat: 28.6, fromLon: 77.2 }); // Delhi
    assert.equal(r.code, "no_stops");
    assert.deepEqual(r.journeys, []);
  });

  await t.test("rejects malformed requests", () => {
    assert.equal(JSON.parse(api.plan("not json")).code, "bad_request");
    assert.equal(plan({ ...q, depart: 86400 }).code, "bad_request");
    assert.equal(JSON.parse(api.plan(42)).code, "bad_request");
  });
});
