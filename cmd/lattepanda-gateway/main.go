package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"bluei.kr/edge/internal/gatewayagent"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "check-config":
		checkConfig(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lattepanda-gateway <command> [flags]")
	fmt.Fprintln(os.Stderr, "commands: run, check-config")
}

func checkConfig(args []string) {
	fs := flag.NewFlagSet("check-config", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/lattepanda-gateway.example.yaml", "path to gateway config")
	fs.Parse(args)
	if _, err := gatewayagent.LoadConfig(*cfgPath); err != nil {
		slog.Error("config validation failed", "error", err)
		os.Exit(1)
	}
	fmt.Println("gateway config OK")
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/lattepanda-gateway.example.yaml", "path to gateway config")
	fs.Parse(args)
	cfg, err := gatewayagent.LoadConfig(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := gatewayagent.New(*cfg).Run(ctx); err != nil && err != context.Canceled {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}
