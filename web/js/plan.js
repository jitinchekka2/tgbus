// The directions panel: a form (built once, so typing never loses focus) and
// a results region that is re-rendered as plans arrive.
import { createCombobox } from "./combobox.js";
import { el } from "./dom.js";
import { clockSec, formatDuration, formatMetres, formatMinutes, parseClock, routeDesc, searchStops, shortLabel } from "./util.js";

/** One colour per bus ride, cycling. Walks are drawn dashed instead. */
export const RIDE_COLORS = ["#1f6feb", "#e8590c", "#2da44e", "#a371f7", "#d29922", "#db61a2"];

const stopCoord = (s) => [s.lon, s.lat];

/**
 * Turn one journey into map features: dashed walks, coloured bus lines, the
 * boarding and alighting stops, and the two end points.
 * `origin` and `dest` are { lat, lon }.
 */
export function journeyToGeoJSON(journey, origin, dest) {
  const features = [];
  const o = [origin.lon, origin.lat];
  const d = [dest.lon, dest.lat];
  let ride = 0;
  for (const leg of journey.legs) {
    const from = leg.from ? stopCoord(leg.from) : o;
    const to = leg.to ? stopCoord(leg.to) : d;
    if (leg.mode === "walk") {
      if (from[0] !== to[0] || from[1] !== to[1]) {
        features.push({ type: "Feature", properties: { kind: "walk" }, geometry: { type: "LineString", coordinates: [from, to] } });
      }
      continue;
    }
    const color = RIDE_COLORS[ride % RIDE_COLORS.length];
    ride++;
    features.push({
      type: "Feature",
      properties: { kind: "ride", color, route: shortLabel(leg.route.id, leg.route.short) },
      geometry: { type: "LineString", coordinates: leg.stops.map(stopCoord) },
    });
    features.push(
      { type: "Feature", properties: { kind: "stop", color, name: leg.from.name, role: "board" }, geometry: { type: "Point", coordinates: from } },
      { type: "Feature", properties: { kind: "stop", color, name: leg.to.name, role: "alight" }, geometry: { type: "Point", coordinates: to } },
    );
  }
  features.push(
    { type: "Feature", properties: { kind: "origin", label: "A" }, geometry: { type: "Point", coordinates: o } },
    { type: "Feature", properties: { kind: "dest", label: "B" }, geometry: { type: "Point", coordinates: d } },
  );
  return { type: "FeatureCollection", features };
}

/** Walking distance of a leg in metres. The planner omits the field when it is zero. */
export const walkMetres = (l) => l.walkM ?? 0;

/** A walk too short to mention, such as stepping from the start point onto a stop that is right there. */
export const isTrivialWalk = (l) => l.mode === "walk" && l.arr - l.dep < 30 && walkMetres(l) < 30;

/** A short text summary of the legs, for the option list: "walk 15 min · 277 · walk 1 min". */
export function legChips(journey) {
  return journey.legs.filter((l) => !isTrivialWalk(l)).map((l) =>
    l.mode === "walk"
      ? { kind: "walk", text: formatDuration(l.arr - l.dep) }
      : { kind: "ride", text: shortLabel(l.route.id, l.route.short) });
}

