package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyunseok/smart-k6/internal/ai"
	"github.com/hyunseok/smart-k6/internal/evidence"
	"github.com/hyunseok/smart-k6/internal/k6"
	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/report"
	"github.com/hyunseok/smart-k6/internal/runner"
	"github.com/hyunseok/smart-k6/internal/scenario"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	spec            string
	tps             int
	scale           string
	prompt          string
	output          string
	outputDir       string
	baseURL         string
	duration        string
	runK6           bool
	yes             bool
	summary         string
	report          string
	openReport      bool
	model           string
	aiProvider      string
	aiTimeout       time.Duration
	aiConfirmed     bool
	allowUnsafe     bool
	includeCommands bool
	clean           bool
	fromTests       string

	authToken         string
	authTokenFile     string
	authLoginPath     string
	authUsername      string
	authPassword      string
	authUsernameField string
	authPasswordField string
	authTokenJSONPath string
}

// Execute runs the sk6 command line interface.
func Execute() {
	ctx, stop := signalContext(context.Background())
	defer stop()

	if err := newRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := rootOptions{}

	rootCmd := &cobra.Command{
		Use:           "sk6 [spec] [scenario]",
		Short:         "Lightweight AI-powered k6 wrapper for Backend Testing",
		Args:          cobra.ArbitraryArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveInputs(&opts, args, os.Stdin, os.Stdout); err != nil {
				return err
			}
			prepareOutputPaths(cmd, &opts)
			return run(cmd.Context(), opts, shouldPromptForMode(cmd, opts))
		},
	}

	rootCmd.Flags().StringVarP(&opts.spec, "spec", "s", "", "Swagger/OpenAPI URL or file path")
	rootCmd.Flags().IntVarP(&opts.tps, "tps", "t", 1, "Target TPS")
	rootCmd.Flags().StringVarP(&opts.scale, "scale", "c", "1M", "DB seed data scale (e.g. 1M, 10M, 100M)")
	rootCmd.Flags().StringVarP(&opts.prompt, "prompt", "p", "", "Natural language test scenario")
	rootCmd.Flags().StringVarP(&opts.output, "output", "o", "", "Generated k6 script path; defaults to <output-dir>/<timestamp>/generated_script.js")
	rootCmd.Flags().StringVar(&opts.outputDir, "output-dir", "sk6-results", "Directory for generated run artifacts when output paths are not specified")
	rootCmd.Flags().StringVar(&opts.baseURL, "base-url", "", "Override API base URL used by generated k6 script")
	rootCmd.Flags().StringVar(&opts.duration, "duration", "1m", "k6 constant-arrival-rate duration")
	rootCmd.Flags().BoolVar(&opts.runK6, "run", false, "Run k6 after generating the script")
	rootCmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive mode selection and use flags/defaults")
	rootCmd.Flags().StringVar(&opts.summary, "summary", "", "k6 JSON summary path; defaults to <output-dir>/<timestamp>/k6-summary.json")
	rootCmd.Flags().StringVar(&opts.report, "report", "", "HTML report path; defaults to <output-dir>/<timestamp>/report.html")
	rootCmd.Flags().BoolVar(&opts.openReport, "open-report", false, "Open HTML report in the default browser after k6 finishes")
	rootCmd.Flags().StringVar(&opts.model, "model", "", "AI model override; Codex ChatGPT accounts usually require leaving this empty")
	rootCmd.Flags().StringVar(&opts.aiProvider, "ai-provider", "codex", "AI provider for scenario mapping: codex, openai-api, or auto")
	rootCmd.Flags().DurationVar(&opts.aiTimeout, "ai-timeout", 60*time.Second, "AI scenario mapping timeout")
	rootCmd.Flags().BoolVar(&opts.allowUnsafe, "allow-unsafe", false, "Allow static mode to call non-GET or auth-required operations from the spec")
	rootCmd.Flags().BoolVar(&opts.includeCommands, "include-commands", false, "Include static POST/PUT/PATCH command operations; DELETE still requires --allow-unsafe")
	rootCmd.Flags().BoolVar(&opts.clean, "clean", false, "Remove generated script, k6 summary, and HTML report after the command finishes")
	rootCmd.Flags().StringVar(&opts.fromTests, "from-tests", "", "Read JSON test evidence and synthesize a precise scenario")
	rootCmd.Flags().StringVar(&opts.authToken, "auth-token", "", "Bearer token for auth-required operations; passed to k6 as AUTH_TOKEN")
	rootCmd.Flags().StringVar(&opts.authTokenFile, "auth-token-file", "", "Read bearer token from a local file and pass it to k6 as AUTH_TOKEN")
	rootCmd.Flags().StringVar(&opts.authLoginPath, "auth-login-path", "", "Login endpoint path or URL used to fetch a bearer token before running k6")
	rootCmd.Flags().StringVar(&opts.authUsername, "auth-username", "", "Test account username/email for --auth-login-path")
	rootCmd.Flags().StringVar(&opts.authPassword, "auth-password", "", "Test account password for --auth-login-path")
	rootCmd.Flags().StringVar(&opts.authUsernameField, "auth-username-field", "email", "JSON field name for the login username/email")
	rootCmd.Flags().StringVar(&opts.authPasswordField, "auth-password-field", "password", "JSON field name for the login password")
	rootCmd.Flags().StringVar(&opts.authTokenJSONPath, "auth-token-json-path", "accessToken", "Dot path to the token in the login JSON response")

	return rootCmd
}

