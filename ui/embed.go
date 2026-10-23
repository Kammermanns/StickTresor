// Package ui enthält die Oberfläche (eine HTML-Datei, ohne Firmendaten).
package ui

import _ "embed"

//go:embed index.html
var index string

// Page liefert die fertige Seite.
func Page() string { return index }
