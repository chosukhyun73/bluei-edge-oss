// processing-bluei — 가공공장 관리 서비스. GX10 한 대에서 단독으로 돈다.
// bluei-edge(양식)와 저장소·이벤트 기록을 같이 쓰지만, 양식 기능과 장비 제어는 들어 있지 않다.
// References: docs/54-haccp-process-flow-ccp-design.md §19, §25.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/plantapi"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

const defaultConfig = "configs/proce.example.yaml"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "check-config":
		loadConfig(os.Args[2:], "check-config")
		fmt.Println("config OK")
	case "migrate":
		cfg := loadConfig(os.Args[2:], "migrate")
		store := openStore(cfg)
		defer store.Close()
		migrate(store)
		fmt.Println("migration OK")
	case "health":
		healthCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: processing-bluei <command> [flags]")
	fmt.Fprintln(os.Stderr, "commands: run, check-config, migrate, health")
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

func loadConfig(args []string, name string) *config.Config {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfig, "path to config")
	fs.Parse(args)
	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		fatal("config load failed", err)
	}
	if err := config.ValidateProcessing(cfg); err != nil {
		fatal("config validation failed", err)
	}
	return cfg
}

func openStore(cfg *config.Config) storage.Store {
	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		fatal("storage open failed", err)
	}
	return store
}

func migrate(store storage.Store) {
	for _, p := range storage.MigrationPaths {
		if err := storage.Migrate(store, p); err != nil {
			fatal("migration failed: "+p, err)
		}
	}
}

func runCmd(args []string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := loadConfig(args, "run")
	store := openStore(cfg)
	defer store.Close()
	if cfg.Storage.MigrationMode == "auto" {
		migrate(store)
	}

	app := runtime.NewApp(cfg, store)
	srv := plantapi.NewServer(cfg, app, store)
	mgr := runtime.NewManager(srv, plantapi.NewProcessWorker(srv))
	startCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Runtime.StartupTimeoutSec)*time.Second)
	defer cancel()
	if err := mgr.Start(startCtx); err != nil {
		fatal("startup failed", err)
	}
	slog.Info("processing-bluei running", "site_id", cfg.Site.SiteID, "edge_id", cfg.Edge.EdgeID,
		"api", fmt.Sprintf("%s:%d", cfg.API.BindHost, cfg.API.Port))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	mgr.Stop()
	slog.Info("processing-bluei stopped")
}

func healthCmd(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/healthz", "healthz URL")
	fs.Parse(args)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(*url)
	if err != nil {
		fatal("health check failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health check returned %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Printf("health OK (HTTP %d)\n", resp.StatusCode)
}
