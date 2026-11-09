package shift

import (
	"net/http"
	"strconv"

	"crewtopia/internal/webutil"
	"crewtopia/internal/weekday"
)

type Handlers struct {
	store *Store
}

func NewHandlers(store *Store) *Handlers {
	return &Handlers{store: store}
}

func parseForm(r *http.Request) Shift {
	mask := weekday.FromForm(func(name string) bool { return r.PostForm.Has(name) })
	return Shift{
		Name:       r.PostFormValue("name"),
		StartTime:  r.PostFormValue("start_time"),
		EndTime:    r.PostFormValue("end_time"),
		ActiveDays: mask,
	}
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.List(r.Context())
	if err != nil {
		webutil.ServerError(w, r, err, "list shifts")
		return
	}
	webutil.Page(w, r, "shift_list.html", map[string]any{
		"ActiveNav": "shifts",
		"Shifts":    list,
	})
}

func (h *Handlers) NewForm(w http.ResponseWriter, r *http.Request) {
	webutil.Page(w, r, "shift_form.html", map[string]any{
		"ActiveNav": "shifts",
		"IsNew":     true,
		"Shift":     &Shift{},
	})
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}
	sh := parseForm(r)
	if err := h.store.Save(r.Context(), &sh); err != nil {
		webutil.ServerError(w, r, err, "save new shift")
		return
	}
	http.Redirect(w, r, "/shifts", http.StatusSeeOther)
}

func (h *Handlers) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	sh, err := h.store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	webutil.Page(w, r, "shift_form.html", map[string]any{
		"ActiveNav": "shifts",
		"IsNew":     false,
		"Shift":     sh,
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
	sh := parseForm(r)
	sh.ID = id
	if err := h.store.Save(r.Context(), &sh); err != nil {
		webutil.ServerError(w, r, err, "save shift update")
		return
	}
	http.Redirect(w, r, "/shifts", http.StatusSeeOther)
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		webutil.ServerError(w, r, err, "delete shift")
		return
	}
	http.Redirect(w, r, "/shifts", http.StatusSeeOther)
}