func prepareOutputPaths(cmd *cobra.Command, opts *rootOptions) {
	runDir := filepath.Join(opts.outputDir, time.Now().Format("20060102-150405"))
	if !cmd.Flags().Changed("output") || strings.TrimSpace(opts.output) == "" {
		opts.output = filepath.Join(runDir, "generated_script.js")
	}
	if !cmd.Flags().Changed("summary") || strings.TrimSpace(opts.summary) == "" {
		opts.summary = filepath.Join(runDir, "k6-summary.json")
	}
	if !cmd.Flags().Changed("report") || strings.TrimSpace(opts.report) == "" {
		opts.report = filepath.Join(runDir, "report.html")
	}
}

func resolveInputs(opts *rootOptions, args []string, in io.Reader, out io.Writer) error {
	if opts.spec == "" && len(args) > 0 {
		opts.spec = args[0]
	}
	if opts.prompt == "" && len(args) > 1 {
		opts.prompt = strings.Join(args[1:], " ")
	}
	if opts.spec != "" {
		return nil
	}

	reader := bufio.NewReader(in)
	spec, err := ask(reader, out, "Swagger/OpenAPI path or URL")
	if err != nil {
		return err
	}
	if spec == "" {
		return fmt.Errorf("spec is required")
	}
	opts.spec = spec
	return nil
}

