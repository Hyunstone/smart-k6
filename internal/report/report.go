package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/browser"
)

type Options struct {
	SummaryPath         string
	ReportPath          string
	Open                bool
	Spec                string
	BaseURL             string
	TPS                 int
	Duration            string
	Scale               string
	OperationCount      int
	ScenarioStepCount   int
	AllowUnsafeStatic   bool
	ScenarioWasAIGuided bool
}

type Summary struct {
	SuccessRate       float64
	HTTPFailureRate   float64
	AvgMS             float64
	MedianMS          float64
	P90MS             float64
	P95MS             float64
	P99MS             float64
	MaxMS             float64
	TPS               float64
	IterationRate     float64
	RequestsPerIter   float64
	Requests          float64
	Iterations        float64
	DroppedIterations float64
	VUsMax            float64
	CheckPasses       float64
	CheckFails        float64
	DataSentBytes     float64
	DataReceivedBytes float64
	Checks            []CheckSummary
	Config            ReportConfig
	Status            string
	StatusClass       string
	Interpretation    string
	MaxLatency        float64
}

type rawSummary struct {
	RootGroup rawGroup             `json:"root_group"`
	Metrics   map[string]rawMetric `json:"metrics"`
}

type rawGroup struct {
	Checks map[string]rawCheck `json:"checks"`
}

type rawCheck struct {
	Name   string  `json:"name"`
	Passes float64 `json:"passes"`
	Fails  float64 `json:"fails"`
}

type rawMetric struct {
	Count  float64            `json:"count"`
	Rate   float64            `json:"rate"`
	Value  float64            `json:"value"`
	Passes float64            `json:"passes"`
	Fails  float64            `json:"fails"`
	Avg    float64            `json:"avg"`
	Min    float64            `json:"min"`
	Med    float64            `json:"med"`
	Max    float64            `json:"max"`
	P90    float64            `json:"p(90)"`
	P95    float64            `json:"p(95)"`
	P99    float64            `json:"p(99)"`
	Values map[string]float64 `json:"values"`
}

type CheckSummary struct {
	Name     string
	Passes   float64
	Fails    float64
	PassRate float64
}

type ReportConfig struct {
	Spec              string
	BaseURL           string
	TPS               int
	Duration          string
	Scale             string
	OperationCount    int
	ScenarioStepCount int
	Mode              string
}

