"use strict";

const DIR_COLORS = { 0: "#1f6feb", 1: "#e8590c" };
const DIR_EXPR = ["match", ["get", "direction"], 0, DIR_COLORS[0], 1, DIR_COLORS[1], "#6e7781"];
const EMPTY = { type: "FeatureCollection", features: [] };
const $ = (id) => document.getElementById(id);

// Build DOM nodes without innerHTML: stop and route names come from the feed.
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
    else node.setAttribute(k, v);
  }
  for (const c of children) if (c) node.append(c);
  return node;
}

// 62 routes in the feed carry their internal id as the route number; show a dash instead.
const shortLabel = (id, short) => (!short || short === id ? "—" : short);

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

const map = new maplibregl.Map({
  container: "map",
  center: [78.4867, 17.385],
  zoom: 10,
  style: {
    version: 8,
    glyphs: "https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf",
    sources: {
      osm: {
        type: "raster",
        tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
        tileSize: 256,
        maxzoom: 19,
        attribution: "© OpenStreetMap contributors",
      },
    },
    layers: [{
      id: "osm", type: "raster", source: "osm",
      paint: { "raster-saturation": -0.85, "raster-brightness-max": 0.92 },
    }],
  },
});
map.on("error", (e) => console.error("map error:", e.error && e.error.message ? e.error.message : e));
map.addControl(new maplibregl.NavigationControl(), "top-right");
map.addControl(new maplibregl.ScaleControl(), "bottom-right");

let selectedId = null;
let popup = null;

function showPopup(lngLat, content) {
  if (popup) popup.remove();
  popup = new maplibregl.Popup({ maxWidth: "280px" }).setLngLat(lngLat).setDOMContent(content).addTo(map);
}

map.on("load", async () => {
  // Fetch first and create the sources with their data: calling setData right
  // after creating a source with empty data can leave the first tiles stale.
  let overview = EMPTY, stops = EMPTY;
  try {
    [overview, stops] = await Promise.all([getJSON("/api/overview"), getJSON("/api/stops")]);
    $("status").textContent =
      `${overview.features.length.toLocaleString()} route lines, ${stops.features.length.toLocaleString()} stops`;
  } catch (err) {
    $("status").textContent = "Failed to load data: " + err.message;
  }

  map.addSource("overview", { type: "geojson", data: overview });
  map.addSource("stops", {
    type: "geojson", data: stops, cluster: true, clusterRadius: 45, clusterMaxZoom: 13,
  });
  map.addSource("heat", { type: "geojson", data: stops });
  map.addSource("route", { type: "geojson", data: EMPTY });

  map.addLayer({
    id: "network", type: "line", source: "overview",
    paint: {
      "line-color": "#4b5d78",
      "line-opacity": ["interpolate", ["linear"], ["zoom"], 8, 0.18, 14, 0.4],
      "line-width": ["interpolate", ["linear"], ["zoom"], 8, 0.5, 14, 2],
    },
  });
  map.addLayer({
    id: "heat", type: "heatmap", source: "heat", layout: { visibility: "none" },
    paint: {
      "heatmap-weight": ["interpolate", ["linear"], ["get", "trips"], 0, 0, 60, 0.12, 1500, 1],
      "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 7, 0.3, 14, 1.2],
      "heatmap-radius": ["interpolate", ["linear"], ["zoom"], 7, 6, 12, 20, 15, 40],
      "heatmap-opacity": 0.85,
      "heatmap-color": [
        "interpolate", ["linear"], ["heatmap-density"],
        0, "rgba(255,255,178,0)", 0.2, "#fecc5c", 0.5, "#fd8d3c", 0.8, "#e31a1c", 1, "#800026",
      ],
    },
  });
  map.addLayer({
    id: "clusters", type: "circle", source: "stops", filter: ["has", "point_count"],
    paint: {
      "circle-color": ["step", ["get", "point_count"], "#74a9cf", 25, "#3690c0", 100, "#0570b0"],
      "circle-radius": ["step", ["get", "point_count"], 14, 25, 19, 100, 25],
      "circle-stroke-width": 2, "circle-stroke-color": "#fff",
    },
  });
  map.addLayer({
    id: "cluster-count", type: "symbol", source: "stops", filter: ["has", "point_count"],
    layout: {
      "text-field": ["get", "point_count_abbreviated"],
      "text-font": ["Noto Sans Regular"], "text-size": 12,
    },
    paint: { "text-color": "#fff" },
  });
  map.addLayer({
    id: "stop-points", type: "circle", source: "stops", filter: ["!", ["has", "point_count"]],
    paint: {
      "circle-color": ["step", ["get", "trips"], "#8fb5d6", 20, "#3b82c4", 100, "#0b4f94"],
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 10, 2.5, 16, 7],
      "circle-stroke-width": 1, "circle-stroke-color": "#fff",
    },
  });

  // Selected route, drawn above everything else.
  map.addLayer({
    id: "route-casing", type: "line", source: "route", filter: ["==", ["get", "kind"], "line"],
    layout: { "line-join": "round", "line-cap": "round" },
    paint: { "line-color": "#fff", "line-width": 8, "line-opacity": 0.9 },
  });
  map.addLayer({
    id: "route-line", type: "line", source: "route", filter: ["==", ["get", "kind"], "line"],
    layout: { "line-join": "round", "line-cap": "round" },
    paint: { "line-color": DIR_EXPR, "line-width": 4.5 },
  });
  map.addLayer({
    id: "route-stops", type: "circle", source: "route", filter: ["==", ["get", "kind"], "stop"],
    paint: {
      "circle-color": "#fff", "circle-stroke-color": DIR_EXPR, "circle-stroke-width": 2.5,
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 9, 3, 15, 8],
    },
  });
  map.addLayer({
    id: "route-stop-labels", type: "symbol", source: "route", minzoom: 12.5,
    filter: ["==", ["get", "kind"], "stop"],
    layout: {
      "text-field": ["to-string", ["get", "seq"]], "text-font": ["Noto Sans Regular"],
      "text-size": 10, "text-allow-overlap": false,
    },
    paint: { "text-color": "#1f2328" },
  });

  wireInteractions();
  wireControls();

  const fromHash = decodeURIComponent(location.hash.replace(/^#route=/, ""));
  if (location.hash.startsWith("#route=") && fromHash) selectRoute(fromHash);
});

