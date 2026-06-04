package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/hyunseok/smart-k6/internal/process"
)

type Options struct {
	ScriptPath  string
	SummaryPath string
	Env         map[string]string
}

func Run(ctx context.Context, opts Options) error {
	if opts.ScriptPath == "" {
		return fmt.Errorf("script path is required")
	}
	if opts.SummaryPath == "" {
		opts.SummaryPath = "k6-summary.json"
	}
	if _, err := exec.LookPath("k6"); err != nil {
		return fmt.Errorf("k6 executable not found; install k6 and retry: %w", err)
	}

	cmd := process.CommandContext(ctx, "k6", "run", "--summary-export", opts.SummaryPath, opts.ScriptPath)
	if len(opts.Env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range opts.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("capture k6 stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("capture k6 stderr: %w", err)
	}
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start k6: %w", err)
	}
	go stream("k6", stdout)
	go stream("k6", stderr)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("k6 run failed: %w", err)
	}
	return nil
}

func stream(prefix string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Printf("[%s] %s\n", prefix, scanner.Text())
	}
}
