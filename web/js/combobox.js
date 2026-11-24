// An accessible search combobox (ARIA 1.2 list autocomplete). The caller
// supplies the results; this owns focus, keyboard handling and announcing.
import { el } from "./dom.js";

let nextId = 0;

/**
 * @param {object} cfg
 * @param {HTMLInputElement} cfg.input   must already have role="combobox"
 * @param {HTMLUListElement} cfg.list    an empty <ul role="listbox" hidden>
 * @param {(query: string) => Array<{heading?: string, items: Array<{label: string, sub?: string, badge?: string, value: any}>}>} cfg.search
 * @param {(value: any) => void} cfg.onChoose
 * @param {string} [cfg.emptyText]
 */
export function createCombobox({ input, list, search, onChoose, emptyText = "No matches" }) {
  const prefix = `cb${++nextId}`;
  let options = [];
  let active = -1;
  let timer;

  function close() {
    list.replaceChildren();
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
    options = [];
    active = -1;
  }

  function setActive(i) {
    active = i;
    options.forEach((o, n) => o.node.setAttribute("aria-selected", String(n === i)));
    if (i >= 0) {
      input.setAttribute("aria-activedescendant", options[i].node.id);
      options[i].node.scrollIntoView({ block: "nearest" });
    } else {
      input.removeAttribute("aria-activedescendant");
    }
  }

  function choose(opt) {
    close();
    onChoose(opt.value);
  }

  function render(query) {
    if (!query.trim()) {
      close();
      return;
    }
    list.replaceChildren();
    options = [];
    for (const group of search(query)) {
      if (!group.items.length) continue;
      if (group.heading) list.append(el("li", { role: "presentation", class: "group", text: group.heading }));
      for (const item of group.items) {
        const node = el("li", { role: "option", id: `${prefix}-${options.length}`, "aria-selected": "false", class: "opt" },
          item.badge ? el("span", { class: item.badgeClass || "badge", text: item.badge }) : null,
          el("span", { class: "grow", text: item.label }),
          item.sub ? el("span", { class: "muted small", text: item.sub }) : null);
        const opt = { node, value: item.value };
        node.addEventListener("mousedown", (e) => e.preventDefault()); // keep focus in the input
        node.addEventListener("click", () => choose(opt));
        options.push(opt);
        list.append(node);
      }
    }
    if (!options.length) list.append(el("li", { role: "presentation", class: "empty", text: emptyText }));
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
    setActive(-1);
  }

  input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(() => render(input.value), 120);
  });
  input.addEventListener("keydown", (e) => {
    const n = options.length;
    if (e.key === "ArrowDown" && n) { e.preventDefault(); setActive((active + 1) % n); }
    else if (e.key === "ArrowUp" && n) { e.preventDefault(); setActive((active - 1 + n) % n); }
    else if (e.key === "Enter" && n) { e.preventDefault(); choose(options[Math.max(active, 0)]); }
    else if (e.key === "Escape") { if (n) close(); else input.value = ""; }
  });
  input.addEventListener("blur", () => setTimeout(close, 100));
  input.setAttribute("aria-controls", list.id);

  return { close, render: () => render(input.value) };
}
