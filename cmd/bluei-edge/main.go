package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bluei.kr/edge/internal/api"
	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/biomass"
	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/collector"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/environmental_safety"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/inference"
	"bluei.kr/edge/internal/knowledge"
	"bluei.kr/edge/internal/learned_safety"
	"bluei.kr/edge/internal/llm"
	"bluei.kr/edge/internal/media"
	"bluei.kr/edge/internal/predictive"
	"bluei.kr/edge/internal/retention"
	"bluei.kr/edge/internal/rules"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/schedule"
	"bluei.kr/edge/internal/storage"
	bsync "bluei.kr/edge/internal/sync"
	"bluei.kr/edge/internal/tempcontrol"
	"bluei.kr/edge/internal/udp_listener"
	"bluei.kr/edge/internal/vision_pipeline"
	"bluei.kr/edge/internal/waterquality"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "check-config":
		checkConfigCmd(os.Args[2:])
	case "migrate":
		migrateCmd(os.Args[2:])
	case "health":
		healthCmd(os.Args[2:])
	case "record-feeding":
		recordFeedingCmd(os.Args[2:])
	case "import-water-fixture":
		importWaterFixtureCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: bluei-edge <command> [flags]")
	fmt.Fprintln(os.Stderr, "commands: run, check-config, migrate, health, record-feeding, import-water-fixture")
}

// check-config validates the config file and exits.
func checkConfigCmd(args []string) {
	fs := flag.NewFlagSet("check-config", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/edge.example.yaml", "path to edge config")
	fs.Parse(args)

	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		slog.Error("config validation failed", "error", err)
		os.Exit(1)
	}
	fmt.Println("config OK")
}

