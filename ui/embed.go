// Package ui holds the dashboard: the page template, the script, the
// stylesheet, and the fonts. The binary embeds all of them.
package ui

import "embed"

// FS holds index.html.tmpl and the static directory.
//
//go:embed index.html.tmpl static
var FS embed.FS
