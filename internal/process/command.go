package process

import (
	"context"
	"os/exec"
	"time"
)

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		return terminateProcessGroup(cmd)
	}
	cmd.WaitDelay = 3 * time.Second
	return cmd
}
