// Package web embeds the built frontend so the API binary serves the SPA.
package web

import "embed"

// Dist holds apps/web/dist copied in by `make build-web`.
// Only .gitkeep is committed; the real bundle is generated at build time.
//
//go:embed dist/*
var Dist embed.FS
