package roster

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crewtopia/internal/authctx"
	"crewtopia/internal/kratos"
	"crewtopia/internal/webutil"
)

const (
	isoDate      = "2006-01-02"
	isoYearMonth = "2006-01"
)

type Handlers struct {
	store  *Store
	kratos *kratos.Client
}

func NewHandlers(store *Store, kratosClient *kratos.Client) *Handlers {
	return &Handlers{store: store, kratos: kratosClient}
}

// mondayOf returns the Monday (00:00) of the week containing t.
func mondayOf(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return t.AddDate(0, 0, -offset)
}

// weekFromQuery resolves the ?week= param to an ISO date, defaulting to and
// normalizing onto the Monday of the current week.
func weekFromQuery(r *http.Request) string {
	if raw := r.URL.Query().Get("week"); raw != "" {
		if t, err := time.Parse(isoDate, raw); err == nil {
			return mondayOf(t).Format(isoDate)
		}
	}
	return mondayOf(time.Now()).Format(isoDate)
}

func weekNav(weekStart string) (prev, next string) {
	t, err := time.Parse(isoDate, weekStart)
	if err != nil {
		return "", ""
	}
	return t.AddDate(0, 0, -7).Format(isoDate), t.AddDate(0, 0, 7).Format(isoDate)
}

// View renders the read-only roster — the one page in the app that doesn't
// require a session. Since Oathkeeper's rule for this route is a plain
// "noop" pass-through (no cookie check, no injected headers), whether the
// visitor happens to be logged in is otherwise invisible to us; the WhoAmI
// call here is a soft, best-effort check purely so a logged-in visitor sees
// the normal full nav instead of the logged-out one. Nothing access-related
// depends on it — if it fails or is skipped, the page still renders fine as
// the logged-out view.
func (h *Handlers) View(w http.ResponseWriter, r *http.Request) {
	week := weekFromQuery(r)
	view, err := h.store.GetByWeek(r.Context(), week)
	if err != nil {
		webutil.ServerError(w, r, err, "load roster for view")
		return
	}
	prev, next := weekNav(week)

	publicView := true
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		if sess, err := h.kratos.WhoAmI(r.Context(), cookie); err == nil {
			u := authctx.User{
				ID:    sess.Identity.ID,
				Email: sess.Identity.Traits.Email,
				Role:  sess.Identity.MetadataPublic.Role,
			}
			r = r.WithContext(authctx.WithUser(r.Context(), u))
			publicView = false
		}
	}

	webutil.Page(w, r, "roster_view.html", map[string]any{
		"ActiveNav":    "roster",
		"PublicView":   publicView,
		"WeekStart":    week,
		"PrevWeek":     prev,
		"NextWeek":     next,
		"Days":         view.Days,
		"PrintTable":   buildWeekPrintTable(week, view.Days),
		"CurrentMonth": week[:7],
	})
}

func (h *Handlers) Edit(w http.ResponseWriter, r *http.Request) {
	week := weekFromQuery(r)
	_, days, err := h.store.GetByWeekWithOptions(r.Context(), week)
	if err != nil {
		webutil.ServerError(w, r, err, "load roster for edit")
		return
	}
	prev, next := weekNav(week)

	// Computed once and stamped onto every cell *and* the page data below,
	// rather than letting Page() generate it independently — see the
	// comment on webutil.Page for why that would risk a mismatch.
	token := webutil.CSRFToken(w, r)

	plainDays := make([]DayView, len(days))
	for i, dc := range days {
		dv := DayView{Bit: dc.Bit, Label: dc.Label}
		for j := range dc.Cells {
			dc.Cells[j].CSRFToken = token
			dv.Assignments = append(dv.Assignments, dc.Cells[j].Assignment)
		}
		plainDays[i] = dv
	}

	webutil.Page(w, r, "roster_edit.html", map[string]any{
		"ActiveNav":    "roster-edit",
		"CSRFToken":    token,
		"WeekStart":    week,
		"PrevWeek":     prev,
		"NextWeek":     next,
		"Days":         days,
		"PrintTable":   buildWeekPrintTable(week, plainDays),
		"CurrentMonth": week[:7],
	})
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}
	week := r.PostFormValue("week")
	t, err := time.Parse(isoDate, week)
	if err != nil {
		http.Error(w, "invalid week", http.StatusBadRequest)
		return
	}
	week = mondayOf(t).Format(isoDate)

	if _, err := h.store.Generate(r.Context(), week); err != nil {
		webutil.ServerError(w, r, err, "generate roster")
		return
	}
	http.Redirect(w, r, "/roster/edit?week="+week, http.StatusSeeOther)
}

func (h *Handlers) Reassign(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}

	var staffID *int64
	if raw := r.PostFormValue("staff_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid staff_id", http.StatusBadRequest)
			return
		}
		staffID = &v
	}

	// Reassign's own error (e.g. "overlapping shift") is safe to show
	// verbatim — it's a validation message about the requested change, not
	// an internal error, and it's what CellView.Error is for.
	var reassignErr string
	if err := h.store.Reassign(r.Context(), id, staffID); err != nil {
		reassignErr = err.Error()
	}

	assignment, err := h.store.GetAssignment(r.Context(), id)
	if err != nil {
		webutil.ServerError(w, r, err, "reload assignment after reassign")
		return
	}
	options, err := h.store.StaffOptionsForAssignment(r.Context(), id)
	if err != nil {
		webutil.ServerError(w, r, err, "load staff options after reassign")
		return
	}

	webutil.Partial(w, r, "roster_cell.html", "roster_cell", CellView{
		Assignment: *assignment,
		Options:    options,
		Error:      reassignErr,
		CSRFToken:  webutil.CSRFToken(w, r),
	})
}

// ExportCSV streams every assignment falling within the given calendar
// month (?month=YYYY-MM, defaulting to the current month) as a CSV file,
// shaped as a shift × day matrix (shifts down the rows, days across the
// columns) rather than one flat row per assignment.
func (h *Handlers) ExportCSV(w http.ResponseWriter, r *http.Request) {
	month := time.Now()
	if raw := r.URL.Query().Get("month"); raw != "" {
		if t, err := time.Parse(isoYearMonth, raw); err == nil {
			month = t
		}
	}

	rows, err := h.store.MonthlyAssignments(r.Context(), month.Year(), month.Month())
	if err != nil {
		webutil.ServerError(w, r, err, "load monthly assignments for csv export")
		return
	}
	table := buildMonthlyPrintTable(rows)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roster-%s.csv"`, month.Format(isoYearMonth)))

	cw := csv.NewWriter(w)

	header := []string{"Shift", "Start", "End"}
	for _, col := range table.Columns {
		header = append(header, fmt.Sprintf("%s %s", col.Short, col.Date))
	}
	cw.Write(header)

	for _, row := range table.Rows {
		record := []string{csvSafe(row.ShiftName), row.StartTime, row.EndTime}
		for _, cell := range row.Cells {
			switch {
			case !cell.Scheduled:
				record = append(record, "")
			case cell.Name == "":
				record = append(record, "Unfilled")
			default:
				record = append(record, csvSafe(cell.Name))
			}
		}
		cw.Write(record)
	}
	cw.Flush()
}

// csvSafe prefixes values that start with a formula-triggering character
// (=, +, -, @) with a single quote. Spreadsheet apps (Excel, Sheets) treat
// a leading =/+/-/@ as the start of a formula; without this, a staff or
// shift name like "=cmd|calc" would execute as a formula for whoever opens
// the exported file. The leading quote makes it display as literal text
// instead.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}
