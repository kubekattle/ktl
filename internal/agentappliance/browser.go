package agentappliance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type browserScriptConfig struct {
	RepoDir        string   `json:"repoDir"`
	OutDir         string   `json:"outDir"`
	ArtifactPrefix string   `json:"artifactPrefix"`
	URLs           []string `json:"urls"`
	Mode           string   `json:"mode"`
	TimeoutMillis  int64    `json:"timeoutMillis"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
}

func runBrowser(ctx context.Context, repoDir, outDir string, opts Options) BrowserReport {
	report := BrowserReport{
		Mode:  opts.BrowserMode,
		Total: len(opts.BrowserURLs),
	}
	if len(opts.BrowserURLs) == 0 {
		return report
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return skippedBrowserReport(report, opts.BrowserURLs, "node executable not found")
	}
	if _, err := commandOutput(ctx, repoDir, opts.Timeout, nodePath, "-e", "require.resolve('playwright', { paths: [process.cwd()] })"); err != nil {
		return skippedBrowserReport(report, opts.BrowserURLs, "playwright package not found from repository root")
	}

	browserDir := filepath.Join(outDir, "browser")
	if err := os.MkdirAll(browserDir, 0o755); err != nil {
		return failedBrowserReport(report, opts.BrowserURLs, err.Error())
	}
	configPath := filepath.Join(browserDir, "config.json")
	scriptPath := filepath.Join(browserDir, "run_playwright.cjs")
	resultPath := filepath.Join(browserDir, "result.json")
	cfg := browserScriptConfig{
		RepoDir:        repoDir,
		OutDir:         browserDir,
		ArtifactPrefix: "browser",
		URLs:           opts.BrowserURLs,
		Mode:           opts.BrowserMode,
		TimeoutMillis:  opts.Timeout.Milliseconds(),
		MaxOutputBytes: opts.MaxOutputBytes,
	}
	if err := writeJSON(configPath, cfg); err != nil {
		return failedBrowserReport(report, opts.BrowserURLs, err.Error())
	}
	if err := os.WriteFile(scriptPath, []byte(playwrightRunnerScript), 0o755); err != nil {
		return failedBrowserReport(report, opts.BrowserURLs, err.Error())
	}

	runTimeout := opts.Timeout*time.Duration(len(opts.BrowserURLs)) + 10*time.Second
	start := time.Now()
	out, err := commandOutput(ctx, repoDir, runTimeout, nodePath, scriptPath, configPath)
	if err != nil && out == "" {
		parsed := readBrowserResult(resultPath)
		if parsed.Total > 0 || len(parsed.Captures) > 0 {
			report = parsed
		} else {
			return failedBrowserReport(report, opts.BrowserURLs, redactMultiline(err.Error()))
		}
	} else {
		report = readBrowserResult(resultPath)
	}
	if report.Total == 0 {
		report.Total = len(opts.BrowserURLs)
	}
	if report.Mode == "" {
		report.Mode = opts.BrowserMode
	}
	report.PlaywrightAvailable = true
	redactBrowserDOM(outDir, report.Captures)
	if err != nil {
		for i := range report.Captures {
			if report.Captures[i].Error == "" && !report.Captures[i].Passed {
				report.Captures[i].Error = redactMultiline(err.Error())
			}
		}
	}
	if len(report.Captures) == 0 {
		report = failedBrowserReport(report, opts.BrowserURLs, fmt.Sprintf("browser runner produced no captures after %s", time.Since(start).Round(time.Millisecond)))
		report.PlaywrightAvailable = true
	}
	return report
}

func readBrowserResult(path string) BrowserReport {
	raw, err := os.ReadFile(path)
	if err != nil {
		return BrowserReport{}
	}
	var report BrowserReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return BrowserReport{}
	}
	return report
}

func skippedBrowserReport(report BrowserReport, urls []string, reason string) BrowserReport {
	report.Skipped = len(urls)
	report.PlaywrightAvailable = false
	for _, url := range urls {
		report.Captures = append(report.Captures, BrowserCapture{
			URL:     url,
			Passed:  true,
			Skipped: true,
			Reason:  reason,
		})
	}
	return report
}

func failedBrowserReport(report BrowserReport, urls []string, reason string) BrowserReport {
	report.Failed = len(urls)
	for _, url := range urls {
		report.Captures = append(report.Captures, BrowserCapture{
			URL:    url,
			Passed: false,
			Error:  redactMultiline(reason),
		})
	}
	return report
}

func redactBrowserDOM(outDir string, captures []BrowserCapture) {
	for _, capture := range captures {
		if capture.DOMPath == "" {
			continue
		}
		path := filepath.Join(outDir, filepath.FromSlash(capture.DOMPath))
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		redacted := redactMultiline(string(raw))
		_ = os.WriteFile(path, []byte(redacted), 0o644)
	}
}

const playwrightRunnerScript = `
const fs = require("fs");
const path = require("path");

async function main() {
  const cfg = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
  const result = {
    mode: cfg.mode || "headless",
    total: cfg.urls.length,
    captured: 0,
    skipped: 0,
    failed: 0,
    playwrightAvailable: true,
    captures: []
  };
  let chromium;
  try {
    const playwrightPath = require.resolve("playwright", { paths: [cfg.repoDir, process.cwd()] });
    chromium = require(playwrightPath).chromium;
  } catch (err) {
    result.playwrightAvailable = false;
    result.skipped = cfg.urls.length;
    result.captures = cfg.urls.map((url) => ({
      url,
      passed: true,
      skipped: true,
      reason: "playwright package not found"
    }));
    writeResult(cfg, result);
    return;
  }

  const browser = await chromium.launch({ headless: cfg.mode !== "visible" });
  for (let i = 0; i < cfg.urls.length; i++) {
    const url = cfg.urls[i];
    const index = String(i + 1).padStart(2, "0");
    const screenshotName = "capture-" + index + ".png";
    const domName = "capture-" + index + ".html";
    const harName = "capture-" + index + ".har";
    const consoleName = "capture-" + index + ".console.json";
    const screenshotPath = path.join(cfg.outDir, screenshotName);
    const domPath = path.join(cfg.outDir, domName);
    const harPath = path.join(cfg.outDir, harName);
    const consolePath = path.join(cfg.outDir, consoleName);
    const rel = (name) => cfg.artifactPrefix + "/" + name;
    const started = Date.now();
    const consoleEntries = [];
    const capture = { url, passed: false };
    let context;
    try {
      context = await browser.newContext({ recordHar: { path: harPath, content: "omit" } });
      const page = await context.newPage();
      page.on("console", (msg) => {
        consoleEntries.push({ type: msg.type(), text: msg.text(), location: msg.location() });
      });
      const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: cfg.timeoutMillis });
      capture.statusCode = response ? response.status() : 0;
      await page.screenshot({ path: screenshotPath, fullPage: true });
      let html = await page.content();
      if (html.length > cfg.maxOutputBytes) {
        html = html.slice(0, cfg.maxOutputBytes);
        capture.warnings = ["dom artifact truncated"];
      }
      fs.writeFileSync(domPath, html, "utf8");
      fs.writeFileSync(consolePath, JSON.stringify(consoleEntries, null, 2) + "\n", "utf8");
      capture.durationMillis = Date.now() - started;
      capture.screenshotPath = rel(screenshotName);
      capture.domPath = rel(domName);
      capture.harPath = rel(harName);
      capture.consolePath = rel(consoleName);
      capture.passed = capture.statusCode >= 200 && capture.statusCode < 400;
      if (!capture.passed) {
        capture.error = "browser response status " + capture.statusCode;
        result.failed++;
      } else {
        result.captured++;
      }
    } catch (err) {
      capture.durationMillis = Date.now() - started;
      capture.error = String(err && err.message ? err.message : err);
      result.failed++;
    } finally {
      if (context) {
        await context.close().catch(() => {});
      }
    }
    result.captures.push(capture);
  }
  await browser.close();
  writeResult(cfg, result);
}

function writeResult(cfg, result) {
  fs.writeFileSync(path.join(cfg.outDir, "result.json"), JSON.stringify(result, null, 2) + "\n", "utf8");
}

main().catch((err) => {
  const cfg = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
  const result = {
    mode: cfg.mode || "headless",
    total: cfg.urls.length,
    captured: 0,
    skipped: 0,
    failed: cfg.urls.length,
    playwrightAvailable: true,
    captures: cfg.urls.map((url) => ({ url, passed: false, error: String(err && err.message ? err.message : err) }))
  };
  writeResult(cfg, result);
  process.exitCode = 1;
});
`