func ask(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	if _, err := fmt.Fprintf(out, "%s: ", label); err != nil {
		return "", err
	}
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func shouldPromptForMode(cmd *cobra.Command, opts rootOptions) bool {
	if opts.yes || opts.prompt != "" || opts.allowUnsafe || opts.fromTests != "" {
		return false
	}
	return true
}

func run(ctx context.Context, opts rootOptions, promptForMode bool) error {
	fmt.Printf("sk6: parsing spec=%s target_tps=%d scale=%s\n", opts.spec, opts.tps, opts.scale)

	summary, err := openapi.Parse(opts.spec)
	if err != nil {
		return err
	}
	if len(summary.Operations) == 0 {
		return fmt.Errorf("no HTTP operations found in %s", opts.spec)
	}

	if opts.baseURL != "" {
		summary.BaseURL = opts.baseURL
	}

	if err := maybeSelectMode(summary, &opts, promptForMode, os.Stdin, os.Stdout); err != nil {
		return err
	}
	if opts.baseURL != "" {
		summary.BaseURL = opts.baseURL
	}
	if err := maybeConfigureAuth(summary, &opts, os.Stdin, os.Stdout); err != nil {
		return err
	}
	annotatePromptForConfiguredAuth(&opts)
	if err := maybeConfirmAIUse(summary, &opts, os.Stdin, os.Stdout); err != nil {
		return err
	}

	plan, err := buildScenario(ctx, summary, opts)
	if err != nil {
		return err
	}
	if err := validateScenario(plan, summary.Operations); err != nil {
		return err
	}

	script, err := k6.Render(k6.ScriptData{
		SpecTitle:  summary.Title,
		BaseURL:    summary.BaseURL,
		TPS:        opts.tps,
		Scale:      opts.scale,
		Duration:   opts.duration,
		Operations: summary.Operations,
		Scenario:   plan,
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(opts.output), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(opts.output, []byte(script), 0644); err != nil {
		return fmt.Errorf("write k6 script: %w", err)
	}
	if opts.clean {
		defer cleanupFiles(opts.output)
	}

	fmt.Printf("sk6: wrote %s with %d operations\n", opts.output, len(summary.Operations))
	if !opts.runK6 {
		return nil
	}
	if opts.clean {
		defer cleanupFiles(opts.summary, opts.report)
	}

	if err := os.MkdirAll(filepath.Dir(opts.summary), 0755); err != nil {
		return fmt.Errorf("create summary directory: %w", err)
	}
	authToken, err := resolveAuthToken(ctx, opts, summary.BaseURL)
	if err != nil {
		return err
	}
	env := map[string]string{}
	if authToken != "" {
		env["AUTH_TOKEN"] = authToken
		fmt.Println("sk6: using AUTH_TOKEN for auth-required operations")
	}
	if err := runner.Run(ctx, runner.Options{
		ScriptPath:  opts.output,
		SummaryPath: opts.summary,
		Env:         env,
	}); err != nil {
		return err
	}

	reportSummary, err := report.Generate(report.Options{
		SummaryPath:         opts.summary,
		ReportPath:          opts.report,
		Open:                opts.openReport,
		Spec:                opts.spec,
		BaseURL:             summary.BaseURL,
		TPS:                 opts.tps,
		Duration:            opts.duration,
		Scale:               opts.scale,
		OperationCount:      len(summary.Operations),
		ScenarioStepCount:   len(plan.Steps),
		AllowUnsafeStatic:   opts.allowUnsafe,
		ScenarioWasAIGuided: opts.prompt != "",
	})
	if err != nil {
		return err
	}
	fmt.Printf("sk6: report=%s success_rate=%.2f avg_ms=%.1f p95_ms=%.1f p99_ms=%.1f iteration_tps=%.1f request_rate=%.1f\n",
		opts.report,
		reportSummary.SuccessRate,
		reportSummary.AvgMS,
		reportSummary.P95MS,
		reportSummary.P99MS,
		reportSummary.IterationRate,
		reportSummary.TPS,
	)
	return nil
}

func maybeConfigureAuth(summary openapi.SpecSummary, opts *rootOptions, in io.Reader, out io.Writer) error {
	if !opts.runK6 || opts.yes || !isInteractiveInput(in) || countAuthRequiredOperations(summary.Operations) == 0 || hasAuthInput(*opts) || os.Getenv("AUTH_TOKEN") != "" {
		return nil
	}
	return configureAuth(summary, opts, bufio.NewReader(in), out)
}

func configureAuth(summary openapi.SpecSummary, opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	if _, err := fmt.Fprintf(out, "\nAuth-required operations detected: %d\n", countAuthRequiredOperations(summary.Operations)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Choose auth setup for this run:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  1) Continue with existing AUTH_TOKEN or no auth"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  2) Paste bearer token"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  3) Read bearer token from file"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  4) Login with test account before running"); err != nil {
		return err
	}

	choice, err := ask(reader, out, "Auth selection [1]")
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "1":
		clearAuthOptions(opts)
		return nil
	case "2":
		token, err := ask(reader, out, "Bearer token")
		if err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("bearer token is required")
		}
		clearAuthOptions(opts)
		opts.authToken = token
	case "3":
		path, err := ask(reader, out, "Token file")
		if err != nil {
			return err
		}
		if path == "" {
			return fmt.Errorf("token file is required")
		}
		clearAuthOptions(opts)
		opts.authTokenFile = path
	case "4":
		loginPath, err := ask(reader, out, "Login path or URL")
		if err != nil {
			return err
		}
		if loginPath == "" {
			return fmt.Errorf("login path or URL is required")
		}
		username, err := ask(reader, out, "Test account email/username")
		if err != nil {
			return err
		}
		if username == "" {
			return fmt.Errorf("test account email/username is required")
		}
		password, err := ask(reader, out, "Test account password")
		if err != nil {
			return err
		}
		if password == "" {
			return fmt.Errorf("test account password is required")
		}
		tokenPath, err := ask(reader, out, "Token JSON path [accessToken]")
		if err != nil {
			return err
		}
		clearAuthOptions(opts)
		opts.authLoginPath = loginPath
		opts.authUsername = username
		opts.authPassword = password
		if tokenPath != "" {
			opts.authTokenJSONPath = tokenPath
		}
	default:
		return fmt.Errorf("invalid auth selection %q", choice)
	}
	return nil
}

