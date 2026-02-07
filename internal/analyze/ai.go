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

	"github.com/example/ktl/internal/secrets"
)

type AIAnalyzer struct {
	Provider string
	Model    string
}

func NewAIAnalyzer(provider string) *AIAnalyzer {
	return &AIAnalyzer{
		Provider: provider,
		Model:    os.Getenv("KTL_AI_MODEL"),
	}
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

	model := a.Model
	if model == "" {
		model = "gpt-4-turbo-preview"
	}

	prompt := buildPrompt(evidence)

	reqBody := map[string]interface{}{
		"model": model,
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
	b.WriteString("Analyze this pod failure with holistic cluster context.\n")

	// 1. Pod Context
	if e.Pod != nil {
		b.WriteString(fmt.Sprintf("Target Pod: %s (Phase: %s, Node: %s)\n", e.Pod.Name, e.Pod.Status.Phase, e.Pod.Spec.NodeName))
	}

	// 2. Node Context (Infrastructure Health)
	if e.Node != nil {
		b.WriteString("\n--- Infrastructure Context (Node) ---\n")
		b.WriteString(fmt.Sprintf("Node Name: %s\n", e.Node.Name))
		for _, cond := range e.Node.Status.Conditions {
			if cond.Status == "True" && cond.Type != "Ready" {
				b.WriteString(fmt.Sprintf("WARNING: Node Condition %s is True (Reason: %s, Msg: %s)\n", cond.Type, cond.Reason, cond.Message))
			} else if cond.Type == "Ready" && cond.Status != "True" {
				b.WriteString(fmt.Sprintf("CRITICAL: Node is NOT Ready (Reason: %s)\n", cond.Reason))
			}
		}
		// Check allocatable vs capacity? (Omitted for brevity, AI can infer from OOMKilled events)
	}

	// 3. Direct Events
	b.WriteString("\n--- Pod Events ---\n")
	for _, ev := range e.Events {
		// Redact events too, just in case
		msg := secrets.RedactText(ev.Message)
		b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", ev.Type, ev.Reason, msg))
	}

	// 4. Neighborhood Context (Namespace Events)
	// Filter out events we already showed for the pod to reduce noise
	b.WriteString("\n--- Namespace Context (Potential Noisy Neighbors / Quotas) ---\n")
	shownEvents := make(map[string]bool)
	for _, ev := range e.Events {
		shownEvents[string(ev.UID)] = true
	}
	count := 0
	for _, ev := range e.NamespaceEvents {
		if shownEvents[string(ev.UID)] {
			continue
		}
		if ev.Type == "Warning" { // Only show warnings from others to save tokens
			msg := secrets.RedactText(ev.Message)
			b.WriteString(fmt.Sprintf("- [%s] %s (on %s): %s\n", ev.Type, ev.Reason, ev.InvolvedObject.Name, msg))
			count++
		}
		if count >= 10 {
			break
		} // Limit to top 10 external warnings
	}

	// 5. Logs
	b.WriteString("\n--- Container Logs ---\n")
	for c, l := range e.Logs {
		// Smart truncation: keep last 20 lines + any lines with "error", "fatal", "panic"
		truncated := smartTruncateLogs(l, 50)
		redacted := secrets.RedactText(truncated)
		b.WriteString(fmt.Sprintf("Container: %s\n%s\n", c, redacted))
	}
	b.WriteString("\nProvide response in JSON format with keys: rootCause, suggestion, explanation, confidenceScore (float), patch (optional kubectl patch string or JSON patch).")
	return b.String()
}

func smartTruncateLogs(logs string, maxLines int) string {
	lines := strings.Split(logs, "\n")
	if len(lines) <= maxLines {
		return logs
	}

	var importantLines []string
	// Always keep last N/2 lines
	tailSize := maxLines / 2
	if tailSize < 1 {
		tailSize = 1
	}

	// Scan first part for keywords
	keywordLimit := maxLines - tailSize
	found := 0
	for _, line := range lines[:len(lines)-tailSize] {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") || strings.Contains(lower, "exception") {
			importantLines = append(importantLines, line)
			found++
			if found >= keywordLimit {
				break
			}
		}
	}

	// Add separator if we skipped lines
	if len(importantLines) < len(lines)-tailSize {
		importantLines = append(importantLines, "... (skipped non-error lines) ...")
	}

	// Add tail
	importantLines = append(importantLines, lines[len(lines)-tailSize:]...)

	return strings.Join(importantLines, "\n")
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

	// 1. Check Node Health (Holistic Check)
	if evidence.Node != nil {
		for _, cond := range evidence.Node.Status.Conditions {
			if cond.Type == "DiskPressure" && cond.Status == "True" {
				return &Diagnosis{
					ConfidenceScore: 0.95,
					RootCause:       "Infrastructure Failure: Node Disk Pressure",
					Suggestion:      "Free up disk space on node " + evidence.Node.Name + " or evict low-priority pods.",
					Explanation:     fmt.Sprintf("The pod is failing because the underlying node (%s) is under Disk Pressure. This is an infrastructure issue, not a pod configuration issue.", evidence.Node.Name),
				}
			}
		}
	}

	// 2. Check Logs
	for _, log := range evidence.Logs {
		if strings.Contains(log, "cgroup") {
			d.RootCause = "Simulated AI Analysis: Cgroup v2 Incompatibility"
			d.Suggestion = "Mount /sys/fs/cgroup from host or use a privileged security context."
			d.Explanation = "Logs indicate a failure to access cgroup controllers, common in nested container environments on modern Linux kernels."
			d.Patch = "" // No easy patch for this one
			return d
		}
	}

	return d
}