function wireInteractions() {
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
    } catch { /* cluster vanished while zooming */ }
  });

  map.on("click", "stop-points", async (e) => {
    const f = e.features[0];
    const lngLat = f.geometry.coordinates.slice();
    try {
      const stop = await getJSON(`/api/stops/${encodeURIComponent(f.properties.id)}`);
      const list = el("ul", { class: "popup-routes" });
      for (const r of stop.routes) {
        list.append(el("li", {}, el("button", {
          type: "button", title: r.long, onclick: () => selectRoute(r.id),
        }, el("strong", { text: shortLabel(r.id, r.short) }), el("span", { text: r.long.replace(/^.*? - /, "") }),
          el("span", { class: "c", text: `${r.trips}/day` }))));
      }
      showPopup(lngLat, el("div", {},
        el("div", { class: "popup-title", text: stop.name }),
        el("div", { class: "popup-meta", text: `${stop.trips} trips/day · ${stop.routes.length} routes` }),
        list));
    } catch (err) {
      $("status").textContent = err.message;
    }
  });

  map.on("click", "route-stops", (e) => {
    const p = e.features[0].properties;
    showPopup(e.features[0].geometry.coordinates.slice(), el("div", {},
      el("div", { class: "popup-title", text: `${p.seq}. ${p.name}` }),
      p.time ? el("div", { class: "popup-meta", text: `Departs ${p.time} (first trip shown)` }) : null));
  });

  // Clicking the faint network selects a route, unless a stop/route marker was hit.
  map.on("click", "network", (e) => {
    const hit = map.queryRenderedFeatures(e.point, { layers: ["clusters", "stop-points", "route-stops"] });
    if (hit.length) return;
    const ids = [...new Set(e.features.map((f) => f.properties.id))];
    if (ids.length === 1) {
      selectRoute(ids[0]);
      return;
    }
    // Several routes share this road: let the user pick.
    const list = el("ul", { class: "popup-routes" });
    const seen = new Set();
    for (const f of e.features) {
      if (seen.has(f.properties.id)) continue;
      seen.add(f.properties.id);
      list.append(el("li", {}, el("button", {
        type: "button", onclick: () => selectRoute(f.properties.id),
      }, el("strong", { text: shortLabel(f.properties.id, f.properties.n) }))));
    }
    showPopup(e.lngLat, el("div", {}, el("div", { class: "popup-title", text: "Routes here" }), list));
  });
}