func clearAuthOptions(opts *rootOptions) {
	opts.authToken = ""
	opts.authTokenFile = ""
	opts.authLoginPath = ""
	opts.authUsername = ""
	opts.authPassword = ""
}

func hasAuthInput(opts rootOptions) bool {
	return strings.TrimSpace(opts.authToken) != "" ||
		strings.TrimSpace(opts.authTokenFile) != "" ||
		strings.TrimSpace(opts.authLoginPath) != ""
}

func annotatePromptForConfiguredAuth(opts *rootOptions) {
	if opts.prompt == "" || !hasRuntimeAuthConfigured(*opts) {
		return
	}
	const instruction = "Authentication is already configured for the generated k6 run via AUTH_TOKEN. Do not add signup or login steps unless the user explicitly requested them."
	if strings.Contains(opts.prompt, instruction) {
		return
	}
	opts.prompt = instruction + "\n\nUser scenario: " + opts.prompt
}

func hasRuntimeAuthConfigured(opts rootOptions) bool {
	return hasAuthInput(opts) || os.Getenv("AUTH_TOKEN") != ""
}

func resolveAuthToken(ctx context.Context, opts rootOptions, baseURL string) (string, error) {
	if token := strings.TrimSpace(opts.authToken); token != "" {
		return token, nil
	}
	if strings.TrimSpace(opts.authTokenFile) != "" {
		data, err := os.ReadFile(opts.authTokenFile)
		if err != nil {
			return "", fmt.Errorf("read auth token file: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("auth token file is empty")
		}
		return token, nil
	}
	if strings.TrimSpace(opts.authLoginPath) != "" {
		return loginForAuthToken(ctx, opts, baseURL)
	}
	return "", nil
}

func loginForAuthToken(ctx context.Context, opts rootOptions, baseURL string) (string, error) {
	if strings.TrimSpace(opts.authUsername) == "" {
		return "", fmt.Errorf("--auth-username is required with --auth-login-path")
	}
	if strings.TrimSpace(opts.authPassword) == "" {
		return "", fmt.Errorf("--auth-password is required with --auth-login-path")
	}
	loginURL, err := resolveLoginURL(baseURL, opts.authLoginPath)
	if err != nil {
		return "", err
	}
	body := map[string]string{
		displayString(opts.authUsernameField, "email"):    opts.authUsername,
		displayString(opts.authPasswordField, "password"): opts.authPassword,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode login request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("login request failed with status %s", resp.Status)
	}
	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	token, ok := stringAtJSONPath(response, displayString(opts.authTokenJSONPath, "accessToken"))
	if !ok || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("token path %q not found in login response", displayString(opts.authTokenJSONPath, "accessToken"))
	}
	return strings.TrimSpace(token), nil
}

func resolveLoginURL(baseURL, loginPath string) (string, error) {
	loginPath = strings.TrimSpace(loginPath)
	parsed, err := url.Parse(loginPath)
	if err != nil {
		return "", fmt.Errorf("invalid login URL/path: %w", err)
	}
	if parsed.IsAbs() {
		return loginPath, nil
	}
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("--base-url or spec server URL is required when --auth-login-path is relative")
	}
	base, err := url.Parse(baseURL)
	if err != nil || !base.IsAbs() {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(loginPath, "/"), nil
}

func stringAtJSONPath(value map[string]any, path string) (string, bool) {
	current := any(value)
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	token, ok := current.(string)
	return token, ok
}

