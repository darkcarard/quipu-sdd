// Command sdd is the Spec-Driven Development framework CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/darkcarard/quipu-sdd/internal/cli"
)

func main() {
	// Cancel on Ctrl+C / SIGTERM so long-running phases can checkpoint and exit cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdd:", err)
		os.Exit(cli.ExitError)
	}
	app := &cli.App{Dir: dir, Stdout: os.Stdout, Stderr: os.Stderr}
	code := app.Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}