function placeField({ id, label, stops, hint, onChoose, onPick, onLocate, geocode, onAddress }) {
  const input = el("input", {
    id, type: "search", autocomplete: "off", placeholder: hint, role: "combobox",
    "aria-expanded": "false", "aria-autocomplete": "list",
  });
  const list = el("ul", { id: `${id}-list`, role: "listbox", hidden: true, "aria-label": `${label} suggestions` });
  const geo = el("div", { class: "geo", "aria-live": "polite" });
  let geoSeq = 0;
  const showGeo = (...nodes) => geo.replaceChildren(...nodes);
  async function submitAddress() {
    if (!geocode) return;
    const q = input.value.trim();
    const seq = ++geoSeq;
    if (q.length < 2) { showGeo(el("p", { class: "muted small", text: "Type a place or address first." })); return; }
    showGeo(el("p", { class: "muted small", role: "status", text: "Searching…" }));
    try {
      const results = await geocode(q);
      if (seq !== geoSeq) return; // a newer search or typing superseded this one
      if (!results.length) {
        showGeo(el("p", { class: "muted small", text: "No places found in the bus network area. Try a landmark or an area name." }));
        return;
      }
      const ul = el("ul", { class: "plain geo-results", "aria-label": `${label} address results` });
      for (const r of results) {
        ul.append(el("li", {}, el("button", { type: "button", class: "row", onclick: () => { showGeo(); onAddress(r); } },
          el("span", { class: "grow", text: r.label }),
          r.kind ? el("span", { class: "muted small", text: r.kind }) : null)));
      }
      showGeo(ul, el("p", { class: "muted small", text: "Address search by OpenStreetMap Nominatim." }));
    } catch (err) {
      if (seq === geoSeq) showGeo(el("p", { class: "error-text small", role: "alert", text: err.message }));
    }
  }
  input.addEventListener("input", () => { geoSeq++; showGeo(); }); // typing again discards old address results

  createCombobox({
    input, list, onSubmit: geocode ? submitAddress : undefined,
    emptyText: geocode ? "No matching stops. Press Enter to search addresses." : "No matching stops",
    search: (q) => [{
      heading: "Stops",
      items: searchStops(stops(), q, 8).map((s) => ({ label: s.name, sub: `${s.trips}/day`, badge: "●", badgeClass: "pin", value: s })),
    }],
    onChoose,
  });
  const buttons = el("div", { class: "field-actions" },
    geocode ? el("button", { type: "button", class: "btn small-btn", onclick: submitAddress, text: "Search address" }) : null,
    onLocate ? el("button", { type: "button", class: "btn small-btn", onclick: onLocate, text: "My location" }) : null,
    el("button", { type: "button", class: "btn small-btn", onclick: onPick, text: "Pick on map" }));
  const node = el("div", { class: "place" },
    el("label", { for: id, text: label }),
    el("div", { class: "search-box" }, input, list),
    buttons, geo);
  return { node, input, buttons };
}

/**
 * Build the directions view.
 * @param {object} o
 * @param {() => Array<{id: string, name: string, trips: number}>} o.stops
 * @param {((query: string) => Promise<Array<{label: string, lat: number, lon: number, kind: string}>>) | null} [o.geocode]
 *        address search; leave out to hide the feature
 * @param {object} o.on  callbacks: from(stop), to(stop), fromAddress(r), toAddress(r), pickFrom(), pickTo(),
 *                       locateFrom(), swap(), minutes(min|null), now(), select(index), openStop(id),
 *                       openRoute(id), close()
 */
export function createPlanView({ stops, on, geocode = null }) {
  const from = placeField({
    id: "plan-from", label: "From", hint: "Stop or address, or pick on the map", stops, geocode,
    onChoose: (s) => on.from(s), onAddress: (r) => on.fromAddress(r), onPick: () => on.pickFrom(), onLocate: () => on.locateFrom(),
  });
  const to = placeField({
    id: "plan-to", label: "To", hint: "Stop or address, or pick on the map", stops, geocode,
    onChoose: (s) => on.to(s), onAddress: (r) => on.toAddress(r), onPick: () => on.pickTo(),
  });
  const time = el("input", { id: "plan-time", type: "time", value: "08:00" });
  time.addEventListener("input", () => on.minutes(parseClock(time.value)));
  const results = el("div", { class: "results", "aria-live": "polite" });

  const node = el("section", { class: "card plan" },
    el("div", { class: "card-head" },
      el("h2", { class: "big small", text: "Directions" }),
      el("button", { type: "button", class: "icon-btn", "aria-label": "Close directions", onclick: () => on.close(), text: "×" })),
    from.node,
    el("div", { class: "swap" }, el("button", { type: "button", class: "btn small-btn", onclick: () => on.swap(), "aria-label": "Swap origin and destination", text: "⇅ Swap" })),
    to.node,
    el("div", { class: "field" },
      el("label", { for: "plan-time", text: "Leave at" }), time,
      el("button", { type: "button", class: "btn small-btn", onclick: () => on.now(), text: "Now" }),
      el("span", { class: "muted", text: "(India time)" })),
    results);

  const view = {
    node,
    /** Show a place in a field; `text` is its label, or "" to clear. */
    setFrom(text) { from.input.value = text; },
    setTo(text) { to.input.value = text; },
    setMinutes(min) { if (min !== null) time.value = formatMinutes(min); },
    focusFrom() { from.input.focus(); },
    /** Replace the results area with a message (kind: "info" | "error"). */
    message(text, kind = "info") {
      results.replaceChildren(el("p", { class: kind === "error" ? "error-text" : "muted", role: kind === "error" ? "alert" : "status", text }));
    },
    /** Render journeys; `selected` is the one drawn on the map. */
    journeys(list, selected) {
      const ul = el("ol", { class: "options plain" });
      list.forEach((j, i) => ul.append(optionItem(j, i, i === selected, on)));
      results.replaceChildren(
        ul,
        el("p", { class: "muted small", text: "Walking is estimated from straight-line distance (×1.3, at 4.5 km/h) and times follow the published timetable, not live traffic." }));
    },
  };
  return view;
}

