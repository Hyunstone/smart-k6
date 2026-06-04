//go:build unix

package process

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return err
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	time.Sleep(2 * time.Second)
	if err := syscall.Kill(-pgid, 0); err == nil {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
	}
	return nil
}
