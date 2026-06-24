package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	"bluei.kr/edge/internal/learned_safety"
	"bluei.kr/edge/internal/llm"
	"bluei.kr/edge/internal/predictive"
	"bluei.kr/edge/internal/retention"
	"bluei.kr/edge/internal/rules"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/schedule"
	"bluei.kr/edge/internal/storage"
	bsync "bluei.kr/edge/internal/sync"
	"bluei.kr/edge/internal/udp_listener"
	"bluei.kr/edge/internal/vision_pipeline"
	"bluei.kr/edge/internal/waterquality"
)

// migrationPaths — 순서대로 적용. 각각 idempotent (CREATE TABLE IF NOT EXISTS).
var migrationPaths = []string{
	"migrations/001_init.sql",
	"migrations/002_autonomous_mode.sql",
	"migrations/003_lifecycle.sql",
	"migrations/004_sampling.sql",
	"migrations/005_fcr_calibration.sql",
	"migrations/006_decision_policy.sql",
	"migrations/007_weight_history.sql",
	"migrations/008_groups.sql",
	"migrations/009_farms_sites.sql",
	"migrations/010_water_treatment_groups.sql",
	"migrations/011_controllers.sql",
	"migrations/012_actuators_sensors.sql",
	"migrations/013_species_profiles.sql",
	"migrations/014_feeding_schedules.sql",
	"migrations/015_predictive_events.sql",
	"migrations/016_learned_safety.sql",
	"migrations/017_arbiter_decisions.sql",
	"migrations/018_arbiter_preemption.sql",
	"migrations/019_feed_cycle_intent.sql",
	"migrations/020_sync_batch_event_seq_index.sql",
	"migrations/021_tank_physical_dimensions.sql",
	"migrations/022_camera_models.sql",
	"migrations/023_camera_view_geometry.sql",
	"migrations/024_sensor_models.sql",
	"migrations/025_actuator_models.sql",
	"migrations/026_feed_cycle_motor_outputs.sql",
	"migrations/027_events_type_sequence_index.sql",
	"migrations/028_feed_cycle_load_cell.sql",
	"migrations/029_actuator_model_category_specs.sql",
	"migrations/030_traceability_lifecycle.sql",
	"migrations/031_species_fao.sql",
	"migrations/032_documents.sql",
	"migrations/033_inventory.sql",
	"migrations/034_document_subject.sql",
	"migrations/035_partners.sql",
	"migrations/036_site_trade.sql",
	"migrations/037_broodstock.sql",
	"migrations/038_spawn_batches.sql",
	"migrations/039_larval_livefeed.sql",
	"migrations/040_hatchery_treatments.sql",
}

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
	}, nil)
	fcWorker.SetCaptureWorker(captureWorker)

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
		captureWorker.SetOnResult(func(ctx context.Context, r *capture.Result) {
			// 모든 영상에 대해 media.clip.stored 적재 — training pool 의 단일 진입점.
			appendMediaClipStored(ctx, app, r)
			// feeding (cycle 시점) 일 때만 LRCN 호출 + VisionObservation 추가 적재.
			if r.Phase == capture.PhaseFeeding {
				visionPipeline.OnCaptureResult(ctx, r)
			}
		})
		slog.Info("G-3 vision pipeline wired", "lrcn_endpoint", cfg.Inference.LRCN.Endpoint)
	}

	// R6.1/R6.2 — 상시 캡처 loop. cfg.Capture.Continuous=true 시 가동.
	// resolveCamera 는 mode=fixture 면 unused, mode=rtsp 면 CameraResolver 어댑터 (강릉 D-18+).
	// decidePhase 는 store.ListActiveFeedCycles 로 phase + cycle_id 결정.
	if cfg.Capture.Enabled && cfg.Capture.Continuous {
		decidePhase := func(ctx context.Context, tankID string, capturedAt time.Time) (string, string) {
			cycles, err := store.ListActiveFeedCycles(ctx)
			if err != nil {
				return capture.PhaseBaseline, ""
			}
			for _, c := range cycles {
				if c.TankID == tankID {
					return capture.PhaseFeeding, c.CycleID
				}
			}
			return capture.PhaseBaseline, ""
		}
		// mode=fixture 는 resolveCamera 사용 X. mode=rtsp 는 별도 어댑터 필요 (강릉 단계).
		if err := captureWorker.RunContinuous(ctx, nil, decidePhase); err != nil {
			slog.Warn("continuous capture failed to start", "err", err)
		} else {
			slog.Info("R6.1 continuous capture loop started",
				"tanks", cfg.Capture.ContinuousTanks,
				"duration_s", cfg.Capture.DurationSeconds)
		}
	}

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

	mgr := runtime.NewManager(colSvc, rulesSvc, ctrlSvc, syncSvc, baselineSvc, decisionTimer, historyWorker, fcWorker, schedWorker, aiScheduler, envMonitor, retentionSvc, apiSrv)

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

	mgr.Stop()
	app.RecordShutdown(stopCtx, "signal", sig.String())
	slog.Info("bluei-edge stopped")
}

// runMigrations — migrationPaths 를 순서대로 적용. 각 파일은 idempotent.
func runMigrations(store storage.Store) error {
	for _, p := range migrationPaths {
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
func appendMediaClipStored(ctx context.Context, app *runtime.App, r *capture.Result) {
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
	payload := events.MediaClipStoredPayload{
		ClipID:    r.ClipID,
		CameraID:  r.CameraID,
		TankID:    r.TankID,
		Reason:    "continuous_capture",
		StartedAt: startedAt,
		EndedAt:   endedAt,
		URI:       r.MP4Path,
		MimeType:  "video/mp4",
		SizeBytes: size,
		Evidence: map[string]any{
			"phase":    r.Phase,
			"cycle_id": r.CycleID,
		},
	}
	if _, err := app.AppendEvent(ctx, "capture", "continuous", r.CameraID,
		events.EventMediaClipStored, r.ClipID, payload); err != nil {
		slog.Warn("media.clip.stored append failed",
			"clip_id", r.ClipID, "tank_id", r.TankID, "err", err)
	}
}
