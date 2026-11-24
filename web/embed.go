// Package web embeds the static front end (page, modules, styles, vendored
// MapLibre). `tgbus build` copies these files into the generated site.
package web

import "embed"

// FS holds the site's static assets. embed.go itself is skipped when copying.
//
//go:embed index.html style.css js vendor
var FS embed.FS
