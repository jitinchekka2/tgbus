import { el } from "./dom.js";
import { getMeta, getOverview, getRoute, getSearch, getStop, getStops } from "./data.js";
import {
  EMPTY, applyVisibility, boundsOf, createMap, currentScheme, installOverlay, setBasemap, setSource,
} from "./map.js";
import { errorCard, homeCard, loadingCard, routeCard, stopCard } from "./panel.js";
import { hashFor, parseHash, routeDesc, searchRoutes, searchStops, shortLabel } from "./util.js";

const $ = (id) => document.getElementById(id);

const state = {
  scheme: currentScheme(),
  overview: EMPTY, stops: EMPTY, route: EMPTY, selected: EMPTY,
  visible: { network: true, stops: true, heat: false },
  dim: false,
};
let map;
let searchRows = [];
let stopList = [];
let navSeq = 0;

const setView = (node) => $("view").replaceChildren(node);
const setStatus = (text) => { $("status").textContent = text; };
const navigate = (view, id) => { location.hash = hashFor(view, id); };
const pointFeature = (c) => ({ type: "Feature", properties: {}, geometry: { type: "Point", coordinates: c } });

function renderHome() {
  const busiest = [...stopList].sort((a, b) => b.trips - a.trips).slice(0, 8);
  setView(homeCard(busiest, (id) => navigate("stop", id)));
}

function clearSelection() {
  state.route = EMPTY;
  state.selected = EMPTY;
  state.dim = false;
  setSource(map, "route", EMPTY);
  setSource(map, "selected", EMPTY);
  applyVisibility(map, state);
}

const fitPadding = () =>
  window.innerWidth > 640 ? { top: 50, right: 50, bottom: 50, left: 390 }
    : { top: 50, right: 30, bottom: Math.round(window.innerHeight * 0.5), left: 30 };

function showRoute(data) {
  state.route = data;
  state.selected = EMPTY;
  state.dim = true;
  setSource(map, "route", data);
  setSource(map, "selected", EMPTY);
  applyVisibility(map, state);
  const bounds = boundsOf(data.features);
  if (!bounds.isEmpty()) map.fitBounds(bounds, { padding: fitPadding(), maxZoom: 15, duration: 700 });
  setView(routeCard(data, {
    onClose: () => navigate("home"),
    onOpenStop: (id) => navigate("stop", id),
    onFocusStop: (c) => setSource(map, "selected", c ? pointFeature(c) : EMPTY),
  }));
  document.title = `${shortLabel(data.route.id, data.route.short)} · TGSRTC bus network`;
}

function showStop(stop) {
  state.route = EMPTY;
  state.dim = false;
  state.selected = pointFeature([stop.lon, stop.lat]);
  setSource(map, "route", EMPTY);
  setSource(map, "selected", state.selected);
  applyVisibility(map, state);
  map.flyTo({ center: [stop.lon, stop.lat], zoom: Math.max(map.getZoom(), 14.5) });
  setView(stopCard(stop, {
    onClose: () => navigate("home"),
    onOpenRoute: (id) => navigate("route", id),
  }));
  document.title = `${stop.name} · TGSRTC bus network`;
}

// The URL hash is the single source of truth for what is shown.
async function applyHash() {
  const seq = ++navSeq;
  const { view, id } = parseHash(location.hash);
  if (view === "home") {
    clearSelection();
    renderHome();
    document.title = "TGSRTC bus network";
    return;
  }
  setView(loadingCard("Loading…"));
  try {
    const data = view === "route" ? await getRoute(id) : await getStop(id);
    if (seq !== navSeq) return; // a newer navigation superseded this one
    if (view === "route") showRoute(data); else showStop(data);
  } catch (err) {
    if (seq !== navSeq) return;
    clearSelection();
    const what = view === "route" ? "route" : "stop";
    setView(errorCard(`Could not load this ${what}. It may not exist in the current data.`, applyHash));
    console.error(err);
  }
}

async function loadData() {
  setStatus("Loading network…");
  try {
    const [overview, stops, search, meta] = await Promise.all([getOverview(), getStops(), getSearch(), getMeta()]);
    state.overview = overview;
    state.stops = stops;
    searchRows = search.routes;
    stopList = stops.features.map((f) => ({ id: f.properties.id, name: f.properties.name, trips: f.properties.trips }));
    for (const id of ["overview", "stops", "heat"]) setSource(map, id, id === "overview" ? overview : stops);
    setStatus(`${meta.routes.toLocaleString()} routes · ${meta.stops.toLocaleString()} stops · ${meta.trips.toLocaleString()} trips per day`);
    $("feed-info").textContent = `Feed: ${meta.feed} · built ${meta.builtAt.slice(0, 10)}`;
    if (parseHash(location.hash).view === "home") renderHome();
  } catch (err) {
    console.error(err);
    setStatus("Could not load the network data.");
    if (parseHash(location.hash).view === "home") setView(errorCard("Could not load the network data.", loadData));
  }
}

/* ---------- search combobox ---------- */

const search = { input: null, list: null, options: [], active: -1 };

function closeResults() {
  search.list.replaceChildren();
  search.list.hidden = true;
  search.input.setAttribute("aria-expanded", "false");
  search.input.removeAttribute("aria-activedescendant");
  search.options = [];
  search.active = -1;
}

