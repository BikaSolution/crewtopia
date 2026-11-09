// Package shift manages the set of shift definitions (name, time range, and
// which weekdays each one runs on).
package shift

import (
	"context"
	"database/sql"
	"fmt"

	"crewtopia/internal/weekday"
)

// Shift is one row in the shifts setup page, e.g. "Barman, 17:00, 21:00".
type Shift struct {
	ID         int64
	Name       string
	StartTime  string // "HH:MM", 24h
	EndTime    string // "HH:MM", 24h
	ActiveDays weekday.Mask
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List(ctx context.Context) ([]Shift, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, start_time, end_time, active_days
		FROM shift ORDER BY start_time, name`)
	if err != nil {
		return nil, fmt.Errorf("list shifts: %w", err)
	}
	defer rows.Close()

	var out []Shift
	for rows.Next() {
		var sh Shift
		if err := rows.Scan(&sh.ID, &sh.Name, &sh.StartTime, &sh.EndTime, &sh.ActiveDays); err != nil {
			return nil, fmt.Errorf("scan shift: %w", err)
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (*Shift, error) {
	var sh Shift
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, start_time, end_time, active_days
		FROM shift WHERE id = ?`, id,
	).Scan(&sh.ID, &sh.Name, &sh.StartTime, &sh.EndTime, &sh.ActiveDays)
	if err != nil {
		return nil, fmt.Errorf("get shift %d: %w", id, err)
	}
	return &sh, nil
}

func (s *Store) Save(ctx context.Context, sh *Shift) error {
	if sh.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO shift (name, start_time, end_time, active_days)
			VALUES (?, ?, ?, ?)`,
			sh.Name, sh.StartTime, sh.EndTime, sh.ActiveDays)
		if err != nil {
			return fmt.Errorf("insert shift: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("shift last insert id: %w", err)
		}
		sh.ID = id
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE shift SET name = ?, start_time = ?, end_time = ?, active_days = ?
		WHERE id = ?`,
		sh.Name, sh.StartTime, sh.EndTime, sh.ActiveDays, sh.ID)
	if err != nil {
		return fmt.Errorf("update shift: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM shift WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete shift %d: %w", id, err)
	}
	return nil
}