func countAuthRequiredOperations(operations []openapi.OperationSummary) int {
	count := 0
	for _, operation := range operations {
		if operation.RequiresAuth {
			count++
		}
	}
	return count
}

func maybeSelectMode(summary openapi.SpecSummary, opts *rootOptions, promptForMode bool, in io.Reader, out io.Writer) error {
	if !promptForMode || !isInteractiveInput(in) {
		return nil
	}
	return selectMode(summary, opts, in, out)
}

func selectMode(summary openapi.SpecSummary, opts *rootOptions, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		if err := renderModeMenu(summary, opts, out); err != nil {
			return err
		}

		defaultSelection := "1"
		choice, err := ask(reader, out, "Selection ["+defaultSelection+"]")
		if err != nil {
			return err
		}
		if strings.TrimSpace(choice) == "" {
			choice = defaultSelection
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			opts.includeCommands = false
			return askRunAfterGenerating(opts, reader, out)
		case "2":
			path, err := ask(reader, out, "Test evidence JSON path")
			if err != nil {
				return err
			}
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("test evidence JSON path is required")
			}
			opts.fromTests = path
			opts.includeCommands = false
			return askRunAfterGenerating(opts, reader, out)
		case "3":
			opts.includeCommands = true
			return askRunAfterGenerating(opts, reader, out)
		case "4":
			prompt, err := ask(reader, out, "AI scenario prompt")
			if err != nil {
				return err
			}
			if prompt == "" {
				return fmt.Errorf("AI scenario prompt is required")
			}
			opts.prompt = prompt
			return askRunAfterGenerating(opts, reader, out)
		case "5":
			confirm, err := ask(reader, out, "This may call POST/PATCH/DELETE/auth APIs. Type allow to continue")
			if err != nil {
				return err
			}
			if confirm != "allow" {
				return fmt.Errorf("unsafe static mode cancelled")
			}
			opts.allowUnsafe = true
			return askRunAfterGenerating(opts, reader, out)
		case "6", "settings", "config":
			if err := configureRunSettings(summary, opts, reader, out); err != nil {
				return err
			}
		case "q", "quit", "cancel":
			return fmt.Errorf("cancelled")
		default:
			return fmt.Errorf("invalid selection %q", choice)
		}
	}
}

func askRunAfterGenerating(opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	defaultRun := opts.runK6
	prompt := "Run k6 after generating? [y/N]"
	if defaultRun {
		prompt = "Run k6 after generating? [Y/n]"
	}
	answer, err := ask(reader, out, prompt)
	if err != nil {
		return err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		opts.runK6 = defaultRun
		return nil
	}
	opts.runK6 = isYes(answer)
	return nil
}

func renderModeMenu(summary openapi.SpecSummary, opts *rootOptions, out io.Writer) error {
	safeCount := countSafeStaticOperations(summary.Operations)
	if _, err := fmt.Fprintf(out, "\nParsed %s\n", displayString(summary.Title, opts.spec)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Base URL: %s\n", displayString(displayString(opts.baseURL, summary.BaseURL), "(not specified)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Operations: %d total, %d safe public GET/HEAD\n", len(summary.Operations), safeCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Settings: tps=%d duration=%s scale=%s\n\n", opts.tps, opts.duration, opts.scale); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Choose what to do:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  1) Safe public read scenario (GET/HEAD only)"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  2) Precise scenario from test evidence JSON"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "     Uses recorded/tested flows with request overrides and exact checks."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  3) Mixed read/command scenario"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "     Includes POST/PUT/PATCH and excludes DELETE; may create or update data."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  4) Enter AI scenario prompt"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  5) Allow unsafe static all operations"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  6) Adjust run settings"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  q) Cancel"); err != nil {
		return err
	}
	return nil
}