function wireControls() {
  let timer;
  $("search").addEventListener("input", (e) => {
    clearTimeout(timer);
    const q = e.target.value.trim();
    timer = setTimeout(() => runSearch(q), 150);
  });
  $("route-clear").addEventListener("click", clearRoute);

  const toggle = (checkbox, layers) =>
    $(checkbox).addEventListener("change", (e) => {
      for (const l of layers) map.setLayoutProperty(l, "visibility", e.target.checked ? "visible" : "none");
    });
  toggle("toggle-network", ["network"]);
  toggle("toggle-stops", ["clusters", "cluster-count", "stop-points"]);
  toggle("toggle-heat", ["heat"]);

  window.addEventListener("hashchange", () => {
    const m = location.hash.match(/^#route=(.+)$/);
    if (m && decodeURIComponent(m[1]) !== selectedId) selectRoute(decodeURIComponent(m[1]));
    if (!m && selectedId) clearRoute();
  });
}

let searchSeq = 0;
async function runSearch(q) {
  const results = $("results");
  const seq = ++searchSeq;
  if (!q) {
    results.replaceChildren();
    return;
  }
  try {
    const found = await getJSON(`/api/routes?q=${encodeURIComponent(q)}`);
    if (seq !== searchSeq) return; // a newer search superseded this one
    results.replaceChildren(...found.map((r) =>
      el("li", {}, el("button", { type: "button", onclick: () => selectRoute(r.id) },
        el("span", { class: "short", text: shortLabel(r.id, r.short) }),
        el("span", { class: "long", text: r.long.replace(/^.*? - /, "") })))));
    if (!found.length) results.append(el("li", { class: "sub", text: "No routes found" }));
  } catch (err) {
    $("status").textContent = err.message;
  }
}

async function selectRoute(id) {
  let data;
  try {
    data = await getJSON(`/api/routes/${encodeURIComponent(id)}`);
  } catch (err) {
    $("status").textContent = `Could not load route: ${err.message}`;
    return;
  }
  selectedId = id;
  dimStops(true);
  if (popup) popup.remove();
  $("results").replaceChildren();
  map.getSource("route").setData(data);
  if (location.hash !== `#route=${encodeURIComponent(id)}`) {
    history.replaceState(null, "", `#route=${encodeURIComponent(id)}`);
  }

  const bounds = new maplibregl.LngLatBounds();
  for (const f of data.features) {
    if (f.geometry.type === "LineString") f.geometry.coordinates.forEach((c) => bounds.extend(c));
    else bounds.extend(f.geometry.coordinates);
  }
  if (!bounds.isEmpty()) {
    const wide = window.innerWidth > 640;
    map.fitBounds(bounds, {
      padding: wide ? { top: 50, right: 50, bottom: 50, left: 390 } : 50, maxZoom: 15, duration: 700,
    });
  }
  renderCard(data);
}

function renderCard(data) {
  const r = data.route;
  $("route-short").textContent = shortLabel(r.id, r.short);
  $("route-long").textContent = r.long;

  const stats = $("route-stats");
  stats.replaceChildren();
  const row = (k, v) => stats.append(el("dt", { text: k }), el("dd", { text: v }));
  row("Trips per day", String(r.trips));
  if (r.firstDeparture) row("First / last departure", `${r.firstDeparture} / ${r.lastDeparture}`);

  const lines = [];
  for (const f of data.features) if (f.properties.kind === "line") lines.push(f.properties);
  const stopsByDir = new Map();
  for (const f of data.features) {
    if (f.properties.kind !== "stop") continue;
    const d = f.properties.direction;
    if (!stopsByDir.has(d)) stopsByDir.set(d, []);
    stopsByDir.get(d).push(f);
  }

  const box = $("route-lines");
  box.replaceChildren();
  for (const [dir, feats] of stopsByDir) {
    const info = lines.find((l) => l.direction === dir);
    const ol = el("ol", { class: "stops" });
    for (const f of feats) {
      const p = f.properties;
      ol.append(el("li", {}, el("button", {
        type: "button",
        onclick: () => map.flyTo({ center: f.geometry.coordinates, zoom: Math.max(map.getZoom(), 14) }),
      }, el("span", { class: "n", text: String(p.seq) }), el("span", { text: p.name }),
        el("span", { class: "t", text: p.time }))));
    }
    const first = feats[0].properties.name, last = feats[feats.length - 1].properties.name;
    const sw = el("span", { class: "swatch" });
    sw.style.background = DIR_COLORS[dir] || "#6e7781";
    box.append(el("details", dir === 0 || stopsByDir.size === 1 ? { open: "" } : {},
      el("summary", {}, sw, document.createTextNode(
        `${first} → ${last} (${feats.length} stops${info ? `, ${info.trips} trips` : ""})`)),
      ol));
  }
  $("route-card").hidden = false;
}

// Fade stop clusters while a route is selected so the route stands out.
function dimStops(dim) {
  const o = dim ? 0.25 : 1;
  for (const [layer, props] of [
    ["clusters", ["circle-opacity", "circle-stroke-opacity"]],
    ["cluster-count", ["text-opacity"]],
    ["stop-points", ["circle-opacity", "circle-stroke-opacity"]],
  ]) for (const prop of props) map.setPaintProperty(layer, prop, o);
}

function clearRoute() {
  selectedId = null;
  dimStops(false);
  map.getSource("route").setData(EMPTY);
  $("route-card").hidden = true;
  if (location.hash) history.replaceState(null, "", location.pathname + location.search);
}
