// Package roster generates weekly rosters and lets an admin manually
// reassign individual slots afterwards.
package roster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"crewtopia/internal/shift"
	"crewtopia/internal/staff"
	"crewtopia/internal/weekday"
)

// adjacencyPenalty multiplies a candidate's score when they already have a
// shift immediately before/after the one being filled that day: it makes
// back-to-back shifts less likely to win, without excluding the candidate.
const adjacencyPenalty = 0.35

// Assignment is one shift slot on one day of a generated roster.
type Assignment struct {
	ID        int64
	ShiftID   int64
	ShiftName string
	StartTime string
	EndTime   string
	Weekday   weekday.Mask
	StaffID   *int64
	StaffName string // "" when unfilled
}

func (a Assignment) Unfilled() bool { return a.StaffID == nil }

// StaffIDValue returns the assigned staff ID, or 0 (never a real ID, since
// SQLite AUTOINCREMENT starts at 1) when the slot is unfilled — handy for
// template comparisons where a *int64 is awkward to compare against.
func (a Assignment) StaffIDValue() int64 {
	if a.StaffID == nil {
		return 0
	}
	return *a.StaffID
}

// CellView is one roster slot plus the candidate list for its reassignment
// dropdown — the unit rendered by the "roster_cell" partial template, both
// inline on the editable roster page and standalone after an htmx reassign.
type CellView struct {
	Assignment
	Options []EligibleStaff
	Error   string
	// CSRFToken is set by the handler, not the store — CellView is rendered
	// both inline (as part of the page's own template data, which already
	// carries it) and standalone after an htmx reassign (where it doesn't),
	// so it's carried on the value itself rather than assumed available from
	// an enclosing template's root data.
	CSRFToken string
}

type Roster struct {
	ID            int64
	WeekStartDate string
}

// DayView groups a roster's assignments by weekday for rendering.
type DayView struct {
	Bit         weekday.Mask
	Label       string
	Assignments []Assignment
}

type WeekView struct {
	Roster *Roster
	Days   []DayView
}

// EligibleStaff is a candidate offered in the manual-reassignment dropdown.
type EligibleStaff struct {
	ID       int64
	Name     string
	Eligible bool // available that day AND marked capable of this shift
}

type Store struct {
	db    *sql.DB
	staff *staff.Store
	shift *shift.Store
}

func NewStore(db *sql.DB, staffStore *staff.Store, shiftStore *shift.Store) *Store {
	return &Store{db: db, staff: staffStore, shift: shiftStore}
}

func clockMinutes(hhmm string) (int, error) {
	h, m, ok := strings.Cut(hhmm, ":")
	if !ok {
		return 0, fmt.Errorf("invalid time %q", hhmm)
	}
	hh, err := strconv.Atoi(h)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q: %w", hhmm, err)
	}
	mm, err := strconv.Atoi(m)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q: %w", hhmm, err)
	}
	return hh*60 + mm, nil
}

