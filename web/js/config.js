// Everything a fork might want to change lives here.

export const CONFIG = {
  // Basemap styles (OpenFreeMap vector tiles, no API key). Swap these to use
  // another provider; the overlay layers only need a style that defines
  // `glyphs` for the font named in `font`.
  styles: {
    light: "https://tiles.openfreemap.org/styles/positron",
    dark: "https://tiles.openfreemap.org/styles/dark",
  },
  font: ["Noto Sans Regular"],
  center: [78.4867, 17.385], // Hyderabad
  zoom: 10,
  timeZone: "Asia/Kolkata", // the feed's schedule is in local Indian time
  // Address search (OpenStreetMap Nominatim). Set to null to remove the feature
  // and any request to a third party. See web/js/geocoder.js for the usage rules.
  geocoder: { url: "https://nominatim.openstreetmap.org/search" },
  dataBase: "data/", // relative, so the site works from any sub-path
  attribution:
    'Data: <a href="https://data.telangana.gov.in/" target="_blank" rel="noopener">Open Data Telangana</a> / TGSRTC',

  palette: {
    light: { network: "#4b5d78", casing: "#ffffff", label: "#1f2328", stroke: "#ffffff" },
    dark: { network: "#9db4d3", casing: "#0d1117", label: "#e6edf3", stroke: "#0d1117" },
  },
  directionColors: { 0: "#1f6feb", 1: "#e8590c" },
};

// Used when the basemap cannot be fetched, so routes and stops still show.
export const BLANK_STYLE = {
  version: 8,
  glyphs: "https://tiles.openfreemap.org/fonts/{fontstack}/{range}.pbf",
  sources: {},
  layers: [{ id: "bg", type: "background", paint: { "background-color": "#e8eaed" } }],
};
