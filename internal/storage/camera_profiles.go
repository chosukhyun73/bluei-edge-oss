package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *sqliteStore) UpsertCameraProfile(ctx context.Context, profile *CameraProfile) error {
	purposeJSON, err := json.Marshal(profile.Purpose)
	if err != nil {
		return err
	}
	streamsJSON, err := json.Marshal(profile.StreamProfiles)
	if err != nil {
		return err
	}
	clipJSON, err := json.Marshal(profile.ClipPolicy)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(profile.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO camera_profiles(camera_id,tank_id,display_name,vendor,host,rtsp_port,http_port,username,password_secret_ref,position,purpose_json,stream_profiles_json,clip_policy_json,status,metadata_json,updated_at,model_id,mounting_height_m,underwater_depth_m,mount_location,view_angle,height_from_water_m,tilt_deg)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(camera_id) DO UPDATE SET
           tank_id=excluded.tank_id,
           display_name=excluded.display_name,
           vendor=excluded.vendor,
           host=excluded.host,
           rtsp_port=excluded.rtsp_port,
           http_port=excluded.http_port,
           username=excluded.username,
           password_secret_ref=excluded.password_secret_ref,
           position=excluded.position,
           purpose_json=excluded.purpose_json,
           stream_profiles_json=excluded.stream_profiles_json,
           clip_policy_json=excluded.clip_policy_json,
           status=excluded.status,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at,
           model_id=excluded.model_id,
           mounting_height_m=excluded.mounting_height_m,
           underwater_depth_m=excluded.underwater_depth_m,
           mount_location=excluded.mount_location,
           view_angle=excluded.view_angle,
           height_from_water_m=excluded.height_from_water_m,
           tilt_deg=excluded.tilt_deg`,
		profile.CameraID,
		nullStr(profile.TankID),
		profile.DisplayName,
		nullStr(profile.Vendor),
		nullStr(profile.Host),
		nullInt(profile.RTSPPort),
		nullInt(profile.HTTPPort),
		nullStr(profile.Username),
		nullStr(profile.PasswordSecretRef),
		nullStr(profile.Position),
		string(purposeJSON),
		string(streamsJSON),
		string(clipJSON),
		profile.Status,
		string(metadataJSON),
		fmtNow(),
		nullStr(profile.ModelID),
		ptrFloat(profile.MountingHeightM),
		ptrFloat(profile.UnderwaterDepthM),
		nullStr(profile.MountLocation),
		nullStr(profile.ViewAngle),
		ptrFloat(profile.HeightFromWaterM),
		ptrFloat(profile.TiltDeg),
	)
	return err
}

// DeleteCameraProfile removes a camera_profiles row by camera_id.
func (s *sqliteStore) DeleteCameraProfile(ctx context.Context, cameraID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM camera_profiles WHERE camera_id=?`, cameraID)
	return err
}

func (s *sqliteStore) GetCameraProfile(ctx context.Context, cameraID string) (*CameraProfile, error) {
	return s.scanCameraProfile(s.db.QueryRowContext(ctx, cameraProfileSelectSQL+` WHERE camera_id=?`, cameraID))
}

func (s *sqliteStore) ListCameraProfiles(ctx context.Context, tankID string) ([]*CameraProfile, error) {
	query := cameraProfileSelectSQL
	args := []any{}
	if tankID != "" {
		query += ` WHERE tank_id=?`
		args = append(args, tankID)
	}
	query += ` ORDER BY tank_id, camera_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CameraProfile{}
	for rows.Next() {
		p, err := scanCameraProfileScanner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const cameraProfileSelectSQL = `SELECT camera_id,COALESCE(tank_id,''),display_name,COALESCE(vendor,''),COALESCE(host,''),rtsp_port,http_port,COALESCE(username,''),COALESCE(password_secret_ref,''),COALESCE(position,''),purpose_json,stream_profiles_json,clip_policy_json,status,metadata_json,updated_at,COALESCE(model_id,''),mounting_height_m,underwater_depth_m,COALESCE(mount_location,''),COALESCE(view_angle,''),height_from_water_m,tilt_deg FROM camera_profiles`

func (s *sqliteStore) scanCameraProfile(row *sql.Row) (*CameraProfile, error) {
	profile, err := scanCameraProfileScanner(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return profile, err
}

func scanCameraProfileScanner(row scanner) (*CameraProfile, error) {
	p := &CameraProfile{}
	var rtspPort, httpPort sql.NullInt64
	var mountingHeight, underwaterDepth, heightFromWater, tilt sql.NullFloat64
	var purposeJSON, streamsJSON, clipJSON, metadataJSON string
	if err := row.Scan(&p.CameraID, &p.TankID, &p.DisplayName, &p.Vendor, &p.Host, &rtspPort, &httpPort, &p.Username, &p.PasswordSecretRef, &p.Position, &purposeJSON, &streamsJSON, &clipJSON, &p.Status, &metadataJSON, &p.UpdatedAt, &p.ModelID, &mountingHeight, &underwaterDepth, &p.MountLocation, &p.ViewAngle, &heightFromWater, &tilt); err != nil {
		return nil, err
	}
	if rtspPort.Valid {
		p.RTSPPort = int(rtspPort.Int64)
	}
	if httpPort.Valid {
		p.HTTPPort = int(httpPort.Int64)
	}
	if mountingHeight.Valid {
		v := mountingHeight.Float64
		p.MountingHeightM = &v
	}
	if underwaterDepth.Valid {
		v := underwaterDepth.Float64
		p.UnderwaterDepthM = &v
	}
	if heightFromWater.Valid {
		v := heightFromWater.Float64
		p.HeightFromWaterM = &v
	}
	if tilt.Valid {
		v := tilt.Float64
		p.TiltDeg = &v
	}
	_ = json.Unmarshal([]byte(purposeJSON), &p.Purpose)
	_ = json.Unmarshal([]byte(streamsJSON), &p.StreamProfiles)
	_ = json.Unmarshal([]byte(clipJSON), &p.ClipPolicy)
	_ = json.Unmarshal([]byte(metadataJSON), &p.Metadata)
	if p.Purpose == nil {
		p.Purpose = []string{}
	}
	if p.StreamProfiles == nil {
		p.StreamProfiles = map[string]any{}
	}
	if p.ClipPolicy == nil {
		p.ClipPolicy = map[string]any{}
	}
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	return p, nil
}
