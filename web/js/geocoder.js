// Address and place search through OpenStreetMap's Nominatim service.
//
// Nominatim's usage policy forbids as-you-type search and allows about one
// request per second, so this only runs when the visitor asks for it (Enter
// or the "Search address" button), spaces requests out, and remembers
// answers. Only the typed text (and the browser's IP address and Referer, as
// with any request) leaves the page, and only when a search is made.

/** Request URL for a search, limited to a bounding box [west, south, east, north]. */
export function geocodeUrl(base, query, bbox, limit = 5) {
  const p = new URLSearchParams({
    q: query.trim(),
    format: "jsonv2",
    limit: String(limit),
    countrycodes: "in",
    "accept-language": "en",
  });
  if (bbox) {
    // Nominatim wants left,top,right,bottom.
    p.set("viewbox", `${bbox[0]},${bbox[3]},${bbox[2]},${bbox[1]}`);
    p.set("bounded", "1");
  }
  return `${base}?${p}`;
}

/**
 * Nominatim rows -> [{ label, lat, lon, kind }]. The long display name is
 * shortened to its first three parts, and duplicates are dropped.
 */
export function parseNominatim(rows, limit = 5) {
  if (!Array.isArray(rows)) return [];
  const seen = new Set();
  const out = [];
  for (const r of rows) {
    const lat = Number(r.lat);
    const lon = Number(r.lon);
    if (!Number.isFinite(lat) || !Number.isFinite(lon) || typeof r.display_name !== "string") continue;
    const label = r.display_name.split(", ").slice(0, 3).join(", ");
    const key = `${label}|${lat.toFixed(4)}|${lon.toFixed(4)}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ label, lat, lon, kind: String(r.type || r.category || "").replace(/_/g, " ") });
    if (out.length >= limit) break;
  }
  return out;
}

/** Bounding box [west, south, east, north] of points, padded by `pad` degrees. */
export function boundsOfPoints(points, pad = 0.1) {
  if (!points.length) return null;
  let w = Infinity, s = Infinity, e = -Infinity, n = -Infinity;
  for (const p of points) {
    w = Math.min(w, p.lon); e = Math.max(e, p.lon);
    s = Math.min(s, p.lat); n = Math.max(n, p.lat);
  }
  return [w - pad, s - pad, e + pad, n + pad];
}

/**
 * @param {object} o
 * @param {string} o.url              Nominatim search endpoint
 * @param {number} [o.minGapMs=1100]  minimum time between requests
 * @param {typeof fetch} [o.fetchFn]  injectable for tests
 * @param {(ms: number) => Promise<void>} [o.sleep]
 * @param {() => number} [o.now]
 */
export function createGeocoder({ url, minGapMs = 1100, fetchFn = (...a) => fetch(...a), sleep = (ms) => new Promise((r) => setTimeout(r, ms)), now = () => Date.now() }) {
  const cache = new Map();
  let lastAt = -Infinity;
  let chain = Promise.resolve();

  async function request(query, bbox) {
    const wait = lastAt + minGapMs - now();
    if (wait > 0) await sleep(wait);
    lastAt = now();
    let res;
    try {
      res = await fetchFn(geocodeUrl(url, query, bbox));
    } catch {
      throw new Error("Address search is not reachable right now.");
    }
    if (res.status === 429) throw new Error("Address search is busy. Try again in a moment.");
    if (!res.ok) throw new Error("Address search failed. Try again later.");
    return parseNominatim(await res.json());
  }

  return {
    /** Resolves with [{ label, lat, lon, kind }]; rejects with a user-readable Error. */
    search(query, bbox) {
      const q = (query || "").trim();
      if (q.length < 2) return Promise.resolve([]);
      const key = `${q.toLowerCase()}|${bbox ? bbox.map((v) => v.toFixed(2)).join(",") : ""}`;
      if (cache.has(key)) return Promise.resolve(cache.get(key));
      // Serialise so two quick searches cannot exceed the rate limit.
      const run = chain.then(() => request(q, bbox)).then((r) => { cache.set(key, r); return r; });
      chain = run.catch(() => {});
      return run;
    },
  };
}
