package helper

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"charon/database"
)

// phoneBackfillKey marks the one-time normalisation in system_settings. Delete
// the row to run the backfill again:
//
//	DELETE FROM system_settings WHERE key = 'phone_backfill_v1';
const phoneBackfillKey = "phone_backfill_v1"

// phoneBackfillBackupTable records the old and new value of every row the
// backfill rewrites, so the change can be undone. See docs/DR.md.
const phoneBackfillBackupTable = "phone_backfill_backup_v1"

// maxLoggedSkips bounds the sample of unparseable values written to the log. A
// database full of LIDs must not flood startup output.
const maxLoggedSkips = 10

// collisionPolicy decides what happens when two rows normalise to one value.
type collisionPolicy int

const (
	// collisionAllow normalises every row. Duplicates are expected and
	// harmless: two outbox rows may target the same number, and a stale
	// instance row may shadow a live one.
	collisionAllow collisionPolicy = iota

	// collisionKeepLatest normalises only the most recently updated row of a
	// colliding group. warming_rooms carries a partial unique index on
	// whitelisted_number, and auto-reply routing assumes one room per number.
	collisionKeepLatest
)

type phoneRow struct {
	ID        string
	Value     string
	UpdatedAt time.Time
}

type phoneUpdate struct {
	ID       string
	OldValue string
	NewValue string
}

type phoneCandidate struct {
	row    phoneRow
	target string
}

// backfillTarget is one column to normalise. Its identifiers are compile-time
// constants, never user input.
type backfillTarget struct {
	db            *sql.DB
	table         string
	idColumn      string
	column        string
	where         string
	timestampExpr string
	policy        collisionPolicy
}

// phoneBackfillTargets is built at call time, because database.OutboxDB is only
// assigned once the pools are open. outbox may live in a different database
// than the other two, so a single transaction cannot span them.
func phoneBackfillTargets() []backfillTarget {
	return []backfillTarget{
		{
			db:            database.AppDB,
			table:         "instances",
			idColumn:      "id",
			column:        "phone_number",
			where:         "phone_number IS NOT NULL AND phone_number <> ''",
			timestampExpr: "NOW()",
			policy:        collisionAllow,
		},
		{
			db:            database.AppDB,
			table:         "warming_rooms",
			idColumn:      "id",
			column:        "whitelisted_number",
			where:         "room_type = 'HUMAN_VS_BOT' AND whitelisted_number IS NOT NULL AND whitelisted_number <> ''",
			timestampExpr: "updated_at",
			policy:        collisionKeepLatest,
		},
		{
			// Only pending rows: a sent or failed row is an audit record, and
			// rewriting what was actually sent loses that. A destination with
			// an "@" is a group id, not a phone number.
			db:            database.OutboxDB,
			table:         "outbox",
			idColumn:      "id_outbox",
			column:        "destination",
			where:         "status = 0 AND destination NOT LIKE '%@%'",
			timestampExpr: "NOW()",
			policy:        collisionAllow,
		},
	}
}

// RunPhoneBackfill rewrites every stored phone number into its canonical form,
// once. It is best-effort: a failure is reported and startup continues, exactly
// like the schema migrations it runs after.
func RunPhoneBackfill() {
	claimed, err := claimPhoneBackfill()
	if err != nil {
		log.Printf("Warning: phone backfill could not claim its marker: %v", err)
		return
	}
	if !claimed {
		return
	}

	log.Println("Running the one-time phone number normalisation...")
	for _, target := range phoneBackfillTargets() {
		if target.db == nil {
			log.Printf("phone backfill: %s skipped, its database pool is not open", target.table)
			continue
		}
		runPhoneBackfillTarget(target)
	}
	log.Println("Phone number normalisation finished")
}

