// Package data embeds the instrument definitions and scoring configuration
// (PRD §5.2 FR-05: instruments are data, versioned; thresholds live in config).
// Embedding keeps them available in the distroless/alpine runtime image, which
// ships only the binary.
package data

import "embed"

//go:embed *.json
var FS embed.FS

// Read returns the embedded file content.
func Read(name string) ([]byte, error) { return FS.ReadFile(name) }
