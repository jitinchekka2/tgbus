// Run with: node --test web/test/*.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import {
  clock, clockSec, formatDuration, formatMetres, formatMinutes, hashFor, isSafeId, nextDepartures, nowMinutes,
  parseClock, parseCoord, parseHash, planHash, routeDesc, searchRoutes, searchStops, shortLabel, waitText,
} from "../js/util.js";

// Each test is a table of cases, mirroring the Go tests.
const table = (name, cases, fn) =>
  test(name, async (t) => {
    for (const c of cases) await t.test(c.name, () => fn(c));
  });

table("formatMinutes", [
  { name: "midnight", in: 0, want: "00:00" },
  { name: "morning", in: 485, want: "08:05" },
  { name: "past midnight keeps counting", in: 1510, want: "25:10" },
], (c) => assert.equal(formatMinutes(c.in), c.want));

table("clock wraps past midnight", [
  { name: "same day", in: 485, want: "08:05" },
  { name: "past midnight", in: 1510, want: "01:10" },
  { name: "exactly 24h", in: 1440, want: "00:00" },
  { name: "negative", in: -10, want: "23:50" },
], (c) => assert.equal(clock(c.in), c.want));

table("parseClock", [
  { name: "valid", in: "08:05", want: 485 },
  { name: "single digit hour", in: "8:05", want: 485 },
  { name: "end of day", in: "23:59", want: 1439 },
  { name: "hour out of range", in: "24:00", want: null },
  { name: "minute out of range", in: "10:60", want: null },
  { name: "garbage", in: "abc", want: null },
  { name: "empty", in: "", want: null },
  { name: "undefined", in: undefined, want: null },
], (c) => assert.equal(parseClock(c.in), c.want));

table("nowMinutes in India time", [
  { name: "UTC midnight is 05:30 IST", iso: "2026-01-01T00:00:00Z", want: 330 },
  { name: "18:30 UTC is midnight IST", iso: "2026-01-01T18:30:00Z", want: 0 },
  { name: "no daylight saving shift", iso: "2026-07-01T12:00:00Z", want: 17 * 60 + 30 },
], (c) => assert.equal(nowMinutes("Asia/Kolkata", new Date(c.iso)), c.want));

table("nextDepartures", [
  { name: "later today first", deps: [480, 600, 1200], now: 500, n: 2, want: [600, 1200] },
  { name: "wraps to tomorrow", deps: [480, 600], now: 1300, n: 2, want: [480, 600] },
  { name: "departure exactly now", deps: [480, 600], now: 480, n: 1, want: [480] },
  { name: "past-midnight trip shows wall-clock time", deps: [1510], now: 30, n: 1, want: [70] },
  { name: "fewer than n", deps: [480], now: 0, n: 3, want: [480] },
  { name: "none", deps: [], now: 0, n: 3, want: [] },
], (c) => assert.deepEqual(nextDepartures(c.deps, c.now, c.n).map((d) => d.tod), c.want));

test("nextDepartures wait minutes", () => {
  assert.deepEqual(nextDepartures([600], 590, 1), [{ tod: 600, wait: 10 }]);
  assert.deepEqual(nextDepartures([480], 1300, 1), [{ tod: 480, wait: 620 }]);
});

table("waitText", [
  { name: "now", in: 0, want: "now" },
  { name: "minutes", in: 7, want: "in 7 min" },
  { name: "whole hours", in: 120, want: "in 2 h" },
  { name: "hours and minutes", in: 65, want: "in 1 h 5 min" },
], (c) => assert.equal(waitText(c.in), c.want));

table("shortLabel and routeDesc", [
  { name: "normal number", id: "abc", short: "277D", label: "277D" },
  { name: "id used as number", id: "abc", short: "abc", label: "—" },
  { name: "empty number", id: "abc", short: "", label: "—" },
], (c) => assert.equal(shortLabel(c.id, c.short), c.label));

table("routeDesc", [
  { name: "strips number prefix", in: "277D - KOTI to UPPAL", want: "KOTI to UPPAL" },
  { name: "keeps later dashes", in: "10 - A - B", want: "A - B" },
  { name: "no prefix unchanged", in: "KOTI to UPPAL", want: "KOTI to UPPAL" },
], (c) => assert.equal(routeDesc(c.in), c.want));