func configureRunSettings(summary openapi.SpecSummary, opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	for {
		if err := renderRunSettings(summary, opts, out); err != nil {
			return err
		}
		choice, err := ask(reader, out, "Setting to edit [done]")
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "", "d", "done", "q", "back":
			return nil
		case "1", "tps":
			if err := configureTPS(opts, reader, out); err != nil {
				fmt.Fprintf(out, "%v\n", err)
			}
		case "2", "duration":
			if err := configureDuration(opts, reader, out); err != nil {
				fmt.Fprintf(out, "%v\n", err)
			}
		case "3", "scale":
			if err := configureScale(opts, reader, out); err != nil {
				fmt.Fprintf(out, "%v\n", err)
			}
		case "4", "base-url", "baseurl":
			if err := configureBaseURL(summary, opts, reader, out); err != nil {
				fmt.Fprintf(out, "%v\n", err)
			}
		case "5", "auth":
			if err := configureAuth(summary, opts, reader, out); err != nil {
				fmt.Fprintf(out, "%v\n", err)
			}
		default:
			fmt.Fprintf(out, "invalid setting selection %q\n", choice)
		}
	}
}

func renderRunSettings(summary openapi.SpecSummary, opts *rootOptions, out io.Writer) error {
	if _, err := fmt.Fprintln(out, "\nRun settings:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  1) TPS: %d\n", opts.tps); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  2) Duration: %s\n", opts.duration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  3) Scale: %s\n", opts.scale); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  4) Base URL: %s\n", displayString(displayString(opts.baseURL, summary.BaseURL), "(not specified)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  5) Auth: %s\n", authDisplay(*opts)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  done) Back to main menu"); err != nil {
		return err
	}
	return nil
}

func authDisplay(opts rootOptions) string {
	switch {
	case strings.TrimSpace(opts.authToken) != "":
		return "bearer token provided"
	case strings.TrimSpace(opts.authTokenFile) != "":
		return "token file: " + opts.authTokenFile
	case strings.TrimSpace(opts.authLoginPath) != "":
		user := displayString(opts.authUsername, "(username not set)")
		return "login: " + user + " via " + opts.authLoginPath
	case os.Getenv("AUTH_TOKEN") != "":
		return "AUTH_TOKEN env"
	default:
		return "not configured"
	}
}

func configureTPS(opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	value, err := ask(reader, out, fmt.Sprintf("TPS [%d]", opts.tps))
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("TPS must be a positive integer")
	}
	opts.tps = parsed
	return nil
}

func configureDuration(opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	value, err := ask(reader, out, fmt.Sprintf("Duration [%s]", opts.duration))
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("duration must be a positive Go duration such as 30s, 1m, or 2m30s")
	}
	opts.duration = value
	return nil
}

func configureScale(opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	value, err := ask(reader, out, fmt.Sprintf("Scale [%s]", opts.scale))
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) != "" {
		opts.scale = value
	}
	return nil
}

func configureBaseURL(summary openapi.SpecSummary, opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	currentBaseURL := displayString(opts.baseURL, summary.BaseURL)
	value, err := ask(reader, out, fmt.Sprintf("Base URL [%s]", displayString(currentBaseURL, "(not specified)")))
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) != "" {
		opts.baseURL = value
	}
	return nil
}

func maybeConfirmAIUse(summary openapi.SpecSummary, opts *rootOptions, in io.Reader, out io.Writer) error {
	if opts.prompt == "" || opts.yes || opts.aiConfirmed || !isInteractiveInput(in) {
		return nil
	}
	reader := bufio.NewReader(in)
	return confirmAIUse(summary, opts, reader, out)
}

func confirmAIUse(summary openapi.SpecSummary, opts *rootOptions, reader *bufio.Reader, out io.Writer) error {
	if _, err := fmt.Fprintln(out, "\nAI scenario mapping will send the following to the selected AI provider:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  Provider: %s\n", opts.aiProvider); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  Operation summary count: %d\n", len(summary.Operations)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  User scenario: %s\n", opts.prompt); err != nil {
		return err
	}
	answer, err := ask(reader, out, "Continue with AI mapping? [y/N]")
	if err != nil {
		return err
	}
	if !isYes(answer) {
		return fmt.Errorf("AI scenario mapping cancelled")
	}
	opts.aiConfirmed = true
	return nil
}

func isInteractiveInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func isYes(value string) bool {
	return strings.EqualFold(value, "y") || strings.EqualFold(value, "yes")
}

