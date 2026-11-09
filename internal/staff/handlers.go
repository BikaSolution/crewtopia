package staff

import (
	"fmt"
	"net/http"
	"strconv"

	"crewtopia/internal/shift"
	"crewtopia/internal/webutil"
	"crewtopia/internal/weekday"
)

type Handlers struct {
	store  *Store
	shifts *shift.Store
}

func NewHandlers(store *Store, shiftStore *shift.Store) *Handlers {
	return &Handlers{store: store, shifts: shiftStore}
}

func parseForm(r *http.Request) (Staff, error) {
	desire, err := strconv.ParseFloat(r.PostFormValue("desire"), 64)
	if err != nil {
		return Staff{}, fmt.Errorf("invalid desire: %w", err)
	}

	mask := weekday.FromForm(func(name string) bool { return r.PostForm.Has(name) })

	var shiftIDs []int64
	for _, v := range r.PostForm["shift_id"] {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		shiftIDs = append(shiftIDs, id)
	}

	return Staff{
		Name:            r.PostFormValue("name"),
		Nickname:        r.PostFormValue("nickname"),
		ContactNumber:   r.PostFormValue("contact_number"),
		Desire:          desire,
		AvailableDays:   mask,
		CapableShiftIDs: shiftIDs,
	}, nil
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.List(r.Context())
	if err != nil {
		webutil.ServerError(w, r, err, "list staff")
		return
	}
	webutil.Page(w, r, "staff_list.html", map[string]any{
		"ActiveNav": "staff",
		"Staff":     list,
	})
}

func (h *Handlers) NewForm(w http.ResponseWriter, r *http.Request) {
	shifts, err := h.shifts.List(r.Context())
	if err != nil {
		webutil.ServerError(w, r, err, "list shifts for new staff form")
		return
	}
	webutil.Page(w, r, "staff_form.html", map[string]any{
		"ActiveNav": "staff",
		"IsNew":     true,
		"Staff":     &Staff{},
		"Shifts":    shifts,
	})
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}
	st, err := parseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.Save(r.Context(), &st); err != nil {
		webutil.ServerError(w, r, err, "save new staff")
		return
	}
	http.Redirect(w, r, "/staff", http.StatusSeeOther)
}

func (h *Handlers) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	st, err := h.store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	shifts, err := h.shifts.List(r.Context())
	if err != nil {
		webutil.ServerError(w, r, err, "list shifts for edit staff form")
		return
	}
	webutil.Page(w, r, "staff_form.html", map[string]any{
		"ActiveNav": "staff",
		"IsNew":     false,
		"Staff":     st,
		"Shifts":    shifts,
	})
}

func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}
	st, err := parseForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st.ID = id
	if err := h.store.Save(r.Context(), &st); err != nil {
		webutil.ServerError(w, r, err, "save staff update")
		return
	}
	http.Redirect(w, r, "/staff", http.StatusSeeOther)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		webutil.ServerError(w, r, err, "delete staff")
		return
	}
	http.Redirect(w, r, "/staff", http.StatusSeeOther)
}
