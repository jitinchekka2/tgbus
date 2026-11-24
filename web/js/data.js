import { CONFIG } from "./config.js";
import { isSafeId } from "./util.js";

const cache = new Map();

/** Fetch JSON once per path; failures are not cached so a retry can succeed. */
export function getJSON(path) {
  if (cache.has(path)) return cache.get(path);
  const p = fetch(path).then((res) => {
    if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
    return res.json();
  });
  cache.set(path, p);
  p.catch(() => cache.delete(path));
  return p;
}

function detail(kind, id) {
  if (!isSafeId(id)) return Promise.reject(new Error(`invalid ${kind} id`));
  return getJSON(`${CONFIG.dataBase}${kind}s/${id}.json`);
}

export const getRoute = (id) => detail("route", id);
export const getStop = (id) => detail("stop", id);
export const getMeta = () => getJSON(`${CONFIG.dataBase}meta.json`);
export const getOverview = () => getJSON(`${CONFIG.dataBase}overview.json`);
export const getStops = () => getJSON(`${CONFIG.dataBase}stops.json`);
export const getSearch = () => getJSON(`${CONFIG.dataBase}search.json`);