func displayString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cleanupFiles(paths ...string) {
	seen := map[string]struct{}{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if err := os.Remove(path); err == nil {
			fmt.Printf("sk6: cleaned %s\n", path)
		}
	}
}

func buildScenario(ctx context.Context, summary openapi.SpecSummary, opts rootOptions) (scenario.Plan, error) {
	if strings.TrimSpace(opts.fromTests) != "" {
		file, err := evidence.Load(opts.fromTests)
		if err != nil {
			return scenario.Plan{}, err
		}
		plan, err := evidence.Synthesize(file, summary)
		if err != nil {
			return scenario.Plan{}, err
		}
		fmt.Printf("sk6: synthesized scenario from test evidence %s\n", opts.fromTests)
		return plan, nil
	}
	if opts.prompt == "" {
		return buildStaticScenario(summary, opts)
	}
	if opts.aiTimeout <= 0 {
		return scenario.Plan{}, fmt.Errorf("ai-timeout must be greater than 0")
	}
	ctx, cancel := context.WithTimeout(ctx, opts.aiTimeout)
	defer cancel()

	switch opts.aiProvider {
	case "codex":
		fmt.Printf("sk6: mapping AI scenario with authenticated Codex/OpenAI login (%s)\n", displayString(opts.model, "account default"))
		plan, err := ai.MapWithCodex(ctx, summary, opts.model, opts.prompt)
		if err != nil {
			return scenario.Plan{}, err
		}
		return plan, nil
	case "openai-api":
		return mapWithOpenAIAPI(ctx, summary, opts)
	case "auto":
		if os.Getenv("OPENAI_API_KEY") != "" {
			return mapWithOpenAIAPI(ctx, summary, opts)
		}
		fmt.Printf("sk6: OPENAI_API_KEY is not set; using authenticated Codex/OpenAI login (%s)\n", displayString(opts.model, "account default"))
		plan, err := ai.MapWithCodex(ctx, summary, opts.model, opts.prompt)
		if err != nil {
			return scenario.Plan{}, err
		}
		return plan, nil
	default:
		return scenario.Plan{}, fmt.Errorf("invalid --ai-provider %q: use codex, openai-api, or auto", opts.aiProvider)
	}
}

func buildStaticScenario(summary openapi.SpecSummary, opts rootOptions) (scenario.Plan, error) {
	if opts.allowUnsafe {
		return planFromOperations(summary.Operations), nil
	}

	operations := make([]openapi.OperationSummary, 0, len(summary.Operations))
	for _, operation := range summary.Operations {
		if opts.includeCommands {
			if isStaticCommandMixOperation(operation, hasRuntimeAuthConfigured(opts)) {
				operations = append(operations, operation)
			}
			continue
		}
		if isSafeStaticOperation(operation) {
			operations = append(operations, operation)
		}
	}
	if len(operations) == 0 {
		if opts.includeCommands {
			return scenario.Plan{}, fmt.Errorf("no static read/command operations found; configure auth, provide a scenario prompt, or rerun with --allow-unsafe against disposable data")
		}
		return scenario.Plan{}, fmt.Errorf("no safe unauthenticated GET/HEAD operations found; provide a scenario prompt or rerun with --allow-unsafe against disposable data")
	}
	return planFromOperations(operations), nil
}

func planFromOperations(operations []openapi.OperationSummary) scenario.Plan {
	steps := make([]scenario.Step, 0, len(operations))
	for i, operation := range operations {
		steps = append(steps, scenario.Step{
			Step:             i + 1,
			APIID:            operation.APIID,
			ExtractVariables: map[string]string{},
			UseVariables:     map[string]string{},
			Checks:           operationStatusChecks(operation),
		})
	}
	return scenario.Plan{Steps: steps}
}

func operationStatusChecks(operation openapi.OperationSummary) []scenario.Check {
	for _, status := range operation.ResponseStatuses {
		if status >= 200 && status < 300 {
			return []scenario.Check{{Type: "status", Operator: "eq", Value: status}}
		}
	}
	return nil
}

func countSafeStaticOperations(operations []openapi.OperationSummary) int {
	count := 0
	for _, operation := range operations {
		if isSafeStaticOperation(operation) {
			count++
		}
	}
	return count
}

