# Charon Disaster Recovery Runbook

This runbook covers recovery procedures for Charon in production. Keep a printed
copy available; an operator who cannot log in to the platform still needs to be
able to execute these steps from raw database and filesystem access.

## Recovery Objectives

| Metric | Target | Notes                                                        |
| ------ | ------ | ------------------------------------------------------------ |
| RPO    | 1 hour | PostgreSQL PITR + hourly logical dump of `APP_DATABASE_URL`. |
| RTO    | 2 hours | Restore database, re-mount uploads volume, redeploy image.  |

The WhatsApp session blob (`instances.session_data`) is part of the backup but
is re-usable only while the paired device has not been logged out. If the pair
is lost, operators must re-scan QR codes — that is a user-driven step, not a
restore step.

## Backup Layers

1. **PostgreSQL logical dump** — `pg_dump` the three databases on `APP_DATABASE_URL`,
   `DATABASE_URL` (whatsmeow), and (when separate) `OUTBOX_DATABASE_URL` every
   hour. Example:

   ```bash
   pg_dump --format=custom --file="charon-app-$(date -u +%Y%m%dT%H%M%SZ).dump" \
       "$APP_DATABASE_URL"
   ```

2. **PostgreSQL physical backups** — if WAL archiving is enabled on the host,
   keep at least 72 hours of WAL plus one daily base backup to support
   point-in-time recovery.

3. **Uploads volume** — `./uploads/` (bind-mounted `uploads` volume in the
   production compose file). Snapshot nightly; this holds avatars, system
   assets, and any files operators uploaded via the admin file manager.

4. **Environment** — `.env` is kept in the secret manager, not in the backup
   bundle. Losing it means regenerating session keys and rotating `GEMINI_API_KEY`.

5. **API keys** — `api_keys.key_hash` is SHA-256 hashed in the DB. If all keys
   are lost the admin must re-issue via `POST /api/admin/api-keys` after login.

## Full Restore Procedure

Assumes a fresh Coolify environment or a fresh server with the production image
available.

1. Provision PostgreSQL. Restore the most recent logical dump:

   ```bash
   pg_restore --clean --if-exists --dbname="$APP_DATABASE_URL" charon-app-latest.dump
   pg_restore --clean --if-exists --dbname="$DATABASE_URL"     charon-wa-latest.dump
   ```

   If `OUTBOX_DATABASE_URL` is separate, restore it the same way.

2. Re-mount the uploads volume from the last nightly snapshot.

3. Populate `.env` from the secret manager. Confirm these settings explicitly:

   | Variable              | Required production value                              |
   | --------------------- | ------------------------------------------------------ |
   | `CORS_ALLOW_ORIGINS`  | Explicit list, no wildcards                            |
   | `COOKIE_SECURE`       | `true`                                                 |
   | `BEHIND_PROXY`        | `true` when behind Coolify/nginx                       |
   | `BASEURL`             | Public host (without protocol)                         |
   | `DATABASE_URL`        | Includes `sslmode=require` in production               |
   | `OUTBOX_API_USER/PASS`| Rotated pair (old credentials no longer valid)         |
   | `SESSION_EXPIRY`      | `168h` (sliding) — absolute cap is 30d, hard-coded     |

4. Deploy the image and wait for `/health` to respond.

5. Revoke all existing sessions. Sessions do not survive the restore
   transparently — force a global logout so stale tokens are not honored:

   ```sql
   DELETE FROM sessions;
   ```

6. Reset the admin password if the restore replayed a compromised hash:

   ```sql
   -- bcrypt cost 10 hash of the new password, generated out of band:
   UPDATE users SET password_hash = '<bcrypt-hash>',
                    failed_login_count = 0,
                    locked_until = NULL
   WHERE username = 'admin';
   ```

7. Ask each operator to re-scan QR codes for instances whose `session_data`
   rejects the paired device (WhatsApp security expires the pair aggressively
   if the server disappears for > 14 days).

## Session & API Key Revocation

- **Session revocation (global):** `DELETE FROM sessions;`
- **Session revocation (per user):** `DELETE FROM sessions WHERE user_id = $1;`
  (this matches `model.DeleteAllUserAuthSessions` and is triggered automatically
  on admin disable/role change).
- **API key revocation:** `DELETE FROM api_keys WHERE key_hash = $1;` where
  `key_hash = encode(sha256('raw_key_here'), 'hex')`. For a full reset,
  `TRUNCATE api_keys; TRUNCATE outbox_worker_config;` then re-issue.

## Partial Restores

- **Lost uploads only:** restore the nightly snapshot, restart the app (avatar
  URLs resolve immediately; missing files fall back to 404 until re-uploaded).
- **Lost outbox only:** restore `outbox`, `outbox_worker_config`, `api_keys`
  tables from a logical dump. Workers reclaim stale processing rows via the
  reaper within 5 minutes of startup.
- **Corrupted `instances` table:** restoring from the prior hourly dump is
  safe — the `is_connected` flag will briefly report stale values until each
  WhatsApp client reconnects and updates its row.

## Rolling Back the Phone Number Backfill

The one-time phone normalisation (`phone_backfill_v1`) records the old and new
value of every row it rewrote in `phone_backfill_backup_v1`. The table lives in
each pool it touched, so run the statements for `outbox` against
`OUTBOX_DATABASE_URL` when that points at a separate database.

Inspect first:

```sql
SELECT table_name, count(*) FROM phone_backfill_backup_v1 GROUP BY 1;
```

Restore, one statement per table. The `AND` guard leaves any row that was
changed again after the backfill alone:

```sql
UPDATE instances t SET phone_number = b.old_value
FROM phone_backfill_backup_v1 b
WHERE b.table_name = 'instances' AND t.id::text = b.row_id
  AND t.phone_number = b.new_value;

UPDATE warming_rooms t SET whitelisted_number = b.old_value
FROM phone_backfill_backup_v1 b
WHERE b.table_name = 'warming_rooms' AND t.id::text = b.row_id
  AND t.whitelisted_number = b.new_value;

UPDATE outbox t SET destination = b.old_value
FROM phone_backfill_backup_v1 b
WHERE b.table_name = 'outbox' AND t.id_outbox::text = b.row_id
  AND t.destination = b.new_value;
```

Then clear the marker, so a later startup can run the backfill again:

```sql
DELETE FROM system_settings WHERE key = 'phone_backfill_v1';
```

Rolling back the data without also rolling back the application leaves the
stored numbers in a shape the current code does not write. Do it only together
with a deploy of the previous release.

## Post-Restore Validation

1. `curl -H "Cookie: session=<admin-session>" $BASEURL/api/admin/dashboard`
   returns non-zero instance counts.
2. `SELECT COUNT(*) FROM sessions;` returns 0.
3. Outbox worker log shows `✅ reaper reclaimed 0 stale rows` within 5 minutes.
4. Webhook receiver logs show HMAC verification successes after the first
   incoming message (if webhooks are configured).
5. Coolify health check on `/health` returns 200 for 5 consecutive minutes.

## Known Limitations

- No automated failover. Restore is manual.
- WhatsApp session keys (`instances.session_data`) are stored unencrypted in
  PostgreSQL. Anyone who can read the backup file can impersonate every paired
  WhatsApp device. Encrypt the backup artifacts at rest.
- `admin/admin123` seeded on first startup. The restore must not be used to
  re-introduce the default — step 6 above is mandatory.
