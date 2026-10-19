package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/ent/setting"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"golang.org/x/crypto/bcrypt"
)

func createBootstrapCredentialUser(t *testing.T, client *dbent.Client, email, role string) *dbent.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("original-password"), bcrypt.MinCost)
	require.NoError(t, err)
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash(string(hash)).
		SetRole(role).
		SetStatus(service.StatusActive).
		SetUsername("existing-operator").
		SetNotes("preserve operator settings").
		SetBalance(27.25).
		SetFrozenBalance(3.5).
		SetConcurrency(17).
		SetTotpEnabled(true).
		SetTotpSecretEncrypted("existing-encrypted-totp-secret").
		SetTotpEnabledAt(createdAt).
		SetCreatedAt(createdAt).
		SetUpdatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func bootstrapCredentialConfig(email string) *config.Config {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "bootstrap-credential-test-secret", ExpireHour: 1}}
	cfg.Default.AdminEmail = email
	return cfg
}

func TestEnsureAdminCredentialsFromEnvUpdatesExistingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, envEmail, envPassword, wantEmail, wantPassword string
	}{
		{"email_and_password", "  NEW-ADMIN@Example.COM  ", "new-password", "new-admin@example.com", "new-password"},
		{"password_only", "", "new-password", "old-admin@example.com", "new-password"},
		{"email_only_preserves_password", "new-admin@example.com", "", "new-admin@example.com", "original-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ADMIN_EMAIL", tc.envEmail)
			t.Setenv("ADMIN_PASSWORD", tc.envPassword)
			repo, client := newUserEntRepo(t)
			ctx := context.Background()
			before := createBootstrapCredentialUser(t, client, "old-admin@example.com", service.RoleAdmin)
			key, err := client.APIKey.Create().SetUserID(before.ID).SetKey("existing-key").SetName("existing-key").Save(ctx)
			require.NoError(t, err)
			job, err := client.BatchImageJob.Create().SetBatchID("existing-batch").SetUserID(before.ID).
				SetProvider("openai").SetModel("gpt-image-1").SetItemCount(1).Save(ctx)
			require.NoError(t, err)
			cfg := bootstrapCredentialConfig(before.Email)
			auth := service.NewAuthService(repo, nil, cfg, nil, nil)
			oldToken, _, err := auth.Login(ctx, before.Email, "original-password")
			require.NoError(t, err)

			require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
			after, err := client.User.Get(ctx, before.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantEmail, after.Email)
			require.NoError(t, bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte(tc.wantPassword)))
			if tc.envPassword == "" {
				require.Equal(t, before.PasswordHash, after.PasswordHash)
			} else {
				require.NotEqual(t, before.PasswordHash, after.PasswordHash)
			}
			require.Equal(t, before.Role, after.Role)
			require.Equal(t, before.Status, after.Status)
			require.Equal(t, before.Username, after.Username)
			require.Equal(t, before.Notes, after.Notes)
			require.Equal(t, before.Balance, after.Balance)
			require.Equal(t, before.FrozenBalance, after.FrozenBalance)
			require.Equal(t, before.Concurrency, after.Concurrency)
			require.Equal(t, before.TotpEnabled, after.TotpEnabled)
			require.Equal(t, before.TotpSecretEncrypted, after.TotpSecretEncrypted)
			require.Equal(t, before.TotpEnabledAt, after.TotpEnabledAt)
			require.Equal(t, before.CreatedAt, after.CreatedAt)
			require.Equal(t, before.ID, client.APIKey.GetX(ctx, key.ID).UserID)
			require.Equal(t, before.ID, client.BatchImageJob.GetX(ctx, job.ID).UserID)
			require.Equal(t, 1, client.User.Query().CountX(ctx))

			require.Equal(t, tc.wantEmail, cfg.Default.AdminEmail)
			newToken, user, err := auth.Login(ctx, tc.wantEmail, tc.wantPassword)
			require.NoError(t, err)
			require.Equal(t, before.ID, user.ID)
			_, _, err = auth.Login(ctx, before.Email, "original-password")
			require.ErrorIs(t, err, service.ErrInvalidCredentials)
			_, err = auth.RefreshToken(ctx, oldToken)
			require.ErrorIs(t, err, service.ErrTokenRevoked)
			_, err = auth.RefreshToken(ctx, newToken)
			require.NoError(t, err)

			// The same environment on the next restart must not rehash the password
			// or touch the row, otherwise its existing sessions would be revoked.
			require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
			repeated := client.User.GetX(ctx, before.ID)
			require.Equal(t, after.PasswordHash, repeated.PasswordHash)
			require.Equal(t, after.UpdatedAt, repeated.UpdatedAt)
			_, err = auth.RefreshToken(ctx, newToken)
			require.NoError(t, err)
		})
	}
}