const ROUTES = [
  ["r1", "10A", "10A - Koti, to Uppal", 3],
  ["r2", "100", "100 - Alpha to Beta", 5],
  ["r3", "20", "20 - Via 10A junction", 1],
  ["r4", "9", "9 - Gamma to Delta", 2],
];

table("searchRoutes ranking", [
  { name: "exact number first", q: "10a", want: ["r1", "r3"] },
  { name: "prefix before long-name match", q: "10", want: ["r1", "r2", "r3"] },
  { name: "long name, case-insensitive", q: "ALPHA", want: ["r2"] },
  { name: "surrounding space trimmed", q: "  9 ", want: ["r4"] },
  { name: "no match", q: "zzz", want: [] },
  { name: "empty", q: "", want: [] },
  { name: "limit", q: "1", limit: 1, want: ["r1"] },
  { name: "zero limit", q: "1", limit: 0, want: [] },
], (c) => assert.deepEqual(searchRoutes(ROUTES, c.q, c.limit ?? 20).map((r) => r.id), c.want));

const STOPS = [
  { id: "s1", name: "Koti", trips: 50 },
  { id: "s2", name: "Women's College Koti", trips: 10 },
  { id: "s3", name: "Kotagiri", trips: 99 },
  { id: "s4", name: "Charminar", trips: 200 },
];

table("searchStops ranking", [
  { name: "prefix matches first, busiest first", q: "kot", want: ["s3", "s1", "s2"] },
  { name: "word start beats mid-word", q: "col", want: ["s2"] },
  { name: "mid-word match", q: "min", want: ["s4"] },
  { name: "single character ignored", q: "k", want: [] },
  { name: "no match", q: "zz", want: [] },
], (c) => assert.deepEqual(searchStops(STOPS, c.q).map((s) => s.id), c.want));

table("parseHash", [
  { name: "empty", in: "", want: { view: "home" } },
  { name: "bare hash", in: "#", want: { view: "home" } },
  { name: "route", in: "#/route/00OqzfuB", want: { view: "route", id: "00OqzfuB" } },
  { name: "stop", in: "#/stop/RgqMpbyP", want: { view: "stop", id: "RgqMpbyP" } },
  { name: "legacy route link", in: "#route=00OqzfuB", want: { view: "route", id: "00OqzfuB" } },
  { name: "path traversal rejected", in: "#/route/..%2F..%2Fsecret", want: { view: "home" } },
  { name: "slash in id rejected", in: "#/route/a/b", want: { view: "home" } },
  { name: "unsafe legacy id rejected", in: "#route=../x", want: { view: "home" } },
  { name: "bad percent-encoding", in: "#/route/%E0%A4%A", want: { view: "home" } },
  { name: "unknown view", in: "#/nope/abc", want: { view: "home" } },
], (c) => assert.deepEqual(parseHash(c.in), c.want));

table("hashFor round-trips", [
  { name: "home", view: "home", id: undefined, want: "#/" },
  { name: "route", view: "route", id: "abc123", want: "#/route/abc123" },
  { name: "stop", view: "stop", id: "XyZ", want: "#/stop/XyZ" },
  { name: "route without id", view: "route", id: "", want: "#/" },
], (c) => {
  assert.equal(hashFor(c.view, c.id), c.want);
  if (c.view !== "home" && c.id) assert.deepEqual(parseHash(hashFor(c.view, c.id)), { view: c.view, id: c.id });
});

table("isSafeId", [
  { name: "alphanumeric", in: "RgqMpbyP", want: true },
  { name: "dash and underscore", in: "a-b_c", want: true },
  { name: "empty", in: "", want: false },
  { name: "dot", in: "a.b", want: false },
  { name: "slash", in: "a/b", want: false },
  { name: "not a string", in: 12, want: false },
], (c) => assert.equal(isSafeId(c.in), c.want));

