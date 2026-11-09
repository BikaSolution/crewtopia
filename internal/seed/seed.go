// Package seed fills the staff pool and shifts list with sample data, purely
// so a fresh install has something to look at and generate a roster from.
// Restricted to super admins (authctx.RequireSuperAdmin) — a live site
// shouldn't let just anyone repopulate the database with sample data.
package seed

import (
	"context"
	"fmt"
	"net/http"

	"crewtopia/internal/authctx"
	"crewtopia/internal/shift"
	"crewtopia/internal/staff"
	"crewtopia/internal/webutil"
	"crewtopia/internal/weekday"
)

var seedShifts = []shift.Shift{
	{Name: "Barman", StartTime: "17:00", EndTime: "21:00", ActiveDays: weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday | weekday.Saturday | weekday.Sunday},
	{Name: "Waiter", StartTime: "12:00", EndTime: "17:00", ActiveDays: weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday | weekday.Saturday | weekday.Sunday},
	{Name: "Manager", StartTime: "09:00", EndTime: "17:00", ActiveDays: weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday},
	{Name: "Cleaner", StartTime: "06:00", EndTime: "09:00", ActiveDays: weekday.Monday | weekday.Wednesday | weekday.Friday},
}

var seedStaff = []struct {
	Name          string
	Nickname      string
	ContactNumber string
	Desire        float64
	AvailableDays weekday.Mask
	Capable       []string // shift names, matched against seedShifts
}{
	{"Alice Johnson", "Ally", "082 555 0101", 72.5, weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday, []string{"Barman", "Waiter"}},
	{"Bongani Khumalo", "Bongz", "083 555 0102", 55, weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday | weekday.Saturday | weekday.Sunday, []string{"Waiter", "Cleaner"}},
	{"Chloe Adams", "Coco", "084 555 0103", 90, weekday.Monday | weekday.Tuesday | weekday.Wednesday | weekday.Thursday | weekday.Friday, []string{"Manager", "Barman"}},
	{"Dumisani Nkosi", "Dumi", "071 555 0104", 40, weekday.Friday | weekday.Saturday | weekday.Sunday, []string{"Barman", "Cleaner"}},
	{"Emma Peters", "Em", "060 555 0105", 65, weekday.Monday | weekday.Wednesday | weekday.Friday | weekday.Saturday, []string{"Waiter", "Manager"}},
}

// Run inserts the sample shifts and staff, skipping any whose name already
// exists so clicking "Seed test data" more than once doesn't pile up
// duplicates.
func Run(ctx context.Context, staffStore *staff.Store, shiftStore *shift.Store) error {
	existingShifts, err := shiftStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list existing shifts: %w", err)
	}
	shiftIDByName := make(map[string]int64, len(existingShifts))
	for _, sh := range existingShifts {
		shiftIDByName[sh.Name] = sh.ID
	}

	for _, sh := range seedShifts {
		if _, exists := shiftIDByName[sh.Name]; exists {
			continue
		}
		if err := shiftStore.Save(ctx, &sh); err != nil {
			return fmt.Errorf("seed shift %q: %w", sh.Name, err)
		}
		shiftIDByName[sh.Name] = sh.ID
	}

	existingStaff, err := staffStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list existing staff: %w", err)
	}
	existingStaffNames := make(map[string]bool, len(existingStaff))
	for _, st := range existingStaff {
		existingStaffNames[st.Name] = true
	}

	for _, sd := range seedStaff {
		if existingStaffNames[sd.Name] {
			continue
		}
		var capableIDs []int64
		for _, shiftName := range sd.Capable {
			if id, ok := shiftIDByName[shiftName]; ok {
				capableIDs = append(capableIDs, id)
			}
		}
		st := staff.Staff{
			Name:            sd.Name,
			Nickname:        sd.Nickname,
			ContactNumber:   sd.ContactNumber,
			Desire:          sd.Desire,
			AvailableDays:   sd.AvailableDays,
			CapableShiftIDs: capableIDs,
		}
		if err := staffStore.Save(ctx, &st); err != nil {
			return fmt.Errorf("seed staff %q: %w", sd.Name, err)
		}
	}

	return nil
}

type Handler struct {
	staff *staff.Store
	shift *shift.Store
}

func NewHandler(staffStore *staff.Store, shiftStore *shift.Store) *Handler {
	return &Handler{staff: staffStore, shift: shiftStore}
}

func (h *Handler) Seed(w http.ResponseWriter, r *http.Request) {
	if !authctx.RequireSuperAdmin(w, r) {
		return
	}
	if err := Run(r.Context(), h.staff, h.shift); err != nil {
		webutil.ServerError(w, r, err, "run seed")
		return
	}
	redirectTo := r.Referer()
	if redirectTo == "" {
		redirectTo = "/staff"
	}
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
}