// claimPhoneBackfill inserts the marker row and reports whether this process
// won the claim. The unique key on system_settings.key makes it atomic, so
// several replicas starting at once cannot run the backfill twice.
func claimPhoneBackfill() (bool, error) {
	value := fmt.Sprintf(`{"applied_at":%q}`, time.Now().UTC().Format(time.RFC3339))

	result, err := database.AppDB.Exec(`
		INSERT INTO system_settings (key, value)
		VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO NOTHING
	`, phoneBackfillKey, value)
	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func runPhoneBackfillTarget(t backfillTarget) {
	if err := ensureBackfillBackupTable(t.db); err != nil {
		log.Printf("phone backfill: %s skipped, backup table unavailable: %v", t.table, err)
		return
	}

	rows, err := loadPhoneRows(t)
	if err != nil {
		log.Printf("phone backfill: %s skipped, could not read rows: %v", t.table, err)
		return
	}

	updates, skipped, collisions := planBackfill(rows, t.policy)
	reportUnchangedRows(t, skipped, collisions)

	applied, err := applyPhoneUpdates(t, updates)
	if err != nil {
		log.Printf("phone backfill: %s stopped after %d of %d rows: %v", t.table, applied, len(updates), err)
		log.Printf("phone backfill: delete system_settings key %q to run it again", phoneBackfillKey)
		return
	}

	log.Printf("phone backfill: %s.%s normalised %d of %d rows (%d unparseable, %d collisions)",
		t.table, t.column, applied, len(rows), len(skipped), len(collisions))
}

func reportUnchangedRows(t backfillTarget, skipped []phoneRow, collisions [][]phoneRow) {
	for i, row := range skipped {
		if i == maxLoggedSkips {
			log.Printf("phone backfill: %s has %d more unparseable values", t.table, len(skipped)-maxLoggedSkips)
			break
		}
		log.Printf("phone backfill: %s row %s left as-is, %s is not a phone number",
			t.table, row.ID, SanitizeLogValue(row.Value))
	}

	for _, group := range collisions {
		for _, row := range group {
			log.Printf("phone backfill: %s row %s collides on the normalised value of %s",
				t.table, row.ID, SanitizeLogValue(row.Value))
		}
	}
}

func ensureBackfillBackupTable(db *sql.DB) error {
	// #nosec G201 -- the table name is a package-level constant, never user input.
	stmt := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			table_name VARCHAR(64) NOT NULL,
			column_name VARCHAR(64) NOT NULL,
			row_id VARCHAR(64) NOT NULL,
			old_value VARCHAR(100) NOT NULL,
			new_value VARCHAR(100) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`, phoneBackfillBackupTable)

	_, err := db.Exec(stmt)
	return err
}

func loadPhoneRows(t backfillTarget) ([]phoneRow, error) {
	// #nosec G201 -- every identifier comes from phoneBackfillTargets, never from a request.
	query := fmt.Sprintf("SELECT %s::text, %s, %s FROM %s WHERE %s",
		t.idColumn, t.column, t.timestampExpr, t.table, t.where)

	rows, err := t.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []phoneRow
	for rows.Next() {
		var row phoneRow
		if err := rows.Scan(&row.ID, &row.Value, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// applyPhoneUpdates rewrites one row per transaction. A table-wide transaction
// is not an option: instances is written by the device loader during startup,
// and the three tables do not share a database. The guard on the old value
// makes a concurrent writer win instead of being overwritten, and the backup
// row is committed together with the change it describes.
func applyPhoneUpdates(t backfillTarget, updates []phoneUpdate) (int, error) {
	applied := 0
	for _, update := range updates {
		changed, err := applyPhoneUpdate(t, update)
		if err != nil {
			return applied, err
		}
		if changed {
			applied++
		}
	}
	return applied, nil
}

func applyPhoneUpdate(t backfillTarget, update phoneUpdate) (bool, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// #nosec G201 -- the table name is a package-level constant, never user input.
	backupStmt := fmt.Sprintf(`
		INSERT INTO %s (table_name, column_name, row_id, old_value, new_value)
		VALUES ($1, $2, $3, $4, $5)
	`, phoneBackfillBackupTable)

	if _, err := tx.Exec(backupStmt, t.table, t.column, update.ID, update.OldValue, update.NewValue); err != nil {
		return false, err
	}

	// #nosec G201 -- every identifier comes from phoneBackfillTargets, never from a request.
	updateStmt := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s::text = $2 AND %s = $3",
		t.table, t.column, t.idColumn, t.column)

	result, err := tx.Exec(updateStmt, update.NewValue, update.ID, update.OldValue)
	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		// Someone else changed the row first. Drop the backup row with it.
		return false, nil
	}

	return true, tx.Commit()
}

// planBackfill decides what the backfill would write, without touching a
// database. It is the whole decision logic of the backfill, so it can be tested
// on its own.
func planBackfill(rows []phoneRow, policy collisionPolicy) ([]phoneUpdate, []phoneRow, [][]phoneRow) {
	candidates, skipped := classifyPhoneRows(rows)

	if policy == collisionKeepLatest {
		updates, collisions := planKeepLatest(candidates)
		return updates, skipped, collisions
	}

	var updates []phoneUpdate
	for _, candidate := range candidates {
		if update, ok := changedValue(candidate); ok {
			updates = append(updates, update)
		}
	}
	return updates, skipped, nil
}

// classifyPhoneRows splits the rows into the ones that normalise and the ones
// that do not. An unparseable value is left alone: it is a LID, a group id, or
// data the operator has to look at.
func classifyPhoneRows(rows []phoneRow) ([]phoneCandidate, []phoneRow) {
	candidates := make([]phoneCandidate, 0, len(rows))
	var skipped []phoneRow

	for _, row := range rows {
		normalized, err := NormalizePhone(row.Value)
		if err != nil {
			skipped = append(skipped, row)
			continue
		}
		candidates = append(candidates, phoneCandidate{row: row, target: normalized})
	}

	return candidates, skipped
}

func changedValue(c phoneCandidate) (phoneUpdate, bool) {
	if c.target == c.row.Value {
		return phoneUpdate{}, false
	}
	return phoneUpdate{ID: c.row.ID, OldValue: c.row.Value, NewValue: c.target}, true
}

// planKeepLatest normalises one row per target value, the most recently updated
// one, and reports the rest as a collision.
func planKeepLatest(candidates []phoneCandidate) ([]phoneUpdate, [][]phoneRow) {
	order, groups := groupByTarget(candidates)

	var updates []phoneUpdate
	var collisions [][]phoneRow

	for _, target := range order {
		group := groups[target]
		if len(group) > 1 {
			collisions = append(collisions, rowsOf(group))
		}
		if update, ok := changedValue(latestCandidate(group)); ok {
			updates = append(updates, update)
		}
	}

	return updates, collisions
}

// groupByTarget buckets the candidates by the value they normalise to, keeping
// first-seen order so the plan is deterministic.
func groupByTarget(candidates []phoneCandidate) ([]string, map[string][]phoneCandidate) {
	var order []string
	groups := make(map[string][]phoneCandidate, len(candidates))

	for _, candidate := range candidates {
		if _, seen := groups[candidate.target]; !seen {
			order = append(order, candidate.target)
		}
		groups[candidate.target] = append(groups[candidate.target], candidate)
	}

	return order, groups
}

// latestCandidate picks the most recently updated row, falling back to the
// larger id when the timestamps are equal.
func latestCandidate(group []phoneCandidate) phoneCandidate {
	winner := group[0]
	for _, candidate := range group[1:] {
		if candidate.row.UpdatedAt.After(winner.row.UpdatedAt) {
			winner = candidate
			continue
		}
		if candidate.row.UpdatedAt.Equal(winner.row.UpdatedAt) && candidate.row.ID > winner.row.ID {
			winner = candidate
		}
	}
	return winner
}

func rowsOf(group []phoneCandidate) []phoneRow {
	out := make([]phoneRow, 0, len(group))
	for _, candidate := range group {
		out = append(out, candidate.row)
	}
	return out
}
