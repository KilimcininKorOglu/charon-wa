package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"charon/config"
)

// phoneConfigKey is the system_settings row an admin edits from the settings
// page. It must match model.KeyPhoneConfig.
const phoneConfigKey = "phone_config"

// storedPhoneConfig mirrors model.PhoneConfig. The worker cannot import
// internal/model, because that package binds to the API's own pools.
type storedPhoneConfig struct {
	DefaultRegion string `json:"default_region"`
}

// refreshPhoneRegion re-reads the deployment-wide region from the database, so
// an admin change reaches the worker without a restart. The API applies the
// same value in memory the moment it saves it; the worker is a separate
// process, so it picks the change up on this reload instead.
//
// PHONE_DEFAULT_REGION stays the value in force until a row exists, and any
// failure here leaves the current region untouched.
func refreshPhoneRegion(ctx context.Context) {
	var value []byte

	err := ConfigDB.QueryRowContext(ctx,
		"SELECT value FROM system_settings WHERE key = $1", phoneConfigKey).Scan(&value)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("Could not read the stored phone configuration: %v", err)
		}
		return
	}

	var stored storedPhoneConfig
	if err := json.Unmarshal(value, &stored); err != nil {
		log.Printf("Stored phone configuration is not readable: %v", err)
		return
	}

	if stored.DefaultRegion == config.PhoneRegion() {
		return
	}

	previous := config.PhoneRegion()
	if !config.SetPhoneRegion(stored.DefaultRegion) {
		log.Printf("Stored phone region %q is not a supported region code; keeping %q", stored.DefaultRegion, previous)
		return
	}
	log.Printf("Phone default region changed from %q to %q", previous, config.PhoneRegion())
}