function setActive(i) {
  search.active = i;
  search.options.forEach((o, n) => o.node.setAttribute("aria-selected", String(n === i)));
  if (i >= 0) {
    search.input.setAttribute("aria-activedescendant", search.options[i].node.id);
    search.options[i].node.scrollIntoView({ block: "nearest" });
  } else {
    search.input.removeAttribute("aria-activedescendant");
  }
}

function choose(opt) {
  closeResults();
  search.input.value = "";
  navigate(opt.view, opt.id);
}

function renderResults(query) {
  const q = query.trim();
  if (!q) { closeResults(); return; }
  const routes = searchRoutes(searchRows, q, 8);
  const stops = searchStops(stopList, q, 6);
  search.list.replaceChildren();
  search.options = [];

  const add = (view, item, label, sub) => {
    const node = el("li", { role: "option", id: `opt-${search.options.length}`, "aria-selected": "false", class: "opt" },
      el("span", { class: view === "route" ? "badge" : "pin", text: label }),
      el("span", { class: "grow", text: sub }));
    const opt = { view, id: item.id, node };
    node.addEventListener("mousedown", (e) => e.preventDefault()); // keep input focus
    node.addEventListener("click", () => choose(opt));
    search.options.push(opt);
    search.list.append(node);
  };
  const heading = (text) => search.list.append(el("li", { role: "presentation", class: "group", text }));

  if (routes.length) {
    heading("Routes");
    for (const r of routes) add("route", r, shortLabel(r.id, r.short), routeDesc(r.long));
  }
  if (stops.length) {
    heading("Stops");
    for (const s of stops) add("stop", s, "●", s.name);
  }
  if (!search.options.length) {
    search.list.append(el("li", { role: "presentation", class: "empty", text: stopList.length ? "No matching routes or stops" : "Data is still loading…" }));
  }
  search.list.hidden = false;
  search.input.setAttribute("aria-expanded", "true");
  setActive(-1);
}

function wireSearch() {
  search.input = $("search");
  search.list = $("results");
  let timer;
  search.input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(() => renderResults(search.input.value), 120);
  });
  search.input.addEventListener("keydown", (e) => {
    const n = search.options.length;
    if (e.key === "ArrowDown" && n) { e.preventDefault(); setActive((search.active + 1) % n); }
    else if (e.key === "ArrowUp" && n) { e.preventDefault(); setActive((search.active - 1 + n) % n); }
    else if (e.key === "Enter" && n) { e.preventDefault(); choose(search.options[Math.max(search.active, 0)]); }
    else if (e.key === "Escape") { if (n) closeResults(); else search.input.value = ""; }
  });
  search.input.addEventListener("blur", () => setTimeout(closeResults, 100));
}

/* ---------- map interactions ---------- */

function wireMap() {
  const pointer = () => (map.getCanvas().style.cursor = "pointer");
  const reset = () => (map.getCanvas().style.cursor = "");
  for (const id of ["network", "clusters", "stop-points", "route-stops"]) {
    map.on("mouseenter", id, pointer);
    map.on("mouseleave", id, reset);
  }

  map.on("click", "clusters", async (e) => {
    const f = e.features[0];
    try {
      const zoom = await map.getSource("stops").getClusterExpansionZoom(f.properties.cluster_id);
      map.easeTo({ center: f.geometry.coordinates, zoom });
    } catch { /* the cluster changed while zooming */ }
  });
  map.on("click", "stop-points", (e) => navigate("stop", e.features[0].properties.id));
  map.on("click", "route-stops", (e) => navigate("stop", e.features[0].properties.id));

  map.on("click", "network", (e) => {
    const hit = map.queryRenderedFeatures(e.point, { layers: ["clusters", "stop-points", "route-stops"] });
    if (hit.length) return;
    const seen = new Map();
    for (const f of e.features) if (!seen.has(f.properties.id)) seen.set(f.properties.id, f.properties.n);
    if (seen.size === 1) { navigate("route", [...seen.keys()][0]); return; }
    const list = el("ul", { class: "popup-routes" });
    for (const [id, n] of seen) {
      list.append(el("li", {}, el("button", { type: "button", onclick: () => { popup.remove(); navigate("route", id); } },
        el("strong", { text: shortLabel(id, n) }))));
    }
    const popup = new maplibregl.Popup({ maxWidth: "240px" }).setLngLat(e.lngLat)
      .setDOMContent(el("div", {}, el("div", { class: "popup-title", text: "Routes here" }), list)).addTo(map);
  });
}

function wireControls() {
  const toggle = (id, key) => $(id).addEventListener("change", (e) => {
    state.visible[key] = e.target.checked;
    applyVisibility(map, state);
  });
  toggle("toggle-network", "network");
  toggle("toggle-stops", "stops");
  toggle("toggle-heat", "heat");

  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", async () => {
    state.scheme = currentScheme();
    await setBasemap(map, state.scheme); // style.load re-installs the overlay
  });
  window.addEventListener("hashchange", applyHash);
}

async function main() {
  setView(loadingCard("Loading…"));
  wireSearch();
  map = await createMap("map", state.scheme);
  const firstStyle = new Promise((resolve) => map.once("style.load", resolve));
  map.on("style.load", () => installOverlay(map, state));
  map.on("error", (e) => console.error("map error:", e.error?.message ?? e));
  wireMap();
  wireControls();
  if (new URLSearchParams(location.search).has("debug")) window.__tgbus = { map, state }; // for browser tests
  loadData();
  await firstStyle;
  applyHash();
}

main().catch((err) => {
  console.error(err);
  setView(errorCard("Something went wrong starting the map. Reload the page to try again."));
});
