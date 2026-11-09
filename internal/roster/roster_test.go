package roster

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"crewtopia/internal/db"
	"crewtopia/internal/shift"
	"crewtopia/internal/staff"
	"crewtopia/internal/weekday"
)

func seededRand(seed uint64) func() float64 {
	r := rand.New(rand.NewPCG(seed, seed^0xC0FFEE))
	return r.Float64
}

func TestAssignDay_NoOverlapEver(t *testing.T) {
	morning := shift.Shift{ID: 1, Name: "Morning", StartTime: "08:00", EndTime: "12:00"}
	afternoon := shift.Shift{ID: 2, Name: "Afternoon", StartTime: "11:00", EndTime: "16:00"} // overlaps morning
	evening := shift.Shift{ID: 3, Name: "Evening", StartTime: "18:00", EndTime: "22:00"}
	dayShifts := []shift.Shift{morning, afternoon, evening}

	allStaff := []staff.Staff{
		{ID: 1, Name: "Alice", Desire: 80, AvailableDays: weekday.Monday},
		{ID: 2, Name: "Bob", Desire: 60, AvailableDays: weekday.Monday},
	}
	capable := map[int64]map[int64]bool{
		1: {1: true, 2: true, 3: true},
		2: {1: true, 2: true, 3: true},
	}

	for seed := uint64(0); seed < 500; seed++ {
		results, err := assignDay(weekday.Monday, dayShifts, allStaff, capable, seededRand(seed))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}

		byStaff := map[int64][]shift.Shift{}
		byShiftID := map[int64]shift.Shift{1: morning, 2: afternoon, 3: evening}
		for _, r := range results {
			if r.StaffID == nil {
				continue
			}
			byStaff[*r.StaffID] = append(byStaff[*r.StaffID], byShiftID[r.ShiftID])
		}

		for staffID, shifts := range byStaff {
			for i := 0; i < len(shifts); i++ {
				for j := i + 1; j < len(shifts); j++ {
					aStart, _ := clockMinutes(shifts[i].StartTime)
					aEnd, _ := clockMinutes(shifts[i].EndTime)
					bStart, _ := clockMinutes(shifts[j].StartTime)
					bEnd, _ := clockMinutes(shifts[j].EndTime)
					if overlaps(aStart, aEnd, bStart, bEnd) {
						t.Fatalf("seed %d: staff %d double-booked overlapping shifts %q and %q",
							seed, staffID, shifts[i].Name, shifts[j].Name)
					}
				}
			}
		}
	}
}