// overlaps reports true overlap; touching endpoints (16:00-17:00 followed by
// 17:00-18:00) are not considered overlapping.
func overlaps(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

func adjacent(aStart, aEnd, bStart, bEnd int) bool {
	return aEnd == bStart || bEnd == aStart
}

type interval struct{ start, end int }

// dayAssignment is the pure-logic result for one shift slot, before it's
// written to (or read back from) the database.
type dayAssignment struct {
	ShiftID int64
	StaffID *int64
}

// assignDay picks a staff member for each of dayShifts (already filtered to
// shifts active on dayBit, sorted by start time). It has no database
// dependency so it can be property-tested directly with a seeded randFloat64.
func assignDay(dayBit weekday.Mask, dayShifts []shift.Shift, allStaff []staff.Staff, capable map[int64]map[int64]bool, randFloat64 func() float64) ([]dayAssignment, error) {
	assignedToday := make(map[int64][]interval)
	out := make([]dayAssignment, 0, len(dayShifts))

	for _, sh := range dayShifts {
		shStart, err := clockMinutes(sh.StartTime)
		if err != nil {
			return nil, err
		}
		shEnd, err := clockMinutes(sh.EndTime)
		if err != nil {
			return nil, err
		}

		var bestStaffID int64
		bestFound := false
		bestScore := -1.0

		for _, st := range allStaff {
			if !st.AvailableDays.Has(dayBit) || !capable[st.ID][sh.ID] {
				continue
			}

			excluded := false
			adjacentHit := false
			for _, iv := range assignedToday[st.ID] {
				if overlaps(shStart, shEnd, iv.start, iv.end) {
					excluded = true
					break
				}
				if adjacent(shStart, shEnd, iv.start, iv.end) {
					adjacentHit = true
				}
			}
			if excluded {
				continue
			}

			score := randFloat64() * st.Desire
			if adjacentHit {
				score *= adjacencyPenalty
			}
			if score > bestScore {
				bestScore = score
				bestStaffID = st.ID
				bestFound = true
			}
		}

		da := dayAssignment{ShiftID: sh.ID}
		if bestFound {
			id := bestStaffID
			da.StaffID = &id
			assignedToday[bestStaffID] = append(assignedToday[bestStaffID], interval{start: shStart, end: shEnd})
		}
		out = append(out, da)
	}

	return out, nil
}

// Generate (re)builds the roster for the week starting weekStartDate (an ISO
// date, e.g. "2026-08-03"), replacing any assignments already generated for
// that week.
func (s *Store) Generate(ctx context.Context, weekStartDate string) (*Roster, error) {
	return s.generate(ctx, weekStartDate, rand.Float64)
}

// generate is the testable core: randFloat64 is injected so tests can seed
// a deterministic source instead of the real RNG.
func (s *Store) generate(ctx context.Context, weekStartDate string, randFloat64 func() float64) (*Roster, error) {
	allStaff, err := s.staff.ListFull(ctx)
	if err != nil {
		return nil, fmt.Errorf("load staff: %w", err)
	}
	shifts, err := s.shift.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load shifts: %w", err)
	}

	capable := make(map[int64]map[int64]bool, len(allStaff))
	for _, st := range allStaff {
		set := make(map[int64]bool, len(st.CapableShiftIDs))
		for _, id := range st.CapableShiftIDs {
			set[id] = true
		}
		capable[st.ID] = set
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO roster (week_start_date) VALUES (?)
		ON CONFLICT(week_start_date) DO NOTHING`, weekStartDate); err != nil {
		return nil, fmt.Errorf("upsert roster: %w", err)
	}
	var rosterID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM roster WHERE week_start_date = ?`, weekStartDate).Scan(&rosterID); err != nil {
		return nil, fmt.Errorf("load roster id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roster_assignment WHERE roster_id = ?`, rosterID); err != nil {
		return nil, fmt.Errorf("clear existing assignments: %w", err)
	}

	for _, day := range weekday.Ordered {
		var dayShifts []shift.Shift
		for _, sh := range shifts {
			if sh.ActiveDays.Has(day.Bit) {
				dayShifts = append(dayShifts, sh)
			}
		}
		sort.Slice(dayShifts, func(i, j int) bool { return dayShifts[i].StartTime < dayShifts[j].StartTime })

		results, err := assignDay(day.Bit, dayShifts, allStaff, capable, randFloat64)
		if err != nil {
			return nil, err
		}

		for _, r := range results {
			var staffIDArg sql.NullInt64
			if r.StaffID != nil {
				staffIDArg = sql.NullInt64{Int64: *r.StaffID, Valid: true}
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO roster_assignment (roster_id, shift_id, weekday, staff_id)
				VALUES (?, ?, ?, ?)`,
				rosterID, r.ShiftID, int(day.Bit), staffIDArg); err != nil {
				return nil, fmt.Errorf("insert assignment: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &Roster{ID: rosterID, WeekStartDate: weekStartDate}, nil
}

// GetByWeek loads the roster for weekStartDate, or an empty WeekView if one
// hasn't been generated yet.
func (s *Store) GetByWeek(ctx context.Context, weekStartDate string) (*WeekView, error) {
	var rosterID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM roster WHERE week_start_date = ?`, weekStartDate).Scan(&rosterID)
	if errors.Is(err, sql.ErrNoRows) {
		view := &WeekView{Roster: &Roster{WeekStartDate: weekStartDate}}
		for _, d := range weekday.Ordered {
			view.Days = append(view.Days, DayView{Bit: d.Bit, Label: d.Label})
		}
		return view, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load roster: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ra.id, ra.shift_id, sh.name, sh.start_time, sh.end_time, ra.weekday, ra.staff_id, COALESCE(st.name, '')
		FROM roster_assignment ra
		JOIN shift sh ON sh.id = ra.shift_id
		LEFT JOIN staff st ON st.id = ra.staff_id
		WHERE ra.roster_id = ?
		ORDER BY ra.weekday, sh.start_time`, rosterID)
	if err != nil {
		return nil, fmt.Errorf("load assignments: %w", err)
	}
	defer rows.Close()

	byDay := make(map[weekday.Mask][]Assignment)
	for rows.Next() {
		var a Assignment
		var staffID sql.NullInt64
		var wd int
		if err := rows.Scan(&a.ID, &a.ShiftID, &a.ShiftName, &a.StartTime, &a.EndTime, &wd, &staffID, &a.StaffName); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		a.Weekday = weekday.Mask(wd)
		if staffID.Valid {
			id := staffID.Int64
			a.StaffID = &id
		}
		byDay[a.Weekday] = append(byDay[a.Weekday], a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	view := &WeekView{Roster: &Roster{ID: rosterID, WeekStartDate: weekStartDate}}
	for _, d := range weekday.Ordered {
		view.Days = append(view.Days, DayView{Bit: d.Bit, Label: d.Label, Assignments: byDay[d.Bit]})
	}
	return view, nil
}

// GetAssignment loads a single roster_assignment row, joined with its shift
// and (if filled) staff names, for re-rendering after a reassign.
func (s *Store) GetAssignment(ctx context.Context, id int64) (*Assignment, error) {
	var a Assignment
	var staffID sql.NullInt64
	var wd int
	err := s.db.QueryRowContext(ctx, `
		SELECT ra.id, ra.shift_id, sh.name, sh.start_time, sh.end_time, ra.weekday, ra.staff_id, COALESCE(st.name, '')
		FROM roster_assignment ra
		JOIN shift sh ON sh.id = ra.shift_id
		LEFT JOIN staff st ON st.id = ra.staff_id
		WHERE ra.id = ?`, id,
	).Scan(&a.ID, &a.ShiftID, &a.ShiftName, &a.StartTime, &a.EndTime, &wd, &staffID, &a.StaffName)
	if err != nil {
		return nil, fmt.Errorf("get assignment %d: %w", id, err)
	}
	a.Weekday = weekday.Mask(wd)
	if staffID.Valid {
		v := staffID.Int64
		a.StaffID = &v
	}
	return &a, nil
}

// DayCells is one weekday's worth of CellViews, ready to render.
type DayCells struct {
	Bit   weekday.Mask
	Label string
	Cells []CellView
}

// GetByWeekWithOptions is GetByWeek plus, for every slot, the eligible-staff
// dropdown options the editable roster page needs.
func (s *Store) GetByWeekWithOptions(ctx context.Context, weekStartDate string) (*Roster, []DayCells, error) {
	view, err := s.GetByWeek(ctx, weekStartDate)
	if err != nil {
		return nil, nil, err
	}

	days := make([]DayCells, 0, len(view.Days))
	for _, d := range view.Days {
		dc := DayCells{Bit: d.Bit, Label: d.Label}
		for _, a := range d.Assignments {
			opts, err := s.StaffOptionsForAssignment(ctx, a.ID)
			if err != nil {
				return nil, nil, err
			}
			dc.Cells = append(dc.Cells, CellView{Assignment: a, Options: opts})
		}
		days = append(days, dc)
	}
	return view.Roster, days, nil
}

// MonthlyRow is one CSV export row: an assignment resolved to its actual
// calendar date (roster_assignment only stores a week_start_date + weekday
// bit, not a date, so this is where the two get combined).
type MonthlyRow struct {
	Date      string // ISO date
	Weekday   string // "Monday"
	ShiftName string
	StartTime string
	EndTime   string
	StaffName string // "" when unfilled
}

// offsetOf returns how many days after Monday the given day bit falls
// (Monday=0 ... Sunday=6), matching weekday.Ordered's position.
func offsetOf(bit weekday.Mask) int {
	for i, d := range weekday.Ordered {
		if d.Bit == bit {
			return i
		}
	}
	return 0
}

// MonthlyAssignments returns every assignment whose actual calendar date
// falls within the given month, across however many weekly rosters that
// spans, sorted by date.
func (s *Store) MonthlyAssignments(ctx context.Context, year int, month time.Month) ([]MonthlyRow, error) {
	firstOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)

	// A week's Monday can fall up to 6 days before the month starts and
	// still have days that land inside it, so widen the roster-row scan.
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.week_start_date, ra.weekday, sh.name, sh.start_time, sh.end_time, COALESCE(st.name, '')
		FROM roster_assignment ra
		JOIN roster r ON r.id = ra.roster_id
		JOIN shift sh ON sh.id = ra.shift_id
		LEFT JOIN staff st ON st.id = ra.staff_id
		WHERE r.week_start_date >= ? AND r.week_start_date <= ?
		ORDER BY r.week_start_date, ra.weekday, sh.start_time`,
		firstOfMonth.AddDate(0, 0, -6).Format(isoDate), lastOfMonth.Format(isoDate))
	if err != nil {
		return nil, fmt.Errorf("query monthly assignments: %w", err)
	}
	defer rows.Close()

	var out []MonthlyRow
	for rows.Next() {
		var weekStartDate, shiftName, startTime, endTime, staffName string
		var wd int
		if err := rows.Scan(&weekStartDate, &wd, &shiftName, &startTime, &endTime, &staffName); err != nil {
			return nil, fmt.Errorf("scan monthly assignment: %w", err)
		}

		weekStart, err := time.Parse(isoDate, weekStartDate)
		if err != nil {
			return nil, fmt.Errorf("parse week_start_date %q: %w", weekStartDate, err)
		}
		date := weekStart.AddDate(0, 0, offsetOf(weekday.Mask(wd)))
		if date.Before(firstOfMonth) || date.After(lastOfMonth) {
			continue
		}

		out = append(out, MonthlyRow{
			Date:      date.Format(isoDate),
			Weekday:   date.Weekday().String(),
			ShiftName: shiftName,
			StartTime: startTime,
			EndTime:   endTime,
			StaffName: staffName,
		})
	}
	return out, rows.Err()
}

// PrintColumn is one day/date header in a transposed roster table (used by
// both the printed page and the CSV export): shift down the rows, day across
// the columns.
type PrintColumn struct {
	Short string // "Mon"
	Date  string // "Aug 3"
}

// PrintCell is one (shift, day) intersection.
type PrintCell struct {
	Scheduled bool   // does this shift run on this day at all
	Name      string // staff name; "" + Scheduled=true means unfilled
}

type PrintRow struct {
	ShiftName string
	StartTime string
	EndTime   string
	Cells     []PrintCell
}

type PrintTable struct {
	Columns []PrintColumn
	Rows    []PrintRow
}

type matrixEntry struct {
	colKey, colShort, colDate                string
	shiftName, startTime, endTime, staffName string
}

// buildMatrix turns a flat list of (day, shift, staff) entries into a
// shift-rows × day-columns table. If columns is non-nil it's used as-is
// (weekly print wants exactly 7 columns, Mon–Sun, even for days with no
// shifts at all); otherwise columns are derived from entries in the order
// first seen (monthly CSV export — only dates that actually have a shift
// scheduled are worth a column).
func buildMatrix(entries []matrixEntry, columns []PrintColumn, columnKeys []string) PrintTable {
	var table PrintTable
	colIdx := map[string]int{}

	if columns != nil {
		table.Columns = columns
		for i, k := range columnKeys {
			colIdx[k] = i
		}
	} else {
		for _, e := range entries {
			if _, ok := colIdx[e.colKey]; !ok {
				colIdx[e.colKey] = len(table.Columns)
				table.Columns = append(table.Columns, PrintColumn{Short: e.colShort, Date: e.colDate})
			}
		}
	}

	type rowKey struct{ name, start, end string }
	rowIdx := map[rowKey]int{}
	for _, e := range entries {
		rk := rowKey{e.shiftName, e.startTime, e.endTime}
		if _, ok := rowIdx[rk]; !ok {
			rowIdx[rk] = len(table.Rows)
			table.Rows = append(table.Rows, PrintRow{
				ShiftName: e.shiftName,
				StartTime: e.startTime,
				EndTime:   e.endTime,
				Cells:     make([]PrintCell, len(table.Columns)),
			})
		}
	}

	for _, e := range entries {
		ci, ok := colIdx[e.colKey]
		if !ok {
			continue
		}
		ri := rowIdx[rowKey{e.shiftName, e.startTime, e.endTime}]
		table.Rows[ri].Cells[ci] = PrintCell{Scheduled: true, Name: e.staffName}
	}

	sort.Slice(table.Rows, func(i, j int) bool {
		if table.Rows[i].StartTime != table.Rows[j].StartTime {
			return table.Rows[i].StartTime < table.Rows[j].StartTime
		}
		return table.Rows[i].ShiftName < table.Rows[j].ShiftName
	})

	return table
}

func shortLabel(s string) string {
	if len(s) > 3 {
		return s[:3]
	}
	return s
}

// buildWeekPrintTable transposes a week's DayViews into a shift × day table,
// for the printed version of the roster (the on-screen version stays
// day-cards-with-shifts, this is print/CSV only).
func buildWeekPrintTable(weekStart string, days []DayView) PrintTable {
	weekStartTime, _ := time.Parse(isoDate, weekStart)

	columns := make([]PrintColumn, len(days))
	columnKeys := make([]string, len(days))
	for i, d := range days {
		columns[i] = PrintColumn{
			Short: shortLabel(d.Label),
			Date:  weekStartTime.AddDate(0, 0, i).Format("Jan 2"),
		}
		columnKeys[i] = d.Label
	}

	var entries []matrixEntry
	for _, d := range days {
		for _, a := range d.Assignments {
			entries = append(entries, matrixEntry{
				colKey:    d.Label,
				shiftName: a.ShiftName,
				startTime: a.StartTime,
				endTime:   a.EndTime,
				staffName: a.StaffName,
			})
		}
	}

	return buildMatrix(entries, columns, columnKeys)
}

// buildMonthlyPrintTable transposes a month's worth of MonthlyRows (already
// resolved to actual calendar dates) into a shift × day table for CSV export.
func buildMonthlyPrintTable(rows []MonthlyRow) PrintTable {
	var entries []matrixEntry
	for _, r := range rows {
		dateLabel := r.Date
		if t, err := time.Parse(isoDate, r.Date); err == nil {
			dateLabel = t.Format("Jan 2")
		}
		entries = append(entries, matrixEntry{
			colKey:    r.Date,
			colShort:  shortLabel(r.Weekday),
			colDate:   dateLabel,
			shiftName: r.ShiftName,
			startTime: r.StartTime,
			endTime:   r.EndTime,
			staffName: r.StaffName,
		})
	}
	return buildMatrix(entries, nil, nil)
}

// StaffOptionsForAssignment lists every staff member for the reassignment
// dropdown, flagged Eligible (available that day and marked capable of the
// shift) so the template can sort/highlight accordingly. Reassign itself only
// hard-blocks genuine time overlaps, so an admin can still deliberately pick
// an "ineligible" candidate for an edge case.
func (s *Store) StaffOptionsForAssignment(ctx context.Context, assignmentID int64) ([]EligibleStaff, error) {
	var shiftID int64
	var wd int
	if err := s.db.QueryRowContext(ctx, `SELECT shift_id, weekday FROM roster_assignment WHERE id = ?`, assignmentID).Scan(&shiftID, &wd); err != nil {
		return nil, fmt.Errorf("load assignment: %w", err)
	}

	allStaff, err := s.staff.ListFull(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]EligibleStaff, 0, len(allStaff))
	for _, st := range allStaff {
		eligible := st.AvailableDays.Has(weekday.Mask(wd))
		if eligible {
			capable := false
			for _, id := range st.CapableShiftIDs {
				if id == shiftID {
					capable = true
					break
				}
			}
			eligible = capable
		}
		out = append(out, EligibleStaff{ID: st.ID, Name: st.Name, Eligible: eligible})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Eligible != out[j].Eligible {
			return out[i].Eligible
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Reassign sets (or clears, if staffID is nil) the staff member on one
// assignment. The only hard rule enforced is no time overlap with another
// shift that staff member already has that same day.
func (s *Store) Reassign(ctx context.Context, assignmentID int64, staffID *int64) error {
	var rosterID, shiftID int64
	var wd int
	if err := s.db.QueryRowContext(ctx, `SELECT roster_id, shift_id, weekday FROM roster_assignment WHERE id = ?`, assignmentID).Scan(&rosterID, &shiftID, &wd); err != nil {
		return fmt.Errorf("load assignment: %w", err)
	}

	if staffID != nil {
		sh, err := s.shift.Get(ctx, shiftID)
		if err != nil {
			return err
		}
		shStart, err := clockMinutes(sh.StartTime)
		if err != nil {
			return err
		}
		shEnd, err := clockMinutes(sh.EndTime)
		if err != nil {
			return err
		}

		rows, err := s.db.QueryContext(ctx, `
			SELECT sh2.start_time, sh2.end_time
			FROM roster_assignment ra
			JOIN shift sh2 ON sh2.id = ra.shift_id
			WHERE ra.roster_id = ? AND ra.weekday = ? AND ra.staff_id = ? AND ra.id != ?`,
			rosterID, wd, *staffID, assignmentID)
		if err != nil {
			return fmt.Errorf("check overlaps: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var otherStart, otherEnd string
			if err := rows.Scan(&otherStart, &otherEnd); err != nil {
				return err
			}
			os, err := clockMinutes(otherStart)
			if err != nil {
				return err
			}
			oe, err := clockMinutes(otherEnd)
			if err != nil {
				return err
			}
			if overlaps(shStart, shEnd, os, oe) {
				return fmt.Errorf("that staff member already has an overlapping shift that day")
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	var arg sql.NullInt64
	if staffID != nil {
		arg = sql.NullInt64{Int64: *staffID, Valid: true}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE roster_assignment SET staff_id = ? WHERE id = ?`, arg, assignmentID); err != nil {
		return fmt.Errorf("reassign: %w", err)
	}
	return nil
}
