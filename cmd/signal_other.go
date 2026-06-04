//go:build !unix

package cmd

import (
	"context"
	"os"
	"os/signal"
)

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			cancel()
			closeStdinIfInteractive()

			<-signals
			signal.Stop(signals)
			os.Exit(130)
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
