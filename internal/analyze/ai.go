package analyze

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type AIAnalyzer struct {
	Provider string
}

func NewAIAnalyzer(provider string) *AIAnalyzer {
	return &AIAnalyzer{Provider: provider}
}

func (a *AIAnalyzer) Analyze(ctx context.Context, evidence *Evidence) (*Diagnosis, error) {
	if a.Provider == "mock" {
		return a.mockAnalysis(evidence), nil
	}
	if a.Provider == "openai" {
		// In a real implementation, we would call OpenAI API here.
		// Since we can't hardcode keys, we check env var.
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}
		// TODO: Implement actual API call
		return nil, fmt.Errorf("OpenAI provider implementation is a placeholder. Please use --provider=mock for demo.")
	}
	return nil, fmt.Errorf("unknown provider: %s", a.Provider)
}

func (a *AIAnalyzer) mockAnalysis(evidence *Evidence) *Diagnosis {
	// Simulate "Smart" analysis by looking deeper than the heuristic analyzer
	d := &Diagnosis{
		ConfidenceScore: 0.99,
		RootCause:       "Simulated AI Analysis: Database Connection Failure",
		Suggestion:      "Inject the 'DB_PASSWORD' secret into the pod environment.",
		Explanation:     "I analyzed the logs and found a 'Connection Refused' error on port 5432. The environment variable dump (from crash logs) shows 'DB_PASSWORD' is empty, which matches the 'Access Denied' error pattern.",
	}
	
	// Make it slightly dynamic based on input logs to prove we "looked" at them
	for _, log := range evidence.Logs {
		if strings.Contains(log, "cgroup") {
			d.RootCause = "Simulated AI Analysis: Cgroup v2 Incompatibility"
			d.Suggestion = "Mount /sys/fs/cgroup from host or use a privileged security context."
			d.Explanation = "Logs indicate a failure to access cgroup controllers, common in nested container environments on modern Linux kernels."
			return d
		}
	}

	return d
}
