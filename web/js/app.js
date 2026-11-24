import { CONFIG } from "./config.js";
import { createCombobox } from "./combobox.js";
import { getMeta, getOverview, getRoute, getSearch, getStop, getStops } from "./data.js";
import { el } from "./dom.js";
import {
  EMPTY, applyVisibility, boundsOf, createMap, currentScheme, installOverlay, setBasemap, setSource,
} from "./map.js";
import { errorCard, homeCard, loadingCard, routeCard, stopCard } from "./panel.js";
import { createPlanner } from "./planner.js";
import { createPlanView, journeyToGeoJSON } from "./plan.js";
import { hashFor, nowMinutes, parseHash, planHash, routeDesc, searchRoutes, searchStops, shortLabel } from "./util.js";

const $ = (id) => document.getElementById(id);

const state = {
  scheme: currentScheme(),
  overview: EMPTY, stops: EMPTY, route: EMPTY, selected: EMPTY, plan: EMPTY,
  visible: { network: true, stops: true, heat: false },
  dim: false,
};
let map;
let searchRows = [];
let stopList = []; // { id, name, trips, lat, lon }
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
  clearPlanDrawing();
  applyVisibility(map, state);
}

const fitPadding = () =>
  window.innerWidth > 640 ? { top: 50, right: 50, bottom: 50, left: 390 }
    : { top: 50, right: 30, bottom: Math.round(window.innerHeight * 0.5), left: 30 };

function showRoute(data) {
  clearPlanDrawing();
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
  clearPlanDrawing();
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
    onDirections: (role) => {
      const place = { lat: stop.lat, lon: stop.lon };
      labels.set(placeKey(place), stop.name);
      goPlan(role === "from" ? place : plan.from, role === "to" ? place : plan.to);
    },
  }));
  document.title = `${stop.name} · TGSRTC bus network`;
}

/* ---------- directions ---------- */

const plan = { from: null, to: null, minutes: null, journeys: [], selected: 0, pick: null, seq: 0 };
const labels = new Map(); // "lat,lon" -> a name the user chose (stop name, "My location")
let planView = null;
// Re-plan when the planner becomes usable (or fails). "loading" is skipped:
// runPlan already shows that state, and re-entering from it would loop.
const planner = createPlanner((s) => {
  if (s !== "loading" && parseHash(location.hash).view === "plan") runPlan();
});

const round5 = (v) => Math.round(v * 1e5) / 1e5;
const placeKey = (p) => `${round5(p.lat)},${round5(p.lon)}`;

function distMetres(a, b) {
  const dy = (a.lat - b.lat) * 110540;
  const dx = (a.lon - b.lon) * 111320 * Math.cos(((a.lat + b.lat) / 2) * Math.PI / 180);
  return Math.hypot(dx, dy);
}

/** A readable name for a place: one the user picked, a stop within 25 m, or its coordinates. */
function labelFor(p) {
  const known = labels.get(placeKey(p));
  if (known) return known;
  let best = null;
  let bestD = 25;
  for (const s of stopList) {
    const d = distMetres(p, s);
    if (d < bestD) { best = s; bestD = d; }
  }
  return best ? best.name : `Pin at ${round5(p.lat)}, ${round5(p.lon)}`;
}

function goPlan(from, to, minutes = plan.minutes) {
  location.hash = planHash(from, to, minutes);
}

function setPlaceFromStop(role, s) {
  const place = { lat: s.lat, lon: s.lon };
  labels.set(placeKey(place), s.name);
  goPlan(role === "from" ? place : plan.from, role === "to" ? place : plan.to);
}

function setPick(role) {
  plan.pick = role;
  map.getCanvas().style.cursor = role ? "crosshair" : "";
  if (role) planView.message(`Click the map to set the ${role === "from" ? "start" : "destination"}. Press Esc to cancel.`);
  else runPlan();
}

function locateFrom() {
  if (!navigator.geolocation) {
    planView.message("This browser cannot find your location. Search for a stop or pick a point on the map.", "error");
    return;
  }
  planView.message("Finding your location…");
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      const place = { lat: pos.coords.latitude, lon: pos.coords.longitude };
      labels.set(placeKey(place), "My location");
      goPlan(place, plan.to);
    },
    (err) => planView.message(`Could not get your location (${err.message}). Search for a stop or pick a point on the map.`, "error"),
    { timeout: 10000 },
  );
}

