// Renders the side panel. Functions return DOM nodes; app.js decides where they go.
import { CONFIG } from "./config.js";
import { el } from "./dom.js";
import { clock, nextDepartures, parseClock, formatMinutes, nowMinutes, routeDesc, shortLabel, waitText } from "./util.js";

export function loadingCard(text) {
  return el("p", { class: "muted", role: "status", text });
}

export function errorCard(message, onRetry) {
  return el("div", { class: "error", role: "alert" },
    el("p", { text: message }),
    onRetry ? el("button", { type: "button", class: "btn", onclick: onRetry, text: "Try again" }) : null);
}

export function homeCard(busiest, onOpenStop) {
  const list = el("ol", { class: "plain" });
  for (const s of busiest) {
    list.append(el("li", {}, el("button", { type: "button", class: "row", onclick: () => onOpenStop(s.id) },
      el("span", { class: "grow", text: s.name }),
      el("span", { class: "muted", text: `${s.trips} trips/day` }))));
  }
  return el("section", {},
    el("p", { text: "Search for a route number or a stop, or click a line or stop on the map." }),
    busiest.length ? el("h2", { text: "Busiest stops" }) : null,
    busiest.length ? list : null);
}

export function routeCard(data, { onClose, onOpenStop, onFocusStop }) {
  const r = data.route;
  const stats = el("dl", { class: "stats" });
  const row = (k, v) => stats.append(el("dt", { text: k }), el("dd", { text: v }));
  row("Trips per day", String(r.trips));
  if (r.firstDeparture) row("First / last departure", `${r.firstDeparture} / ${r.lastDeparture}`);

  const lines = data.features.filter((f) => f.properties.kind === "line").map((f) => f.properties);
  const byDir = new Map();
  for (const f of data.features) {
    if (f.properties.kind !== "stop") continue;
    const d = f.properties.direction;
    if (!byDir.has(d)) byDir.set(d, []);
    byDir.get(d).push(f);
  }

  const patterns = el("div");
  if (!byDir.size) patterns.append(el("p", { class: "muted", text: "This route has no scheduled trips in the feed." }));
  for (const [dir, feats] of byDir) {
    const info = lines.find((l) => l.direction === dir);
    const ol = el("ol", { class: "stops" });
    for (const f of feats) {
      const p = f.properties;
      ol.append(el("li", {}, el("button", {
        type: "button", class: "row", title: "Open stop",
        onmouseenter: () => onFocusStop(f.geometry.coordinates),
        onfocus: () => onFocusStop(f.geometry.coordinates),
        onmouseleave: () => onFocusStop(null),
        onblur: () => onFocusStop(null),
        onclick: () => onOpenStop(p.id),
      }, el("span", { class: "n", text: String(p.seq) }), el("span", { class: "grow", text: p.name }),
        el("span", { class: "muted tnum", text: p.time }))));
    }
    const swatch = el("span", { class: "swatch" });
    swatch.style.background = CONFIG.directionColors[dir] || "#6e7781";
    const first = feats[0].properties.name;
    const last = feats[feats.length - 1].properties.name;
    patterns.append(el("details", byDir.size === 1 || dir === 0 ? { open: true } : {},
      el("summary", {}, swatch, document.createTextNode(
        `${first} → ${last} (${feats.length} stops${info ? `, ${info.trips} trips` : ""})`)),
      ol));
  }

  return el("section", { class: "card" },
    el("div", { class: "card-head" },
      el("div", {},
        el("h2", { class: "big", text: shortLabel(r.id, r.short) }),
        el("div", { class: "muted", text: r.long })),
      el("button", { type: "button", class: "icon-btn", "aria-label": "Close route", onclick: onClose, text: "×" })),
    stats, patterns);
}

export function stopCard(stop, { onClose, onOpenRoute }) {
  const head = el("div", { class: "card-head" },
    el("div", {},
      el("h2", { class: "big small", text: stop.name }),
      el("div", { class: "muted", text: `${stop.trips} trips/day · ${stop.routes.length} routes` })),
    el("button", { type: "button", class: "icon-btn", "aria-label": "Close stop", onclick: onClose, text: "×" }));

  const body = el("div");
  const initial = nowMinutes(CONFIG.timeZone);
  const timeInput = el("input", { type: "time", id: "from-time", value: formatMinutes(initial) });
  const control = el("div", { class: "field" },
    el("label", { for: "from-time", text: "Departures from" }), timeInput,
    el("span", { class: "muted", text: "(India time)" }));

  const renderRoutes = () => {
    const from = parseClock(timeInput.value) ?? initial;
    body.replaceChildren();
    const withDeps = stop.routes.filter((r) => r.deps.length);
    if (!withDeps.length) {
      body.append(el("p", { class: "muted", text: "No buses depart from this stop: every trip here ends at it." }));
      return;
    }
    // Soonest departure first, so the bus you can catch next is at the top.
    const upcoming = withDeps
      .map((r) => ({ r, next: nextDepartures(r.deps, from, 3) }))
      .sort((a, b) => a.next[0].wait - b.next[0].wait || b.r.trips - a.r.trips);
    for (const { r, next } of upcoming) {
      const nextLine = el("div", { class: "next" },
        ...next.flatMap((d, i) => [
          i ? document.createTextNode("  ·  ") : null,
          el("strong", { class: "tnum", text: clock(d.tod) }),
          i === 0 ? el("span", { class: "muted", text: ` ${waitText(d.wait)}` }) : null,
        ]).filter(Boolean));
      const all = el("div", { class: "times" },
        ...[...r.deps].sort((a, b) => (a % 1440) - (b % 1440)).map((d) => el("span", { class: "tnum", text: clock(d) })));
      body.append(el("div", { class: "route-at-stop" },
        el("button", { type: "button", class: "row", onclick: () => onOpenRoute(r.id) },
          el("span", { class: "badge", text: shortLabel(r.id, r.short) }),
          el("span", { class: "grow", text: routeDesc(r.long) })),
        nextLine,
        el("details", {}, el("summary", { text: `All ${r.deps.length} departures` }), all)));
    }
  };
  timeInput.addEventListener("input", renderRoutes);
  renderRoutes();

  return el("section", { class: "card" }, head, control, body);
}
