// Classic worker (wasm_exec.js is not an ES module). It loads the Go
// WebAssembly planner and the timetable, then answers plan requests, so the
// page never blocks while a journey is being computed.
"use strict";

let started = false;

function post(message) {
  self.postMessage(message);
}

async function start() {
  if (started) return;
  started = true;
  try {
    importScripts("../wasm_exec.js"); // defines Go; resolved relative to this file
    const [wasmRes, ttRes] = await Promise.all([fetch("../planner.wasm"), fetch("../data/timetable.bin")]);
    if (!wasmRes.ok || !ttRes.ok) {
      // A site built without the planner (no `make wasm`): not an error to the user.
      post({ type: "unavailable" });
      return;
    }
    const go = new Go();
    // arrayBuffer + instantiate works even if the host serves the wrong MIME type.
    const { instance } = await WebAssembly.instantiate(await wasmRes.arrayBuffer(), go.importObject);
    go.run(instance); // runs for the life of the worker
    const err = self.tgbusPlanner.load(new Uint8Array(await ttRes.arrayBuffer()));
    if (err) {
      post({ type: "error", message: err });
      return;
    }
    post({ type: "ready", counts: self.tgbusPlanner.counts() });
  } catch (err) {
    // importScripts throws a NetworkError when wasm_exec.js is missing.
    if (err && err.name === "NetworkError") post({ type: "unavailable" });
    else post({ type: "error", message: String((err && err.message) || err) });
  }
}

self.onmessage = (event) => {
  const m = event.data;
  if (m.type === "init") {
    start();
  } else if (m.type === "plan") {
    if (!self.tgbusPlanner) {
      post({ type: "result", id: m.id, response: { journeys: [], code: "not_loaded", error: "The planner is not loaded yet." } });
      return;
    }
    let response;
    try {
      response = JSON.parse(self.tgbusPlanner.plan(JSON.stringify(m.request)));
    } catch (err) {
      response = { journeys: [], code: "internal", error: String((err && err.message) || err) };
    }
    post({ type: "result", id: m.id, response });
  }
};