func TestAssignDay_TouchingShiftsAreNotOverlap(t *testing.T) {
	morning := shift.Shift{ID: 1, Name: "Morning", StartTime: "16:00", EndTime: "17:00"}
	evening := shift.Shift{ID: 2, Name: "Evening", StartTime: "17:00", EndTime: "18:00"}
	dayShifts := []shift.Shift{morning, evening}

	allStaff := []staff.Staff{
		{ID: 1, Name: "Alice", Desire: 100, AvailableDays: weekday.Monday},
	}
	capable := map[int64]map[int64]bool{1: {1: true, 2: true}}

	results, err := assignDay(weekday.Monday, dayShifts, allStaff, capable, seededRand(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.StaffID == nil || *r.StaffID != 1 {
			t.Errorf("expected the only eligible staff member to be assigned shift %d, got %v", r.ShiftID, r.StaffID)
		}
	}
}

func TestAssignDay_HigherDesireWinsMoreOften(t *testing.T) {
	sh := shift.Shift{ID: 1, Name: "Bar", StartTime: "17:00", EndTime: "21:00"}
	allStaff := []staff.Staff{
		{ID: 1, Name: "HighDesire", Desire: 90, AvailableDays: weekday.Friday},
		{ID: 2, Name: "LowDesire", Desire: 10, AvailableDays: weekday.Friday},
	}
	capable := map[int64]map[int64]bool{1: {1: true}, 2: {1: true}}

	const trials = 2000
	winsHigh := 0
	for seed := uint64(0); seed < trials; seed++ {
		results, err := assignDay(weekday.Friday, []shift.Shift{sh}, allStaff, capable, seededRand(seed))
		if err != nil {
			t.Fatal(err)
		}
		if results[0].StaffID != nil && *results[0].StaffID == 1 {
			winsHigh++
		}
	}

	// With desire 90 vs 10, the high-desire candidate should win the clear
	// majority of draws even though selection is randomized.
	if ratio := float64(winsHigh) / trials; ratio < 0.80 {
		t.Errorf("high-desire staff only won %.1f%% of %d trials, expected >= 80%%", ratio*100, trials)
	}
}

func TestAssignDay_AdjacencyPenaltyReducesRepeatSelection(t *testing.T) {
	morning := shift.Shift{ID: 1, Name: "Morning", StartTime: "08:00", EndTime: "12:00"}
	afternoon := shift.Shift{ID: 2, Name: "Afternoon", StartTime: "12:00", EndTime: "16:00"} // adjacent, not overlapping
	dayShifts := []shift.Shift{morning, afternoon}

	allStaff := []staff.Staff{
		{ID: 1, Name: "Alice", Desire: 50, AvailableDays: weekday.Monday},
		{ID: 2, Name: "Bob", Desire: 50, AvailableDays: weekday.Monday},
	}
	capable := map[int64]map[int64]bool{
		1: {1: true, 2: true},
		2: {1: true, 2: true},
	}

	const trials = 3000
	sameStaffBothShifts := 0
	for seed := uint64(0); seed < trials; seed++ {
		results, err := assignDay(weekday.Monday, dayShifts, allStaff, capable, seededRand(seed))
		if err != nil {
			t.Fatal(err)
		}
		if results[0].StaffID != nil && results[1].StaffID != nil && *results[0].StaffID == *results[1].StaffID {
			sameStaffBothShifts++
		}
	}

	// With equal desire and no penalty, the same staff member would take both
	// adjacent shifts roughly half the time. The adjacency penalty should pull
	// that noticeably below 50%.
	if ratio := float64(sameStaffBothShifts) / trials; ratio > 0.40 {
		t.Errorf("same staff covered both adjacent shifts %.1f%% of %d trials, expected < 40%% given the adjacency penalty", ratio*100, trials)
	}
}

func TestGenerateAndReassign_Integration(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	staffStore := staff.NewStore(sqlDB)
	shiftStore := shift.NewStore(sqlDB)
	rosterStore := NewStore(sqlDB, staffStore, shiftStore)

	bar := shift.Shift{Name: "Barman", StartTime: "17:00", EndTime: "21:00", ActiveDays: weekday.Monday | weekday.Tuesday}
	if err := shiftStore.Save(ctx, &bar); err != nil {
		t.Fatal(err)
	}

	alice := staff.Staff{Name: "Alice", Desire: 80, AvailableDays: weekday.Monday | weekday.Tuesday, CapableShiftIDs: []int64{bar.ID}}
	if err := staffStore.Save(ctx, &alice); err != nil {
		t.Fatal(err)
	}
	bob := staff.Staff{Name: "Bob", Desire: 40, AvailableDays: weekday.Monday | weekday.Tuesday, CapableShiftIDs: []int64{bar.ID}}
	if err := staffStore.Save(ctx, &bob); err != nil {
		t.Fatal(err)
	}

	if _, err := rosterStore.Generate(ctx, "2026-08-03"); err != nil {
		t.Fatalf("generate: %v", err)
	}

	view, err := rosterStore.GetByWeek(ctx, "2026-08-03")
	if err != nil {
		t.Fatalf("get by week: %v", err)
	}

	var mondayAssignment *Assignment
	for _, d := range view.Days {
		if d.Bit == weekday.Monday && len(d.Assignments) == 1 {
			mondayAssignment = &d.Assignments[0]
		}
	}
	if mondayAssignment == nil {
		t.Fatal("expected exactly one assignment on Monday")
	}
	if mondayAssignment.StaffID == nil {
		t.Fatal("expected Monday's Barman shift to be filled (both staff are eligible)")
	}

	// Reassign to whichever of the two wasn't picked, should succeed (no overlap).
	other := alice.ID
	if *mondayAssignment.StaffID == alice.ID {
		other = bob.ID
	}
	if err := rosterStore.Reassign(ctx, mondayAssignment.ID, &other); err != nil {
		t.Fatalf("reassign: %v", err)
	}

	view, err = rosterStore.GetByWeek(ctx, "2026-08-03")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range view.Days {
		if d.Bit != weekday.Monday {
			continue
		}
		if *d.Assignments[0].StaffID != other {
			t.Errorf("expected reassigned staff %d, got %d", other, *d.Assignments[0].StaffID)
		}
	}
}
