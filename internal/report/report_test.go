package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateExtractsMetricsAndWritesHTML(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.json")
	reportPath := filepath.Join(dir, "reports", "report.html")
	raw := `{
  "root_group": {
    "checks": {
      "GET /health status is 2xx-4xx": {
        "name": "GET /health status is 2xx-4xx",
        "passes": 118,
        "fails": 2
      }
    }
  },
  "metrics": {
    "checks": { "passes": 118, "fails": 2, "value": 0.9833333333 },
    "http_req_failed": { "value": 0.01 },
    "http_req_duration": { "avg": 12.3, "med": 10.1, "p(90)": 30.5, "p(95)": 45.6, "p(99)": 78.9, "max": 100.1 },
    "http_reqs": { "count": 120, "rate": 19.5 },
    "iterations": { "count": 60, "rate": 9.75 },
    "dropped_iterations": { "count": 1 },
    "vus_max": { "value": 10 },
    "data_sent": { "count": 2048 },
    "data_received": { "count": 4096 }
  }
}`
	if err := os.WriteFile(summaryPath, []byte(raw), 0644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	summary, err := Generate(Options{
		SummaryPath:       summaryPath,
		ReportPath:        reportPath,
		Spec:              "openapi.yaml",
		BaseURL:           "https://api.example.com",
		TPS:               20,
		Duration:          "1m",
		Scale:             "10M",
		OperationCount:    30,
		ScenarioStepCount: 3,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if summary.SuccessRate < 0.98 || summary.AvgMS != 12.3 || summary.P95MS != 45.6 || summary.P99MS != 78.9 || summary.TPS != 19.5 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.IterationRate != 9.75 || summary.RequestsPerIter != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.CheckFails != 2 || len(summary.Checks) != 1 {
		t.Fatalf("unexpected checks: %+v", summary)
	}
	if summary.Status != "Needs attention" {
		t.Fatalf("status = %q", summary.Status)
	}

	html, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(html), "smart-k6 Report") {
		t.Fatalf("report missing title:\n%s", html)
	}
	if !strings.Contains(string(html), "Run Context") || !strings.Contains(string(html), "Checks By Endpoint") {
		t.Fatalf("report missing explanation sections:\n%s", html)
	}
	if !strings.Contains(string(html), "Request rate") || !strings.Contains(string(html), "Iteration rate") || !strings.Contains(string(html), "Requests per iteration") {
		t.Fatalf("report missing throughput breakdown:\n%s", html)
	}
}

func TestGenerateKeepsHealthyVerdictWhenOnlyK6HTTPFailureIsPresent(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.json")
	reportPath := filepath.Join(dir, "report.html")
	raw := `{
  "metrics": {
    "checks": { "passes": 100, "fails": 0, "value": 1 },
    "http_req_failed": { "value": 0.44 },
    "http_req_duration": { "avg": 2.3, "med": 1.1, "p(90)": 3.5, "p(95)": 4.6, "p(99)": 7.9, "max": 10.1 },
    "http_reqs": { "count": 100, "rate": 10 }
  }
}`
	if err := os.WriteFile(summaryPath, []byte(raw), 0644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	summary, err := Generate(Options{
		SummaryPath: summaryPath,
		ReportPath:  reportPath,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if summary.Status != "Healthy" {
		t.Fatalf("status = %q, summary = %+v", summary.Status, summary)
	}
}