table("parseCoord", [
  { name: "typical", in: "17.385,78.486", want: { lat: 17.385, lon: 78.486 } },
  { name: "negative", in: "-33.9,-70.5", want: { lat: -33.9, lon: -70.5 } },
  { name: "integers", in: "17,78", want: { lat: 17, lon: 78 } },
  { name: "latitude out of range", in: "91,10", want: null },
  { name: "longitude out of range", in: "10,181", want: null },
  { name: "spaces rejected", in: "17.3, 78.4", want: null },
  { name: "one number", in: "17.3", want: null },
  { name: "letters", in: "a,b", want: null },
  { name: "empty", in: "", want: null },
  { name: "undefined", in: undefined, want: null },
], (c) => assert.deepEqual(parseCoord(c.in), c.want));

table("formatDuration", [
  { name: "under a minute rounds to 0", in: 20, want: "0 min" },
  { name: "rounds to the nearest minute", in: 89, want: "1 min" },
  { name: "minutes", in: 45 * 60, want: "45 min" },
  { name: "an hour exactly", in: 3600, want: "1 h" },
  { name: "hours and minutes", in: 3600 + 5 * 60, want: "1 h 5 min" },
  { name: "negative clamps to zero", in: -50, want: "0 min" },
], (c) => assert.equal(formatDuration(c.in), c.want));

table("formatMetres", [
  { name: "rounds to ten metres", in: 347, want: "350 m" },
  { name: "just under a km", in: 994, want: "990 m" },
  { name: "kilometres", in: 1234, want: "1.2 km" },
], (c) => assert.equal(formatMetres(c.in), c.want));

table("clockSec marks the next day", [
  { name: "morning", in: 8 * 3600 + 5 * 60, want: "08:05" },
  { name: "last minute of the day", in: 86399, want: "23:59" },
  { name: "past midnight", in: 86400 + 70 * 60, want: "01:10 +1" },
  { name: "two days on", in: 2 * 86400 + 60, want: "00:01 +2" },
], (c) => assert.equal(clockSec(c.in), c.want));

table("planHash and parseHash for directions", [
  { name: "no places", hash: "#/plan", want: { view: "plan", from: null, to: null, minutes: null } },
  { name: "both places and a time", hash: "#/plan/17.385,78.486/17.19144,78.64767/0650",
    want: { view: "plan", from: { lat: 17.385, lon: 78.486 }, to: { lat: 17.19144, lon: 78.64767 }, minutes: 410 } },
  { name: "only the destination", hash: "#/plan/-/17.19,78.64",
    want: { view: "plan", from: null, to: { lat: 17.19, lon: 78.64 }, minutes: null } },
  { name: "bad coordinates become missing places", hash: "#/plan/99,500/17.19,78.64/0830",
    want: { view: "plan", from: null, to: { lat: 17.19, lon: 78.64 }, minutes: 510 } },
  { name: "bad time is ignored", hash: "#/plan/-/-/2560", want: { view: "plan", from: null, to: null, minutes: null } },
  { name: "extra path segments rejected", hash: "#/plan/-/-/0830/x", want: { view: "home" } },
], (c) => assert.deepEqual(parseHash(c.hash), c.want));

table("planHash round trip", [
  { name: "full", from: { lat: 17.385, lon: 78.486 }, to: { lat: 17.19144, lon: 78.64767 }, minutes: 410, want: "#/plan/17.385,78.486/17.19144,78.64767/0650" },
  { name: "coordinates are rounded to 5 places", from: { lat: 17.3850001234, lon: 78.4860009 }, to: null, minutes: null, want: "#/plan/17.385,78.486/-" },
  { name: "invalid time dropped", from: null, to: null, minutes: 1500, want: "#/plan/-/-" },
  { name: "midnight is a valid time", from: null, to: null, minutes: 0, want: "#/plan/-/-/0000" },
], (c) => {
  const h = planHash(c.from, c.to, c.minutes);
  assert.equal(h, c.want);
  const back = parseHash(h);
  assert.equal(back.view, "plan");
  assert.deepEqual(back.to, c.to && { lat: Number(c.to.lat.toFixed(5)), lon: Number(c.to.lon.toFixed(5)) });
});

// --- journeyToGeoJSON (pure; no DOM needed) ---------------------------------
import { isTrivialWalk, journeyToGeoJSON, legChips, RIDE_COLORS, walkMetres } from "../js/plan.js";