func isSafeStaticOperation(operation openapi.OperationSummary) bool {
	if operation.RequiresAuth {
		return false
	}
	switch strings.ToUpper(operation.Method) {
	case "GET", "HEAD":
		return true
	default:
		return false
	}
}

func isStaticCommandMixOperation(operation openapi.OperationSummary, includeAuth bool) bool {
	if operation.RequiresAuth && !includeAuth {
		return false
	}
	switch strings.ToUpper(operation.Method) {
	case "GET", "HEAD", "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

func mapWithOpenAIAPI(ctx context.Context, summary openapi.SpecSummary, opts rootOptions) (scenario.Plan, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return scenario.Plan{}, fmt.Errorf("--ai-provider openai-api requires OPENAI_API_KEY")
	}
	fmt.Printf("sk6: mapping AI scenario with OpenAI API (%s)\n", displayString(opts.model, "gpt-4o-mini"))
	plan, err := ai.NewMapper(apiKey, opts.model).Map(ctx, summary, opts.prompt)
	if err != nil {
		return scenario.Plan{}, err
	}
	return plan, nil
}

func validateScenario(plan scenario.Plan, operations []openapi.OperationSummary) error {
	if len(plan.Steps) == 0 {
		return fmt.Errorf("scenario must include at least one step")
	}

	known := map[string]struct{}{}
	for _, operation := range operations {
		known[operation.APIID] = struct{}{}
	}

	seenSteps := map[int]struct{}{}
	for i, step := range plan.Steps {
		if step.Step <= 0 {
			return fmt.Errorf("scenario step %d has invalid step number %d", i+1, step.Step)
		}
		if _, exists := seenSteps[step.Step]; exists {
			return fmt.Errorf("scenario has duplicate step number %d", step.Step)
		}
		seenSteps[step.Step] = struct{}{}
		if _, ok := known[step.APIID]; !ok {
			return fmt.Errorf("scenario step %d references unknown api_id %q", step.Step, step.APIID)
		}
		if step.ExtractVariables == nil {
			plan.Steps[i].ExtractVariables = map[string]string{}
		}
		if step.UseVariables == nil {
			plan.Steps[i].UseVariables = map[string]string{}
		}
		for name := range step.ExtractVariables {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("scenario step %d has empty extract variable name", step.Step)
			}
		}
		for field, variable := range step.UseVariables {
			if strings.TrimSpace(field) == "" || strings.TrimSpace(variable) == "" {
				return fmt.Errorf("scenario step %d has invalid variable binding %q -> %q", step.Step, field, variable)
			}
		}
		for _, check := range step.Checks {
			if err := validateCheck(step.Step, check); err != nil {
				return err
			}
		}
	}

	for expected := 1; expected <= len(plan.Steps); expected++ {
		if _, ok := seenSteps[expected]; !ok {
			return fmt.Errorf("scenario step numbers must be contiguous from 1; missing %d", expected)
		}
	}
	return nil
}

func validateCheck(step int, check scenario.Check) error {
	checkType := strings.TrimSpace(check.Type)
	if checkType == "" {
		return fmt.Errorf("scenario step %d has check with empty type", step)
	}
	switch strings.TrimSpace(check.Operator) {
	case "", "eq", "exists", "matches", "gte", "lte":
	default:
		return fmt.Errorf("scenario step %d has unsupported check operator %q", step, check.Operator)
	}
	switch checkType {
	case "status":
		if check.Value == nil {
			return fmt.Errorf("scenario step %d has status check with empty value", step)
		}
		return nil
	case "json_path", "header":
		if strings.TrimSpace(check.Path) == "" {
			return fmt.Errorf("scenario step %d has %s check with empty path", step, checkType)
		}
		return nil
	case "body_contains":
		if check.Value == nil {
			return fmt.Errorf("scenario step %d has body_contains check with empty value", step)
		}
		return nil
	default:
		return fmt.Errorf("scenario step %d has unsupported check type %q", step, check.Type)
	}
}

func apiIDs(operations []openapi.OperationSummary) []string {
	ids := make([]string, 0, len(operations))
	for _, operation := range operations {
		ids = append(ids, operation.APIID)
	}
	return ids
}
