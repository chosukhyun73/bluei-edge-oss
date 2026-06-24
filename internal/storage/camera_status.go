package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *sqliteStore) UpsertCameraStatus(ctx context.Context, camera *CurrentCameraStatus, detailsJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_camera_status(camera_id,tank_id,status,ingest_fps,last_event_id,last_frame_at,reconnect_count,dropped_frames,updated_at,details_json)
         VALUES(?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(camera_id) DO UPDATE SET
           tank_id=excluded.tank_id,
           status=excluded.status,
           ingest_fps=excluded.ingest_fps,
           last_event_id=excluded.last_event_id,
           last_frame_at=excluded.last_frame_at,
           reconnect_count=excluded.reconnect_count,
           dropped_frames=excluded.dropped_frames,
           updated_at=excluded.updated_at,
           details_json=excluded.details_json`,
		camera.CameraID,
		nullableString(camera.TankID),
		camera.Status,
		camera.IngestFPS,
		camera.LastEventID,
		nullableString(camera.LastFrameAt),
		camera.ReconnectCount,
		camera.DroppedFrames,
		fmtNow(),
		detailsJSON,
	)
	return err
}

func (s *sqliteStore) ListCameraStatuses(ctx context.Context, tankID string) ([]*CurrentCameraStatus, error) {
	query := `SELECT camera_id,COALESCE(tank_id,''),status,ingest_fps,last_event_id,COALESCE(last_frame_at,''),reconnect_count,dropped_frames,updated_at,details_json
         FROM current_camera_status`
	args := []any{}
	if tankID != "" {
		query += ` WHERE tank_id=?`
		args = append(args, tankID)
	}
	query += ` ORDER BY camera_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*CurrentCameraStatus, 0)
	for rows.Next() {
		camera := &CurrentCameraStatus{}
		var detailsJSON string
		if err := rows.Scan(&camera.CameraID, &camera.TankID, &camera.Status, &camera.IngestFPS, &camera.LastEventID, &camera.LastFrameAt, &camera.ReconnectCount, &camera.DroppedFrames, &camera.UpdatedAt, &detailsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(detailsJSON), &camera.Details); err != nil {
			camera.Details = map[string]any{}
		}
		out = append(out, camera)
	}
	return out, rows.Err()
}

func nullableString(v string) any {
	if v == "" {
		return sql.NullString{}
	}
	return v
}
