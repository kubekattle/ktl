package docs

import _ "embed"

var (
	//go:embed architecture.md
	ArchitectureMD string

	//go:embed deps.md
	DepsMD string

	//go:embed config-atlas.md
	ConfigAtlasMD string

	//go:embed recipes.md
	RecipesMD string

	//go:embed troubleshooting.md
	TroubleshootingMD string
)
