package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// HatcheryTreatment — 종묘장 처치 기록(투약/약품 CTE). 산란/자어 batch 단위.
// lot_code(알/종자 lot)로 GDST 수출규제 KDE(휴약·물질·용량)에 귀속된다.
type HatcheryTreatment struct {
	TreatmentID     string         `json:"treatment_id"`
	GroupID         string         `json:"group_id"`
	SubjectKind     string         `json:"subject_kind"` // spawn | larval
	BatchID         string         `json:"batch_id,omitempty"`
	LotCode         string         `json:"lot_code,omitempty"`
	TankID          string         `json:"tank_id,omitempty"`
	Species         string         `json:"species,omitempty"`
	TreatmentType   string         `json:"treatment_type"`
	Substance       string         `json:"substance"`
	Dose            float64        `json:"dose,omitempty"`
	DoseUnit        string         `json:"dose_unit,omitempty"`
	Route           string         `json:"route,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	WithdrawalUntil string         `json:"withdrawal_until,omitempty"`
	AdministeredAt  string         `json:"administered_at"`
	OperatorID      string         `json:"operator_id,omitempty"`
	ItemID          string         `json:"item_id,omitempty"`      // 오프라인 전용(재고)
	ConsumedQty     float64        `json:"consumed_qty,omitempty"` // 오프라인 전용(원가)
	Notes           string         `json:"notes,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
}

const hatcheryTreatmentCols = `treatment_id,group_id,subject_kind,batch_id,lot_code,tank_id,species,` +
	`treatment_type,substance,dose,dose_unit,route,reason,withdrawal_until,administered_at,` +
	`operator_id,item_id,consumed_qty,notes,metadata_json,created_at,updated_at`

func (s *sqliteStore) UpsertHatcheryTreatment(ctx context.Context, t *HatcheryTreatment) error {
	metaJSON, err := json.Marshal(t.Metadata)
	if err != nil {
		return err
	}
	now := fmtNow()
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO hatchery_treatments(`+hatcheryTreatmentCols+`)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(treatment_id) DO UPDATE SET
           group_id=excluded.group_id, subject_kind=excluded.subject_kind,
           batch_id=excluded.batch_id, lot_code=excluded.lot_code, tank_id=excluded.tank_id,
           species=excluded.species, treatment_type=excluded.treatment_type,
           substance=excluded.substance, dose=excluded.dose, dose_unit=excluded.dose_unit,
           route=excluded.route, reason=excluded.reason, withdrawal_until=excluded.withdrawal_until,
           administered_at=excluded.administered_at, operator_id=excluded.operator_id,
           item_id=excluded.item_id, consumed_qty=excluded.consumed_qty, notes=excluded.notes,
           metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		t.TreatmentID, t.GroupID, t.SubjectKind, t.BatchID, t.LotCode, t.TankID, t.Species,
		t.TreatmentType, t.Substance, t.Dose, t.DoseUnit, t.Route, t.Reason, t.WithdrawalUntil,
		t.AdministeredAt, t.OperatorID, t.ItemID, t.ConsumedQty, t.Notes, string(metaJSON),
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) GetHatcheryTreatment(ctx context.Context, treatmentID string) (*HatcheryTreatment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+hatcheryTreatmentCols+` FROM hatchery_treatments WHERE treatment_id=?`, treatmentID)
	t, err := scanHatcheryTreatment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *sqliteStore) ListHatcheryTreatmentsByGroup(ctx context.Context, groupID string) ([]*HatcheryTreatment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+hatcheryTreatmentCols+` FROM hatchery_treatments WHERE group_id=? ORDER BY administered_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*HatcheryTreatment{}
	for rows.Next() {
		t, err := scanHatcheryTreatment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteHatcheryTreatment(ctx context.Context, treatmentID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM hatchery_treatments WHERE treatment_id=?`, treatmentID)
	return err
}

func scanHatcheryTreatment(row groupScanner) (*HatcheryTreatment, error) {
	t := &HatcheryTreatment{}
	var batchID, lotCode, tankID, species, doseUnit, route, reason, withdrawal sql.NullString
	var operatorID, itemID, notes, metaJSON sql.NullString
	var dose, consumedQty sql.NullFloat64
	if err := row.Scan(
		&t.TreatmentID, &t.GroupID, &t.SubjectKind, &batchID, &lotCode, &tankID, &species,
		&t.TreatmentType, &t.Substance, &dose, &doseUnit, &route, &reason, &withdrawal,
		&t.AdministeredAt, &operatorID, &itemID, &consumedQty, &notes, &metaJSON,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	t.BatchID = batchID.String
	t.LotCode = lotCode.String
	t.TankID = tankID.String
	t.Species = species.String
	t.Dose = dose.Float64
	t.DoseUnit = doseUnit.String
	t.Route = route.String
	t.Reason = reason.String
	t.WithdrawalUntil = withdrawal.String
	t.OperatorID = operatorID.String
	t.ItemID = itemID.String
	t.ConsumedQty = consumedQty.Float64
	t.Notes = notes.String
	_ = json.Unmarshal([]byte(metaJSON.String), &t.Metadata)
	if t.Metadata == nil {
		t.Metadata = map[string]any{}
	}
	return t, nil
}
