// Package weekday packs the seven weekdays into a single int using bit flags,
// so availability/active-days live as one column instead of seven.
package weekday

type Mask int

const (
	Monday Mask = 1 << iota
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

// Ordered lists Mon-Sun in display order, paired with their bit and label.
var Ordered = []struct {
	Bit   Mask
	Label string
	Short string
}{
	{Monday, "Monday", "Mon"},
	{Tuesday, "Tuesday", "Tue"},
	{Wednesday, "Wednesday", "Wed"},
	{Thursday, "Thursday", "Thu"},
	{Friday, "Friday", "Fri"},
	{Saturday, "Saturday", "Sat"},
	{Sunday, "Sunday", "Sun"},
}

// Has reports whether the given day bit is set in the mask.
func (m Mask) Has(day Mask) bool {
	return m&day != 0
}

// FromForm builds a mask from the checkbox names checked in an HTML form,
// where present() reports whether a given form field name was submitted.
func FromForm(present func(fieldName string) bool) Mask {
	var m Mask
	for _, d := range Ordered {
		if present(d.Short) {
			m |= d.Bit
		}
	}
	return m
}