function optionItem(j, index, selected, on) {
  const chips = el("div", { class: "chips" });
  legChips(j).forEach((c, i) => {
    if (i) chips.append(el("span", { class: "muted", "aria-hidden": "true", text: "›" }));
    chips.append(c.kind === "walk"
      ? el("span", { class: "chip walk", text: `Walk ${c.text}` })
      : el("span", { class: "badge", text: c.text }));
  });
  const buses = j.rides === 1 ? "1 bus" : `${j.rides} buses`;
  const head = el("button", {
    type: "button", class: "option-head", "aria-expanded": String(selected), onclick: () => on.select(index),
  },
    el("div", { class: "opt-top" },
      el("strong", { text: formatDuration(j.arrive - j.depart) }),
      el("span", { class: "tnum", text: `${clockSec(j.depart)} → ${clockSec(j.arrive)}` })),
    chips,
    el("div", { class: "muted small", text: j.rides ? buses : "Walking only" }));
  const li = el("li", { class: selected ? "option selected" : "option" }, head);
  if (selected) li.append(steps(j, on));
  return li;
}

function steps(j, on) {
  const ol = el("ol", { class: "steps" });
  j.legs.forEach((l, i) => {
    if (l.mode === "walk") {
      if (isTrivialWalk(l)) return;
      const target = l.to ? l.to.name : "your destination";
      ol.append(el("li", { class: "step walk" },
        el("span", { class: "tnum muted", text: clockSec(l.dep) }),
        el("span", { class: "grow", text: `Walk ${formatDuration(l.arr - l.dep)} (${formatMetres(walkMetres(l))}) to ${target}` })));
      return;
    }
    const prev = j.legs[i - 1];
    const wait = prev ? l.dep - prev.arr : 0;
    if (wait >= 60) {
      ol.append(el("li", { class: "step wait" },
        el("span", { class: "tnum muted", text: clockSec(prev.arr) }),
        el("span", { class: "grow muted", text: `Wait ${formatDuration(wait)} at ${l.from.name}` })));
    }
    ol.append(el("li", { class: "step ride" },
      el("span", { class: "tnum muted", text: clockSec(l.dep) }),
      el("div", { class: "grow" },
        el("div", {},
          el("button", { type: "button", class: "badge linklike", title: routeDesc(l.route.long), onclick: () => on.openRoute(l.route.id), text: shortLabel(l.route.id, l.route.short) }),
          document.createTextNode(" "),
          el("button", { type: "button", class: "linklike", onclick: () => on.openStop(l.from.id), text: l.from.name })),
        el("div", { class: "muted small", text: `${l.stops.length - 1} stops · arrive ${clockSec(l.arr)} at ${l.to.name} (${formatDuration(l.arr - l.dep)})` }))));
  });
  ol.append(el("li", { class: "step end" },
    el("span", { class: "tnum", text: clockSec(j.arrive) }),
    el("strong", { class: "grow", text: "Arrive" })));
  return ol;
}
