package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		return a.callOpenAI(ctx, evidence)
	}
	return nil, fmt.Errorf("unknown provider: %s", a.Provider)
}

func (a *AIAnalyzer) callOpenAI(ctx context.Context, evidence *Evidence) (*Diagnosis, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	prompt := buildPrompt(evidence)

	reqBody := map[string]interface{}{
		"model": "gpt-4-turbo-preview", // Use a smart model
		"messages": []map[string]string{
			{"role": "system", "content": "You are a Kubernetes Expert. Analyze the provided pod logs, events, and status to diagnose the failure. Output JSON only."},
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %s", string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from AI")
	}

	content := result.Choices[0].Message.Content
	var d Diagnosis
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}
	return &d, nil
}

func buildPrompt(e *Evidence) string {
	var b strings.Builder
	b.WriteString("Analyze this pod failure.\n")
	if e.Pod != nil {
		b.WriteString(fmt.Sprintf("Pod: %s (Status: %s)\n", e.Pod.Name, e.Pod.Status.Phase))
	}
	b.WriteString("Events:\n")
	for _, ev := range e.Events {
		b.WriteString(fmt.Sprintf("- %s: %s\n", ev.Reason, ev.Message))
	}
	b.WriteString("Logs:\n")
	for c, l := range e.Logs {
		b.WriteString(fmt.Sprintf("--- Container %s ---\n%s\n", c, l))
	}
	b.WriteString("\nProvide response in JSON format with keys: rootCause, suggestion, explanation, confidenceScore (float), patch (optional kubectl patch string or JSON patch).")
	return b.String()
}

func (a *AIAnalyzer) mockAnalysis(evidence *Evidence) *Diagnosis {
	// Simulate "Smart" analysis by looking deeper than the heuristic analyzer
	d := &Diagnosis{
		ConfidenceScore: 0.99,
		RootCause:       "Simulated AI Analysis: Database Connection Failure",
		Suggestion:      "Inject the 'DB_PASSWORD' secret into the pod environment.",
		Explanation:     "I analyzed the logs and found a 'Connection Refused' error on port 5432. The environment variable dump (from crash logs) shows 'DB_PASSWORD' is empty, which matches the 'Access Denied' error pattern.",
		Patch:           `{"spec": {"containers": [{"name": "db", "env": [{"name": "DB_PASSWORD", "valueFrom": {"secretKeyRef": {"name": "db-secret", "key": "password"}}}]}]}}`,
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
