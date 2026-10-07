// Package guide supplies the canonical operating instructions.
package guide

import _ "embed"

// Text is the canonical operating guide embedded in the executable.
//
//go:embed operating-guide.md
var Text string