func TestEnsureAdminCredentialsFromEnvNoEnvironmentIsNoOp(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	_, client := newUserEntRepo(t)
	before := createBootstrapCredentialUser(t, client, "old-admin@example.com", service.RoleAdmin)
	cfg := bootstrapCredentialConfig("different-config-admin@example.com")
	cfg.Default.AdminPassword = "config-password-must-not-reset-account"

	require.NoError(t, ensureAdminCredentialsFromEnv(context.Background(), client, cfg))
	after := client.User.GetX(context.Background(), before.ID)
	require.Equal(t, before.Email, after.Email)
	require.Equal(t, before.PasswordHash, after.PasswordHash)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt)
	require.Equal(t, "different-config-admin@example.com", cfg.Default.AdminEmail)
	require.Zero(t, client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).CountX(context.Background()))
}

func TestEnsureAdminCredentialsFromEnvKeepsSavedIdentityAcrossRestarts(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "renamed-admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "replacement-password")
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	admin := createBootstrapCredentialUser(t, client, "installation-admin@example.com", service.RoleAdmin)
	cfg := bootstrapCredentialConfig(admin.Email)
	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
	require.Equal(t, "renamed-admin@example.com", cfg.Default.AdminEmail)
	renamed := client.User.GetX(ctx, admin.ID)
	marker := client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).OnlyX(ctx)

	// config.yaml still has the installation email after environment variables
	// are removed. Every fresh process must recover the saved identity.
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	for restart := 0; restart < 2; restart++ {
		cfg = bootstrapCredentialConfig(admin.Email)
		require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
		require.Equal(t, renamed.Email, cfg.Default.AdminEmail)
		auth := service.NewAuthService(repo, nil, cfg, nil, nil)
		_, signedIn, err := auth.Login(ctx, renamed.Email, "replacement-password")
		require.NoError(t, err)
		require.Equal(t, admin.ID, signedIn.ID)
	}
	afterRestarts := client.User.GetX(ctx, admin.ID)
	require.Equal(t, renamed.PasswordHash, afterRestarts.PasswordHash)
	require.Equal(t, renamed.UpdatedAt, afterRestarts.UpdatedAt)

	// The old email may subsequently belong to another user. A password-only
	// change must follow the saved administrator ID instead of that stale email.
	ordinary := createBootstrapCredentialUser(t, client, admin.Email, service.RoleUser)
	t.Setenv("ADMIN_PASSWORD", "second-replacement-password")
	cfg = bootstrapCredentialConfig(admin.Email)
	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
	require.Equal(t, renamed.Email, cfg.Default.AdminEmail)
	afterPassword := client.User.GetX(ctx, admin.ID)
	require.Equal(t, renamed.Email, afterPassword.Email)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(afterPassword.PasswordHash), []byte("second-replacement-password")))
	untouchedOrdinary := client.User.GetX(ctx, ordinary.ID)
	require.Equal(t, ordinary.PasswordHash, untouchedOrdinary.PasswordHash)
	require.Equal(t, ordinary.UpdatedAt, untouchedOrdinary.UpdatedAt)
	require.Equal(t, ordinary.Role, untouchedOrdinary.Role)

	// A second administrator must not make a later rename ambiguous once the
	// environment-managed administrator identity has been recorded.
	other := createBootstrapCredentialUser(t, client, "other-admin@example.com", service.RoleAdmin)
	t.Setenv("ADMIN_EMAIL", "renamed-again@example.com")
	t.Setenv("ADMIN_PASSWORD", "")
	cfg = bootstrapCredentialConfig(admin.Email)
	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
	require.Equal(t, "renamed-again@example.com", cfg.Default.AdminEmail)
	afterRename := client.User.GetX(ctx, admin.ID)
	require.Equal(t, cfg.Default.AdminEmail, afterRename.Email)
	require.Equal(t, afterPassword.PasswordHash, afterRename.PasswordHash)
	untouchedAdmin := client.User.GetX(ctx, other.ID)
	require.Equal(t, other.Email, untouchedAdmin.Email)
	require.Equal(t, other.PasswordHash, untouchedAdmin.PasswordHash)
	require.Equal(t, other.UpdatedAt, untouchedAdmin.UpdatedAt)
	finalMarker := client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).OnlyX(ctx)
	require.Equal(t, marker.Value, finalMarker.Value)
	require.Equal(t, marker.UpdatedAt, finalMarker.UpdatedAt)
	auth := service.NewAuthService(repo, nil, cfg, nil, nil)
	_, signedIn, err := auth.Login(ctx, cfg.Default.AdminEmail, "second-replacement-password")
	require.NoError(t, err)
	require.Equal(t, admin.ID, signedIn.ID)
}

