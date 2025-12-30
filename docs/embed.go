package docs

import _ "embed"

var (
	//go:embed architecture.md
	ArchitectureMD string

	//go:embed deps.md
	DepsMD string
)
