//go:build js && wasm

// Command planner-wasm is the journey planner compiled to WebAssembly. A page
// loads it in a Web Worker, hands it timetable.bin, then sends JSON queries.
package main

import (
	"strconv"
	"syscall/js"

	"github.com/jitinchekka2/tgbus/planapi"
	"github.com/jitinchekka2/tgbus/raptor"
)

var service *planapi.Service

// load(bytes Uint8Array) -> "" on success, else an error message.
func load(_ js.Value, args []js.Value) any {
	if len(args) != 1 {
		return "load needs one argument: the timetable bytes"
	}
	buf := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(buf, args[0])
	s, err := planapi.Load(buf, raptor.DefaultOptions())
	if err != nil {
		return err.Error()
	}
	service = s
	return ""
}

// plan(requestJSON string) -> responseJSON string.
func plan(_ js.Value, args []js.Value) any {
	if service == nil {
		return `{"journeys":[],"code":"not_loaded","error":"the timetable has not been loaded"}`
	}
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return `{"journeys":[],"code":"bad_request","error":"plan needs one JSON string argument"}`
	}
	return service.PlanJSON(args[0].String())
}

// counts() -> "stops,routes".
func counts(_ js.Value, _ []js.Value) any {
	if service == nil {
		return "0,0"
	}
	st, rt := service.Counts()
	return strconv.Itoa(st) + "," + strconv.Itoa(rt)
}

func main() {
	js.Global().Set("tgbusPlanner", map[string]any{
		"load":   js.FuncOf(load),
		"plan":   js.FuncOf(plan),
		"counts": js.FuncOf(counts),
	})
	select {} // stay alive to serve calls
}
