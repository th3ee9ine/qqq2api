package setup

import (
	"context"
	"database/sql/driver"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/spf13/viper"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"golang.org/x/crypto/bcrypt"
)

type passwordHashMatcher struct {
	password string
}

func (m passwordHashMatcher) Match(value driver.Value) bool {
	hash, ok := value.(string)
	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(m.password)) == nil
}

func TestDecideAdminBootstrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		totalUsers           int64
		configuredAdminUsers int64
		should               bool
		reason               string
	}{
		{
			name:                 "empty database should create admin",
			totalUsers:           0,
			configuredAdminUsers: 0,
			should:               true,
			reason:               adminBootstrapReasonEmptyDatabase,
		},
		{
			name:                 "configured admin exists should preserve password",
			totalUsers:           10,
			configuredAdminUsers: 1,
			should:               false,
			reason:               adminBootstrapReasonAdminExists,
		},
		{
			name:                 "other users including admins do not replace configured admin",
			totalUsers:           5,
			configuredAdminUsers: 0,
			should:               true,
			reason:               adminBootstrapReasonUsersExistWithoutAdmin,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideAdminBootstrap(tc.totalUsers, tc.configuredAdminUsers)
			if got.shouldCreate != tc.should {
				t.Fatalf("shouldCreate=%v, want %v", got.shouldCreate, tc.should)
			}
			if got.reason != tc.reason {
				t.Fatalf("reason=%q, want %q", got.reason, tc.reason)
			}
		})
	}
}

func TestCreateAdminUserWithDBCreatesConfiguredAdminWhenOtherAdminExists(t *testing.T) {
	t.Setenv("RUN_MODE", "standard")

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		adminID       = int64(42)
		adminEmail    = "system-admin@example.com"
		adminPassword = "environment-password"
	)
	cfg := &SetupConfig{Admin: AdminConfig{Email: adminEmail, Password: adminPassword}}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(1) FROM users")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))
	// The configured address is absent even though the populated database may
	// already contain another active administrator.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, role, status FROM users WHERE LOWER(BTRIM(email)) = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1")).
		WithArgs(adminEmail).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role", "status"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO users .* ON CONFLICT \\(email\\) WHERE deleted_at IS NULL").
		WithArgs(
			adminEmail,
			passwordHashMatcher{password: adminPassword},
			service.RoleAdmin,
			float64(0),
			defaultUserConcurrency,
			service.StatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(adminID))
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("UPDATE batch_image_jobs").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	created, reason, err := createAdminUserWithDB(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("createAdminUserWithDB() error = %v", err)
	}
	if !created {
		t.Fatal("createAdminUserWithDB() created = false, want true")
	}
	if reason != adminBootstrapReasonUsersExistWithoutAdmin {
		t.Fatalf("createAdminUserWithDB() reason = %q, want %q", reason, adminBootstrapReasonUsersExistWithoutAdmin)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCreateAdminUserWithDBPreservesExistingConfiguredAdminPassword(t *testing.T) {
	t.Setenv("RUN_MODE", "standard")

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		adminID         = int64(42)
		configuredEmail = " System-Admin@Example.COM "
		normalizedEmail = "system-admin@example.com"
		environmentPass = "stale-environment-password"
	)
	cfg := &SetupConfig{Admin: AdminConfig{Email: configuredEmail, Password: environmentPass}}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(1) FROM users")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, role, status FROM users WHERE LOWER(BTRIM(email)) = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1")).
		WithArgs(normalizedEmail).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role", "status"}).AddRow(adminID, service.RoleAdmin, service.StatusActive))
	mock.ExpectBegin()
	// No users UPDATE is expected: a fully bootstrapped administrator keeps its
	// password hash and later account changes.
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("UPDATE batch_image_jobs").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	created, reason, err := createAdminUserWithDB(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("createAdminUserWithDB() error = %v", err)
	}
	if created {
		t.Fatal("createAdminUserWithDB() created = true, want false for existing configured admin")
	}
	if reason != adminBootstrapReasonAdminExists {
		t.Fatalf("createAdminUserWithDB() reason = %q, want %q", reason, adminBootstrapReasonAdminExists)
	}
	if cfg.Admin.Email != normalizedEmail {
		t.Fatalf("configured admin email = %q, want %q", cfg.Admin.Email, normalizedEmail)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCreateAdminUserWithDBResetsPasswordWhenPromotingConfiguredUser(t *testing.T) {
	t.Setenv("RUN_MODE", "standard")

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		adminID       = int64(84)
		adminEmail    = "system-admin@example.com"
		adminPassword = "environment-password"
	)
	cfg := &SetupConfig{Admin: AdminConfig{Email: adminEmail, Password: adminPassword}}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(1) FROM users")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, role, status FROM users WHERE LOWER(BTRIM(email)) = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1")).
		WithArgs(adminEmail).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role", "status"}).AddRow(adminID, service.RoleUser, service.StatusDisabled))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE users SET email = $2, password_hash = $3, role = $4, concurrency = $5, status = $6, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL RETURNING id")).
		WithArgs(
			adminID,
			adminEmail,
			passwordHashMatcher{password: adminPassword},
			service.RoleAdmin,
			defaultUserConcurrency,
			service.StatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(adminID))
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("UPDATE batch_image_jobs").
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	created, reason, err := createAdminUserWithDB(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("createAdminUserWithDB() error = %v", err)
	}
	if !created {
		t.Fatal("createAdminUserWithDB() created = false, want bootstrap to run for inactive non-admin user")
	}
	if reason != adminBootstrapReasonUsersExistWithoutAdmin {
		t.Fatalf("createAdminUserWithDB() reason = %q, want %q", reason, adminBootstrapReasonUsersExistWithoutAdmin)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSetupDefaultAdminConcurrency(t *testing.T) {
	t.Run("simple mode admin uses higher concurrency", func(t *testing.T) {
		t.Setenv("RUN_MODE", "simple")
		if got := setupDefaultAdminConcurrency(); got != simpleModeAdminConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, simpleModeAdminConcurrency)
		}
	})

	t.Run("standard mode keeps existing default", func(t *testing.T) {
		t.Setenv("RUN_MODE", "standard")
		if got := setupDefaultAdminConcurrency(); got != defaultUserConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, defaultUserConcurrency)
		}
	})
}