function ensurePlanView() {
  if (planView) return;
  planView = createPlanView({
    stops: () => stopList,
    on: {
      from: (s) => setPlaceFromStop("from", s),
      to: (s) => setPlaceFromStop("to", s),
      pickFrom: () => setPick("from"),
      pickTo: () => setPick("to"),
      locateFrom,
      swap: () => goPlan(plan.to, plan.from),
      minutes: (m) => {
        if (m === null) return;
        plan.minutes = m;
        history.replaceState(null, "", planHash(plan.from, plan.to, m)); // a time tweak is not a new page
        runPlan();
      },
      now: () => {
        plan.minutes = nowMinutes(CONFIG.timeZone);
        planView.setMinutes(plan.minutes);
        history.replaceState(null, "", planHash(plan.from, plan.to, plan.minutes));
        runPlan();
      },
      select: (i) => { plan.selected = i; renderPlan(true); },
      openStop: (id) => navigate("stop", id),
      openRoute: (id) => navigate("route", id),
      close: () => navigate("home"),
    },
  });
}

function clearPlanDrawing() {
  plan.pick = null;
  if (map) map.getCanvas().style.cursor = "";
  state.plan = EMPTY;
  setSource(map, "plan", EMPTY);
}

function endpoints() {
  const features = [];
  if (plan.from) features.push({ type: "Feature", properties: { kind: "origin", label: "A" }, geometry: { type: "Point", coordinates: [plan.from.lon, plan.from.lat] } });
  if (plan.to) features.push({ type: "Feature", properties: { kind: "dest", label: "B" }, geometry: { type: "Point", coordinates: [plan.to.lon, plan.to.lat] } });
  return { type: "FeatureCollection", features };
}

function drawPlan(fc, fit) {
  state.plan = fc;
  setSource(map, "plan", fc);
  if (!fit) return;
  const bounds = boundsOf(fc.features);
  if (!bounds.isEmpty()) map.fitBounds(bounds, { padding: fitPadding(), maxZoom: 15, duration: 700 });
}

function renderPlan(fit) {
  planView.journeys(plan.journeys, plan.selected);
  drawPlan(journeyToGeoJSON(plan.journeys[plan.selected], plan.from, plan.to), fit);
}

async function runPlan() {
  if (!planView) return;
  const seq = ++plan.seq;
  plan.journeys = [];
  plan.selected = 0;
  drawPlan(endpoints(), false);

  if (!plan.from || !plan.to) {
    planView.message("Choose a start and a destination: search for a stop, use your location, or pick points on the map.");
    if (plan.from || plan.to) drawPlan(endpoints(), true);
    return;
  }
  if (planner.state === "unavailable" || planner.state === "error") {
    planView.message(planner.message || "The journey planner could not start.", "error");
    return;
  }
  if (planner.state !== "ready") {
    planView.message("Loading the journey planner (about 2 MB, once)…");
    planner.init();
    return; // the planner's state callback calls runPlan again when it is ready
  }
  planView.message("Finding journeys…");
  const resp = await planner.plan({
    fromLat: plan.from.lat, fromLon: plan.from.lon, toLat: plan.to.lat, toLon: plan.to.lon,
    depart: (plan.minutes ?? nowMinutes(CONFIG.timeZone)) * 60,
  });
  if (seq !== plan.seq) return; // a newer query superseded this one
  if (resp.journeys.length) {
    plan.journeys = resp.journeys;
    renderPlan(true);
    return;
  }
  const text = {
    no_stops: "There is no bus stop within about 1 km of the start or the destination. Try picking points closer to a stop.",
    no_journey: "No journey found at that time. Try a different time, or a start and destination nearer the network.",
  }[resp.code] || resp.error || "Something went wrong planning that journey.";
  planView.message(text, resp.code === "no_stops" || resp.code === "no_journey" ? "info" : "error");
  drawPlan(endpoints(), true);
}

function showPlan(h) {
  ensurePlanView();
  state.route = EMPTY;
  state.selected = EMPTY;
  state.dim = true;
  setSource(map, "route", EMPTY);
  setSource(map, "selected", EMPTY);
  applyVisibility(map, state);

  plan.from = h.from;
  plan.to = h.to;
  plan.minutes = h.minutes ?? plan.minutes ?? nowMinutes(CONFIG.timeZone);
  planView.setFrom(plan.from ? labelFor(plan.from) : "");
  planView.setTo(plan.to ? labelFor(plan.to) : "");
  planView.setMinutes(plan.minutes);
  setView(planView.node);
  planner.init();
  document.title = "Directions · TGSRTC bus network";
  updateTabs("plan"); // after the places above are stored, so the tab remembers them
  runPlan();
}

function setCurrent(node, on) {
  if (on) node.setAttribute("aria-current", "page");
  else node.removeAttribute("aria-current");
}

