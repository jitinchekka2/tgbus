import { CONFIG, BLANK_STYLE } from "./config.js";

export const EMPTY = { type: "FeatureCollection", features: [] };

export const currentScheme = () =>
  window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";

/** Use the configured basemap if it is reachable, else a plain background. */
export async function resolveStyle(scheme) {
  const url = CONFIG.styles[scheme];
  try {
    const res = await fetch(url, { method: "GET" });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return url;
  } catch (err) {
    console.warn("basemap unavailable, using a blank background:", err.message);
    return BLANK_STYLE;
  }
}

export async function createMap(container, scheme) {
  const style = await resolveStyle(scheme);
  const map = new maplibregl.Map({
    container, style, center: CONFIG.center, zoom: CONFIG.zoom, attributionControl: false,
  });
  map.addControl(new maplibregl.AttributionControl({ compact: true, customAttribution: CONFIG.attribution }));
  map.addControl(new maplibregl.NavigationControl(), "top-right");
  map.addControl(new maplibregl.ScaleControl(), "bottom-right");
  return map;
}

export async function setBasemap(map, scheme) {
  map.setStyle(await resolveStyle(scheme), { diff: false });
}

const dirColor = ["match", ["get", "direction"], 0, CONFIG.directionColors[0], 1, CONFIG.directionColors[1], "#6e7781"];

/**
 * (Re)create our sources and layers. Called on every `style.load`, because
 * switching the basemap style discards custom layers. `state` holds the data
 * and visibility to restore: { overview, stops, route, selected, visible, dim, scheme }.
 */
export function installOverlay(map, state) {
  const pal = CONFIG.palette[state.scheme];
  const font = CONFIG.font;

  map.addSource("overview", { type: "geojson", data: state.overview });
  map.addSource("stops", { type: "geojson", data: state.stops, cluster: true, clusterRadius: 45, clusterMaxZoom: 13 });
  map.addSource("heat", { type: "geojson", data: state.stops });
  map.addSource("route", { type: "geojson", data: state.route });
  map.addSource("selected", { type: "geojson", data: state.selected });

  map.addLayer({
    id: "network", type: "line", source: "overview",
    paint: {
      "line-color": pal.network,
      "line-opacity": ["interpolate", ["linear"], ["zoom"], 8, 0.18, 14, 0.45],
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
      "circle-stroke-width": 2, "circle-stroke-color": pal.stroke,
    },
  });
  map.addLayer({
    id: "cluster-count", type: "symbol", source: "stops", filter: ["has", "point_count"],
    layout: { "text-field": ["get", "point_count_abbreviated"], "text-font": font, "text-size": 12 },
    paint: { "text-color": "#fff" },
  });
  map.addLayer({
    id: "stop-points", type: "circle", source: "stops", filter: ["!", ["has", "point_count"]],
    paint: {
      "circle-color": ["step", ["get", "trips"], "#8fb5d6", 20, "#3b82c4", 100, "#0b4f94"],
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 10, 2.5, 16, 7],
      "circle-stroke-width": 1, "circle-stroke-color": pal.stroke,
    },
  });

  // The selected route, above everything else.
  const lineFilter = ["==", ["get", "kind"], "line"];
  const stopFilter = ["==", ["get", "kind"], "stop"];
  map.addLayer({
    id: "route-casing", type: "line", source: "route", filter: lineFilter,
    layout: { "line-join": "round", "line-cap": "round" },
    paint: { "line-color": pal.casing, "line-width": 8, "line-opacity": 0.9 },
  });
  map.addLayer({
    id: "route-line", type: "line", source: "route", filter: lineFilter,
    layout: { "line-join": "round", "line-cap": "round" },
    paint: { "line-color": dirColor, "line-width": 4.5 },
  });
  map.addLayer({
    id: "route-stops", type: "circle", source: "route", filter: stopFilter,
    paint: {
      "circle-color": pal.casing, "circle-stroke-color": dirColor, "circle-stroke-width": 2.5,
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 9, 3, 15, 8],
    },
  });
  map.addLayer({
    id: "route-stop-labels", type: "symbol", source: "route", minzoom: 12.5, filter: stopFilter,
    layout: { "text-field": ["to-string", ["get", "seq"]], "text-font": font, "text-size": 10 },
    paint: { "text-color": pal.label },
  });
  map.addLayer({
    id: "selected-stop", type: "circle", source: "selected",
    paint: {
      "circle-radius": 11, "circle-color": "rgba(232,89,12,0.25)",
      "circle-stroke-color": "#e8590c", "circle-stroke-width": 3,
    },
  });

  applyVisibility(map, state);
}

const GROUPS = {
  network: ["network"],
  stops: ["clusters", "cluster-count", "stop-points"],
  heat: ["heat"],
};

/** Show or hide the optional layers and fade stops while a route is selected. */
export function applyVisibility(map, state) {
  for (const [name, layers] of Object.entries(GROUPS)) {
    for (const id of layers) {
      if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", state.visible[name] ? "visible" : "none");
    }
  }
  const o = state.dim ? 0.25 : 1;
  const fades = [
    ["clusters", ["circle-opacity", "circle-stroke-opacity"]],
    ["cluster-count", ["text-opacity"]],
    ["stop-points", ["circle-opacity", "circle-stroke-opacity"]],
  ];
  for (const [id, props] of fades) {
    if (!map.getLayer(id)) continue;
    for (const p of props) map.setPaintProperty(id, p, o);
  }
}

export function setSource(map, id, data) {
  const src = map.getSource(id);
  if (src) src.setData(data);
}

export function boundsOf(features) {
  const b = new maplibregl.LngLatBounds();
  for (const f of features) {
    if (f.geometry.type === "LineString") f.geometry.coordinates.forEach((c) => b.extend(c));
    else b.extend(f.geometry.coordinates);
  }
  return b;
}
