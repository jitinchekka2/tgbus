# Builds the full site: static data + journey planner (WebAssembly).
#   make site FEED=path/to/feed      # writes ./dist
#   make serve                       # preview at http://localhost:8080
FEED ?= Telangana_opendata_gtfs_TGSRTC_25_September_2026
OUT  ?= dist
NAME ?=

.PHONY: site wasm serve test

site:
	go run ./cmd/tgbus build -feed "$(FEED)" -out "$(OUT)" $(if $(NAME),-name "$(NAME)")
	$(MAKE) wasm OUT="$(OUT)"

# The planner compiled for the browser, with the matching Go runtime shim.
wasm:
	mkdir -p "$(OUT)"
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$(OUT)/planner.wasm" ./cmd/planner-wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" "$(OUT)/wasm_exec.js"

serve:
	go run ./cmd/tgbus serve -dir "$(OUT)"

test:
	go vet ./... && go test -race ./... && node --test web/test/*.test.mjs