func Generate(opts Options) (Summary, error) {
	if opts.SummaryPath == "" {
		opts.SummaryPath = "k6-summary.json"
	}
	if opts.ReportPath == "" {
		opts.ReportPath = "report.html"
	}

	data, err := os.ReadFile(opts.SummaryPath)
	if err != nil {
		return Summary{}, fmt.Errorf("read k6 summary: %w", err)
	}
	var raw rawSummary
	if err := json.Unmarshal(data, &raw); err != nil {
		return Summary{}, fmt.Errorf("parse k6 summary: %w", err)
	}

	summary := extract(raw)
	summary.Config = ReportConfig{
		Spec:              opts.Spec,
		BaseURL:           opts.BaseURL,
		TPS:               opts.TPS,
		Duration:          opts.Duration,
		Scale:             opts.Scale,
		OperationCount:    opts.OperationCount,
		ScenarioStepCount: opts.ScenarioStepCount,
		Mode:              modeLabel(opts),
	}
	summary.Status, summary.StatusClass, summary.Interpretation = verdict(summary)
	html, err := render(summary)
	if err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(filepath.Dir(opts.ReportPath), 0755); err != nil {
		return Summary{}, fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(opts.ReportPath, []byte(html), 0644); err != nil {
		return Summary{}, fmt.Errorf("write report: %w", err)
	}
	if opts.Open {
		abs, err := filepath.Abs(opts.ReportPath)
		if err != nil {
			return Summary{}, fmt.Errorf("resolve report path: %w", err)
		}
		if err := browser.OpenFile(abs); err != nil {
			return Summary{}, fmt.Errorf("open report: %w", err)
		}
	}
	return summary, nil
}

func extract(raw rawSummary) Summary {
	duration := raw.Metrics["http_req_duration"]
	checks := raw.Metrics["checks"]
	requests := raw.Metrics["http_reqs"]
	iterations := raw.Metrics["iterations"]
	summary := Summary{
		SuccessRate:       successRate(checks),
		HTTPFailureRate:   raw.Metrics["http_req_failed"].Value,
		AvgMS:             metricValue(duration, "avg"),
		MedianMS:          metricValue(duration, "med"),
		P90MS:             metricValue(duration, "p(90)"),
		P95MS:             metricValue(duration, "p(95)"),
		P99MS:             metricValue(duration, "p(99)"),
		MaxMS:             metricValue(duration, "max"),
		TPS:               requests.Rate,
		IterationRate:     iterations.Rate,
		Requests:          firstNonZero(requests.Count, requests.Value),
		Iterations:        firstNonZero(iterations.Count, iterations.Value),
		DroppedIterations: firstNonZero(raw.Metrics["dropped_iterations"].Count, raw.Metrics["dropped_iterations"].Value),
		VUsMax:            firstNonZero(raw.Metrics["vus_max"].Max, raw.Metrics["vus_max"].Value),
		CheckPasses:       checks.Passes,
		CheckFails:        checks.Fails,
		DataSentBytes:     firstNonZero(raw.Metrics["data_sent"].Count, raw.Metrics["data_sent"].Value),
		DataReceivedBytes: firstNonZero(raw.Metrics["data_received"].Count, raw.Metrics["data_received"].Value),
	}
	if summary.Iterations > 0 {
		summary.RequestsPerIter = summary.Requests / summary.Iterations
	}
	summary.MaxLatency = math.Max(math.Max(summary.P95MS, summary.P99MS), math.Max(summary.AvgMS, summary.MaxMS))
	summary.Checks = extractChecks(raw.RootGroup.Checks)
	return summary
}

func metricValue(metric rawMetric, key string) float64 {
	if metric.Values != nil {
		if value, ok := metric.Values[key]; ok {
			return value
		}
	}
	switch key {
	case "avg":
		return metric.Avg
	case "min":
		return metric.Min
	case "med":
		return metric.Med
	case "max":
		return metric.Max
	case "p(90)":
		return metric.P90
	case "p(95)":
		return metric.P95
	case "p(99)":
		return metric.P99
	default:
		return 0
	}
}

func successRate(checks rawMetric) float64 {
	if checks.Passes+checks.Fails > 0 {
		return checks.Passes / (checks.Passes + checks.Fails)
	}
	return firstNonZero(checks.Rate, checks.Value)
}

func extractChecks(checks map[string]rawCheck) []CheckSummary {
	rows := make([]CheckSummary, 0, len(checks))
	for key, check := range checks {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			name = key
		}
		total := check.Passes + check.Fails
		passRate := 0.0
		if total > 0 {
			passRate = check.Passes / total
		}
		rows = append(rows, CheckSummary{
			Name:     name,
			Passes:   check.Passes,
			Fails:    check.Fails,
			PassRate: passRate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Fails == rows[j].Fails {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Fails > rows[j].Fails
	})
	if len(rows) > 15 {
		return rows[:15]
	}
	return rows
}

func modeLabel(opts Options) string {
	switch {
	case opts.ScenarioWasAIGuided:
		return "AI scenario"
	case opts.AllowUnsafeStatic:
		return "Static all operations (--allow-unsafe)"
	default:
		return "Static safe mode (public GET/HEAD only)"
	}
}

func verdict(summary Summary) (string, string, string) {
	if summary.CheckFails > 0 {
		return "Needs attention", "bad", "일부 요청이 실패했습니다. 아래 check별 결과에서 실패가 발생한 endpoint를 먼저 확인하세요."
	}
	if summary.DroppedIterations > 0 {
		return "Load target missed", "warn", "k6가 목표 도착률을 유지하지 못했습니다. VU 한도, 백엔드 처리 시간, 네트워크 병목을 확인하세요."
	}
	if summary.SuccessRate >= 0.99 {
		return "Healthy", "good", "sk6 check 기준으로 요청이 안정적으로 통과했습니다. k6 HTTP failure는 기본 응답 분류라 4xx가 포함될 수 있으므로 endpoint check와 함께 해석하세요."
	}
	return "Review", "warn", "전체 성공률이 낮거나 해석할 데이터가 부족합니다. summary JSON과 백엔드 로그를 같이 확인하세요."
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func render(summary Summary) (string, error) {
	funcs := template.FuncMap{
		"pct":        formatPercent,
		"num":        formatNumber,
		"ms":         formatMS,
		"bytes":      formatBytes,
		"bar":        barWidth,
		"latencyBar": func(value float64) int { return barWidth(value, summary.MaxLatency) },
	}
	tpl, err := template.New("report").Funcs(funcs).Parse(reportTemplate)
	if err != nil {
		return "", fmt.Errorf("parse report template: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, summary); err != nil {
		return "", fmt.Errorf("render report template: %w", err)
	}
	return buf.String(), nil
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

func formatNumber(value float64) string {
	if value >= 1000 {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.1f", value)
}

func formatMS(value float64) string {
	return fmt.Sprintf("%.1f ms", value)
}

func formatBytes(value float64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%.0f B", value)
	}
	div := float64(unit)
	exp := 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", value/div, "KMGTPE"[exp])
}

func barWidth(value, max float64) int {
	if max <= 0 || value <= 0 {
		return 0
	}
	width := int(math.Round((value / max) * 100))
	if width < 2 {
		return 2
	}
	if width > 100 {
		return 100
	}
	return width
}

const reportTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>smart-k6 Report</title>
  <style>
    :root { color-scheme: light; --bg: #f6f7f8; --panel: #fff; --line: #d9dee5; --text: #15171a; --muted: #667085; --good: #12805c; --warn: #b76e00; --bad: #b42318; --blue: #2563eb; }
    * { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 0; background: var(--bg); color: var(--text); }
    main { max-width: 1180px; margin: 0 auto; padding: 36px 24px 56px; }
    header { display: flex; justify-content: space-between; gap: 24px; align-items: flex-start; margin-bottom: 24px; }
    h1 { font-size: 34px; line-height: 1.1; margin: 0 0 8px; letter-spacing: 0; }
    h2 { font-size: 18px; margin: 0 0 14px; }
    p { margin: 0; color: var(--muted); line-height: 1.55; }
    .status { min-width: 220px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); padding: 16px; }
    .status strong { display: block; font-size: 22px; margin-top: 4px; }
    .good strong { color: var(--good); } .warn strong { color: var(--warn); } .bad strong { color: var(--bad); }
    .grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin: 18px 0 24px; }
    .metric, .panel { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; padding: 16px; }
    .metric .label { color: var(--muted); font-size: 13px; margin-bottom: 8px; }
    .metric .value { font-size: 28px; font-weight: 750; }
    .metric .hint { color: var(--muted); font-size: 12px; margin-top: 8px; line-height: 1.4; }
    .two { display: grid; grid-template-columns: 1.2fr .8fr; gap: 14px; margin-bottom: 14px; }
    .kv { display: grid; grid-template-columns: 160px 1fr; gap: 8px 14px; font-size: 14px; }
    .kv div:nth-child(odd) { color: var(--muted); }
    .bars { display: grid; gap: 10px; }
    .bar-row { display: grid; grid-template-columns: 70px 1fr 90px; gap: 12px; align-items: center; font-size: 14px; }
    .track { height: 12px; border-radius: 999px; background: #edf1f5; overflow: hidden; }
    .fill { height: 100%; background: var(--blue); border-radius: 999px; min-width: 0; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th, td { padding: 10px 8px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: top; }
    th { color: var(--muted); font-weight: 650; }
    td.num, th.num { text-align: right; white-space: nowrap; }
    .empty { color: var(--muted); font-size: 14px; padding: 8px 0; }
    @media (max-width: 900px) { header, .two { display: block; } .status { margin-top: 14px; } .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
    @media (max-width: 560px) { main { padding: 24px 14px 40px; } .grid { grid-template-columns: 1fr; } .kv { grid-template-columns: 1fr; } .bar-row { grid-template-columns: 60px 1fr; } .bar-row .num { grid-column: 2; } }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>smart-k6 Report</h1>
        <p>{{ .Interpretation }}</p>
      </div>
      <section class="status {{ .StatusClass }}">
        <div class="label">Overall result</div>
        <strong>{{ .Status }}</strong>
      </section>
    </header>

    <section class="grid">
      <div class="metric"><div class="label">Check success</div><div class="value">{{ pct .SuccessRate }}</div><div class="hint">k6 check 통과 비율</div></div>
      <div class="metric"><div class="label">k6 HTTP failure</div><div class="value">{{ pct .HTTPFailureRate }}</div><div class="hint">k6 기본 실패율. 4xx가 포함될 수 있음</div></div>
      <div class="metric"><div class="label">Request rate</div><div class="value">{{ num .TPS }}</div><div class="hint">초당 HTTP 요청 수</div></div>
      <div class="metric"><div class="label">Iteration rate</div><div class="value">{{ num .IterationRate }}</div><div class="hint">k6가 실제 실행한 초당 scenario 횟수</div></div>
      <div class="metric"><div class="label">Requests</div><div class="value">{{ printf "%.0f" .Requests }}</div><div class="hint">총 HTTP 요청 수</div></div>
      <div class="metric"><div class="label">Avg latency</div><div class="value">{{ ms .AvgMS }}</div><div class="hint">평균 응답 시간</div></div>
      <div class="metric"><div class="label">p95 latency</div><div class="value">{{ ms .P95MS }}</div><div class="hint">95% 요청이 이 시간 이하</div></div>
      <div class="metric"><div class="label">p99 latency</div><div class="value">{{ ms .P99MS }}</div><div class="hint">꼬리 지연시간</div></div>
      <div class="metric"><div class="label">Dropped iterations</div><div class="value">{{ printf "%.0f" .DroppedIterations }}</div><div class="hint">목표 TPS를 못 맞춘 횟수</div></div>
    </section>

    <section class="two">
      <div class="panel">
        <h2>Latency Distribution</h2>
        <div class="bars">
          <div class="bar-row"><span>avg</span><div class="track"><div class="fill" style="width: {{ latencyBar .AvgMS }}%"></div></div><span class="num">{{ ms .AvgMS }}</span></div>
          <div class="bar-row"><span>median</span><div class="track"><div class="fill" style="width: {{ latencyBar .MedianMS }}%"></div></div><span class="num">{{ ms .MedianMS }}</span></div>
          <div class="bar-row"><span>p90</span><div class="track"><div class="fill" style="width: {{ latencyBar .P90MS }}%"></div></div><span class="num">{{ ms .P90MS }}</span></div>
          <div class="bar-row"><span>p95</span><div class="track"><div class="fill" style="width: {{ latencyBar .P95MS }}%"></div></div><span class="num">{{ ms .P95MS }}</span></div>
          <div class="bar-row"><span>p99</span><div class="track"><div class="fill" style="width: {{ latencyBar .P99MS }}%"></div></div><span class="num">{{ ms .P99MS }}</span></div>
          <div class="bar-row"><span>max</span><div class="track"><div class="fill" style="width: {{ latencyBar .MaxMS }}%"></div></div><span class="num">{{ ms .MaxMS }}</span></div>
        </div>
      </div>
      <div class="panel">
        <h2>Run Context</h2>
        <div class="kv">
          <div>Mode</div><div>{{ .Config.Mode }}</div>
          <div>Target TPS</div><div>{{ .Config.TPS }}</div>
          <div>Actual iteration TPS</div><div>{{ num .IterationRate }}</div>
          <div>Requests per iteration</div><div>{{ num .RequestsPerIter }}</div>
          <div>Duration</div><div>{{ .Config.Duration }}</div>
          <div>Scale</div><div>{{ .Config.Scale }}</div>
          <div>Operations in spec</div><div>{{ .Config.OperationCount }}</div>
          <div>Scenario steps</div><div>{{ .Config.ScenarioStepCount }}</div>
          <div>VUs max</div><div>{{ printf "%.0f" .VUsMax }}</div>
          <div>Data sent</div><div>{{ bytes .DataSentBytes }}</div>
          <div>Data received</div><div>{{ bytes .DataReceivedBytes }}</div>
          <div>Spec</div><div>{{ .Config.Spec }}</div>
          <div>Base URL</div><div>{{ .Config.BaseURL }}</div>
        </div>
      </div>
    </section>

    <section class="panel">
      <h2>Checks By Endpoint</h2>
      {{ if .Checks }}
      <table>
        <thead><tr><th>Check</th><th class="num">Passes</th><th class="num">Fails</th><th class="num">Pass rate</th></tr></thead>
        <tbody>
          {{ range .Checks }}
          <tr><td>{{ .Name }}</td><td class="num">{{ printf "%.0f" .Passes }}</td><td class="num">{{ printf "%.0f" .Fails }}</td><td class="num">{{ pct .PassRate }}</td></tr>
          {{ end }}
        </tbody>
      </table>
      {{ else }}
      <div class="empty">No per-check data was included in the k6 summary.</div>
      {{ end }}
    </section>
  </main>
</body>
</html>`
