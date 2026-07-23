package hook

import (
	"github.com/fujiwarashigure/ATRI-X/internal/processenv"
)

// ProcessEnvironment is the environment inherited by process Hooks.
type ProcessEnvironment = processenv.Environment

// NewProcessEnvironment merges config values into the ATRI-X process
// environment. Process values win, except that config PATH entries are appended.
func NewProcessEnvironment(base []string, values map[string]string) ProcessEnvironment {
	return processenv.New(base).Fill(values)
}
