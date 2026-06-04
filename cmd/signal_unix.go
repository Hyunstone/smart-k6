//go:build unix

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		select {
		case <-ctx.Done():
			return
		case sig := <-signals:
			cancel()
			closeStdinIfInteractive()

			sig = <-signals
			signal.Stop(signals)
			exitForSignal(sig)
		}
	}()

	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}

func closeStdinIfInteractive() {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return
	}
	_ = os.Stdin.Close()
}

func exitForSignal(sig os.Signal) {
	if syscallSignal, ok := sig.(syscall.Signal); ok {
		os.Exit(128 + int(syscallSignal))
	}
	os.Exit(1)
}
