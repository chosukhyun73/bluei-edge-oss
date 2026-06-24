package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *sqliteStore) UpsertInventoryItem(ctx context.Context, item *InventoryItem) error {
	var reorder any
	if item.ReorderLevel != nil {
		reorder = *item.ReorderLevel
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO inventory_items(
		   item_id,category,name,unit,on_hand_qty,spec,supplier,reorder_level,notes,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(item_id) DO UPDATE SET
		   category=excluded.category,
		   name=excluded.name,
		   unit=excluded.unit,
		   on_hand_qty=excluded.on_hand_qty,
		   spec=excluded.spec,
		   supplier=excluded.supplier,
		   reorder_level=excluded.reorder_level,
		   notes=excluded.notes,
		   updated_at=excluded.updated_at`,
		item.ItemID,
		item.Category,
		item.Name,
		item.Unit,
		item.OnHandQty,
		nullStr(item.Spec),
		nullStr(item.Supplier),
		reorder,
		nullStr(item.Notes),
		fmtTime(item.CreatedAt),
		fmtTime(item.UpdatedAt),
	)
	return err
}

func (s *sqliteStore) GetInventoryItem(ctx context.Context, itemID string) (*InventoryItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT item_id,category,name,unit,on_hand_qty,COALESCE(spec,''),COALESCE(supplier,''),
		        reorder_level,COALESCE(notes,''),created_at,updated_at
		 FROM inventory_items WHERE item_id=?`, itemID)
	return scanInventoryItem(row)
}

func (s *sqliteStore) ListInventoryItems(ctx context.Context, category string) ([]*InventoryItem, error) {
	query := `SELECT item_id,category,name,unit,on_hand_qty,COALESCE(spec,''),COALESCE(supplier,''),
	                 reorder_level,COALESCE(notes,''),created_at,updated_at
	          FROM inventory_items`
	args := []any{}
	if category != "" {
		query += ` WHERE category=?`
		args = append(args, category)
	}
	query += ` ORDER BY category, name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*InventoryItem
	for rows.Next() {
		item, scanErr := scanInventoryRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// AdjustInventoryOnHand — 원자적 증감(구매 +, 사용 −). 갱신 후 on_hand 반환.
func (s *sqliteStore) AdjustInventoryOnHand(ctx context.Context, itemID string, delta float64) (float64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE inventory_items SET on_hand_qty = on_hand_qty + ?, updated_at=? WHERE item_id=?`,
		delta, fmtNow(), itemID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Errorf("inventory item not found: %s", itemID)
	}
	var onHand float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT on_hand_qty FROM inventory_items WHERE item_id=?`, itemID).Scan(&onHand); err != nil {
		return 0, err
	}
	return onHand, nil
}

func scanInventoryItem(row *sql.Row) (*InventoryItem, error) {
	item, err := scanInventoryRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanInventoryRow(sc rowScanner) (*InventoryItem, error) {
	item := &InventoryItem{}
	var reorder sql.NullFloat64
	var createdAt, updatedAt string
	if err := sc.Scan(
		&item.ItemID, &item.Category, &item.Name, &item.Unit, &item.OnHandQty,
		&item.Spec, &item.Supplier, &reorder, &item.Notes, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if reorder.Valid {
		v := reorder.Float64
		item.ReorderLevel = &v
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return item, nil
}
