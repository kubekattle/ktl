package analyze

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/example/ktl/internal/secrets"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIAnalyzer struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
	Client   *http.Client
}

var ErrQuotaExceeded = errors.New("insufficient_quota")

type providerErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func classifyProviderError(provider string, body []byte) error {
	var p providerErrorPayload
	if err := json.Unmarshal(body, &p); err == nil {
		code := strings.TrimSpace(strings.ToLower(p.Error.Code))
		typ := strings.TrimSpace(strings.ToLower(p.Error.Type))
		msg := strings.TrimSpace(p.Error.Message)
		if code == "insufficient_quota" || typ == "insufficient_quota" {
			if msg == "" {
				msg = "provider quota exceeded"
			}
			return fmt.Errorf("%w: %s (%s)", ErrQuotaExceeded, msg, provider)
		}
	}
	return fmt.Errorf("AI Provider (%s) API error: %s", provider, string(body))
}

func NewAIAnalyzer(provider, model string) *AIAnalyzer {
	a := &AIAnalyzer{
		Provider: provider,
		Model:    model,
	}

	// Fallback to env var if model not provided via CLI
	if a.Model == "" {
		a.Model = os.Getenv("KTL_AI_MODEL")
	}

	switch provider {
	case "openai":
		a.BaseURL = "https://api.openai.com/v1"
		a.APIKey = os.Getenv("OPENAI_API_KEY")
		if a.Model == "" {
			a.Model = "gpt-5"
		}
	case "qwen":
		a.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		a.APIKey = os.Getenv("DASHSCOPE_API_KEY")
		if a.APIKey == "" {
			a.APIKey = os.Getenv("QWEN_API_KEY")
		}
		if a.Model == "" {
			a.Model = "qwen-max"
		}
	case "deepseek":
		a.BaseURL = "https://api.deepseek.com"
		a.APIKey = os.Getenv("DEEPSEEK_API_KEY")
		if a.Model == "" {
			a.Model = "deepseek-chat"
		}
	}
	a.Client = &http.Client{
		Timeout: 60 * time.Second,
	}
	return a
}

func (a *AIAnalyzer) Analyze(ctx context.Context, evidence *Evidence) (*Diagnosis, error) {
	if a.Provider == "mock" {
		return a.mockAnalysis(evidence), nil
	}
	return a.callLLM(ctx, evidence)
}

func (a *AIAnalyzer) callLLM(ctx context.Context, evidence *Evidence) (*Diagnosis, error) {
	if a.APIKey == "" {
		return nil, fmt.Errorf("API Key not set for provider %s. Please set %s_API_KEY", a.Provider, strings.ToUpper(a.Provider))
	}

	model := a.Model
	prompt := buildPrompt(evidence)

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a Kubernetes Expert. Analyze the provided pod logs, events, and status to diagnose the failure. Output JSON only."},
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	// DeepSeek and Qwen might not support "response_format": {"type": "json_object"} strictly in the same way,
	// or might require it. OpenAI does.
	// For now we keep it, as most "compatible" APIs try to support it or ignore it.
	// However, if it fails for others, we might need to make it conditional.
	// DeepSeek V2 supports JSON mode. Qwen also supports it.

	jsonBody, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.BaseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, classifyProviderError(a.Provider, body)
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

	// Strip markdown code blocks if present (some models might add them despite JSON instruction)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")

	var d Diagnosis
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w. Content: %s", err, content)
	}
	return &d, nil
}

// StreamChat sends a chat message and streams the response via a callback.
func (a *AIAnalyzer) StreamChat(ctx context.Context, history []Message, callback func(string)) (string, error) {
	if a.Provider == "mock" {
		msg := "This is a mock streaming response. In real mode, I would answer your question based on the pod context."
		// Simulate typing effect
		for _, c := range msg {
			callback(string(c))
			time.Sleep(20 * time.Millisecond)
		}
		return msg, nil
	}

	if a.APIKey == "" {
		return "", fmt.Errorf("API Key not set for provider %s. Please set %s_API_KEY", a.Provider, strings.ToUpper(a.Provider))
	}

	model := a.Model
	var messages []map[string]string
	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}

	jsonBody, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.BaseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// Use a separate client for streaming with no timeout or longer timeout if needed,
	// but standard client is fine if data keeps coming.
	// Actually, standard client timeout applies to the whole request.
	// For streaming, we might want a longer timeout or rely on context cancellation.
	// Let's use a custom client for streaming to avoid the 60s hard limit cutting off long answers.
	streamClient := &http.Client{
		Timeout: 0, // No timeout, rely on context
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", classifyProviderError(a.Provider, body)
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Ignore malformed chunks
			continue
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				callback(content)
				fullResponse.WriteString(content)
			}
		}
	}

	return fullResponse.String(), nil
}

