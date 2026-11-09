// Package staff manages the staff pool: who exists, their contact details,
// desire to work, which weekdays they're available, and which shifts they're
// capable of covering.
package staff

import (
	"context"
	"database/sql"
	"fmt"
	"math/bits"

	"crewtopia/internal/weekday"
)

// Staff is one row in the staff pool.
type Staff struct {
	ID              int64
	Name            string
	Nickname        string
	ContactNumber   string
	Desire          float64 // percentage, e.g. 38.00
	AvailableDays   weekday.Mask
	CapableShiftIDs []int64
}

// WithStats decorates a Staff with values derived from other tables, so they
// never need to be kept in sync manually.
type WithStats struct {
	Staff
	ShiftsAvailable int // popcount(AvailableDays), 0-7
	ShiftsTaken     int // count of roster_assignment rows ever assigned to them
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List(ctx context.Context) ([]WithStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.id, st.name, st.nickname, st.contact_number, st.desire, st.available_days,
		       (SELECT COUNT(*) FROM roster_assignment ra WHERE ra.staff_id = st.id) AS shifts_taken
		FROM staff st
		ORDER BY st.name`)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()

	var out []WithStats
	for rows.Next() {
		var w WithStats
		if err := rows.Scan(&w.ID, &w.Name, &w.Nickname, &w.ContactNumber, &w.Desire, &w.AvailableDays, &w.ShiftsTaken); err != nil {
			return nil, fmt.Errorf("scan staff: %w", err)
		}
		w.ShiftsAvailable = bits.OnesCount(uint(w.AvailableDays))
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (*Staff, error) {
	var st Staff
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, nickname, contact_number, desire, available_days
		FROM staff WHERE id = ?`, id,
	).Scan(&st.ID, &st.Name, &st.Nickname, &st.ContactNumber, &st.Desire, &st.AvailableDays)
	if err != nil {
		return nil, fmt.Errorf("get staff %d: %w", id, err)
	}

	shiftIDs, err := s.CapableShiftIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	st.CapableShiftIDs = shiftIDs
	return &st, nil
}

// Save inserts st if ID is zero, otherwise updates the existing row. It also
// replaces the staff_shift_capability rows to match st.CapableShiftIDs.
func (s *Store) Save(ctx context.Context, st *Staff) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if st.ID == 0 {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO staff (name, nickname, contact_number, desire, available_days)
			VALUES (?, ?, ?, ?, ?)`,
			st.Name, st.Nickname, st.ContactNumber, st.Desire, st.AvailableDays)
		if err != nil {
			return fmt.Errorf("insert staff: %w", err)
		}
		st.ID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("staff last insert id: %w", err)
		}
	} else {
		_, err := tx.ExecContext(ctx, `
			UPDATE staff SET name = ?, nickname = ?, contact_number = ?, desire = ?, available_days = ?
			WHERE id = ?`,
			st.Name, st.Nickname, st.ContactNumber, st.Desire, st.AvailableDays, st.ID)
		if err != nil {
			return fmt.Errorf("update staff: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM staff_shift_capability WHERE staff_id = ?`, st.ID); err != nil {
		return fmt.Errorf("clear capabilities: %w", err)
	}
	for _, shiftID := range st.CapableShiftIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_shift_capability (staff_id, shift_id) VALUES (?, ?)`,
			st.ID, shiftID); err != nil {
			return fmt.Errorf("insert capability: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM staff WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete staff %d: %w", id, err)
	}
	return nil
}

// ListFull returns every staff member with CapableShiftIDs populated, for
// callers (like roster generation) that need the full picture rather than the
// list-page stats. It batches the capability lookup instead of querying per
// staff member.
func (s *Store) ListFull(ctx context.Context) ([]Staff, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, nickname, contact_number, desire, available_days
		FROM staff ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]*Staff)
	var order []int64
	for rows.Next() {
		var st Staff
		if err := rows.Scan(&st.ID, &st.Name, &st.Nickname, &st.ContactNumber, &st.Desire, &st.AvailableDays); err != nil {
			return nil, fmt.Errorf("scan staff: %w", err)
		}
		byID[st.ID] = &st
		order = append(order, st.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	capRows, err := s.db.QueryContext(ctx, `SELECT staff_id, shift_id FROM staff_shift_capability`)
	if err != nil {
		return nil, fmt.Errorf("list capabilities: %w", err)
	}
	defer capRows.Close()
	for capRows.Next() {
		var staffID, shiftID int64
		if err := capRows.Scan(&staffID, &shiftID); err != nil {
			return nil, fmt.Errorf("scan capability: %w", err)
		}
		if st, ok := byID[staffID]; ok {
			st.CapableShiftIDs = append(st.CapableShiftIDs, shiftID)
		}
	}
	if err := capRows.Err(); err != nil {
		return nil, err
	}

	out := make([]Staff, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func (s *Store) CapableShiftIDs(ctx context.Context, staffID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT shift_id FROM staff_shift_capability WHERE staff_id = ? ORDER BY shift_id`, staffID)
	if err != nil {
		return nil, fmt.Errorf("list capabilities: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan capability: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
