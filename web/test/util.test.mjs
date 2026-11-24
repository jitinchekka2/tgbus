// Run with: node --test web/test
import test from "node:test";
import assert from "node:assert/strict";
import {
  clock, formatMinutes, hashFor, isSafeId, nextDepartures, nowMinutes, parseClock, parseHash,
  routeDesc, searchRoutes, searchStops, shortLabel, waitText,
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