func TestEnsureAdminCredentialsFromEnvRejectsInvalidEmailWithoutMutation(t *testing.T) {
	for _, email := range []string{"not-an-email", "Admin <admin@example.com>", "admin@", "admin@example.com,other@example.com"} {
		t.Run(email, func(t *testing.T) {
			for _, savedIdentity := range []bool{false, true} {
				name := "without_saved_identity"
				if savedIdentity {
					name = "with_saved_identity"
				}
				t.Run(name, func(t *testing.T) {
					t.Setenv("ADMIN_EMAIL", "admin@example.com")
					t.Setenv("ADMIN_PASSWORD", "original-password")
					_, client := newUserEntRepo(t)
					ctx := context.Background()
					admin := createBootstrapCredentialUser(t, client, "admin@example.com", service.RoleAdmin)
					cfg := bootstrapCredentialConfig(admin.Email)
					var marker *dbent.Setting
					if savedIdentity {
						require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
						marker = client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).OnlyX(ctx)
					}
					t.Setenv("ADMIN_EMAIL", email)
					t.Setenv("ADMIN_PASSWORD", "must-not-be-applied")

					require.Error(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
					after := client.User.GetX(ctx, admin.ID)
					require.Equal(t, admin.Email, after.Email)
					require.Equal(t, admin.PasswordHash, after.PasswordHash)
					require.Equal(t, admin.UpdatedAt, after.UpdatedAt)
					require.Equal(t, admin.Email, cfg.Default.AdminEmail)
					afterMarker, err := client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).Only(ctx)
					if marker == nil {
						require.True(t, dbent.IsNotFound(err), "invalid email must not create an administrator marker")
					} else {
						require.NoError(t, err)
						require.Equal(t, marker.Value, afterMarker.Value)
						require.Equal(t, marker.UpdatedAt, afterMarker.UpdatedAt)
					}
				})
			}
		})
	}
}

