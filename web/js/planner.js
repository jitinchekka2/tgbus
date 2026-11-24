// Page-side handle on the planner worker. The worker (and its ~1.8 MB of
// WebAssembly and timetable) starts only when directions are first needed.

/**
 * state: "idle" | "loading" | "ready" | "unavailable" | "error".
 * `onState` is called whenever it changes.
 */
export function createPlanner(onState = () => {}) {
  let worker = null;
  let seq = 0;
  const waiting = new Map();
  const self = {
    state: "idle",
    message: "",

    /** Start loading if not started. Safe to call repeatedly. */
    init() {
      if (worker) return; // already started
      if (typeof Worker === "undefined") {
        worker = {}; // remember that we tried
        set("unavailable", "This browser cannot run the planner.");
        return;
      }
      // Create the worker before announcing "loading": the announcement can
      // call back into init(), which must then see that it has started.
      worker = new Worker(new URL("./planner-worker.js", import.meta.url));
      set("loading");
      worker.onmessage = (e) => {
        const m = e.data;
        if (m.type === "ready") set("ready");
        else if (m.type === "unavailable") set("unavailable", "The journey planner is not part of this build of the site.");
        else if (m.type === "error") set("error", m.message);
        else if (m.type === "result") {
          const resolve = waiting.get(m.id);
          waiting.delete(m.id);
          if (resolve) resolve(m.response);
        }
      };
      worker.onerror = (e) => set("error", e.message || "The planner worker failed.");
      worker.postMessage({ type: "init" });
    },

    /** Resolves with the planner's response. Rejects if the planner is not usable. */
    plan(request) {
      if (self.state !== "ready") return Promise.reject(new Error(`planner is ${self.state}`));
      return new Promise((resolve) => {
        const id = ++seq;
        waiting.set(id, resolve);
        worker.postMessage({ type: "plan", id, request });
      });
    },
  };

  function set(state, message = "") {
    self.state = state;
    self.message = message;
    if (state === "error" || state === "unavailable") {
      for (const resolve of waiting.values()) resolve({ journeys: [], code: "not_loaded", error: message });
      waiting.clear();
    }
    onState(state, message);
  }
  return self;
}
