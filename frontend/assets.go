// Package frontend embeds the English browser panel and public target catalogs.
// Backend asset preparation and HTTP serving use these exact source files.
package frontend

import "embed"

//go:embed dashboard.html official-relays.json cod-test-targets.json
var Files embed.FS
