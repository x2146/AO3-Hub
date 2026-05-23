package webassets

import "embed"

// FS contains the compiled Vite bundle. scripts/build-go.mjs refreshes
// web-dist before compiling the release binary.
//
//go:embed web-dist
var FS embed.FS

const Root = "web-dist"
