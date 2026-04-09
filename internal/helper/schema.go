// internal/helper/schema.go
package helper

import (
	"database/sql"
	"encoding/json"
	"log"

	"charon/database"
)

func InitCustomSchema() {
	db := database.AppDB

	baseSchema := `
        CREATE TABLE IF NOT EXISTS instances (
            id                  SERIAL PRIMARY KEY,
            instance_id         VARCHAR(255) UNIQUE NOT NULL,
            phone_number        VARCHAR(50),
            jid                 VARCHAR(255),
            status              VARCHAR(50) NOT NULL DEFAULT 'disconnected',
            is_connected        BOOLEAN NOT NULL DEFAULT false,
            name                VARCHAR(255),
            profile_picture     TEXT,
            about               TEXT,
            platform            VARCHAR(50),
            battery_level       INT,
            battery_charging    BOOLEAN,
            qr_code             TEXT,
            qr_expires_at       TIMESTAMP,
            created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
            connected_at        TIMESTAMP,
            disconnected_at     TIMESTAMP,
            last_seen           TIMESTAMP,

            session_data        BYTEA
        );

        CREATE INDEX IF NOT EXISTS idx_instances_instance_id ON instances(instance_id);
        CREATE INDEX IF NOT EXISTS idx_instances_phone_number ON instances(phone_number);
        CREATE INDEX IF NOT EXISTS idx_instances_status ON instances(status);
    `
	if _, err := db.Exec(baseSchema); err != nil {
		log.Fatalf("failed to init base schema: %v", err)
	}

	alterSchema := `
        ALTER TABLE instances
        ADD COLUMN IF NOT EXISTS circle VARCHAR(255);

        ALTER TABLE instances
        ADD COLUMN IF NOT EXISTS webhook_url TEXT,
        ADD COLUMN IF NOT EXISTS webhook_secret TEXT;

        ALTER TABLE instances
        ADD COLUMN IF NOT EXISTS used BOOLEAN NOT NULL DEFAULT true,
        ADD COLUMN IF NOT EXISTS description TEXT;

        CREATE INDEX IF NOT EXISTS idx_instances_circle ON instances(circle);
        CREATE INDEX IF NOT EXISTS idx_instances_used ON instances(used);
    `
	if _, err := db.Exec(alterSchema); err != nil {
		log.Fatalf("failed to alter schema: %v", err)
	}

	// WhatsApp Warming System Schema
	warmingSchema := `
        -- =====================================================
        -- Table: warming_scripts
        -- Purpose: Header/template for conversation scripts
        -- =====================================================
        CREATE TABLE IF NOT EXISTS warming_scripts (
            id              SERIAL PRIMARY KEY,
            title           VARCHAR(255) NOT NULL,
            description     TEXT,
            category        VARCHAR(100),
            created_at      TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW(),
            updated_at      TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW()
        );

        COMMENT ON TABLE warming_scripts IS 'Header/template for warming conversation scripts';
        COMMENT ON COLUMN warming_scripts.title IS 'Script title, e.g.: Motorcycle Buy-Sell Conversation';
        COMMENT ON COLUMN warming_scripts.description IS 'Brief description of the script';
        COMMENT ON COLUMN warming_scripts.category IS 'Script category for grouping, e.g.: casual, business';

        -- =====================================================
        -- Table: warming_script_lines
        -- Purpose: Conversation dialog sequence for each script
        -- =====================================================
        CREATE TABLE IF NOT EXISTS warming_script_lines (
            id                      SERIAL PRIMARY KEY,
            script_id               INT NOT NULL,
            sequence_order          INT NOT NULL,
            actor_role              VARCHAR(20) NOT NULL CHECK (actor_role IN ('ACTOR_A', 'ACTOR_B')),
            message_content         TEXT NOT NULL,
            typing_duration_sec     INT NOT NULL DEFAULT 3,
            created_at              TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW(),
            
            CONSTRAINT fk_lines_script 
                FOREIGN KEY (script_id) 
                REFERENCES warming_scripts(id) 
                ON DELETE CASCADE,
            
            CONSTRAINT unique_script_sequence 
                UNIQUE (script_id, sequence_order)
        );

        COMMENT ON TABLE warming_script_lines IS 'Conversation dialog sequence for each script';
        COMMENT ON COLUMN warming_script_lines.script_id IS 'Reference to warming_scripts';
        COMMENT ON COLUMN warming_script_lines.sequence_order IS 'Dialog sequence order (1, 2, 3, ...)';
        COMMENT ON COLUMN warming_script_lines.actor_role IS 'Actor role: ACTOR_A (sender) or ACTOR_B (receiver)';
        COMMENT ON COLUMN warming_script_lines.message_content IS 'Spintax formatted text, e.g.: {Hello|Morning}, is the item {ready|available}?';
        COMMENT ON COLUMN warming_script_lines.typing_duration_sec IS 'Simulated duration of "typing..." indicator before message is sent';

        CREATE INDEX IF NOT EXISTS idx_script_lines_script_id ON warming_script_lines(script_id);
        CREATE INDEX IF NOT EXISTS idx_script_lines_script_sequence ON warming_script_lines(script_id, sequence_order);

        -- =====================================================
        -- Table: warming_rooms
        -- Purpose: Execution container that pairs 2 instances
        -- =====================================================
        CREATE TABLE IF NOT EXISTS warming_rooms (
            id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            name                    VARCHAR(255) NOT NULL,
            sender_instance_id      VARCHAR(255) NOT NULL,
            receiver_instance_id    VARCHAR(255) NOT NULL,
            script_id               INT NOT NULL,
            current_sequence        INT NOT NULL DEFAULT 0,
            status                  VARCHAR(20) NOT NULL DEFAULT 'STOPPED' 
                                    CHECK (status IN ('STOPPED', 'ACTIVE', 'PAUSED', 'FINISHED')),
            interval_min_seconds    INT NOT NULL DEFAULT 5,
            interval_max_seconds    INT NOT NULL DEFAULT 15,
            next_run_at             TIMESTAMP(6) WITH TIME ZONE,
            last_run_at             TIMESTAMP(6) WITH TIME ZONE,
            created_at              TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW(),
            updated_at              TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW(),
            
            CONSTRAINT fk_rooms_script 
                FOREIGN KEY (script_id) 
                REFERENCES warming_scripts(id) 
                ON DELETE RESTRICT,
            
            CONSTRAINT check_interval_range 
                CHECK (interval_max_seconds >= interval_min_seconds)
        );

        COMMENT ON TABLE warming_rooms IS 'Execution container that pairs 2 instances to run a specific script';
        COMMENT ON COLUMN warming_rooms.id IS 'UUID for room ID';
        COMMENT ON COLUMN warming_rooms.name IS 'Room name for easy identification';
        COMMENT ON COLUMN warming_rooms.sender_instance_id IS 'Sender instance ID (ACTOR_A)';
        COMMENT ON COLUMN warming_rooms.receiver_instance_id IS 'Receiver instance ID (ACTOR_B)';
        COMMENT ON COLUMN warming_rooms.script_id IS 'Reference to warming_scripts to be executed';
        COMMENT ON COLUMN warming_rooms.current_sequence IS 'Last executed sequence (for resuming)';
        COMMENT ON COLUMN warming_rooms.status IS 'Status room: STOPPED, ACTIVE, PAUSED, FINISHED';
        COMMENT ON COLUMN warming_rooms.interval_min_seconds IS 'Minimum interval between messages (seconds)';
        COMMENT ON COLUMN warming_rooms.interval_max_seconds IS 'Maximum interval between messages (seconds)';
        COMMENT ON COLUMN warming_rooms.next_run_at IS 'Next scheduled execution time (important for Cron/Worker)';

        -- Indexes for worker query performance
        CREATE INDEX IF NOT EXISTS idx_rooms_status ON warming_rooms(status);
        CREATE INDEX IF NOT EXISTS idx_rooms_next_run ON warming_rooms(next_run_at);
        
        -- Composite index for worker query: WHERE status = 'ACTIVE' AND next_run_at <= NOW()
        CREATE INDEX IF NOT EXISTS idx_rooms_status_next_run ON warming_rooms(status, next_run_at);
        
        CREATE INDEX IF NOT EXISTS idx_rooms_script_id ON warming_rooms(script_id);
        CREATE INDEX IF NOT EXISTS idx_rooms_sender_instance ON warming_rooms(sender_instance_id);
        CREATE INDEX IF NOT EXISTS idx_rooms_receiver_instance ON warming_rooms(receiver_instance_id);

        -- =====================================================
        -- Table: warming_logs
        -- Purpose: Warming execution history for audit trail
        -- =====================================================
        CREATE TABLE IF NOT EXISTS warming_logs (
            id                      BIGSERIAL PRIMARY KEY,
            room_id                 UUID NOT NULL,
            script_line_id          INT,
            sender_instance_id      VARCHAR(255) NOT NULL,
            receiver_instance_id    VARCHAR(255) NOT NULL,
            message_content         TEXT NOT NULL,
            status                  VARCHAR(20) NOT NULL CHECK (status IN ('SUCCESS', 'FAILED')),
            error_message           TEXT,
            executed_at             TIMESTAMP(6) WITH TIME ZONE NOT NULL DEFAULT NOW(),
            
            CONSTRAINT fk_logs_room 
                FOREIGN KEY (room_id) 
                REFERENCES warming_rooms(id) 
                ON DELETE CASCADE,
            
            CONSTRAINT fk_logs_script_line 
                FOREIGN KEY (script_line_id) 
                REFERENCES warming_script_lines(id) 
                ON DELETE SET NULL
        );

        COMMENT ON TABLE warming_logs IS 'Warming execution history for audit trail and debugging';
        COMMENT ON COLUMN warming_logs.room_id IS 'Reference to the room that performed the execution';
        COMMENT ON COLUMN warming_logs.script_line_id IS 'Reference to the script line that was executed (nullable if the line has been deleted)';
        COMMENT ON COLUMN warming_logs.sender_instance_id IS 'Sender ID snapshot at execution time';
        COMMENT ON COLUMN warming_logs.receiver_instance_id IS 'Receiver ID snapshot at execution time';
        COMMENT ON COLUMN warming_logs.message_content IS 'Final message that was sent (Spintax render result)';
        COMMENT ON COLUMN warming_logs.status IS 'Execution status: SUCCESS or FAILED';
        COMMENT ON COLUMN warming_logs.error_message IS 'Error details if status is FAILED';

        -- Indexes for history query and monitoring
        CREATE INDEX IF NOT EXISTS idx_logs_room_id ON warming_logs(room_id);
        CREATE INDEX IF NOT EXISTS idx_logs_executed_at ON warming_logs(executed_at);
        CREATE INDEX IF NOT EXISTS idx_logs_status ON warming_logs(status);
        
        -- Composite index for monitoring query: WHERE room_id = ? ORDER BY executed_at DESC
        CREATE INDEX IF NOT EXISTS idx_logs_room_executed ON warming_logs(room_id, executed_at DESC);
    `
	if _, err := db.Exec(warmingSchema); err != nil {
		log.Fatalf("failed to init warming schema: %v", err)
	}

	// Add send_real_message column if not exists (migration for existing tables)
	alterWarmingSchema := `
		ALTER TABLE warming_rooms 
		ADD COLUMN IF NOT EXISTS send_real_message BOOLEAN NOT NULL DEFAULT false;

		COMMENT ON COLUMN warming_rooms.send_real_message IS 'true = send real WA message, false = simulation only (dry-run mode)';
		
		-- HUMAN_VS_BOT feature columns
		ALTER TABLE warming_rooms
		ADD COLUMN IF NOT EXISTS room_type VARCHAR(20) NOT NULL DEFAULT 'BOT_VS_BOT'
			CHECK (room_type IN ('BOT_VS_BOT', 'HUMAN_VS_BOT')),
		ADD COLUMN IF NOT EXISTS whitelisted_number VARCHAR(50),
		ADD COLUMN IF NOT EXISTS reply_delay_min INT NOT NULL DEFAULT 10,
		ADD COLUMN IF NOT EXISTS reply_delay_max INT NOT NULL DEFAULT 60;
		
		COMMENT ON COLUMN warming_rooms.room_type IS 'BOT_VS_BOT: automated script exchange, HUMAN_VS_BOT: auto-reply to human';
		COMMENT ON COLUMN warming_rooms.whitelisted_number IS 'Phone number allowed to trigger auto-reply (format: 905123456789)';
		COMMENT ON COLUMN warming_rooms.reply_delay_min IS 'Minimum delay in seconds before replying (HUMAN_VS_BOT mode)';
		COMMENT ON COLUMN warming_rooms.reply_delay_max IS 'Maximum delay in seconds before replying (HUMAN_VS_BOT mode)';
		
		-- Indexes for HUMAN_VS_BOT queries
		CREATE INDEX IF NOT EXISTS idx_rooms_type ON warming_rooms(room_type);
		CREATE INDEX IF NOT EXISTS idx_rooms_whitelist ON warming_rooms(whitelisted_number);
	`
	if _, err := db.Exec(alterWarmingSchema); err != nil {
		log.Fatalf("failed to alter warming schema: %v", err)
	}

	// Warming Templates (Dynamic Templates)
	templatesSchema := `
		CREATE TABLE IF NOT EXISTS warming_templates (
			id SERIAL PRIMARY KEY,
			category VARCHAR(100) NOT NULL,
			name VARCHAR(255) NOT NULL,
			structure JSONB NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			CONSTRAINT unique_category_name UNIQUE (category, name)
		);
		CREATE INDEX IF NOT EXISTS idx_warming_templates_category ON warming_templates(category);
		COMMENT ON TABLE warming_templates IS 'Dynamic conversation templates for auto-generating dialog';
		COMMENT ON COLUMN warming_templates.category IS 'Template category: casual, business, customer_service';
		COMMENT ON COLUMN warming_templates.structure IS 'JSON array of dialog lines with message options';
	`
	if _, err := db.Exec(templatesSchema); err != nil {
		log.Fatalf("failed to create warming_templates table: %v", err)
	}

	// Auto-seed initial templates if table is empty
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM warming_templates").Scan(&count); err != nil {
		log.Printf("Warning: failed to check warming_templates count: %v", err)
	} else if count == 0 {
		log.Println("Seeding initial warming templates...")
		seedInitialTemplates(db)
	}

	// Add AI configuration fields to warming_rooms (if not exists)
	_, err := db.Exec(`
		ALTER TABLE warming_rooms 
		ADD COLUMN IF NOT EXISTS ai_enabled BOOLEAN DEFAULT FALSE,
		ADD COLUMN IF NOT EXISTS ai_provider VARCHAR(20) DEFAULT 'gemini',
		ADD COLUMN IF NOT EXISTS ai_model VARCHAR(50) DEFAULT 'gemini-1.5-flash',
		ADD COLUMN IF NOT EXISTS ai_system_prompt TEXT DEFAULT 'You are a helpful customer service assistant. Be friendly, concise, and professional.',
		ADD COLUMN IF NOT EXISTS ai_temperature DECIMAL(3,2) DEFAULT 0.7,
		ADD COLUMN IF NOT EXISTS ai_max_tokens INT DEFAULT 150,
		ADD COLUMN IF NOT EXISTS fallback_to_script BOOLEAN DEFAULT TRUE
	`)
	if err != nil {
		log.Printf("⚠️ Warning: Could not add AI fields to warming_rooms: %v", err)
	} else {
		log.Println("✅ AI configuration fields added to warming_rooms")
	}

	// Add sender_type field to warming_logs for AI context tracking
	_, err = db.Exec(`
		ALTER TABLE warming_logs 
		ADD COLUMN IF NOT EXISTS sender_type VARCHAR(10) DEFAULT 'bot'
	`)
	if err != nil {
		log.Printf("⚠️ Warning: Could not add sender_type to warming_logs: %v", err)
	} else {
		log.Println("✅ sender_type field added to warming_logs (for AI context)")
	}

	// =====================================================
	// USER MANAGEMENT SYSTEM SCHEMA (MUST BE BEFORE RBAC)
	// =====================================================
	userManagementSchema := `
		-- =====================================================
		-- Table: users
		-- Purpose: User accounts for authentication & authorization
		-- =====================================================
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username VARCHAR(50) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255),  -- Nullable for OAuth users
			full_name VARCHAR(100),
			avatar_url VARCHAR(500),  -- Profile picture from OAuth provider
			auth_provider VARCHAR(20) NOT NULL DEFAULT 'local',  -- 'local' or 'google'
			oauth_provider_id VARCHAR(255),  -- Google user ID
			role VARCHAR(20) NOT NULL DEFAULT 'user',
			is_active BOOLEAN DEFAULT true,
			email_verified BOOLEAN DEFAULT false,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			last_login_at TIMESTAMP WITH TIME ZONE,
			CONSTRAINT chk_role CHECK (role IN ('admin', 'user', 'viewer')),
			CONSTRAINT chk_auth_provider CHECK (auth_provider IN ('local', 'google'))
		);

		CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
		CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
		CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
		CREATE INDEX IF NOT EXISTS idx_users_auth_provider ON users(auth_provider);
		CREATE INDEX IF NOT EXISTS idx_users_oauth_provider_id ON users(oauth_provider_id);

		COMMENT ON TABLE users IS 'User accounts for authentication and authorization';
		COMMENT ON COLUMN users.username IS 'Unique username for login';
		COMMENT ON COLUMN users.email IS 'Unique email address';
		COMMENT ON COLUMN users.password_hash IS 'Bcrypt hashed password (NULL for OAuth users)';
		COMMENT ON COLUMN users.auth_provider IS 'Authentication provider: local (password) or google (OAuth)';
		COMMENT ON COLUMN users.oauth_provider_id IS 'External provider user ID (e.g., Google user ID)';
		COMMENT ON COLUMN users.role IS 'User role: admin (full access), user (standard), viewer (read-only)';

		-- =====================================================
		-- Table: refresh_tokens
		-- Purpose: Store refresh tokens for long-lived sessions
		-- =====================================================
		CREATE TABLE IF NOT EXISTS refresh_tokens (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) UNIQUE NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			revoked BOOLEAN DEFAULT false,
			ip_address VARCHAR(45),
			user_agent TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
		CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token ON refresh_tokens(token);
		CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

		COMMENT ON TABLE refresh_tokens IS 'Refresh tokens for maintaining user sessions';
		COMMENT ON COLUMN refresh_tokens.token IS 'Unique refresh token string';
		COMMENT ON COLUMN refresh_tokens.expires_at IS 'Token expiration timestamp';
		COMMENT ON COLUMN refresh_tokens.revoked IS 'True if token has been revoked (logout)';

		-- =====================================================
		-- Table: user_instances
		-- Purpose: User-instance access control (Admin/User model)
		-- =====================================================
		CREATE TABLE IF NOT EXISTS user_instances (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			instance_id VARCHAR(255) NOT NULL REFERENCES instances(instance_id) ON DELETE CASCADE,
			permission_level VARCHAR(20) NOT NULL DEFAULT 'access',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE(user_id, instance_id)
		);

		CREATE INDEX IF NOT EXISTS idx_user_instances_user_id ON user_instances(user_id);
		CREATE INDEX IF NOT EXISTS idx_user_instances_instance_id ON user_instances(instance_id);

		COMMENT ON TABLE user_instances IS 'User-instance access control (presence = authorized)';
		COMMENT ON COLUMN user_instances.permission_level IS 'Legacy field, not used for authorization (kept for compatibility)';

		-- =====================================================
		-- Table: audit_logs
		-- Purpose: Audit trail for security and compliance
		-- =====================================================
		CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGSERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			action VARCHAR(50) NOT NULL,
			resource_type VARCHAR(50),
			resource_id VARCHAR(255),
			details JSONB,
			ip_address VARCHAR(45),
			user_agent TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
		CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
		CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
		CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);

		COMMENT ON TABLE audit_logs IS 'Audit trail for all sensitive user actions';
		COMMENT ON COLUMN audit_logs.action IS 'Action performed: user.login, user.register, instance.create, message.send, etc.';
		COMMENT ON COLUMN audit_logs.details IS 'Additional context as JSON';
	`
	if _, err := db.Exec(userManagementSchema); err != nil {
		log.Fatalf("failed to init user management schema: %v", err)
	}

	// Add created_by column to instances table (for user-instance relationship)
	alterInstancesSchema := `
		ALTER TABLE instances
		ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;

		CREATE INDEX IF NOT EXISTS idx_instances_created_by ON instances(created_by);

		COMMENT ON COLUMN instances.created_by IS 'User ID who created this instance';
	`
	if _, err := db.Exec(alterInstancesSchema); err != nil {
		log.Printf("⚠️ Warning: Could not add created_by to instances: %v", err)
	} else {
		log.Println("✅ created_by field added to instances table")
	}

	// =====================================================
	// TOKEN BLACKLIST (for immediate logout/password change)
	// =====================================================
	tokenBlacklistSchema := `
		CREATE TABLE IF NOT EXISTS token_blacklist (
			id BIGSERIAL PRIMARY KEY,
			token TEXT NOT NULL,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			reason VARCHAR(50),
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_token_blacklist_token ON token_blacklist(token);
		CREATE INDEX IF NOT EXISTS idx_token_blacklist_expires_at ON token_blacklist(expires_at);
		CREATE INDEX IF NOT EXISTS idx_token_blacklist_user_id ON token_blacklist(user_id);

		COMMENT ON TABLE token_blacklist IS 'Blacklisted access tokens for immediate logout';
		COMMENT ON COLUMN token_blacklist.reason IS 'logout, password_change, security_breach, etc.';
	`
	if _, err := db.Exec(tokenBlacklistSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create token_blacklist: %v", err)
	} else {
		log.Println("✅ Token blacklist table created successfully")
	}

	// =====================================================
	// SYSTEM SETTINGS TABLE
	// =====================================================
	systemSettingsSchema := `
		CREATE TABLE IF NOT EXISTS system_settings (
			id SERIAL PRIMARY KEY,
			key VARCHAR(100) UNIQUE NOT NULL,
			value JSONB NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_system_settings_key ON system_settings(key);
		
		COMMENT ON TABLE system_settings IS 'Global system settings and configurations';
	`
	if _, err := db.Exec(systemSettingsSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create system_settings table: %v", err)
	} else {
		log.Println("✅ System settings table created successfully")
	}

	log.Println("✅ User management schema created successfully")

	// Add created_by columns to warming tables for RBAC (NOW users table exists)
	_, err = db.Exec(`
		-- Add created_by to warming_scripts
		ALTER TABLE warming_scripts 
		ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
		
		CREATE INDEX IF NOT EXISTS idx_warming_scripts_created_by ON warming_scripts(created_by);
		COMMENT ON COLUMN warming_scripts.created_by IS 'User ID who created this script';

		-- Add created_by to warming_templates
		ALTER TABLE warming_templates 
		ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
		
		CREATE INDEX IF NOT EXISTS idx_warming_templates_created_by ON warming_templates(created_by);
		COMMENT ON COLUMN warming_templates.created_by IS 'User ID who created this template';

		-- Add created_by to warming_rooms
		ALTER TABLE warming_rooms 
		ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
		
		CREATE INDEX IF NOT EXISTS idx_warming_rooms_created_by ON warming_rooms(created_by);
		COMMENT ON COLUMN warming_rooms.created_by IS 'User ID who created this room';

		-- Add created_by to warming_logs
		ALTER TABLE warming_logs 
		ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
		
		CREATE INDEX IF NOT EXISTS idx_warming_logs_created_by ON warming_logs(created_by);
		COMMENT ON COLUMN warming_logs.created_by IS 'User ID who owns the room that generated this log';
	`)
	if err != nil {
		log.Printf("⚠️ Warning: Could not add created_by to warming tables: %v", err)
	} else {
		log.Println("✅ created_by field added to warming tables for RBAC")
	}

	// Add unique constraint for whitelisted_number in ACTIVE HUMAN_VS_BOT rooms
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_active_human_room 
		ON warming_rooms (whitelisted_number) 
		WHERE status = 'ACTIVE' AND room_type = 'HUMAN_VS_BOT' AND whitelisted_number IS NOT NULL
	`)
	if err != nil {
		log.Printf("⚠️ Warning: Could not create unique index for whitelisted_number: %v", err)
	} else {
		log.Println("✅ Unique constraint added: One whitelisted number per active HUMAN_VS_BOT room")
	}

	// Add failed login tracking columns to users table
	_, err = db.Exec(`
		ALTER TABLE users
		ADD COLUMN IF NOT EXISTS failed_login_count INTEGER NOT NULL DEFAULT 0;

		ALTER TABLE users
		ADD COLUMN IF NOT EXISTS locked_until TIMESTAMP WITH TIME ZONE;
	`)
	if err != nil {
		log.Printf("⚠️ Warning: Could not add login lockout columns to users: %v", err)
	}

	log.Println("✅ User management schema created successfully")

	// =====================================================
	// WORKER BLAST OUTBOX SCHEMA
	// =====================================================
	workerConfigSchema := `
		CREATE TABLE IF NOT EXISTS outbox_worker_config (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			worker_name VARCHAR(100) NOT NULL,
			circle VARCHAR(50) NOT NULL,
			application VARCHAR(100) NOT NULL,
			message_type VARCHAR(20) DEFAULT 'direct' NOT NULL CHECK (message_type IN ('direct', 'group')),
			interval_seconds INTEGER DEFAULT 10 NOT NULL,
			enabled BOOLEAN DEFAULT true NOT NULL,
			webhook_url VARCHAR(255),
			webhook_secret VARCHAR(255),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			CONSTRAINT unique_user_worker UNIQUE (user_id, worker_name)
		);

		CREATE INDEX IF NOT EXISTS idx_worker_config_user_id ON outbox_worker_config(user_id);
		CREATE INDEX IF NOT EXISTS idx_worker_config_circle ON outbox_worker_config(circle);
		CREATE INDEX IF NOT EXISTS idx_worker_config_enabled ON outbox_worker_config(enabled);
		CREATE INDEX IF NOT EXISTS idx_worker_config_application ON outbox_worker_config(application);

		COMMENT ON TABLE outbox_worker_config IS 'Database-driven worker configuration for dynamic blast outbox processing';
	`
	if _, err := db.Exec(workerConfigSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create outbox_worker_config table: %v", err)
	} else {
		log.Println("✅ Worker blast outbox configuration table ensured")
	}

	// 3. Worker System Logs Table
	workerSystemLogsSchema := `
		CREATE TABLE IF NOT EXISTS worker_system_logs (
			id SERIAL PRIMARY KEY,
			worker_id INTEGER, -- Optional, links to outbox_worker_config
			worker_name VARCHAR(100) NOT NULL,
			level VARCHAR(10) NOT NULL, -- INFO, WARN, ERROR
			message TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
	`
	if _, err := db.Exec(workerSystemLogsSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create worker_system_logs table structure: %v", err)
	} else {
		log.Println("✅ Worker system logs table ensured")
	}

	log.Println("✅ Worker blast outbox configuration schema finalized")

	// =====================================================
	// ADDING MISSING COLUMNS (FOR EXISTING TABLES)
	// =====================================================
	// Postgres doesn't have "ADD COLUMN IF NOT EXISTS" directly in older versions,
	// so we use a DO block for safety.
	addColumnLogic := `
		DO $$ 
		BEGIN 
			BEGIN
				ALTER TABLE outbox_worker_config ADD COLUMN webhook_url VARCHAR(255);
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column webhook_url already exists, skipping';
			END;
			BEGIN
				ALTER TABLE outbox_worker_config ADD COLUMN webhook_secret VARCHAR(255);
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column webhook_secret already exists, skipping';
			END;
			BEGIN
				ALTER TABLE outbox_worker_config ADD COLUMN allow_media BOOLEAN DEFAULT false NOT NULL;
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column allow_media already exists, skipping';
			END;
			BEGIN
				ALTER TABLE outbox_worker_config ADD COLUMN interval_max_seconds INTEGER DEFAULT 0;
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column interval_max_seconds already exists, skipping';
			END;
			BEGIN
				ALTER TABLE worker_system_logs ADD COLUMN worker_id INTEGER;
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column worker_id already exists, skipping';
			END;
		END $$;
	`
	if _, err := db.Exec(addColumnLogic); err != nil {
		log.Printf("⚠️ Warning: Could not add missing columns: %v", err)
	} else {
		log.Println("✅ Missing columns checked/added")

		// Create indices only AFTER columns are guaranteed to exist
		indexLogic := `
			CREATE INDEX IF NOT EXISTS idx_worker_system_logs_worker_id ON worker_system_logs(worker_id);
			CREATE INDEX IF NOT EXISTS idx_worker_system_logs_worker_name ON worker_system_logs(worker_name);
			CREATE INDEX IF NOT EXISTS idx_worker_system_logs_created_at ON worker_system_logs(created_at);
		`
		if _, err := db.Exec(indexLogic); err != nil {
			log.Printf("⚠️ Warning: Could not create indices for worker_system_logs: %v", err)
		} else {
			log.Println("✅ Worker system logs indices ensured")
		}
	}

	// Expand application column to TEXT for multi-application support
	expandApplicationColumn := `
		ALTER TABLE outbox_worker_config 
		ALTER COLUMN application TYPE TEXT;
	`
	if _, err := db.Exec(expandApplicationColumn); err != nil {
		log.Printf("⚠️ Warning: Could not expand application column: %v", err)
	} else {
		log.Println("✅ Application column expanded to TEXT for multi-application support")
	}

	// =====================================================
	// OUTBOX QUEUE SCHEMA (For Message Blasting)
	// =====================================================
	outboxTableSchema := `
		CREATE TABLE IF NOT EXISTS outbox (
			id_outbox SERIAL PRIMARY KEY,
			type INTEGER DEFAULT 1,
			from_number VARCHAR(20),
			client_id INTEGER,
			destination VARCHAR(100) NOT NULL,
			messages TEXT NOT NULL,
			status INTEGER DEFAULT 0,
			priority INTEGER DEFAULT 0,
			application VARCHAR(100),
			sendingDateTime TIMESTAMP WITH TIME ZONE,
			insertDateTime TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			table_id VARCHAR(100),
			file VARCHAR(255),
			error_count INTEGER DEFAULT 0,
			msg_error TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_outbox_status ON outbox(status);
		CREATE INDEX IF NOT EXISTS idx_outbox_application ON outbox(application);
		CREATE INDEX IF NOT EXISTS idx_outbox_insert_dt ON outbox(insertDateTime);

		COMMENT ON TABLE outbox IS 'Queue table for outgoing WhatsApp messages';
	`
	if _, err := db.Exec(outboxTableSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create outbox table: %v", err)
	} else {
		log.Println("✅ Outbox queue table ensured")
	}

	// Ensure table_id exists in outbox (for older installations)
	addOutboxColumnLogic := `
		DO $$ 
		BEGIN 
			BEGIN
				ALTER TABLE outbox ADD COLUMN table_id VARCHAR(100);
			EXCEPTION
				WHEN duplicate_column THEN RAISE NOTICE 'column table_id already exists, skipping';
			END;
		END $$;
	`
	_, _ = db.Exec(addOutboxColumnLogic)

	// ─── API KEYS TABLE ─────────────────────────────────
	apiKeysSchema := `
		CREATE TABLE IF NOT EXISTS api_keys (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key_hash VARCHAR(64) NOT NULL UNIQUE,
			key_prefix VARCHAR(8) NOT NULL,
			name VARCHAR(100) NOT NULL,
			application VARCHAR(100),
			enabled BOOLEAN DEFAULT true NOT NULL,
			last_used_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
		CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);
	`
	if _, err := db.Exec(apiKeysSchema); err != nil {
		log.Printf("⚠️ Warning: Could not create api_keys table: %v", err)
	} else {
		log.Println("✅ API keys table ensured")
	}
}

// seedInitialTemplates populates warming_templates with initial conversation templates
func seedInitialTemplates(db *sql.DB) {
	type templateLine struct {
		ActorRole      string   `json:"actorRole"`
		MessageOptions []string `json:"messageOptions"`
	}

	templates := []struct {
		Category string
		Name     string
		Lines    []templateLine
	}{
		{
			Category: "casual",
			Name:     "Casual Chat 1",
			Lines: []templateLine{
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Hello|Hi|Morning|Afternoon|Evening} {buddy|mate|friend|pal}",
					"{How's it going|What's up} {man|buddy|mate}?",
					"Are you {busy|free} {right now|today|at the moment}?",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Hello|Hi} to you too, {doing well|all good|fine} here",
					"{Just chilling|Nothing much} {really|man|buddy}",
					"I'm {free|available|not busy} {right now|at the moment}",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Great|Awesome|Good} to hear",
					"{Oh|Wow} {nice|cool|sweet} then",
					"{Good|Awesome} {stuff|news|to know}",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Yeah|Yep|That's right} {man|buddy|mate}",
					"{How about|What about} {you|yourself}?",
					"What are you {doing|up to} {now|today}?",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Good|Fine|Doing well} {here|too|thanks}",
					"I'm {working|at the office|working from home} {right now|today}",
					"{Same old|Nothing special} {really|here|buddy}",
				}},
			},
		},
		{
			Category: "business",
			Name:     "Product Buy and Sell",
			Lines: []templateLine{
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Hello|Hi|Excuse me}, I'd like to {ask|inquire} about something",
					"{Excuse me|Sorry} to {bother|interrupt}",
					"{Hello|Hi}, is the {item|product|stock} {available|ready}?",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Hello|Hi}, {yes|sure} we {have it|do}",
					"{Yes|Sure}, what are you {looking for|interested in}?",
					"{Go ahead|Please}, what would you like to {ask|know}?",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"I'm {looking for|interested in} product {A|B|C}",
					"Is {stock|the item} {available|in stock}?",
					"What's the {price|cost} for {this|that} {product|item}?",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Available|In stock|Ready} for you",
					"The {price|cost} is {around|approximately} {100|200|300}",
					"How {many|much} would you like to {order|get}?",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"I'll {take|order|get} {1|2|3} {for now|please}",
					"{Ok|Sure|Got it}, where do I {send|transfer} payment?",
					"Can I pay by {COD|cash|transfer}?",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Sure|Of course}, {COD|transfer} {works|is fine}",
					"{Ok|Sure}, the {total|total amount} is {around|about} {100|200|300}",
					"What's your {address|location}?",
				}},
			},
		},
		{
			Category: "customer_service",
			Name:     "Customer Support",
			Lines: []templateLine{
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Hello|Hi} {admin|support|there}, I'd like to {ask|inquire} about something",
					"{Excuse me|Sorry}, can you {help|assist} me?",
					"{Hello|Hi}, I {need|want} some {info|information}",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{Hello|Hi}, how can {we help|I assist} you?",
					"Good {morning|afternoon|evening}, {please go ahead|how can I help}?",
					"{Yes|Sure}, what {help|info} do you need?",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"I'd like to {ask|inquire} about your {product|service}",
					"How do I {order|place an order}?",
					"What's the {shipping cost|delivery fee} to {Jakarta|Bandung|Surabaya}?",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"For our {product|service}, we {have|offer} {A|B|C}",
					"To {order|place an order}, you can {chat|contact} {us|our admin}",
					"The {shipping cost|delivery fee} is around {10|20|30}",
				}},
				{ActorRole: "ACTOR_A", MessageOptions: []string{
					"{Oh|Ah} {I see|got it}, {ok|sure|alright} then",
					"{Great|Ok}, {thank you|thanks} {so much|a lot}",
					"{Sure|Ok}, I'll {order|place an order} {later|soon}",
				}},
				{ActorRole: "ACTOR_B", MessageOptions: []string{
					"{You're welcome|No problem|Happy to help}",
					"{Sure|Alright}, we {look forward to|await} your order",
					"{Glad|Happy} to {help|assist} you",
				}},
			},
		},
	}

	for _, tmpl := range templates {
		structureJSON, err := json.Marshal(tmpl.Lines)
		if err != nil {
			log.Printf("Failed to marshal template %s: %v", tmpl.Name, err)
			continue
		}

		_, err = db.Exec(
			"INSERT INTO warming_templates (category, name, structure) VALUES ($1, $2, $3)",
			tmpl.Category,
			tmpl.Name,
			structureJSON,
		)
		if err != nil {
			log.Printf("Failed to insert template %s: %v", tmpl.Name, err)
		} else {
			log.Printf("  ✓ Seeded template: %s (%s)", tmpl.Name, tmpl.Category)
		}
	}

	log.Println("Initial templates seeded successfully")
}