const stopA = { id: "A", name: "Alpha", lat: 17.1, lon: 78.1 };
const stopB = { id: "B", name: "Beta", lat: 17.2, lon: 78.2 };
const stopC = { id: "C", name: "Gamma", lat: 17.3, lon: 78.3 };
const route = (n) => ({ id: `id${n}`, short: String(n), long: `${n} - X to Y` });
const origin = { lat: 17.09, lon: 78.09 };
const dest = { lat: 17.31, lon: 78.31 };
const journey = {
  depart: 100, arrive: 900, rides: 2,
  legs: [
    { mode: "walk", from: null, to: stopA, dep: 100, arr: 160, walkM: 80 },
    { mode: "ride", route: route(1), from: stopA, to: stopB, dep: 200, arr: 400, stops: [stopA, stopB] },
    { mode: "ride", route: route(2), from: stopB, to: stopC, dep: 500, arr: 800, stops: [stopB, stopC] },
    { mode: "walk", from: stopC, to: null, dep: 800, arr: 900, walkM: 120 },
  ],
};

test("journeyToGeoJSON", async (t) => {
  const fc = journeyToGeoJSON(journey, origin, dest);
  const kinds = (k) => fc.features.filter((f) => f.properties.kind === k);

  await t.test("draws dashed walks from the origin point and to the destination point", () => {
    const walks = kinds("walk");
    assert.equal(walks.length, 2);
    assert.deepEqual(walks[0].geometry.coordinates, [[78.09, 17.09], [78.1, 17.1]]);
    assert.deepEqual(walks[1].geometry.coordinates, [[78.3, 17.3], [78.31, 17.31]]);
  });
  await t.test("each ride is a coloured line through its stops, colours cycling", () => {
    const rides = kinds("ride");
    assert.equal(rides.length, 2);
    assert.deepEqual(rides.map((r) => r.properties.color), [RIDE_COLORS[0], RIDE_COLORS[1]]);
    assert.deepEqual(rides[0].geometry.coordinates, [[78.1, 17.1], [78.2, 17.2]]);
    assert.equal(rides[1].properties.route, "2");
  });
  await t.test("marks boarding and alighting stops", () => {
    assert.deepEqual(kinds("stop").map((s) => `${s.properties.role}:${s.properties.name}`),
      ["board:Alpha", "alight:Beta", "board:Beta", "alight:Gamma"]);
  });
  await t.test("marks the origin and destination", () => {
    assert.deepEqual(kinds("origin")[0].geometry.coordinates, [78.09, 17.09]);
    assert.deepEqual(kinds("dest")[0].properties.label, "B");
  });
  await t.test("skips a zero-length walk (two stops at the same spot)", () => {
    const j = { ...journey, legs: [
      journey.legs[0], journey.legs[1],
      { mode: "walk", from: stopB, to: { ...stopB, id: "B2" }, dep: 400, arr: 400, walkM: 0 },
      journey.legs[2], journey.legs[3]] };
    assert.equal(journeyToGeoJSON(j, origin, dest).features.filter((f) => f.properties.kind === "walk").length, 2);
  });
  await t.test("a walking-only journey has just a walk and the end points", () => {
    const j = { depart: 0, arrive: 60, rides: 0, legs: [{ mode: "walk", from: null, to: null, dep: 0, arr: 60, walkM: 70 }] };
    const f = journeyToGeoJSON(j, origin, dest).features;
    assert.deepEqual(f.map((x) => x.properties.kind), ["walk", "origin", "dest"]);
  });
  await t.test("more rides than colours wraps around", () => {
    const legs = Array.from({ length: RIDE_COLORS.length + 1 }, (_, i) =>
      ({ mode: "ride", route: route(i), from: stopA, to: stopB, dep: i, arr: i + 1, stops: [stopA, stopB] }));
    const rides = journeyToGeoJSON({ depart: 0, arrive: 9, rides: legs.length, legs }, origin, dest).features.filter((f) => f.properties.kind === "ride");
    assert.equal(rides.at(-1).properties.color, RIDE_COLORS[0]);
  });
});