func TestNeedsSetupSkipsWhenSkipSetupIsEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "true", value: "true"},
		{name: "one", value: "1"},
		{name: "yes", value: "yes"},
		{name: "trimmed mixed case true", value: "  TrUe  "},
		{name: "trimmed mixed case yes", value: "  YeS  "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATA_DIR", t.TempDir())
			t.Setenv("SKIP_SETUP", tc.value)

			if NeedsSetup() {
				t.Fatalf("NeedsSetup() = true, want false when SKIP_SETUP is enabled")
			}
		})
	}
}

func TestNeedsSetupFallsBackToFileDetectionWhenSkipSetupIsDisabled(t *testing.T) {
	tests := []struct {
		name         string
		skipSetupSet bool
		skipSetup    string
		markerFile   string
		want         bool
	}{
		{
			name: "unset without installation files",
			want: true,
		},
		{
			name:         "false without installation files",
			skipSetupSet: true,
			skipSetup:    " false ",
			want:         true,
		},
		{
			name:         "invalid value without installation files",
			skipSetupSet: true,
			skipSetup:    "enabled",
			want:         true,
		},
		{
			name:         "config file exists",
			skipSetupSet: true,
			skipSetup:    "false",
			markerFile:   ConfigFileName,
			want:         false,
		},
		{
			name:         "install lock file exists",
			skipSetupSet: true,
			skipSetup:    "invalid",
			markerFile:   InstallLockFile,
			want:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("DATA_DIR", dataDir)
			if tc.skipSetupSet {
				t.Setenv("SKIP_SETUP", tc.skipSetup)
			} else {
				originalValue, wasSet := os.LookupEnv("SKIP_SETUP")
				if err := os.Unsetenv("SKIP_SETUP"); err != nil {
					t.Fatalf("Unsetenv(SKIP_SETUP) error = %v", err)
				}
				t.Cleanup(func() {
					if wasSet {
						_ = os.Setenv("SKIP_SETUP", originalValue)
						return
					}
					_ = os.Unsetenv("SKIP_SETUP")
				})
			}

			if tc.markerFile != "" {
				if err := os.WriteFile(filepath.Join(dataDir, tc.markerFile), nil, 0o600); err != nil {
					t.Fatalf("WriteFile(%s) error = %v", tc.markerFile, err)
				}
			}

			if got := NeedsSetup(); got != tc.want {
				t.Fatalf("NeedsSetup() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetupMigrationTimeout(t *testing.T) {
	t.Run("uses default timeout when unset", func(t *testing.T) {
		cfg := &SetupConfig{}
		if got := cfg.migrationTimeout(); got != 60*time.Second {
			t.Fatalf("migrationTimeout()=%s, want 60s", got)
		}
	})

	t.Run("uses configured timeout", func(t *testing.T) {
		cfg := &SetupConfig{MigrationTimeoutSeconds: 300}
		if got := cfg.migrationTimeout(); got != 300*time.Second {
			t.Fatalf("migrationTimeout()=%s, want 300s", got)
		}
	})
}

func TestWriteConfigFileKeepsDefaultUserConcurrency(t *testing.T) {
	t.Setenv("RUN_MODE", "simple")
	t.Setenv("DATA_DIR", t.TempDir())

	if err := writeConfigFile(&SetupConfig{}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(data), "user_concurrency: 5") {
		t.Fatalf("config missing default user concurrency, got:\n%s", string(data))
	}
}

func TestWriteConfigFileIncludesRedisUsername(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	if err := writeConfigFile(&SetupConfig{
		Redis: RedisConfig{
			Host:     "redis",
			Port:     6379,
			Username: "app-user",
		},
	}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(data), "username: app-user") {
		t.Fatalf("config missing Redis username, got:\n%s", string(data))
	}
}

func TestWriteConfigFilePersistsAdminEmailForConfigLoad(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("CONFIG_FILE", GetConfigFilePath())
	t.Setenv("ADMIN_EMAIL", "")

	const (
		adminEmail    = "admin@example.com"
		adminPassword = "must-not-be-persisted"
	)
	if err := writeConfigFile(&SetupConfig{
		Admin:  AdminConfig{Email: adminEmail, Password: adminPassword},
		Server: ServerConfig{Host: "0.0.0.0", Port: 8080, Mode: "release"},
		Database: DatabaseConfig{
			Host: "db", Port: 5432, User: "postgres", DBName: "sub2api", SSLMode: "disable",
		},
		Redis: RedisConfig{Host: "redis", Port: 6379},
		JWT:   JWTConfig{Secret: strings.Repeat("a", 64), ExpireHour: 24},
	}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "admin_email: "+adminEmail) {
		t.Fatalf("config missing persisted admin email, got:\n%s", string(data))
	}
	if strings.Contains(string(data), adminPassword) {
		t.Fatal("config must not persist the bootstrap admin password")
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() without ADMIN_EMAIL error = %v", err)
	}
	if loaded.Default.AdminEmail != adminEmail {
		t.Fatalf("loaded admin email = %q, want persisted %q", loaded.Default.AdminEmail, adminEmail)
	}

	const overrideEmail = "override@example.com"
	viper.Reset()
	t.Setenv("ADMIN_EMAIL", overrideEmail)
	loaded, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load() with ADMIN_EMAIL error = %v", err)
	}
	if loaded.Default.AdminEmail != overrideEmail {
		t.Fatalf("loaded admin email = %q, want environment override %q", loaded.Default.AdminEmail, overrideEmail)
	}
}

func TestBuildDatabaseConnectionDSNsUsesPostgresForBootstrap(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:     "db",
		Port:     5432,
		User:     "sub2api",
		Password: "secret",
		DBName:   "sub2api",
		SSLMode:  "disable",
	}

	bootstrapDSN, targetDSN := buildDatabaseConnectionDSNs(cfg)

	if !strings.Contains(bootstrapDSN, "dbname=postgres") {
		t.Fatalf("bootstrap DSN = %q, want default postgres database", bootstrapDSN)
	}
	if strings.Contains(bootstrapDSN, "dbname=sub2api") {
		t.Fatalf("bootstrap DSN = %q, should not connect to target database before checking/creating it", bootstrapDSN)
	}
	if !strings.Contains(targetDSN, "dbname=sub2api") {
		t.Fatalf("target DSN = %q, want configured database", targetDSN)
	}
}