// migrate runs the SQLite migration.
func migrateCmd(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/edge.example.yaml", "path to edge config")
	fs.Parse(args)

	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		slog.Error("config validation failed", "error", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		slog.Error("storage open failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := runMigrations(store); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	fmt.Println("migration OK")
}

// health pings the /healthz endpoint.
func healthCmd(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/healthz", "healthz URL")
	fs.Parse(args)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		slog.Error("health check failed", "url", *url, "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health check returned %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Printf("health OK (HTTP %d)\n", resp.StatusCode)
}

// recordFeedingCmd appends a manual feeding.recorded event to the local store.
func recordFeedingCmd(args []string) {
	fs := flag.NewFlagSet("record-feeding", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/edge.example.yaml", "path to edge config")
	tankID := fs.String("tank", "", "tank_id")
	amountG := fs.Float64("amount-g", 0, "feed amount in grams")
	feederID := fs.String("feeder", "", "feeder_id, optional")
	feedType := fs.String("feed-type", "", "feed type, optional")
	recordedBy := fs.String("recorded-by", "operator", "recorded by")
	fedAt := fs.String("fed-at", "", "RFC3339 timestamp, default now")
	fs.Parse(args)

	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		slog.Error("storage open failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if cfg.Storage.MigrationMode == "auto" {
		if err := runMigrations(store); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
	}
	when := *fedAt
	if when == "" {
		when = common.FormatTime(common.NowUTC())
	}
	payload := events.FeedingRecordedPayload{
		FeedingID:   common.NewID("feeding"),
		TankID:      *tankID,
		FeederID:    *feederID,
		Source:      events.FeedingSourceManual,
		FeedAmountG: *amountG,
		FeedType:    *feedType,
		FedAt:       when,
		RecordedBy:  *recordedBy,
		Quality:     events.QualityOK,
	}
	if err := payload.Validate(); err != nil {
		slog.Error("invalid feeding record", "error", err)
		os.Exit(1)
	}
	app := runtime.NewApp(cfg, store)
	seq, err := app.AppendEvent(context.Background(), "cli", "operator", payload.FeederID, events.EventFeedingRecorded, payload.FeedingID, payload)
	if err != nil {
		slog.Error("feeding record append failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("feeding recorded: sequence=%d feeding_id=%s tank_id=%s amount_g=%.2f\n", seq, payload.FeedingID, payload.TankID, payload.FeedAmountG)
}

// importWaterFixtureCmd imports a CSV water-quality fixture, appends canonical
// sensor events, and upserts two-minute bucket projections.
func importWaterFixtureCmd(args []string) {
	fs := flag.NewFlagSet("import-water-fixture", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/edge.example.yaml", "path to edge config")
	csvPath := fs.String("csv", "", "path to Gangwon Smart Salmon CSV fixture")
	siteID := fs.String("site-id", "", "override site_id, default config")
	edgeID := fs.String("edge-id", "", "override edge_id, default config")
	deviceID := fs.String("device-id", "dws7000b_mcsc_01", "source water-quality device_id")
	sensorID := fs.String("sensor-id", "", "optional sensor_id override")
	dryRun := fs.Bool("dry-run", false, "parse and summarize without writing")
	fs.Parse(args)

	if *csvPath == "" {
		fmt.Fprintln(os.Stderr, "--csv is required")
		os.Exit(1)
	}
	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	if *siteID == "" {
		*siteID = cfg.Site.SiteID
	}
	if *edgeID == "" {
		*edgeID = cfg.Edge.EdgeID
	}
	f, err := os.Open(*csvPath)
	if err != nil {
		slog.Error("fixture open failed", "path", *csvPath, "error", err)
		os.Exit(1)
	}
	defer f.Close()
	readings, err := waterquality.ParseSmartSalmonCSV(f, waterquality.SmartSalmonOptions{
		SiteID:   *siteID,
		EdgeID:   *edgeID,
		DeviceID: *deviceID,
		SensorID: *sensorID,
	})
	if err != nil {
		slog.Error("fixture parse failed", "error", err)
		os.Exit(1)
	}
	buckets := waterquality.BuildTwoMinuteBuckets(readings)
	if *dryRun {
		fmt.Printf("water fixture dry-run: readings=%d buckets=%d site_id=%s edge_id=%s device_id=%s\n", len(readings), len(buckets), *siteID, *edgeID, *deviceID)
		return
	}

	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		slog.Error("storage open failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if cfg.Storage.MigrationMode == "auto" {
		if err := runMigrations(store); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
	}
	app := runtime.NewApp(cfg, store)
	ctx := context.Background()
	var appended int
	var skipped int
	for _, reading := range readings {
		if _, err := app.AppendEventWithID(ctx, "evt_"+reading.ReadingID, "fixture-import", "smart-salmon-csv", reading.DeviceID, events.EventSensorReadingRecorded, reading.ReadingID, reading); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				skipped++
				continue
			}
			slog.Error("append reading failed", "reading_id", reading.ReadingID, "error", err)
			os.Exit(1)
		}
		appended++
	}
	for _, bucket := range buckets {
		proj, err := waterquality.BucketToStorageProjection(bucket)
		if err != nil {
			slog.Error("bucket projection build failed", "error", err)
			os.Exit(1)
		}
		if err := store.UpsertWaterQualityBucket(ctx, proj); err != nil {
			slog.Error("bucket upsert failed", "tank_id", bucket.TankID, "bucket_start", bucket.BucketStart, "error", err)
			os.Exit(1)
		}
	}
	fmt.Printf("water fixture imported: readings=%d appended=%d skipped=%d buckets=%d site_id=%s edge_id=%s device_id=%s\n", len(readings), appended, skipped, len(buckets), *siteID, *edgeID, *deviceID)
}

// run starts the full edge runtime.
func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "configs/edge.example.yaml", "path to edge config")
	fs.Parse(args)

	initLogger()

	// 1. Load config
	cfg, _, cfgHash, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// 2. Validate config
	if err := config.Validate(cfg); err != nil {
		slog.Error("config validation failed", "error", err)
		os.Exit(1)
	}

	// 2.5 페어링 자격증명 확보 — sync.node_code/access_token이 비어있으면 creds
	// 파일에서 생성·영속(앱이 스캔하는 /v1/pair QR과 sync가 같은 토큰을 공유).
	if err := config.ApplyPairCreds(cfg); err != nil {
		slog.Warn("pair creds apply failed", "error", err)
	}
	// device-login 자격증명이 있으면 sync 토큰을 device 토큰으로 덮어쓴다(pair 토큰은
	// 백엔드 노드 인증에서 더 이상 매칭 안 됨). 반드시 ApplyPairCreds 다음.
	config.ApplyDeviceCreds(cfg)

	// 3. Open storage
	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		slog.Error("storage open failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// 4. Run migrations if mode=auto
	if cfg.Storage.MigrationMode == "auto" {
		if err := runMigrations(store); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
	}

	// 5. Load sub-configs
	var tanksCfg *config.TanksConfig
	if cfg.Tanks.ConfigPath != "" {
		if tc, err := config.LoadTanks(cfg.Tanks.ConfigPath); err != nil {
			slog.Warn("tanks config load failed", "error", err)
		} else {
			tanksCfg = tc
		}
	}

	var devicesCfg *config.DevicesConfig
	if cfg.Devices.ConfigPath != "" {
		if dc, err := config.LoadDevices(cfg.Devices.ConfigPath); err != nil {
			slog.Warn("devices config load failed", "error", err)
		} else {
			devicesCfg = dc
		}
	}

	var rulesCfg *config.RulesConfig
	if cfg.Rules.Enabled && cfg.Rules.ConfigPath != "" {
		if rc, err := config.LoadRules(cfg.Rules.ConfigPath); err != nil {
			slog.Warn("rules config load failed", "error", err)
		} else {
			rulesCfg = rc
		}
	}

	// 6. Build app
	app := runtime.NewApp(cfg, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 7. Load local tank profiles before workers/API start.
	if tanksCfg != nil {
		for _, profile := range tanksCfg.Tanks {
			profile := profile
			if err := store.UpsertTankProfile(ctx, &profile); err != nil {
				slog.Warn("tank profile upsert failed", "tank_id", profile.TankID, "error", err)
			}
		}
	}

	// 7a. 입식 정본으로 tank_profiles 재동기 (매 startup).
	// tank_profiles.avg_weight_g/fish_count 는 급이 회당량 계산 소스인데, config seed 나
	// 수조폼 재저장 등으로 placeholder(예: 500g)로 덮일 수 있다. 활성 입식이 있는 수조는
	// 최신 실측(sampling) 우선, 없으면 lifecycle 입식값으로 복구해 저평가를 막는다.
	// sampling 을 우선하므로 성장분(실측 증가)을 롤백하지 않는다.
	if profiles, err := store.ListTankProfiles(ctx); err == nil {
		for _, tp := range profiles {
			lc, err := store.GetTankLifecycle(ctx, tp.TankID)
			if err != nil || lc == nil || lc.Status != "active" {
				continue
			}
			avg := lc.InitialAvgWeightG
			if samp, err := store.GetTankSampling(ctx, tp.TankID); err == nil && samp != nil && samp.AvgWeightG > 0 {
				avg = samp.AvgWeightG // 최신 실측 우선 (성장 반영)
			}
			// 현재 마리수 = 입식 + 증감(폐사·분조·합치기·부분출하, 이벤트 정본). 재시작에 롤백되지 않게.
			adj, _ := store.SumTankCountAdjustment(ctx, tp.TankID, lc.ActiveStockingID)
			count := lc.InitialCount + adj
			if count < 0 {
				count = 0
			}
			biomass := float64(count) * avg / 1000.0
			if tp.FishCount == count && tp.AvgWeightG == avg {
				continue // 이미 정본과 일치 — 불필요 write 회피
			}
			tp.Species = lc.Species
			tp.FishCount = count
			tp.AvgWeightG = avg
			tp.BiomassKg = biomass
			if err := store.UpsertTankProfile(ctx, tp); err != nil {
				slog.Warn("tank profile lifecycle reconcile failed", "tank_id", tp.TankID, "error", err)
				continue
			}
			slog.Info("tank profile reconciled from stocking/sampling",
				"tank_id", tp.TankID, "avg_weight_g", tp.AvgWeightG,
				"fish_count", tp.FishCount, "biomass_kg", tp.BiomassKg)
		}
	}

	// 7b. Load group profiles from YAML (nullable — 파일 없으면 경고 후 계속).
	if cfg.Groups.ConfigPath != "" {
		groups, err := config.LoadGroups(cfg.Groups.ConfigPath)
		if err != nil {
			slog.Warn("groups config load failed", "error", err)
		} else {
			for _, g := range groups {
				g := g
				if err := store.UpsertGroupProfile(ctx, &g); err != nil {
					slog.Warn("group profile upsert failed", "group_id", g.GroupID, "error", err)
				}
			}
			slog.Info("groups upserted from yaml", "count", len(groups))
		}
	}

	// 7c. Load multi-tank domain registry from YAML (Phase 1 multi-tank).
	// 각 도메인은 config_path 가 비어있으면 skip — zero-config friendly.
	if cfg.Farms.ConfigPath != "" {
		farms, err := config.LoadFarms(cfg.Farms.ConfigPath)
		if err != nil {
			slog.Error("farms config load failed", "error", err)
			os.Exit(1)
		}
		for i := range farms {
			if err := store.UpsertFarm(ctx, &farms[i]); err != nil {
				slog.Error("farm upsert failed", "farm_id", farms[i].FarmID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("farms upserted from yaml", "count", len(farms))
	}

	if cfg.SitesLand.ConfigPath != "" {
		sitesLand, err := config.LoadSitesLand(cfg.SitesLand.ConfigPath)
		if err != nil {
			slog.Error("sites_land config load failed", "error", err)
			os.Exit(1)
		}
		for i := range sitesLand {
			if err := store.UpsertSiteLand(ctx, &sitesLand[i]); err != nil {
				slog.Error("site land upsert failed", "site_id", sitesLand[i].SiteID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("sites_land upserted from yaml", "count", len(sitesLand))
	}

	if cfg.SitesMarine.ConfigPath != "" {
		sitesMarine, err := config.LoadSitesMarine(cfg.SitesMarine.ConfigPath)
		if err != nil {
			slog.Error("sites_marine config load failed", "error", err)
			os.Exit(1)
		}
		for i := range sitesMarine {
			if err := store.UpsertSiteMarine(ctx, &sitesMarine[i]); err != nil {
				slog.Error("site marine upsert failed", "site_id", sitesMarine[i].SiteID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("sites_marine upserted from yaml", "count", len(sitesMarine))
	}

	if cfg.WaterTreatmentGroups.ConfigPath != "" {
		wtgs, err := config.LoadWTGs(cfg.WaterTreatmentGroups.ConfigPath)
		if err != nil {
			slog.Error("water_treatment_groups config load failed", "error", err)
			os.Exit(1)
		}
		for i := range wtgs {
			if err := store.UpsertWTG(ctx, &wtgs[i]); err != nil {
				slog.Error("wtg upsert failed", "wtg_id", wtgs[i].WTGID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("water_treatment_groups upserted from yaml", "count", len(wtgs))
	}

	if cfg.Actuators.ConfigPath != "" {
		acts, err := config.LoadActuators(cfg.Actuators.ConfigPath)
		if err != nil {
			slog.Error("actuators config load failed", "error", err)
			os.Exit(1)
		}
		for i := range acts {
			if err := store.UpsertActuator(ctx, &acts[i]); err != nil {
				slog.Error("actuator upsert failed", "device_id", acts[i].DeviceID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("actuators upserted from yaml", "count", len(acts))
	}

	if cfg.Sensors.ConfigPath != "" {
		sens, err := config.LoadSensors(cfg.Sensors.ConfigPath)
		if err != nil {
			slog.Error("sensors config load failed", "error", err)
			os.Exit(1)
		}
		for i := range sens {
			if err := store.UpsertSensor(ctx, &sens[i]); err != nil {
				slog.Error("sensor upsert failed", "sensor_id", sens[i].SensorID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("sensors upserted from yaml", "count", len(sens))
	}

	if cfg.Controllers.ConfigPath != "" {
		ctrls, err := config.LoadControllers(cfg.Controllers.ConfigPath)
		if err != nil {
			slog.Error("controllers config load failed", "error", err)
			os.Exit(1)
		}
		// yaml seed = 초기값. 이미 store 에 entry 있으면 실 ESP32 가 register 로 채운
		// dynamic 필드 (mac/ip/firmware/status/last_seen/commissioning/metadata) 를 보존.
		// yaml 의 정적 필드 (tank_id/site_id/actuator_id) 만 갱신 — 운영자가 yaml 로 재배치 가능.
		for i := range ctrls {
			yamlCtrl := &ctrls[i]
			existing, _ := store.GetController(ctx, yamlCtrl.ControllerID)
			if existing != nil {
				existing.TankID = yamlCtrl.TankID
				existing.SiteID = yamlCtrl.SiteID
				existing.ActuatorID = yamlCtrl.ActuatorID
				if err := store.UpsertController(ctx, existing); err != nil {
					slog.Error("controller upsert failed", "controller_id", yamlCtrl.ControllerID, "error", err)
					os.Exit(1)
				}
				continue
			}
			if err := store.UpsertController(ctx, yamlCtrl); err != nil {
				slog.Error("controller upsert failed", "controller_id", yamlCtrl.ControllerID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("controllers upserted from yaml", "count", len(ctrls))
	}

	if cfg.SpeciesProfiles.ConfigPath != "" {
		profiles, err := config.LoadSpeciesProfiles(cfg.SpeciesProfiles.ConfigPath)
		if err != nil {
			slog.Error("species_profiles config load failed", "error", err)
			os.Exit(1)
		}
		for key, p := range profiles {
			p := p
			if err := store.UpsertSpeciesProfile(ctx, key, &p); err != nil {
				slog.Error("species profile upsert failed", "species", key, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("species_profiles upserted from yaml", "count", len(profiles))
	}

	if cfg.Cameras.ConfigPath != "" {
		cams, err := config.LoadCameras(cfg.Cameras.ConfigPath)
		if err != nil {
			slog.Error("cameras config load failed", "error", err)
			os.Exit(1)
		}
		for _, c := range cams {
			profile := &storage.CameraProfile{
				CameraID:          c.CameraID,
				TankID:            c.TankID,
				DisplayName:       c.DisplayName,
				Vendor:            c.Vendor,
				Host:              c.Host,
				RTSPPort:          c.RTSPPort,
				HTTPPort:          c.HTTPPort,
				Username:          c.Username,
				PasswordSecretRef: c.PasswordSecretRef,
				Position:          c.Position,
				Purpose:           c.Purpose,
				StreamProfiles:    c.StreamProfiles,
				ClipPolicy:        c.ClipPolicy,
				Status:            c.Status,
				Metadata:          c.Metadata,
			}
			// 재시작 시 이미 verified 인 카메라는 status 보존 — seed 가 configured 로 덮어써
			// 영상이 매번 끊기는 것 방지.
			if existing, err := store.ListCameraProfiles(ctx, c.TankID); err == nil {
				for _, ex := range existing {
					if ex.CameraID == c.CameraID && ex.Status == "verified" {
						profile.Status = "verified"
					}
				}
			}
			if err := store.UpsertCameraProfile(ctx, profile); err != nil {
				slog.Error("camera profile upsert failed", "camera_id", c.CameraID, "error", err)
				os.Exit(1)
			}
		}
		slog.Info("camera_profiles upserted from yaml", "count", len(cams))
	}

	// 8. Record startup
	if err := app.RecordStartup(ctx, cfgHash); err != nil {
		slog.Warn("startup event record failed", "error", err)
	}

	// 9. Wire services
	colSvc := collector.NewService(app, &cfg.Collector, devicesCfg)
	rulesSvc := rules.NewService(app, rulesCfg, store)
	ctrlSvc := control.NewService(app, &cfg.Control, devicesCfg, store)
	syncSvc := bsync.NewService(app, &cfg.Sync, store)

	// Feed cycle worker (Phase 3) + optional Phase 4 safety gates (C-3p / C-3l / C-3w).
	// 각 gate 는 Enabled=true 일 때만 생성. CompositeGate 로 합산.
	var gates []any // [name, gate, name, gate, ...]

	if cfg.PredictiveSafety.Enabled {
		wtgs, err := store.ListWTGs(ctx, "")
		if err != nil {
			slog.Warn("predictive gate: failed to load WTGs; gate disabled", "error", err)
		} else {
			gates = append(gates, "C-3p", predictive.NewGate(store, wtgs, cfg.PredictiveSafety))
			slog.Info("predictive safety gate enabled (C-3p)", "wtg_count", len(wtgs))
		}
	}

	if cfg.LearnedSafety.Enabled {
		gates = append(gates, "C-3l", learned_safety.NewGate(store, cfg.LearnedSafety))
		slog.Info("learned safety gate enabled (C-3l)")
	}

	if cfg.EnvironmentalSafety.Enabled {
		var envSource environmental_safety.Source
		if cfg.EnvironmentalSafety.Source == "http" {
			envSource = &environmental_safety.HTTPSource{Endpoint: cfg.EnvironmentalSafety.HTTPEndpoint}
		} else {
			envSource = &environmental_safety.MockSource{} // calm defaults
		}
		envGate := environmental_safety.NewGate(store, cfg.EnvironmentalSafety, envSource)
		// 해상 site_id 목록 수집
		sites, _ := store.ListSites(ctx, "")
		var marineSiteIDs []string
		for _, s := range sites {
			if s["site_type"] == "marine" {
				if id, ok := s["site_id"].(string); ok {
					marineSiteIDs = append(marineSiteIDs, id)
				}
			}
		}
		envGate.StartRefresh(ctx, marineSiteIDs)
		gates = append(gates, "C-3w", envGate)
		slog.Info("environmental safety gate enabled (C-3w)", "marine_sites", len(marineSiteIDs))
	}

	var safetyGate feed_cycle.SafetyGate
	if len(gates) > 0 {
		safetyGate = feed_cycle.NewCompositeGate(gates...)
	}
	fcWorker := feed_cycle.NewWorker(app, store, ctrlSvc, cfg.FeedCycle, safetyGate)

	// Orphan cycle cleanup — 재가동 시 worker 메모리 잃은 in-flight cycle 강제 종료.
	// completed_at IS NULL 인 모든 cycle 을 aborted_on_startup 으로 마킹 (DB 일관성).
	if n, err := store.AbortOrphanFeedCycles(ctx, "aborted_on_startup", time.Now().UTC()); err != nil {
		slog.Warn("orphan feed cycle cleanup failed", "error", err)
	} else if n > 0 {
		slog.Info("orphan feed cycles cleaned up", "count", n)
	}

	// Phase 5: Arbiter — 다중 소스 우선순위 중재
	arb := arbiter.New(fcWorker, store, cfg.Arbiter.SafetyGate)

	// Phase 1c — AI 자율 스케줄러. cycle 종료 hook + 1h polling fallback.
	aiScheduler := feed_cycle.NewAIScheduler(store, arbiter.NewFeedCycleAdapter(arb))
	fcWorker.SetAIScheduler(aiScheduler)

	// 사이클 완료 자동 보고 — 비전 결과 대기 후 feed.cycle.report 적재 (365일 무인 보고).
	cycleReporter := feed_cycle.NewReporter(store, app)
	// 사후 채널도 라이브와 같은 맥락 게이트를 태운다 (2026-08-01) — 두 채널이 같은
	// 사이클을 다르게 기록하지 않게. 임계는 worker 와 동일 config.
	cycleReporter.SetCompensationGate(
		cfg.FeedCycle.CompensationGateEnabled,
		cfg.FeedCycle.CompensationGateRelMax,
		cfg.FeedCycle.CompensationGateVarMax,
		cfg.FeedCycle.CompensationGateMinConfirmed,
	)
	// 섀도 중단 판정 v1 반응 임계 (2026-08-03). 수조별 덮어쓰기 가능 — 어체 밀도·체중이
	// 다르면 같은 값이 생물학적으로 다른 뜻이 된다. 미설정이면 코드 기본값.
	cycleReporter.SetShadowStop(cfg.FeedCycle.ShadowStop, cfg.FeedCycle.ShadowStopByTank)
	cycleReporter.SetRiseAnchors(cfg.FeedCycle.RiseAnchorByCamera)
	cycleReporter.SetClipIndex(cfg.FeedCycle.ClipIndexLevelP25ByTank,
		cfg.FeedCycle.ClipIndexLevelP75ByTank,
		cfg.FeedCycle.ClipIndexDirectionLow, cfg.FeedCycle.ClipIndexDirectionHigh)
	fcWorker.SetReporter(cycleReporter)

	// G-2 — capture worker. cycle 시작 시 7초 mp4 생성 (LRCN G-3 pipeline 입력).
	// mode=fixture 면 resolver 불필요. mode=rtsp 는 강릉 D-18 이후 CameraResolver 어댑터 wire.
	captureWorker := capture.New(capture.Config{
		Enabled:               cfg.Capture.Enabled,
		Mode:                  cfg.Capture.Mode,
		FixturePath:           cfg.Capture.FixturePath,
		DurationSeconds:       cfg.Capture.DurationSeconds,
		TempDir:               cfg.Capture.TempDir,
		RetentionMinutes:      cfg.Capture.RetentionMinutes,
		Continuous:            cfg.Capture.Continuous,
		ContinuousTanks:       cfg.Capture.ContinuousTanks,
		ContinuousModeSpacing: cfg.Capture.ContinuousModeSpacing,
		MaxDiskPercent:        cfg.Capture.MaxDiskPercent,
		ObservationTailSec:    cfg.Capture.ObservationTailSec,
	}, feedingCameraResolver{store: store})
	fcWorker.SetCaptureWorker(captureWorker)

	// 상시 캡처는 입식된 수조(fish_count>0)만 — 빈 수조 clip 유입 방지. 매 tick 재평가(입식/출하 동적 반영).
	activeTankFilter := func(ctx context.Context, tankID string) bool {
		p, err := store.GetTankProfile(ctx, tankID)
		return err == nil && p != nil && p.FishCount > 0
	}
	captureWorker.SetActiveTankFilter(activeTankFilter)

	// G-3 — vision pipeline. capture.Result → LRCN.Score → VisionObservation 적재.
	// cfg.Inference.LRCN.Enabled=false 면 wire skip (capture 만 동작, observation 미적재).
	//
	// R6.2: 모든 영상 → media.clip.stored 일관 적재 (training pool 의 단일 source).
	//   phase=feeding (cycle 활성) → media.clip.stored + vision_pipeline (LRCN 호출 + VisionObservation 적재)
	//   phase=baseline (cycle 외)  → media.clip.stored 만 (LRCN 안 함, 안정성 학습 자료)
	if cfg.Inference.LRCN.Enabled {
		lrcnTimeout := time.Duration(cfg.Inference.LRCN.TimeoutSec) * time.Second
		lrcnClient := inference.NewLRCNClient(inference.LRCNConfig{
			Endpoint: cfg.Inference.LRCN.Endpoint,
			Timeout:  lrcnTimeout,
		})
		visionPipeline := vision_pipeline.New(lrcnClient, app)
		// 보고 후 세그먼트 사전 채점 (2-head feeding head) — 모달 프리필용.
		cycleReporter.SetSegmentScorer(lrcnClient)
		// r3 섀도 배출판정 (2026-07-22) — 펄스별 판정클립을 같은 채점기로 채점 (config
		// r3_shadow_enabled 로 opt-in). 급이 보상 결정은 무변.
		fcWorker.SetSegmentScorer(lrcnClient)
		captureWorker.SetOnResult(func(ctx context.Context, r *capture.Result) {
			// 모든 영상에 대해 media.clip.stored 적재 — training pool 의 단일 진입점.
			appendMediaClipStored(ctx, app, store, r)
			// feeding (cycle 시점) 일 때만 LRCN 호출 + VisionObservation 추가 적재.
			if r.Phase == capture.PhaseFeeding {
				visionPipeline.OnCaptureResult(ctx, r)
			} else if r.Phase == capture.PhaseBaseline {
				// 평소 활동성 — baseline 클립 요동 대표값 적재 (LRCN 무관).
				visionPipeline.OnBaselineClip(r)
			}
		})
		// dark-sleep 폐기 클립 — 야간(IR) 활동성 측정 후 삭제 (학습 풀 미적재 유지).
		captureWorker.SetOnNightClip(func(_ context.Context, r *capture.Result) {
			visionPipeline.OnNightClip(r)
		})
		slog.Info("G-3 vision pipeline wired", "lrcn_endpoint", cfg.Inference.LRCN.Endpoint)
	}

	// R6.1/R6.2 — 상시 캡처 loop. cfg.Capture.Continuous=true 시 가동.
	// resolveCamera 는 mode=fixture 면 unused, mode=rtsp 면 CameraResolver 어댑터 (강릉 D-18+).
	// 항상 ConfigureRuntime(=RunContinuous 내부) 호출 → 대시보드 "수집 시작/중지" 버튼으로
	// 켜고 끌 수 있게 한다. cfg.Continuous=true 면 부팅 자동시작, 아니면 버튼 대기.
	if cfg.Capture.Enabled {
		// 연속 캡처는 전부 baseline. feeding clip 은 신호게이팅(feed_cycle
		// StartCycleRecording/StopCycleRecording, 급이기 첫 ON~마지막 OFF)이 전담한다.
		// 과거 "사이클 시간대 겹침" feeding 태깅은 펄스 빈틈·미배출·상단뷰 오염을 유발 → 폐기.
		decidePhase := func(_ context.Context, _ string, _ time.Time) (string, string) {
			return capture.PhaseBaseline, ""
		}
		// tank → 그 수조의 AI용 카메라 → RTSP URL (mode=rtsp 상시 캡처 입력).
		// 수중 우선(어체 관찰), 없으면 상단.
		// R6.2 확장 — 수조의 *모든* 카메라를 각각 캡처(카메라 metadata 의 crop 적용) →
		// 수조·카메라별로 분리된 mp4 (파일명 clip_<tank>_<camera_id>_<ts>). 크롭공유 카메라
		// (한 화면 2수조)도 각 수조 영역으로 크롭되어 저장된다.
		resolveCameras := func(ctx context.Context, tankID string) ([]capture.CameraRef, error) {
			cams, err := store.ListCameraProfiles(ctx, tankID)
			if err != nil {
				return nil, err
			}
			if len(cams) == 0 {
				return nil, fmt.Errorf("no camera for tank %s", tankID)
			}
			refs := make([]capture.CameraRef, 0, len(cams))
			for _, c := range cams {
				refs = append(refs, capture.CameraRef{
					CameraID:  c.CameraID,
					RTSPURL:   buildCaptureRTSPURL(c),
					CropLeft:  metaCropFraction(c.Metadata, "crop_left"),
					CropRight: metaCropFraction(c.Metadata, "crop_right"),
				})
			}
			return refs, nil
		}
		if err := captureWorker.RunContinuous(ctx, resolveCameras, decidePhase); err != nil {
			slog.Warn("continuous capture failed to start", "err", err)
		} else if cfg.Capture.Continuous {
			slog.Info("R6.1 continuous capture auto-started (config)", "tanks", cfg.Capture.ContinuousTanks)
		} else {
			slog.Info("capture runtime ready — start via dashboard button", "tanks", cfg.Capture.ContinuousTanks)
		}
	}

	// 라이브 수질 버킷 집계 — 최근 sensor 읽기를 2분 버킷으로 투영(대시보드 24h 차트 +
	// water-forecast 예측모델 입력). import CLI 와 동일한 BuildTwoMinuteBuckets. upsert 라 멱등.
	go func() {
		project := func() {
			since := common.NowUTC().Add(-30 * time.Minute)
			evs, err := store.QueryEvents(ctx, storage.EventFilter{
				EventType: events.EventSensorReadingRecorded,
				Since:     &since,
				Limit:     5000,
			})
			if err != nil {
				slog.Warn("wq bucket query failed", "error", err)
				return
			}
			readings := make([]events.SensorReadingPayload, 0, len(evs))
			for _, e := range evs {
				var p events.SensorReadingPayload
				if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
					continue
				}
				readings = append(readings, p)
			}
			for _, b := range waterquality.BuildTwoMinuteBuckets(readings) {
				proj, err := waterquality.BucketToStorageProjection(b)
				if err != nil {
					continue
				}
				if err := store.UpsertWaterQualityBucket(ctx, proj); err != nil {
					slog.Warn("wq bucket upsert failed", "error", err)
				}
			}
		}
		project() // 즉시 1회
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				project()
			}
		}
	}()

	// 냉각기 일별 에너지 롤업 — 오늘 kWh 를 5분마다 적산해 runtime_kv 에 저장.
	// 전체/1개월/1일 누적을 매번 전체 재계산 없이 합산할 수 있게 한다.
	go func() {
		roll := func() {
			now := time.Now()
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			evs, err := store.QueryEvents(ctx, storage.EventFilter{
				EventType: events.EventSensorReadingRecorded,
				DeviceID:  "chiller_1",
				Since:     &start,
				Limit:     5000,
			})
			if err != nil {
				return
			}
			kwh := api.IntegrateChillerKWh(evs)
			_ = store.KVSet(ctx, "chiller.energy.day."+now.Format("2006-01-02"), strconv.FormatFloat(kwh, 'f', 3, 64))
		}
		roll() // 즉시 1회
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				roll()
			}
		}
	}()

	// Phase 1e-B — 환경 변화 감지 worker.
	// 수온/DO 시계열 5 분 polling → threshold 초과 시 RebuildScheduleForTank.
	envMonitor := feed_cycle.NewEnvironmentMonitor(store, aiScheduler)

	schedCfg := cfg.Schedule
	if schedCfg.IntervalSec <= 0 {
		schedCfg.IntervalSec = 30
	}
	schedWorker := schedule.NewWorker(store, fcWorker, schedule.Config{
		Enabled:     schedCfg.Enabled,
		IntervalSec: schedCfg.IntervalSec,
	})
	schedWorker.SetArbiter(arb) // Arbiter 경유 설정

	// 자동 수온 제어 워커 — 냉각기(onoff_feeder switch, ON=냉각) ON/OFF 로 목표수온 유지.
	// 냉각기 switch ext_id 는 collector 어댑터 설정에서 가져온다(chiller_control.go 와 동일).
	tempCtrlChillerExt := 0
	for _, a := range cfg.Collector.Adapters {
		if a.ChillerSwitchExtID > 0 {
			tempCtrlChillerExt = a.ChillerSwitchExtID
			break
		}
	}
	tempCtrlWorker := tempcontrol.NewWorker(store, cfg.TempControl, tempCtrlChillerExt)
	// 수온 단절 알림 임계 — arbiter 안전 게이트가 급이를 막을 때 쓰는 값과 같게 맞춘다.
	// 두 판단이 어긋나면 "급이는 막혔는데 수온 알림은 없는" 상태가 생긴다 (8-28 사고).
	tempCtrlWorker.SetSensorStaleSec(cfg.Arbiter.SafetyGate.SensorMaxStaleSec)

	// Phase F.1 — operator_intent 종합 판단용 로컬 LLM client.
	// LLM.Enabled=false 면 nil 로 주입 → handler 가 LLM hook skip.
	var llmClient *llm.Client
	if cfg.LLM.Enabled {
		timeout := time.Duration(cfg.LLM.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		llmClient = llm.NewClient(llm.Config{
			Endpoint:  cfg.LLM.Endpoint,
			AuthToken: cfg.LLM.AuthToken,
			Primary:   cfg.LLM.PrimaryModel,
			Fallback:  cfg.LLM.FallbackModel,
			Timeout:   timeout,
		})
	}

	apiSrv := api.NewServer(cfg, app, store, colSvc, ctrlSvc, syncSvc, fcWorker, arb, aiScheduler, llmClient)
	apiSrv.SetCaptureWorker(captureWorker) // 대시보드 "수집 시작/중지" 버튼용

	// 비전 게이트 (2026-08-25 운영자 지시) — 판정 카메라가 끊기면 급이를 시작하지
	// 않고, 진행 중이면 그 순간부터 멈춘다. "카메라가 없다면 먹는지 안 먹는지도
	// 모른 채 사료를 주는 것"이라서다. 판정은 프레임허브를 쥔 api 쪽이 하고
	// feed_cycle 이 주입받는다 (feed_cycle → api 의존을 만들지 않기 위해).
	fcWorker.SetVisionGate(apiSrv.TankVisionGate)

	// 프레임허브 상시 기동 — 뷰어(대시보드)가 없어도 카메라 연결을 유지한다.
	// 없으면 새벽처럼 아무도 연결을 잡지 않는 시간대에 단발 캡처만 남아, 업스트림이
	// 한 번 느려지면 스스로 회복하지 못한다 (2026-08-11 6시간 39분 단절).
	if cfg.Capture.Enabled && strings.EqualFold(cfg.Capture.Mode, capture.ModeRTSP) {
		apiSrv.StartCameraHubKeepalive(ctx, cfg.Capture.ContinuousTanks, activeTankFilter)
	}

	// 축1 1a — 라이브 요동 세션. 신호게이팅 녹화와 같은 창(사이클 경계)에서 급이 관찰
	// 카메라의 프레임허브로 실시간 계산하고, 종료 시 vision.agitation.live 를 적재해
	// 클립 사후 곡선(vision.agitation.recorded)과 파리티를 대조한다.
	liveAgitation := vision_pipeline.NewLiveAgitation(app)

	// 대시보드 실시간 요동 표기 — 진행 중 세션의 현재 series 를 폴링으로 노출
	// (/v1/vision/agitation/live). 사이클 종료 후에야 나오던 곡선을 급이 중 표기.
	apiSrv.SetLiveAgitationProvider(liveAgitation.ActiveSnapshots)

	// 보상 펄스 (2026-07-08 운영자 지시) — worker 의 gap 끝 무배출 판정이 라이브
	// 요동 세션 series 를 조회하는 경로. 세션 없으면 판정 skip.
	fcWorker.SetLiveSeriesProvider(func(cycleID string) ([]float64, time.Time, bool) {
		s := liveAgitation.Session(cycleID)
		if s == nil {
			return nil, time.Time{}, false
		}
		// ⚠ StartedAt 이 아니라 SeriesStart() 다 — 프레임 구독이 세션 시작보다 늦게
		// 붙으면 series[0] 이 그만큼 뒤 시각이라, StartedAt 기준으로 인덱스를 구하면
		// 반응창이 앞으로 밀린다 (2026-08-12 실측 6초).
		return s.Series(), s.SeriesStart(), true
	})

	// 축3 3a — 수중캠 바닥 스틸 수집 (트랙 U). 사이클 직전/직후 + 정기(30분).
	// 대상은 position=underwater 카메라 자동 도출 — 수중캠 없는 수조(트랙 T)는 no-op.
	floorStills := vision_pipeline.NewFloorStillCollector(
		app,
		filepath.Join(cfg.Edge.DataDir, "floor_stills"),
		func(fctx context.Context) ([]vision_pipeline.FloorCamera, error) {
			tanks, err := store.ListTankProfiles(fctx)
			if err != nil {
				return nil, err
			}
			var out []vision_pipeline.FloorCamera
			for _, tk := range tanks {
				cams, err := store.ListCameraProfiles(fctx, tk.TankID)
				if err != nil {
					return nil, err
				}
				for _, c := range cams {
					if strings.EqualFold(c.Position, "underwater") {
						out = append(out, vision_pipeline.FloorCamera{TankID: tk.TankID, CameraID: c.CameraID})
					}
				}
			}
			return out, nil
		},
		func(fctx context.Context, cameraID string) (media.Frame, error) {
			hub, err := apiSrv.EnsureCameraFrameHub(fctx, cameraID, "sub")
			if err != nil {
				return media.Frame{}, err
			}
			wctx, wcancel := context.WithTimeout(fctx, 10*time.Second)
			defer wcancel()
			frame, ok := hub.WaitLatestFresh(wctx, 10*time.Second)
			if !ok {
				return media.Frame{}, fmt.Errorf("no fresh frame from %s", cameraID)
			}
			return frame, nil
		},
	)
	go floorStills.Run(ctx)

	// 외부 연안 수온 수집 (NIFS keyless, 1시간 주기) — 취수 권역 추세 (2026-07-17).
	// 관측소는 현장 config(sea_temp.stations)로 주입 — 미설정이면 Run 이 즉시 비활성.
	go collector.NewSeaTempCollector(app, store, cfg.SeaTemp.Stations).Run(ctx)

	// 폐사 의심 감시 (2026-07-07 운영자 지시, tank_02 실사체로 방법 검증) — 입식 수조의
	// 비수중(탑뷰) 카메라에서 "새로 정지한 옅은 어체 형체"를 주기 감시, 발견 시 warning
	// 알림 1건(open-alert dedupe). 학습 불필요 고전 CV — 검출 로직은 vision_pipeline.
	mortalityWatch := vision_pipeline.NewMortalityWatcher(
		filepath.Join(cfg.Edge.DataDir, "mortality"),
		func(fctx context.Context) ([]vision_pipeline.MortalityCamera, error) {
			tanks, err := store.ListTankProfiles(fctx)
			if err != nil {
				return nil, err
			}
			var out []vision_pipeline.MortalityCamera
			for _, tk := range tanks {
				if tk.FishCount <= 0 {
					continue // 빈 수조 제외
				}
				cams, err := store.ListCameraProfiles(fctx, tk.TankID)
				if err != nil {
					return nil, err
				}
				for _, c := range cams {
					if !strings.EqualFold(c.Position, "underwater") {
						out = append(out, vision_pipeline.MortalityCamera{TankID: tk.TankID, CameraID: c.CameraID})
					}
				}
			}
			return out, nil
		},
		func(fctx context.Context, cameraID string) (media.Frame, error) {
			hub, err := apiSrv.EnsureCameraFrameHub(fctx, cameraID, "sub")
			if err != nil {
				return media.Frame{}, err
			}
			frame, ok := hub.WaitLatestFresh(fctx, 10*time.Second)
			if !ok {
				return media.Frame{}, fmt.Errorf("no fresh frame from %s", cameraID)
			}
			return frame, nil
		},
		func(actx context.Context, tankID, cameraID, message string, evidence map[string]any) {
			key := "vision.mortality.suspect." + tankID
			if existing, err := store.GetOpenAlert(actx, key); err != nil || existing != nil {
				return // 조회 실패 or 이미 열림 — idempotent
			}
			now := time.Now().UTC()
			alertID := common.NewAlertID()
			evidence["camera_id"] = cameraID
			payload := map[string]any{
				"alert_id":   alertID,
				"alert_type": "vision.mortality.suspect",
				"severity":   "warning",
				"status":     "open",
				"subject":    map[string]any{"kind": "tank", "id": tankID},
				"message":    message,
				"evidence":   evidence,
				"raised_at":  common.FormatTime(now),
				"updated_at": common.FormatTime(now),
			}
			body, _ := json.Marshal(payload)
			if _, err := store.UpsertAlert(actx, &storage.OpenAlert{
				AlertID:        alertID,
				AlertDedupeKey: key,
				AlertType:      "vision.mortality.suspect",
				Severity:       "warning",
				SubjectKind:    "tank",
				SubjectID:      tankID,
				Status:         "open",
				RaisedAt:       now,
				UpdatedAt:      now,
				PayloadJSON:    string(body),
			}); err != nil {
				slog.Warn("mortality alert upsert failed", "tank_id", tankID, "err", err)
			}
		},
	)
	go mortalityWatch.Run(ctx)

	// 훅은 사이클 진행 경로에서 sync 호출됨 — 펄스 시작을 지연시키지 않도록 전부 goroutine.
	captureWorker.SetCycleRecordingHooks(
		func(cycleID, tankID string) {
			liveAgitation.StartCycle(cycleID, tankID) // 값싼 등록 — 훅 경로 비블로킹
			go func() {
				hctx, hcancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer hcancel()
				floorStills.CollectTank(hctx, tankID, "pre_cycle", cycleID)
			}()
			go func() {
				hctx, hcancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer hcancel()
				cams, err := store.ListCameraProfiles(hctx, tankID)
				if err != nil {
					slog.Warn("live agitation: camera list failed", "tank_id", tankID, "err", err)
					return
				}
				pick := pickFeedingCamera(cams)
				if pick == nil {
					return
				}
				hub, err := apiSrv.EnsureCameraFrameHub(hctx, pick.CameraID, "sub")
				if err != nil {
					slog.Warn("live agitation: frame hub failed",
						"tank_id", tankID, "camera_id", pick.CameraID, "err", err)
					return
				}
				frames, cancelSub := hub.Subscribe()
				liveAgitation.Attach(cycleID, pick.CameraID, frames, cancelSub)
			}()
		},
		func(cycleID, tankID string) {
			go liveAgitation.StopCycle(cycleID)
			go func() {
				hctx, hcancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer hcancel()
				floorStills.CollectTank(hctx, tankID, "post_cycle", cycleID)
			}()
		},
	)

	// 어시스턴트 RAG 지식팩 적재 (best-effort). 임베딩에 LLM client 필요.
	// 인덱스(rag-index.jsonl)는 게이팅 배포로 기기에 설치됨. 없거나 실패하면 RAG 없이 동작.
	if cfg.Knowledge.Enabled && llmClient != nil && cfg.Knowledge.IndexPath != "" {
		if chunks, err := knowledge.Load(cfg.Knowledge.IndexPath); err != nil {
			slog.Warn("knowledge index load failed — assistant RAG disabled",
				"path", cfg.Knowledge.IndexPath, "err", err)
		} else {
			embedModel := cfg.Knowledge.EmbedModel
			embed := func(ctx context.Context, text string) ([]float64, error) {
				return llmClient.Embed(ctx, embedModel, text)
			}
			apiSrv.SetKnowledgeRetriever(knowledge.New(chunks, embed, cfg.Knowledge.TopK))
			slog.Info("assistant RAG knowledge loaded",
				"chunks", len(chunks), "embed_model", embedModel, "path", cfg.Knowledge.IndexPath)
		}
	}

	// Baseline worker — Cage/Tank별 anomaly score 주기 평가 (docs/29 Phase 1.5)
	baselineScorer := baseline.NewScorer(cfg.Storage.SQLitePath)
	baselineCfg := baseline.Config{
		Enabled:      cfg.BaselineWorker.Enabled,
		Interval:     time.Duration(cfg.BaselineWorker.IntervalSec) * time.Second,
		InitialDelay: time.Duration(cfg.BaselineWorker.InitialDelaySec) * time.Second,
	}
	baselineSvc := baseline.NewWorker(app, store, baselineScorer, baselineCfg)

	// C-4: pending_notify 자동 실행 타이머.
	// 시스템 전역 Enabled=true, Cage/Tank별 policyLookup 이 진짜 게이트.
	decisionTimer := baseline.NewDecisionTimer(
		app, store,
		baseline.TimerConfig{
			Enabled:      true,
			Interval:     time.Minute,
			InitialDelay: 60 * time.Second,
		},
		func(ctx context.Context, tankID string) (bool, int) {
			return apiSrv.EffectiveDecisionPolicy(ctx, tankID)
		},
		apiSrv.DecisionTimerExecutor(),
	)

	// D-5: 일일 추정 체중 스냅샷 worker.
	historyWorker := biomass.NewHistorySnapshotWorker(store, biomass.HistoryWorkerConfig{
		Enabled:      cfg.WeightHistoryWorker.Enabled,
		Interval:     time.Duration(cfg.WeightHistoryWorker.IntervalSec) * time.Second,
		InitialDelay: time.Duration(cfg.WeightHistoryWorker.InitialDelaySec) * time.Second,
	}, slog.Default())

	// Retention worker — 만료 telemetry 를 월별 백업으로 export 후 live DB 에서 제거.
	// 백업 파일은 자동 삭제하지 않는다 (저장/삭제는 운영자 위임).
	retentionArchiveDir := cfg.Retention.ArchiveDir
	if retentionArchiveDir == "" {
		retentionArchiveDir = filepath.Join(filepath.Dir(cfg.Storage.SQLitePath), "archive")
	}
	retentionRules := make([]retention.Rule, 0, len(cfg.Retention.Rules))
	for _, r := range cfg.Retention.Rules {
		retentionRules = append(retentionRules, retention.Rule{
			EventType:      r.EventType,
			KeepDays:       r.KeepDays,
			AggregateDaily: r.AggregateDaily,
			Metrics:        r.Metrics,
			ExcludeMetrics: r.ExcludeMetrics,
		})
	}
	// AggregateDaily 일 경계 기준 timezone (Site.Timezone). 로드 실패 시 UTC.
	retentionLoc, err := time.LoadLocation(cfg.Site.Timezone)
	if err != nil {
		slog.Warn("retention: invalid site timezone; using UTC", "timezone", cfg.Site.Timezone, "error", err)
		retentionLoc = time.UTC
	}
	retentionSvc := retention.NewWorker(store, retention.Config{
		Enabled:      cfg.Retention.Enabled,
		Interval:     time.Duration(cfg.Retention.IntervalSec) * time.Second,
		InitialDelay: time.Duration(cfg.Retention.InitialDelaySec) * time.Second,
		ArchiveDir:   retentionArchiveDir,
		Rules:        retentionRules,
		Location:     retentionLoc,
		Aggregator:   app,
	})

	mgr := runtime.NewManager(colSvc, rulesSvc, ctrlSvc, syncSvc, baselineSvc, decisionTimer, historyWorker, fcWorker, schedWorker, aiScheduler, envMonitor, retentionSvc, tempCtrlWorker, apiSrv)

	// 10. Start all services
	startCtx, startCancel := context.WithTimeout(ctx, time.Duration(cfg.Runtime.StartupTimeoutSec)*time.Second)
	defer startCancel()
	if err := mgr.Start(startCtx); err != nil {
		slog.Error("service start failed", "error", err)
		app.RecordShutdown(ctx, "startup_failure", "")
		os.Exit(1)
	}

	// 부팅 시 device 정체성(farm·수조·hatchery)을 클라우드에 동기화(멱등). 네트워크
	// I/O 이므로 부팅을 막지 않게 비동기. device 토큰 없으면 즉시 no-op.
	go apiSrv.SyncIdentityOnStartup()
	// 앱 어장에 붙은 경우 앱 수조의 관측소 수온·급이·폐사를 30분마다 이어 받는다(없으면 no-op).
	go apiSrv.RunFarmHistoryLoop(ctx)

	// Phase 5 — UDP weight stream listener (ESP32 → backend 1Hz push).
	// 별도 runtime service 로 묶지 않고 ctx 로 lifecycle 관리. bind 실패는 warn 후 계속.
	udpLst := udp_listener.New(":9998", fcWorker, store, slog.Default())
	if err := udpLst.EnableTraceFile("/tmp/weight_trace_10s.tsv"); err != nil {
		slog.Warn("udp weight trace file open failed", "error", err)
	}
	apiSrv.SetLiveWeightProvider(udpLst) // B-7: GET /v1/controllers/{id}/live-weight
	if err := udpLst.Start(ctx); err != nil {
		slog.Warn("udp weight listener failed to start", "error", err)
	}

	slog.Info("bluei-edge running",
		"site_id", cfg.Site.SiteID,
		"edge_id", cfg.Edge.EdgeID,
		"mode", cfg.Edge.Mode,
		"api", fmt.Sprintf("%s:%d", cfg.API.BindHost, cfg.API.Port),
	)

	// 11. Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutdown signal received", "signal", sig.String())

	// 12. Graceful shutdown
	stopCtx, stopCancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.Runtime.ShutdownTimeoutSec)*time.Second)
	defer stopCancel()

	// 진행 중 급이 사이클의 급이기 stop 을 먼저 발행 — 재시작이 펄스 ON 중에 걸려도
	// 급이기가 켜진 채 방치되지 않는다 (2026-07-07 13:34 실사고: 57초 방치).
	fcWorker.EmergencyStopAll()
	mgr.Stop()
	app.RecordShutdown(stopCtx, "signal", sig.String())
	slog.Info("bluei-edge stopped")
}

// runMigrations — storage.MigrationPaths 를 순서대로 적용. 각 파일은 idempotent.
func runMigrations(store storage.Store) error {
	for _, p := range storage.MigrationPaths {
		if err := storage.Migrate(store, p); err != nil {
			return fmt.Errorf("migration %s: %w", p, err)
		}
	}
	return nil
}

func initLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

// appendMediaClipStored — R6.2. 상시 캡처 baseline 영상의 메타를 events 에 적재.
// LRCN inference 안 함. 운영자 라벨링 풀 (phase=baseline) 의 입력.
// feeding phase 는 vision_pipeline 에서 별도 (VisionObservation) 적재.
// feedingCameraResolver — capture.CameraResolver 구현 (사이클 시작 훅 OnCycleStart 용).
// tank → 급이 관찰 카메라 1대 선택 후 RTSP URL/크롭을 반환. 상시 캡처(resolveCameras)와
// 같은 store/URL/crop 로직을 쓰되, 여러 대 중 급이 관찰에 적합한 1대만 고른다.
type feedingCameraResolver struct{ store storage.Store }

func (r feedingCameraResolver) ResolveForFeedingInference(ctx context.Context, tankID string) (capture.CameraRef, error) {
	cams, err := r.store.ListCameraProfiles(ctx, tankID)
	if err != nil {
		return capture.CameraRef{}, err
	}
	pick := pickFeedingCamera(cams)
	if pick == nil {
		return capture.CameraRef{}, fmt.Errorf("no camera for tank %s", tankID)
	}
	return capture.CameraRef{
		CameraID:  pick.CameraID,
		RTSPURL:   buildCaptureRTSPURL(pick),
		CropLeft:  metaCropFraction(pick.Metadata, "crop_left"),
		CropRight: metaCropFraction(pick.Metadata, "crop_right"),
	}, nil
}

// pickFeedingCamera — 라이브 요동 세션용 카메라 우선순위 선택. 요동은 수면 지표라
// **비수중 우선**: 수중캠은 어체 근접 통과로 곡선이 포화(prior 1.00 고정)돼 판정에
// 무의미하다 (2026-07-07 tank_01 실측 — 수중캠만 세션이 열려 반응배열 전부 1.00).
// side > 기타 비수중(탑뷰) > underwater(폴백). 없으면 nil.
func pickFeedingCamera(cams []*storage.CameraProfile) *storage.CameraProfile {
	var side, surface, underwater *storage.CameraProfile
	for _, c := range cams {
		switch strings.ToLower(c.Position) {
		case "underwater":
			if underwater == nil {
				underwater = c
			}
		case "side":
			if side == nil {
				side = c
			}
		default:
			if surface == nil {
				surface = c
			}
		}
	}
	if side != nil {
		return side
	}
	if surface != nil {
		return surface
	}
	return underwater
}

// metaCropFraction — 카메라 metadata 에서 0..1 크롭 비율(crop_left/crop_right)을 읽는다.
// JSON 언마샬 후 숫자는 float64. 없으면 0(크롭 없음).
func metaCropFraction(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

// buildCaptureRTSPURL constructs rtsp://host:port/path from a camera profile
// (sub 우선, 없으면 main) for the continuous capture pipeline.
func buildCaptureRTSPURL(p *storage.CameraProfile) string {
	port := p.RTSPPort
	if port == 0 {
		port = 554
	}
	path := ""
	if sp, ok := p.StreamProfiles["sub"].(map[string]any); ok {
		path, _ = sp["path"].(string)
	}
	if path == "" {
		if sp, ok := p.StreamProfiles["main"].(map[string]any); ok {
			path, _ = sp["path"].(string)
		}
	}
	return fmt.Sprintf("rtsp://%s:%d%s", p.Host, port, path)
}

func appendMediaClipStored(ctx context.Context, app *runtime.App, store storage.Store, r *capture.Result) {
	if r == nil {
		return
	}
	startedAt := r.CapturedAt.UTC().Format(time.RFC3339Nano)
	endedAt := r.CapturedAt.Add(time.Duration(r.DurationS) * time.Second).UTC().Format(time.RFC3339Nano)
	info, statErr := os.Stat(r.MP4Path)
	var size int64
	if statErr == nil {
		size = info.Size()
	}
	// 학습샘플 컨텍스트 임베드 — 캡처 시점 환경(수온/DO/pH) + 급이량.
	// 영상 클립을 자체완결 라벨샘플로 만들어 조건부 학습(FCR·성장 최적화) 가능하게 함.
	evidence := map[string]any{
		"phase":    r.Phase,
		"cycle_id": r.CycleID,
	}
	if store != nil && r.TankID != "" {
		if envs, err := store.ListTankEnvironment(ctx, r.TankID); err == nil && len(envs) > 0 {
			wq := map[string]any{}
			for _, e := range envs {
				if e.Value != nil {
					wq[e.Metric] = *e.Value
				}
			}
			if len(wq) > 0 {
				evidence["water_quality"] = wq
			}
		}
		if r.Phase == capture.PhaseFeeding && r.CycleID != "" {
			if cycles, err := store.ListActiveFeedCycles(ctx); err == nil {
				for _, c := range cycles {
					if c.CycleID == r.CycleID {
						evidence["feed_target_g"] = c.TargetAmountG
						evidence["feed_total_g"] = c.TotalAmountG
						if c.ActualTotalAmountG > 0 {
							evidence["feed_actual_g"] = c.ActualTotalAmountG
						}
						break
					}
				}
			}
		}
	}
	reason := r.Reason
	if reason == "" {
		reason = "continuous_capture"
	}
	payload := events.MediaClipStoredPayload{
		ClipID:    r.ClipID,
		CameraID:  r.CameraID,
		TankID:    r.TankID,
		Reason:    reason,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		URI:       r.MP4Path,
		MimeType:  "video/mp4",
		SizeBytes: size,
		Evidence:  evidence,
	}
	if _, err := app.AppendEvent(ctx, "capture", "continuous", r.CameraID,
		events.EventMediaClipStored, r.ClipID, payload); err != nil {
		slog.Warn("media.clip.stored append failed",
			"clip_id", r.ClipID, "tank_id", r.TankID, "err", err)
	}
}