test("legChips", () => {
  assert.deepEqual(legChips(journey), [
    { kind: "walk", text: "1 min" }, { kind: "ride", text: "1" }, { kind: "ride", text: "2" }, { kind: "walk", text: "2 min" },
  ]);
});

table("isTrivialWalk", [
  { name: "start point is on the stop", leg: { mode: "walk", dep: 100, arr: 100, walkM: 0 }, want: true },
  { name: "a few seconds and metres", leg: { mode: "walk", dep: 100, arr: 120, walkM: 25 }, want: true },
  { name: "long enough to mention", leg: { mode: "walk", dep: 100, arr: 160, walkM: 80 }, want: false },
  { name: "short in time but not in distance", leg: { mode: "walk", dep: 100, arr: 110, walkM: 200 }, want: false },
  { name: "rides are never trivial", leg: { mode: "ride", dep: 100, arr: 100, walkM: 0 }, want: false },
  // The Go planner omits walkM when it is 0, so the field is often absent in real responses.
  { name: "missing walkM means zero metres", leg: { mode: "walk", dep: 100, arr: 100 }, want: true },
  { name: "missing walkM but a long wait is still not a short walk", leg: { mode: "walk", dep: 100, arr: 400 }, want: false },
], (c) => assert.equal(isTrivialWalk(c.leg), c.want));

table("walkMetres", [
  { name: "present", leg: { walkM: 120 }, want: 120 },
  { name: "zero", leg: { walkM: 0 }, want: 0 },
  { name: "omitted by the planner", leg: {}, want: 0 },
], (c) => assert.equal(walkMetres(c.leg), c.want));

test("legChips leaves out trivial walks", () => {
  const j = { legs: [
    { mode: "walk", dep: 0, arr: 0, walkM: 0 },
    { mode: "ride", route: { id: "x", short: "42" }, dep: 60, arr: 600 },
    { mode: "walk", dep: 600, arr: 600, walkM: 0 },
  ] };
  assert.deepEqual(legChips(j), [{ kind: "ride", text: "42" }]);
});

// --- geocoder (no network: fetch and the clock are injected) -----------------
import { boundsOfPoints, createGeocoder, geocodeUrl, parseNominatim } from "../js/geocoder.js";

const row = (name, lat, lon, type = "yes") => ({ display_name: name, lat: String(lat), lon: String(lon), type, category: "place" });

table("geocodeUrl", [
  { name: "encodes the query and limits to India, English", bbox: null, q: "Hi-Tech City, Hyderabad",
    has: ["q=Hi-Tech+City%2C+Hyderabad", "format=jsonv2", "countrycodes=in", "accept-language=en", "limit=5"], lacks: ["viewbox"] },
  { name: "bounds the search to a box (left,top,right,bottom)", bbox: [78, 16, 80, 18], q: "x",
    has: ["viewbox=78%2C18%2C80%2C16", "bounded=1"], lacks: [] },
], (c) => {
  const u = geocodeUrl("https://nominatim.example/search", c.q, c.bbox);
  assert.ok(u.startsWith("https://nominatim.example/search?"));
  for (const h of c.has) assert.ok(u.includes(h), `${u} should include ${h}`);
  for (const l of c.lacks) assert.ok(!u.includes(l), `${u} should not include ${l}`);
});

table("parseNominatim", [
  { name: "shortens the display name to three parts", rows: [row("Charminar, Laad Bazaar, Khilwat, Ward 49, Hyderabad, Telangana, India", 17.36, 78.47, "monument")],
    want: [{ label: "Charminar, Laad Bazaar, Khilwat", lat: 17.36, lon: 78.47, kind: "monument" }] },
  { name: "underscores in the type become spaces", rows: [row("A, B", 1, 2, "bus_stop")], want: [{ label: "A, B", lat: 1, lon: 2, kind: "bus stop" }] },
  { name: "drops duplicates", rows: [row("A, B", 17, 78), row("A, B", 17, 78)], want: [{ label: "A, B", lat: 17, lon: 78, kind: "yes" }] },
  { name: "skips rows without usable coordinates", rows: [{ display_name: "Bad", lat: "x", lon: "y" }, row("Good", 17, 78)], want: [{ label: "Good", lat: 17, lon: 78, kind: "yes" }] },
  { name: "skips rows without a name", rows: [{ lat: "17", lon: "78" }], want: [] },
  { name: "not an array", rows: { error: "boom" }, want: [] },
  { name: "empty", rows: [], want: [] },
], (c) => assert.deepEqual(parseNominatim(c.rows), c.want));

