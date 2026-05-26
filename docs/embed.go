package docs

import _ "embed"

var (
	//go:embed architecture.md
	ArchitectureMD string

	//go:embed architecture-diagrams.md
	ArchitectureDiagramsMD string

	//go:embed deps.md
	DepsMD string

	//go:embed config-atlas.md
	ConfigAtlasMD string

	//go:embed recipes.md
	RecipesMD string

	//go:embed mcp-server-spec.md
	MCPServerSpecMD string

	//go:embed grpc-agent.md
	GRPCAgentMD string

	//go:embed fleet-control-plane-spec.md
	FleetControlPlaneSpecMD string

	//go:embed enterprise-agent-operations.md
	EnterpriseAgentOperationsMD string

	//go:embed s3-build-cache.md
	S3BuildCacheMD string

	//go:embed demos.md
	DemosMD string

	//go:embed troubleshooting.md
	TroubleshootingMD string

	//go:embed capture.md
	CaptureMD string

	//go:embed verifier.md
	VerifierMD string

	//go:embed apply-simulate.md
	ApplySimulateMD string

	//go:embed guardian.md
	GuardianMD string

	//go:embed incident.md
	IncidentMD string

	//go:embed contract.md
	ContractMD string

	//go:embed proof-graph.md
	ProofGraphMD string

	//go:embed secrets-verifier-evidence-spec.md
	SecretsVerifierEvidenceSpecMD string

	//go:embed security-corpus-spec.md
	SecurityCorpusSpecMD string
)