func TestEnsureAdminCredentialsFromEnvSamePasswordDoesNotRehash(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "original-password")
	_, client := newUserEntRepo(t)
	before := createBootstrapCredentialUser(t, client, "admin@example.com", service.RoleAdmin)

	require.NoError(t, ensureAdminCredentialsFromEnv(context.Background(), client, bootstrapCredentialConfig(before.Email)))
	after := client.User.GetX(context.Background(), before.ID)
	require.Equal(t, before.PasswordHash, after.PasswordHash)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt)
}

func TestEnsureAdminCredentialsFromEnvUsesMatchingAdminAmongMultipleAdmins(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "  TARGET@example.com ")
	t.Setenv("ADMIN_PASSWORD", "new-password")
	_, client := newUserEntRepo(t)
	other := createBootstrapCredentialUser(t, client, "other@example.com", service.RoleAdmin)
	target := createBootstrapCredentialUser(t, client, "Target@Example.com", service.RoleAdmin)

	require.NoError(t, ensureAdminCredentialsFromEnv(context.Background(), client, bootstrapCredentialConfig(other.Email)))
	after := client.User.GetX(context.Background(), target.ID)
	require.Equal(t, "target@example.com", after.Email)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("new-password")))
	untouched := client.User.GetX(context.Background(), other.ID)
	require.Equal(t, other.Email, untouched.Email)
	require.Equal(t, other.PasswordHash, untouched.PasswordHash)
	require.Equal(t, other.UpdatedAt, untouched.UpdatedAt)
}

func TestEnsureAdminCredentialsFromEnvPreservesDisabledStatus(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "new-password")
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	admin := createBootstrapCredentialUser(t, client, "admin@example.com", service.RoleAdmin)
	client.User.UpdateOneID(admin.ID).SetStatus(service.StatusDisabled).SaveX(ctx)
	cfg := bootstrapCredentialConfig(admin.Email)

	require.NoError(t, ensureAdminCredentialsFromEnv(ctx, client, cfg))
	after := client.User.GetX(ctx, admin.ID)
	require.Equal(t, service.StatusDisabled, after.Status)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("new-password")))
	auth := service.NewAuthService(repo, nil, cfg, nil, nil)
	_, _, err := auth.Login(ctx, admin.Email, "new-password")
	require.ErrorIs(t, err, service.ErrUserNotActive)
}

func TestEnsureAdminCredentialsFromEnvRejectsConflictsWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []struct{ email, role string }
	}{
		{"target_is_ordinary_user", []struct{ email, role string }{{"old@example.com", service.RoleAdmin}, {"target@example.com", service.RoleUser}}},
		{"target_is_account_admin", []struct{ email, role string }{{"old@example.com", service.RoleAdmin}, {"target@example.com", service.RoleAccountAdmin}}},
		{"duplicate_normalized_email", []struct{ email, role string }{{"Target@Example.com", service.RoleAdmin}, {" target@example.com ", service.RoleAdmin}}},
		{"ambiguous_existing_admins", []struct{ email, role string }{{"first@example.com", service.RoleAdmin}, {"second@example.com", service.RoleAdmin}}},
		{"missing_existing_admin", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ADMIN_EMAIL", "target@example.com")
			t.Setenv("ADMIN_PASSWORD", "new-password")
			_, client := newUserEntRepo(t)
			ctx := context.Background()
			var before []*dbent.User
			for _, row := range tc.rows {
				before = append(before, createBootstrapCredentialUser(t, client, row.email, row.role))
			}

			require.Error(t, ensureAdminCredentialsFromEnv(ctx, client, bootstrapCredentialConfig("target@example.com")))
			require.Equal(t, len(before), client.User.Query().CountX(ctx))
			for _, original := range before {
				after := client.User.GetX(ctx, original.ID)
				require.Equal(t, original.Email, after.Email)
				require.Equal(t, original.PasswordHash, after.PasswordHash)
				require.Equal(t, original.Role, after.Role)
				require.Equal(t, original.Status, after.Status)
				require.Equal(t, original.UpdatedAt, after.UpdatedAt)
			}
		})
	}
}