function updateTabs(view) {
  setCurrent($("tab-explore"), view !== "plan");
  setCurrent($("tab-plan"), view === "plan");
  // The Directions tab remembers the last start, destination and time.
  $("tab-plan").setAttribute("href", plan.from || plan.to ? planHash(plan.from, plan.to, plan.minutes) : "#/plan");
}

// The URL hash is the single source of truth for what is shown.
async function applyHash() {
  const seq = ++navSeq;
  const h = parseHash(location.hash);
  const { view, id } = h;
  updateTabs(view);
  if (view !== "plan") plan.pick = null;
  if (view === "home") {
    clearSelection();
    renderHome();
    document.title = "TGSRTC bus network";
    return;
  }
  if (view === "plan") {
    showPlan(h);
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
    stopList = stops.features.map((f) => ({
      id: f.properties.id, name: f.properties.name, trips: f.properties.trips,
      lon: f.geometry.coordinates[0], lat: f.geometry.coordinates[1],
    }));
    for (const id of ["overview", "stops", "heat"]) setSource(map, id, id === "overview" ? overview : stops);
    setStatus(`${meta.routes.toLocaleString()} routes · ${meta.stops.toLocaleString()} stops · ${meta.trips.toLocaleString()} trips per day`);
    $("feed-info").textContent = `Feed: ${meta.feed} · built ${meta.builtAt.slice(0, 10)}`;
    const view = parseHash(location.hash).view;
    if (view === "home") renderHome();
    else if (view === "plan") applyHash(); // labels for places in the URL can now name nearby stops
  } catch (err) {
    console.error(err);
    setStatus("Could not load the network data.");
    if (parseHash(location.hash).view === "home") setView(errorCard("Could not load the network data.", loadData));
  }
}

/* ---------- search ---------- */

function wireSearch() {
  createCombobox({
    input: $("search"), list: $("results"), emptyText: "No matching routes or stops",
    search: (q) => [
      {
        heading: "Routes",
        items: searchRoutes(searchRows, q, 8).map((r) => ({
          badge: shortLabel(r.id, r.short), label: routeDesc(r.long), value: { view: "route", id: r.id },
        })),
      },
      {
        heading: "Stops",
        items: searchStops(stopList, q, 6).map((s) => ({ badge: "●", badgeClass: "pin", label: s.name, value: { view: "stop", id: s.id } })),
      },
    ],
    onChoose: (v) => { $("search").value = ""; navigate(v.view, v.id); },
  });
}

/* ---------- map interactions ---------- */

function wireMap() {
  const pointer = () => { if (!plan.pick) map.getCanvas().style.cursor = "pointer"; };
  const reset = () => { map.getCanvas().style.cursor = plan.pick ? "crosshair" : ""; };
  for (const id of ["network", "clusters", "stop-points", "route-stops"]) {
    map.on("mouseenter", id, pointer);
    map.on("mouseleave", id, reset);
  }

  map.on("click", "clusters", async (e) => {
    if (plan.pick) return;
    const f = e.features[0];
    try {
      const zoom = await map.getSource("stops").getClusterExpansionZoom(f.properties.cluster_id);
      map.easeTo({ center: f.geometry.coordinates, zoom });
    } catch { /* the cluster changed while zooming */ }
  });
  map.on("click", "stop-points", (e) => {
    const id = e.features[0].properties.id;
    if (plan.pick) { // picking a point: snap to the clicked stop
      const s = stopList.find((x) => x.id === id);
      const role = plan.pick;
      plan.pick = null;
      if (s) setPlaceFromStop(role, s);
      return;
    }
    navigate("stop", id);
  });
  map.on("click", "route-stops", (e) => { if (!plan.pick) navigate("stop", e.features[0].properties.id); });

  map.on("click", "network", (e) => {
    if (plan.pick) return;
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

  // Registered last so the layer handlers above run first and can claim a click on a stop.
  map.on("click", (e) => {
    if (!plan.pick) return;
    const role = plan.pick;
    plan.pick = null;
    map.getCanvas().style.cursor = "";
    const place = { lat: e.lngLat.lat, lon: e.lngLat.lng };
    goPlan(role === "from" ? place : plan.from, role === "to" ? place : plan.to);
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
  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && plan.pick) setPick(null);
  });
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
  if (new URLSearchParams(location.search).has("debug")) window.__tgbus = { map, state, plan, planner };
  loadData();
  await firstStyle;
  applyHash();
}

main().catch((err) => {
  console.error(err);
  setView(errorCard("Something went wrong starting the map. Reload the page to try again."));
});
