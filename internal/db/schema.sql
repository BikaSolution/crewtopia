CREATE TABLE IF NOT EXISTS staff (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    nickname        TEXT NOT NULL DEFAULT '',
    contact_number  TEXT NOT NULL DEFAULT '',
    desire          REAL NOT NULL DEFAULT 0,
    available_days  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS shift (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    start_time      TEXT NOT NULL,
    end_time        TEXT NOT NULL,
    active_days     INTEGER NOT NULL DEFAULT 0
);

-- Which shifts a staff member can theoretically be assigned to.
CREATE TABLE IF NOT EXISTS staff_shift_capability (
    staff_id INTEGER NOT NULL REFERENCES staff(id) ON DELETE CASCADE,
    shift_id INTEGER NOT NULL REFERENCES shift(id) ON DELETE CASCADE,
    PRIMARY KEY (staff_id, shift_id)
);

CREATE TABLE IF NOT EXISTS roster (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    week_start_date TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS roster_assignment (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    roster_id  INTEGER NOT NULL REFERENCES roster(id) ON DELETE CASCADE,
    shift_id   INTEGER NOT NULL REFERENCES shift(id) ON DELETE CASCADE,
    weekday    INTEGER NOT NULL,
    staff_id   INTEGER REFERENCES staff(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_roster_assignment_roster ON roster_assignment(roster_id);
CREATE INDEX IF NOT EXISTS idx_roster_assignment_staff ON roster_assignment(staff_id);
