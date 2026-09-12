package helper

import (
	"testing"
	"time"
)

func row(id, value string, ageMinutes int) phoneRow {
	return phoneRow{
		ID:        id,
		Value:     value,
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(ageMinutes) * time.Minute),
	}
}

func TestPlanBackfillNormalisesEveryRow(t *testing.T) {
	setRegion(t, "TR")

	rows := []phoneRow{
		row("1", "0555 123 45 67", 0),
		row("2", "+90 555 123 45 68", 0),
		row("3", "905551234569", 0),
	}

	updates, skipped, collisions := planBackfill(rows, collisionAllow)

	if len(skipped) != 0 || len(collisions) != 0 {
		t.Fatalf("skipped = %d, collisions = %d, want 0 and 0", len(skipped), len(collisions))
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2 (row 3 is already canonical)", len(updates))
	}
	if updates[0].ID != "1" || updates[0].NewValue != "905551234567" {
		t.Errorf("first update = %+v", updates[0])
	}
	if updates[0].OldValue != "0555 123 45 67" {
		t.Errorf("old value = %q, want the stored value so the guard matches", updates[0].OldValue)
	}
}

func TestPlanBackfillIsIdempotent(t *testing.T) {
	setRegion(t, "TR")

	rows := []phoneRow{row("1", "905551234567", 0), row("2", "628123456789", 0)}

	updates, _, _ := planBackfill(rows, collisionAllow)
	if len(updates) != 0 {
		t.Errorf("updates = %d, want 0 for an already normalised table", len(updates))
	}
}

func TestPlanBackfillSkipsUnparseableValues(t *testing.T) {
	setRegion(t, "TR")

	rows := []phoneRow{
		row("1", "0555 123 45 67", 0),
		row("2", "123456789012345678", 0), // a LID, not a phone number
		row("3", "120363012345678901@g.us", 0),
	}

	updates, skipped, _ := planBackfill(rows, collisionAllow)

	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %d, want 2", len(skipped))
	}
	if skipped[0].ID != "2" || skipped[1].ID != "3" {
		t.Errorf("skipped = %+v, want rows 2 and 3", skipped)
	}
}

func TestPlanBackfillKeepsOnlyTheLatestOfACollidingGroup(t *testing.T) {
	setRegion(t, "TR")

	rows := []phoneRow{
		row("1", "0555 123 45 67", 0),
		row("2", "+905551234567", 10), // newer, same canonical value
		row("3", "05551234599", 0),
	}

	updates, _, collisions := planBackfill(rows, collisionKeepLatest)

	if len(collisions) != 1 {
		t.Fatalf("collisions = %d, want 1", len(collisions))
	}
	if len(collisions[0]) != 2 {
		t.Fatalf("collision group size = %d, want 2", len(collisions[0]))
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2 (the newest of the pair plus the untouched row)", len(updates))
	}
	if updates[0].ID != "2" {
		t.Errorf("winner = %q, want the most recently updated row 2", updates[0].ID)
	}
	if updates[1].ID != "3" {
		t.Errorf("second update = %q, want row 3", updates[1].ID)
	}
}

func TestPlanBackfillCollisionFallsBackToTheHigherID(t *testing.T) {
	setRegion(t, "TR")

	rows := []phoneRow{
		row("7", "0555 123 45 67", 0),
		row("9", "+90 555 123 45 67", 0), // same timestamp
	}

	updates, _, collisions := planBackfill(rows, collisionKeepLatest)

	if len(collisions) != 1 {
		t.Fatalf("collisions = %d, want 1", len(collisions))
	}
	if len(updates) != 1 || updates[0].ID != "9" {
		t.Fatalf("updates = %+v, want only row 9", updates)
	}
}