func (a *AIAnalyzer) Chat(ctx context.Context, history []Message) (string, error) {
	// Fallback to streaming implementation with no-op callback if we wanted to unify,
	// but keeping original for backward compat is safer for now.
	// However, to reduce code dup, let's just implement Chat as a wrapper around StreamChat?
	// No, StreamChat uses stream=true which returns different format.
	// Let's keep Chat as legacy non-streaming or update it.
	// The prompt asked to improve integration. I'll leave Chat as is for non-streaming usage
	// and add StreamChat for the CLI.

	if a.Provider == "mock" {
		return "This is a mock response. In real mode, I would answer your question based on the pod context.", nil
	}

	if a.APIKey == "" {
		return "", fmt.Errorf("API Key not set for provider %s. Please set %s_API_KEY", a.Provider, strings.ToUpper(a.Provider))
	}

	model := a.Model

	// Convert our Message struct to map for JSON
	var messages []map[string]string
	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	jsonBody, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.BaseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", classifyProviderError(a.Provider, body)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	return result.Choices[0].Message.Content, nil
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

	// 4.5 Configuration Validation
	if len(e.ConfigWarnings) > 0 {
		b.WriteString("\n--- Configuration Validation (Missing Resources) ---\n")
		for _, w := range e.ConfigWarnings {
			b.WriteString(fmt.Sprintf("CRITICAL: %s\n", w))
		}
	}

	// 4.6 Network Reachability
	if len(e.NetworkContext) > 0 {
		b.WriteString("\n--- Network Reachability Context ---\n")
		for _, n := range e.NetworkContext {
			b.WriteString(fmt.Sprintf("%s\n", n))
		}
	}

	// 4.65 Resource Analysis
	if len(e.ResourceInfo) > 0 {
		b.WriteString("\n--- Resource Analysis (QoS / Limits) ---\n")
		for _, r := range e.ResourceInfo {
			b.WriteString(fmt.Sprintf("%s\n", r))
		}
	}

	// 4.7 Image Analysis
	if len(e.ImageAnalysis) > 0 {
		b.WriteString("\n--- Image Analysis ---\n")
		for _, i := range e.ImageAnalysis {
			b.WriteString(fmt.Sprintf("%s\n", i))
		}
	}

	// 4.8 Security Audit
	if len(e.SecurityAudit) > 0 {
		b.WriteString("\n--- Security Audit ---\n")
		for _, s := range e.SecurityAudit {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.9 Availability Context (PDB)
	if len(e.Availability) > 0 {
		b.WriteString("\n--- Availability Context (PDB) ---\n")
		for _, a := range e.Availability {
			b.WriteString(fmt.Sprintf("%s\n", a))
		}
	}

	// 4.10 Change Detection (Diff)
	if len(e.ChangeDiff) > 0 {
		b.WriteString("\n--- Change Detection (Recent Deployments) ---\n")
		for _, c := range e.ChangeDiff {
			b.WriteString(fmt.Sprintf("%s\n", c))
		}
	}

	// 4.11 Ingress Context
	if len(e.IngressInfo) > 0 {
		b.WriteString("\n--- Ingress Context (External Access) ---\n")
		for _, i := range e.IngressInfo {
			b.WriteString(fmt.Sprintf("%s\n", i))
		}
	}

	// 4.12 Scaling Context (HPA)
	if len(e.ScalingInfo) > 0 {
		b.WriteString("\n--- Scaling Context (HPA) ---\n")
		for _, s := range e.ScalingInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.13 Storage Context (PVC)
	if len(e.StorageInfo) > 0 {
		b.WriteString("\n--- Storage Context (PVC) ---\n")
		for _, s := range e.StorageInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.14 Scheduling Context
	if len(e.SchedulingInfo) > 0 {
		b.WriteString("\n--- Scheduling Context (Taints/Affinity) ---\n")
		for _, s := range e.SchedulingInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.15 Lifecycle Context
	if len(e.LifecycleInfo) > 0 {
		b.WriteString("\n--- Lifecycle Hooks ---\n")
		for _, l := range e.LifecycleInfo {
			b.WriteString(fmt.Sprintf("%s\n", l))
		}
	}

	// 4.16 Probe Context
	if len(e.ProbeInfo) > 0 {
		b.WriteString("\n--- Probes (Liveness/Readiness) ---\n")
		for _, p := range e.ProbeInfo {
			b.WriteString(fmt.Sprintf("%s\n", p))
		}
	}

	// 4.17 Secret Validation
	if len(e.SecretValidation) > 0 {
		b.WriteString("\n--- Secret Validation (Deep) ---\n")
		for _, s := range e.SecretValidation {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.18 Mesh Context
	if len(e.MeshInfo) > 0 {
		b.WriteString("\n--- Service Mesh Sidecar ---\n")
		for _, m := range e.MeshInfo {
			b.WriteString(fmt.Sprintf("%s\n", m))
		}
	}

	// 4.19 Init Exit Codes
	if len(e.InitExitInfo) > 0 {
		b.WriteString("\n--- Init Container Failures ---\n")
		for _, i := range e.InitExitInfo {
			b.WriteString(fmt.Sprintf("%s\n", i))
		}
	}

	// 4.20 Owner Chain
	if len(e.OwnerChain) > 0 {
		b.WriteString("\n--- Ownership Hierarchy ---\n")
		for _, o := range e.OwnerChain {
			b.WriteString(fmt.Sprintf("%s\n", o))
		}
	}

	// 4.21 Network Policies
	if len(e.NetPolicyInfo) > 0 {
		b.WriteString("\n--- Network Policies (Traffic Rules) ---\n")
		for _, n := range e.NetPolicyInfo {
			b.WriteString(fmt.Sprintf("%s\n", n))
		}
	}

	// 4.22 Certificates
	if len(e.CertInfo) > 0 {
		b.WriteString("\n--- TLS Certificates (Expiration Check) ---\n")
		for _, c := range e.CertInfo {
			b.WriteString(fmt.Sprintf("%s\n", c))
		}
	}

	// 4.23 Resource Quotas
	if len(e.QuotaInfo) > 0 {
		b.WriteString("\n--- Namespace Resource Quotas ---\n")
		for _, q := range e.QuotaInfo {
			b.WriteString(fmt.Sprintf("%s\n", q))
		}
	}

	// 4.25 Log Insights
	if len(e.LogInsights) > 0 {
		b.WriteString("\n--- Log Pattern Matches ---\n")
		for _, l := range e.LogInsights {
			b.WriteString(fmt.Sprintf("%s\n", l))
		}
	}

	// 4.26 Scheduling Constraints (Affinity/Spread/Priority)
	if len(e.AffinityInfo) > 0 || len(e.SpreadInfo) > 0 || len(e.PriorityInfo) > 0 {
		b.WriteString("\n--- Advanced Scheduling ---\n")
		for _, a := range e.AffinityInfo {
			b.WriteString(fmt.Sprintf("%s\n", a))
		}
		for _, s := range e.SpreadInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
		for _, p := range e.PriorityInfo {
			b.WriteString(fmt.Sprintf("%s\n", p))
		}
	}

	// 4.27 PSA & Security
	if len(e.PSAInfo) > 0 {
		b.WriteString("\n--- Pod Security Admission ---\n")
		for _, p := range e.PSAInfo {
			b.WriteString(fmt.Sprintf("%s\n", p))
		}
	}

	// 4.30 Finalizers
	if len(e.FinalizerInfo) > 0 {
		b.WriteString("\n--- Finalizers (Deletion Blockers) ---\n")
		for _, f := range e.FinalizerInfo {
			b.WriteString(fmt.Sprintf("%s\n", f))
		}
	}

	// 4.31 DNS Health
	if len(e.DNSInfo) > 0 {
		b.WriteString("\n--- Cluster DNS Status ---\n")
		for _, d := range e.DNSInfo {
			b.WriteString(fmt.Sprintf("%s\n", d))
		}
	}

	// 4.32 Node Extended Info
	if len(e.NodeExtInfo) > 0 {
		b.WriteString("\n--- Node Extended Details ---\n")
		for _, n := range e.NodeExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", n))
		}
	}

	// 4.34 Security Extended
	if len(e.SecurityExtInfo) > 0 {
		b.WriteString("\n--- Security Audit (Extended) ---\n")
		for _, s := range e.SecurityExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.35 Volume & Resource Extended
	if len(e.VolumeExtInfo) > 0 {
		b.WriteString("\n--- Storage & Resource Extended ---\n")
		for _, v := range e.VolumeExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", v))
		}
	}

	// 4.41 Service Extended
	if len(e.ServiceExtInfo) > 0 {
		b.WriteString("\n--- Service Configuration (Extended) ---\n")
		for _, s := range e.ServiceExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", s))
		}
	}

	// 4.43 Ingress Extended
	if len(e.IngressExtInfo) > 0 {
		b.WriteString("\n--- Ingress Configuration (Extended) ---\n")
		for _, i := range e.IngressExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", i))
		}
	}

	// 4.45 Config Syntax
	if len(e.ConfigSyntaxInfo) > 0 {
		b.WriteString("\n--- Configuration Syntax Checks ---\n")
		for _, c := range e.ConfigSyntaxInfo {
			b.WriteString(fmt.Sprintf("%s\n", c))
		}
	}

	// 4.46 Pod State Extended
	if len(e.PodStateExtInfo) > 0 {
		b.WriteString("\n--- Pod State Analysis (Zombie/Backoff) ---\n")
		for _, p := range e.PodStateExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", p))
		}
	}

	// 4.49 Controller Extended
	if len(e.ControllerExtInfo) > 0 {
		b.WriteString("\n--- Controller Strategy (Deployment/Cron) ---\n")
		for _, c := range e.ControllerExtInfo {
			b.WriteString(fmt.Sprintf("%s\n", c))
		}
	}

	// 5. Local Knowledge Base
	if e.LocalDocs != "" {
		b.WriteString("\n--- Local Knowledge Base (Company Runbook) ---\n")
		// Truncate to avoid token explosion
		docs := e.LocalDocs
		if len(docs) > 2000 {
			docs = docs[:2000] + "...(truncated)"
		}
		b.WriteString(docs + "\n")
	}

	// 5. Logs
	b.WriteString("\n--- Container Logs ---\n")
	for c, l := range e.Logs {
		// Smart truncation
		truncated := smartTruncateLogs(l, 50)
		redacted := secrets.RedactText(truncated)
		b.WriteString(fmt.Sprintf("Container: %s\n%s\n", c, redacted))
	}

	// 5.1 Previous Logs (Time Travel)
	if len(e.PreviousLogs) > 0 {
		b.WriteString("\n--- Previous Container Logs (CRASH HISTORY) ---\n")
		for c, l := range e.PreviousLogs {
			truncated := smartTruncateLogs(l, 50)
			redacted := secrets.RedactText(truncated)
			b.WriteString(fmt.Sprintf("Container: %s (PREVIOUS INSTANCE)\n%s\n", c, redacted))
		}
	}

	// 6. Source Code Snippets (Log-to-Code Correlation)
	if len(e.SourceSnippets) > 0 {
		b.WriteString("\n--- Source Code Context (Found in Local Workspace) ---\n")
		b.WriteString("I found stack traces in the logs that match files in your current workspace. Use this code to explain the crash.\n")
		for _, s := range e.SourceSnippets {
			b.WriteString(fmt.Sprintf("File: %s:%d\n```go\n%s\n```\n", s.File, s.Line, s.Content))
		}
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
