package main

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/akonwi/kit/internal/cli"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	var received atomic.Int32
	go func() {
		sig := <-signals
		if systemSignal, ok := sig.(syscall.Signal); ok {
			received.Store(int32(systemSignal))
		}
		cancel()
	}()

	exitCode := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if exitCode == 130 && received.Load() != 0 {
		exitCode = 128 + int(received.Load())
	}
	os.Exit(exitCode)
}
