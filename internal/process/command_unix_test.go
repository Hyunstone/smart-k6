//go:build unix

package process

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestCommandContextTerminatesProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("get process group: %v", err)
	}

	cancel()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatal("command did not exit after context cancellation")
	case err := <-done:
		if err == nil {
			t.Fatal("expected canceled command to return an error")
		}
	}

	if err := syscall.Kill(-pgid, 0); err == nil || !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group still exists after cancellation: %v", err)
	}
}

func TestCommandContextForceKillsProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "sh", "-c", "trap '' TERM; sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("get process group: %v", err)
	}

	cancel()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(6 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatal("command did not exit after forced cancellation")
	case err := <-done:
		if err == nil {
			t.Fatal("expected canceled command to return an error")
		}
	}

	if err := syscall.Kill(-pgid, 0); err == nil || !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group still exists after forced cancellation: %v", err)
	}
}
