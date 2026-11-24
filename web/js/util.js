// Pure helpers with no DOM or map access, so they can be tested with `node --test`.

const SAFE_ID = /^[A-Za-z0-9_-]+$/;

export const isSafeId = (id) => typeof id === "string" && SAFE_ID.test(id);

/** Minutes since midnight -> "HH:MM". Values past 1440 keep counting (25:10). */
export function formatMinutes(min) {
  const h = Math.floor(min / 60);
  const m = min % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
}

/** Wall-clock "HH:MM" for a minutes value, wrapping past midnight (1510 -> 01:10). */
export const clock = (min) => formatMinutes(((min % 1440) + 1440) % 1440);

/** "HH:MM" -> minutes, or null when malformed. */
export function parseClock(text) {
  const m = /^(\d{1,2}):(\d{2})$/.exec(text || "");
  if (!m || +m[1] > 23 || +m[2] > 59) return null;
  return +m[1] * 60 + +m[2];
}

/** Current minutes since midnight in the given IANA time zone. */
export function nowMinutes(timeZone, date = new Date()) {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone, hour: "2-digit", minute: "2-digit", hourCycle: "h23",
  }).formatToParts(date);
  const get = (type) => Number(parts.find((p) => p.type === type).value);
  return get("hour") * 60 + get("minute");
}

/**
 * Next `n` departures at or after `nowMin`. The service runs every day, so a
 * departure is the same time tomorrow; trips past midnight (>= 1440) count at
 * their wall-clock time. Returns [{ tod, wait }] sorted by waiting time.
 */
export function nextDepartures(deps, nowMin, n = 3) {
  const out = deps.map((d) => {
    const tod = ((d % 1440) + 1440) % 1440;
    return { tod, wait: (tod - nowMin + 1440) % 1440 };
  });
  out.sort((a, b) => a.wait - b.wait || a.tod - b.tod);
  return out.slice(0, n);
}

/** "now", "in 7 min", "in 1 h 5 min". */
export function waitText(wait) {
  if (wait <= 0) return "now";
  if (wait < 60) return `in ${wait} min`;
  const h = Math.floor(wait / 60);
  const m = wait % 60;
  return m ? `in ${h} h ${m} min` : `in ${h} h`;
}

/** Some routes carry their internal id as their number; show a dash instead. */
export const shortLabel = (id, short) => (!short || short === id ? "—" : short);

/** Long names look like "277D - KOTI to IBRAHIMPATNAM"; drop the number prefix. */
export const routeDesc = (long) => long.replace(/^.*? - /, "");

/**
 * Search rows [id, short, long, trips]: exact number first, then number
 * prefix, number substring, long-name substring. Input order breaks ties.
 */
export function searchRoutes(rows, query, limit = 20) {
  const q = (query || "").trim().toLowerCase();
  if (!q || limit <= 0) return [];
  const hits = [];
  for (const row of rows) {
    const short = row[1].toLowerCase();
    let score = -1;
    if (short === q) score = 0;
    else if (short.startsWith(q)) score = 1;
    else if (short.includes(q)) score = 2;
    else if (row[2].toLowerCase().includes(q)) score = 3;
    if (score >= 0) hits.push({ row, score });
  }
  hits.sort((a, b) => a.score - b.score); // stable
  return hits.slice(0, limit).map((h) => ({ id: h.row[0], short: h.row[1], long: h.row[2], trips: h.row[3] }));
}

/** Stops whose name starts with, then contains, the query; busier stops first. */
export function searchStops(stops, query, limit = 8) {
  const q = (query || "").trim().toLowerCase();
  if (q.length < 2 || limit <= 0) return [];
  const hits = [];
  for (const s of stops) {
    const name = s.name.toLowerCase();
    const at = name.indexOf(q);
    if (at < 0) continue;
    const wordStart = at === 0 || name[at - 1] === " ";
    hits.push({ s, score: at === 0 ? 0 : wordStart ? 1 : 2 });
  }
  hits.sort((a, b) => a.score - b.score || b.s.trips - a.s.trips);
  return hits.slice(0, limit).map((h) => h.s);
}

/** "17.385,78.486" -> { lat, lon }, or null when malformed or out of range. */
export function parseCoord(text) {
  const m = /^(-?\d{1,2}(?:\.\d+)?),(-?\d{1,3}(?:\.\d+)?)$/.exec(text || "");
  if (!m) return null;
  const lat = Number(m[1]);
  const lon = Number(m[2]);
  return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 ? { lat, lon } : null;
}

const round5 = (v) => String(Math.round(v * 1e5) / 1e5);

/** "08:30" -> "0830"; and back via parseClock. */
const compactClock = (min) => formatMinutes(min).replace(":", "");

/** "45 min", "1 h 5 min", "2 h". Seconds are rounded to the nearest minute. */
export function formatDuration(sec) {
  const min = Math.max(0, Math.round(sec / 60));
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  const m = min % 60;
  return m ? `${h} h ${m} min` : `${h} h`;
}

/** Walking distance: "350 m", "1.2 km". */
export function formatMetres(m) {
  return m < 1000 ? `${Math.round(m / 10) * 10} m` : `${(m / 1000).toFixed(1)} km`;
}

/** Wall-clock time of a seconds-since-midnight value, with a "+1" marker after midnight. */
export function clockSec(sec) {
  const day = Math.floor(sec / 86400);
  const text = formatMinutes(Math.floor((((sec % 86400) + 86400) % 86400) / 60));
  return day > 0 ? `${text} +${day}` : text;
}

/** Hash for a directions view. Missing places are "-"; the time is optional. */
export function planHash(from, to, minutes) {
  const place = (p) => (p ? `${round5(p.lat)},${round5(p.lon)}` : "-");
  const time = Number.isInteger(minutes) && minutes >= 0 && minutes < 1440 ? `/${compactClock(minutes)}` : "";
  return `#/plan/${place(from)}/${place(to)}${time}`;
}

/**
 * Location hash -> { view: "home" | "route" | "stop", id } or
 * { view: "plan", from, to, minutes } (places are { lat, lon } or null).
 * Ids and coordinates are validated.
 */
export function parseHash(hash) {
  const h = (hash || "").replace(/^#/, "");
  let m = /^\/(route|stop)\/([^/]+)$/.exec(h);
  if (m) {
    let id;
    try { id = decodeURIComponent(m[2]); } catch { return { view: "home" }; }
    return isSafeId(id) ? { view: m[1], id } : { view: "home" };
  }
  m = /^\/plan(?:\/([^/]*)\/([^/]*)(?:\/(\d{4}))?)?$/.exec(h);
  if (m) {
    const from = m[1] && m[1] !== "-" ? parseCoord(m[1]) : null;
    const to = m[2] && m[2] !== "-" ? parseCoord(m[2]) : null;
    const t = m[3] ? parseClock(`${m[3].slice(0, 2)}:${m[3].slice(2)}`) : null;
    return { view: "plan", from, to, minutes: t };
  }
  m = /^route=(.+)$/.exec(h); // links from the first prototype
  if (m) {
    let id;
    try { id = decodeURIComponent(m[1]); } catch { return { view: "home" }; }
    return isSafeId(id) ? { view: "route", id } : { view: "home" };
  }
  return { view: "home" };
}

export function hashFor(view, id) {
  return view === "home" || !id ? "#/" : `#/${view}/${encodeURIComponent(id)}`;
}
