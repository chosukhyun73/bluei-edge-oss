package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// C-11 camera_models — 카메라 모델 라이브러리.
// camera_profiles.model_id 가 이 테이블의 model_id 를 가리킨다 (nullable).

func (s *sqliteStore) UpsertCameraModel(ctx context.Context, m *CameraModel) error {
	protocolsJSON, err := json.Marshal(m.Protocols)
	if err != nil {
		return err
	}
	nightMode := 0
	if m.NightMode {
		nightMode = 1
	}
	createdAt := m.CreatedAt
	if createdAt == "" {
		createdAt = fmtNow()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO camera_models(model_id,vendor,product_code,display_name,lens_type,baseline_mm,stereo_calibration_json,resolution_w,resolution_h,fov_deg,fps,night_mode,protocols_json,notes,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(model_id) DO UPDATE SET
           vendor=excluded.vendor,
           product_code=excluded.product_code,
           display_name=excluded.display_name,
           lens_type=excluded.lens_type,
           baseline_mm=excluded.baseline_mm,
           stereo_calibration_json=excluded.stereo_calibration_json,
           resolution_w=excluded.resolution_w,
           resolution_h=excluded.resolution_h,
           fov_deg=excluded.fov_deg,
           fps=excluded.fps,
           night_mode=excluded.night_mode,
           protocols_json=excluded.protocols_json,
           notes=excluded.notes,
           updated_at=excluded.updated_at`,
		m.ModelID,
		m.Vendor,
		m.ProductCode,
		m.DisplayName,
		m.LensType,
		ptrFloat(m.BaselineMM),
		nullStr(m.StereoCalibrationJSON),
		ptrInt(m.ResolutionW),
		ptrInt(m.ResolutionH),
		ptrFloat(m.FOVDeg),
		ptrInt(m.FPS),
		nightMode,
		string(protocolsJSON),
		nullStr(m.Notes),
		createdAt,
		fmtNow(),
	)
	return err
}

func (s *sqliteStore) GetCameraModel(ctx context.Context, modelID string) (*CameraModel, error) {
	return s.scanCameraModel(s.db.QueryRowContext(ctx, cameraModelSelectSQL+` WHERE model_id=?`, modelID))
}

func (s *sqliteStore) ListCameraModels(ctx context.Context) ([]*CameraModel, error) {
	rows, err := s.db.QueryContext(ctx, cameraModelSelectSQL+` ORDER BY vendor, product_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CameraModel{}
	for rows.Next() {
		m, err := scanCameraModelScanner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteCameraModel(ctx context.Context, modelID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM camera_models WHERE model_id=?`, modelID)
	return err
}

// CountCameraProfilesForModel — FK reject 정책 지원. 자식 인스턴스 있는 모델은 삭제 차단.
func (s *sqliteStore) CountCameraProfilesForModel(ctx context.Context, modelID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM camera_profiles WHERE model_id=?`, modelID).Scan(&n)
	return n, err
}

const cameraModelSelectSQL = `SELECT model_id,vendor,product_code,display_name,lens_type,baseline_mm,COALESCE(stereo_calibration_json,''),resolution_w,resolution_h,fov_deg,fps,night_mode,COALESCE(protocols_json,'[]'),COALESCE(notes,''),created_at,updated_at FROM camera_models`

func (s *sqliteStore) scanCameraModel(row *sql.Row) (*CameraModel, error) {
	m, err := scanCameraModelScanner(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func scanCameraModelScanner(row scanner) (*CameraModel, error) {
	m := &CameraModel{}
	var baseline, fov sql.NullFloat64
	var resW, resH, fps sql.NullInt64
	var nightModeInt int
	var protocolsJSON string
	if err := row.Scan(
		&m.ModelID, &m.Vendor, &m.ProductCode, &m.DisplayName, &m.LensType,
		&baseline, &m.StereoCalibrationJSON,
		&resW, &resH, &fov, &fps, &nightModeInt,
		&protocolsJSON, &m.Notes, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if baseline.Valid {
		v := baseline.Float64
		m.BaselineMM = &v
	}
	if resW.Valid {
		v := int(resW.Int64)
		m.ResolutionW = &v
	}
	if resH.Valid {
		v := int(resH.Int64)
		m.ResolutionH = &v
	}
	if fov.Valid {
		v := fov.Float64
		m.FOVDeg = &v
	}
	if fps.Valid {
		v := int(fps.Int64)
		m.FPS = &v
	}
	m.NightMode = nightModeInt != 0
	_ = json.Unmarshal([]byte(protocolsJSON), &m.Protocols)
	if m.Protocols == nil {
		m.Protocols = []string{}
	}
	return m, nil
}

// ptrFloat / ptrInt — sql null helper for *T columns (nil → NULL).
func ptrFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func ptrInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
