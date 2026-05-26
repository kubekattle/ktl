package agentappliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"
)

func probeAPI(ctx context.Context, opts Options) APIReport {
	report := APIReport{Total: len(opts.APIURLs)}
	if len(opts.APIURLs) == 0 {
		return report
	}
	client := &http.Client{Timeout: opts.Timeout}
	for _, url := range opts.APIURLs {
		probe := probeOneAPI(ctx, client, url, opts)
		report.Probes = append(report.Probes, probe)
		if probe.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report
}

func probeOneAPI(ctx context.Context, client *http.Client, url string, opts Options) APIProbe {
	start := time.Now()
	probe := APIProbe{
		URL:    url,
		Method: http.MethodGet,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		probe.Error = redactMultiline(err.Error())
		probe.DurationMillis = time.Since(start).Milliseconds()
		return probe
	}
	req.Header.Set("User-Agent", "torque-agent-appliance/1")
	resp, err := client.Do(req)
	if err != nil {
		probe.Error = redactMultiline(err.Error())
		probe.DurationMillis = time.Since(start).Milliseconds()
		return probe
	}
	defer resp.Body.Close()
	probe.StatusCode = resp.StatusCode
	probe.ContentType = resp.Header.Get("Content-Type")
	probe.Headers = safeHeaders(resp.Header)

	limit := int64(opts.MaxOutputBytes + 1)
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if readErr != nil {
		probe.Error = redactMultiline(readErr.Error())
	}
	if len(body) > opts.MaxOutputBytes {
		body = body[:opts.MaxOutputBytes]
		probe.BodyTruncated = true
	}
	bodyText := redactMultiline(string(body))
	sum := sha256.Sum256([]byte(bodyText))
	probe.CapturedSHA256 = hex.EncodeToString(sum[:])
	probe.BodyPreview, _ = preview(bodyText, 2048)
	probe.DurationMillis = time.Since(start).Milliseconds()
	probe.Passed = resp.StatusCode >= 200 && resp.StatusCode < 400 && probe.Error == ""
	return probe
}

func safeHeaders(headers http.Header) map[string]string {
	out := map[string]string{}
	for key, values := range headers {
		if sensitiveHeader(key) {
			continue
		}
		cleanValues := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(redactMultiline(value))
			if value != "" {
				cleanValues = append(cleanValues, value)
			}
		}
		if len(cleanValues) > 0 {
			out[http.CanonicalHeaderKey(key)] = strings.Join(cleanValues, ", ")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sensitiveHeader(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
		return true
	}
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password")
}
