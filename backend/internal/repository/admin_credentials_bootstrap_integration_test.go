//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminCredentialsStartupPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var savedMarkerValue string
	var savedMarkerUpdatedAt time.Time
	markerErr := integrationDB.QueryRowContext(ctx,
		"SELECT value, updated_at FROM settings WHERE key = $1", bootstrapAdminUserIDKey).
		Scan(&savedMarkerValue, &savedMarkerUpdatedAt)
	if markerErr != nil {
		require.ErrorIs(t, markerErr, sql.ErrNoRows)
	}
	suffix := time.Now().UnixNano()
	oldEmail := fmt.Sprintf("startup-admin-old-%d@example.com", suffix)
	newEmail := fmt.Sprintf("startup-admin-new-%d@example.com", suffix)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("original-password"), bcrypt.MinCost)
	require.NoError(t, err)
	admin := mustCreateUser(t, integrationEntClient, &service.User{
		Email: oldEmail, PasswordHash: string(oldHash), Role: service.RoleAdmin,
		Status: service.StatusActive, Balance: 31.25, Concurrency: 13,
	})
	keyValue := fmt.Sprintf("sk-startup-admin-%d", suffix)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if markerErr == nil {
			_, err := integrationDB.ExecContext(cleanupCtx,
				`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
				bootstrapAdminUserIDKey, savedMarkerValue, savedMarkerUpdatedAt)
			require.NoError(t, err)
		} else {
			_, err := integrationDB.ExecContext(cleanupCtx, "DELETE FROM settings WHERE key = $1", bootstrapAdminUserIDKey)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE user_id = $1", admin.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", admin.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx,
			"DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')", keyValue)
		require.NoError(t, err)
	})
	key, err := integrationEntClient.APIKey.Create().SetUserID(admin.ID).
		SetKey(keyValue).SetName("startup-existing-key").Save(ctx)
	require.NoError(t, err)

	newConfig := func() *config.Config {
		cfg := &config.Config{JWT: config.JWTConfig{Secret: "postgres-bootstrap-test-secret", ExpireHour: 1}}
		cfg.Default.AdminEmail = oldEmail
		return cfg
	}
	// Select this test's administrator explicitly, even if another integration
	// test previously saved a marker. Cleanup restores that prior setting.
	t.Setenv("ADMIN_EMAIL", oldEmail)
	t.Setenv("ADMIN_PASSWORD", "original-password")
	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, integrationEntClient, newConfig()))
	t.Setenv("ADMIN_EMAIL", newEmail)
	t.Setenv("ADMIN_PASSWORD", "replacement-password")
	cfg := newConfig()
	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, integrationEntClient, cfg))
	require.Equal(t, newEmail, cfg.Default.AdminEmail)
	updated, err := integrationEntClient.User.Get(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, newEmail, updated.Email)
	require.Equal(t, admin.Role, updated.Role)
	require.Equal(t, admin.Balance, updated.Balance)
	require.Equal(t, admin.Concurrency, updated.Concurrency)
	require.NotEqual(t, string(oldHash), updated.PasswordHash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("replacement-password")))
	userRepo := NewUserRepository(integrationEntClient, integrationDB)
	auth := service.NewAuthService(userRepo, nil, cfg, nil, nil)
	token, signedIn, err := auth.Login(ctx, newEmail, "replacement-password")
	require.NoError(t, err)
	require.Equal(t, admin.ID, signedIn.ID)
	_, _, err = auth.Login(ctx, newEmail, "original-password")
	require.ErrorIs(t, err, service.ErrInvalidCredentials)

	// Real PostgreSQL transactions contend on both the bootstrap advisory lock
	// and the administrator row. Unchanged credentials must stay unchanged even
	// when several replicas start at the same time.
	const starters = 8
	start := make(chan struct{})
	results := make(chan error, starters)
	for i := 0; i < starters; i++ {
		go func() {
			<-start
			results <- ensureAdminCredentialsFromEnv(ctx, integrationEntClient, newConfig())
		}()
	}
	close(start)
	for i := 0; i < starters; i++ {
		select {
		case err := <-results:
			require.NoError(t, err, "concurrent startup %d", i+1)
		case <-ctx.Done():
			t.Fatalf("concurrent administrator startup did not finish: %v", ctx.Err())
		}
	}
	afterRestart, err := integrationEntClient.User.Get(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, updated.Email, afterRestart.Email)
	require.Equal(t, updated.PasswordHash, afterRestart.PasswordHash)
	require.Equal(t, updated.UpdatedAt, afterRestart.UpdatedAt)
	ownedKey, err := integrationEntClient.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, admin.ID, ownedKey.UserID)
	require.Equal(t, keyValue, ownedKey.Key)
	_, err = auth.RefreshToken(ctx, token)
	require.NoError(t, err, "unchanged credentials must preserve the authenticated session")

	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	for restart := 0; restart < 2; restart++ {
		restoredCfg := newConfig()
		require.NoError(t, ensureAdminCredentialsFromEnv(ctx, integrationEntClient, restoredCfg))
		require.Equal(t, newEmail, restoredCfg.Default.AdminEmail)
		restoredAuth := service.NewAuthService(userRepo, nil, restoredCfg, nil, nil)
		_, signedIn, err := restoredAuth.Login(ctx, newEmail, "replacement-password")
		require.NoError(t, err)
		require.Equal(t, admin.ID, signedIn.ID)
	}
}