test("parseNominatim respects the limit", () => {
  const rows = Array.from({ length: 9 }, (_, i) => row(`Place ${i}`, 17 + i / 100, 78));
  assert.equal(parseNominatim(rows, 3).length, 3);
});

table("boundsOfPoints", [
  { name: "no points", pts: [], pad: 0.1, want: null },
  { name: "single point is padded", pts: [{ lat: 17, lon: 78 }], pad: 0.5, want: [77.5, 16.5, 78.5, 17.5] },
  { name: "box around several", pts: [{ lat: 16, lon: 78 }, { lat: 18, lon: 80 }, { lat: 17, lon: 79 }], pad: 0, want: [78, 16, 80, 18] },
], (c) => assert.deepEqual(boundsOfPoints(c.pts, c.pad), c.want));

test("createGeocoder", async (t) => {
  const mk = (responses) => {
    const calls = [];
    const clock = { t: 0, slept: [] };
    const g = createGeocoder({
      url: "https://n/search", minGapMs: 1000,
      fetchFn: async (u) => { calls.push(u); const r = responses.shift(); return r instanceof Error ? Promise.reject(r) : r; },
      sleep: async (ms) => { clock.slept.push(ms); clock.t += ms; },
      now: () => clock.t,
    });
    return { g, calls, clock };
  };
  const ok = (rows) => ({ ok: true, status: 200, json: async () => rows });

  await t.test("returns parsed results", async () => {
    const { g, calls } = mk([ok([row("Charminar, Hyderabad", 17.36, 78.47, "monument")])]);
    const r = await g.search("charminar", [78, 16, 80, 18]);
    assert.equal(r[0].label, "Charminar, Hyderabad");
    assert.equal(calls.length, 1);
    assert.ok(calls[0].includes("bounded=1"));
  });
  await t.test("remembers answers: a repeated search makes no request", async () => {
    const { g, calls } = mk([ok([row("A, B", 1, 2)])]);
    await g.search("Same Place");
    await g.search("same place ");
    assert.equal(calls.length, 1);
  });
  await t.test("spaces requests out to respect the rate limit", async () => {
    const { g, clock } = mk([ok([]), ok([])]);
    await g.search("one place");
    await g.search("another place");
    assert.deepEqual(clock.slept, [1000]); // the second waits out the gap; the first does not
  });
  await t.test("two searches started together are serialised", async () => {
    const { g, calls } = mk([ok([row("First", 1, 1)]), ok([row("Second", 2, 2)])]);
    const [a, b] = await Promise.all([g.search("first one"), g.search("second one")]);
    assert.equal(a[0].label, "First");
    assert.equal(b[0].label, "Second");
    assert.equal(calls.length, 2);
  });
  await t.test("too-short queries make no request", async () => {
    const { g, calls } = mk([]);
    assert.deepEqual(await g.search("a"), []);
    assert.deepEqual(await g.search("  "), []);
    assert.equal(calls.length, 0);
  });
  await t.test("a network failure becomes a readable error", async () => {
    const { g } = mk([new TypeError("fetch failed")]);
    await assert.rejects(g.search("somewhere"), /not reachable/);
  });
  await t.test("rate limiting by the server is reported", async () => {
    const { g } = mk([{ ok: false, status: 429, json: async () => ({}) }]);
    await assert.rejects(g.search("somewhere"), /busy/);
  });
  await t.test("a server error is reported, and a failure is not cached", async () => {
    const { g, calls } = mk([{ ok: false, status: 500, json: async () => ({}) }, ok([row("Back, Up", 3, 4)])]);
    await assert.rejects(g.search("somewhere"), /failed/);
    const r = await g.search("somewhere");
    assert.equal(r[0].label, "Back, Up");
    assert.equal(calls.length, 2);
  });
});
