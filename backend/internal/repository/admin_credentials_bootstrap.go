package repository

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"entgo.io/ent/dialect"
	"github.com/go-playground/validator/v10"
	"github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/ent/setting"
	"github.com/th3ee9ine/qqq2api/ent/user"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const bootstrapAdminUserIDKey = "bootstrap_admin_user_id"

// ensureAdminCredentialsFromEnv also runs for installed instances: setup only
// creates the initial administrator, whereas runtime authorization immediately
// follows ADMIN_EMAIL. Keep the existing account's credentials in sync before
// accepting requests, without recreating it or transferring its resources.
func ensureAdminCredentialsFromEnv(ctx context.Context, client *ent.Client, cfg *config.Config) error {
	email := normalizeEmailLookupValue(os.Getenv("ADMIN_EMAIL"))
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" && password == "" {
		// An environment-driven rename must remain usable after the variables
		// are removed, even if config.yaml still contains the installation email.
		adminID, err := loadBootstrapAdminUserID(ctx, client)
		if err != nil || adminID == 0 {
			return err
		}
		admin, err := client.User.Query().Where(user.IDEQ(adminID),
			user.RoleEQ(service.RoleAdmin), user.DeletedAtIsNil()).Only(ctx)
		if err != nil {
			return fmt.Errorf("load saved super administrator: %w", err)
		}
		cfg.Default.AdminEmail = admin.Email
		return nil
	}
	updateEmail := email != ""
	if updateEmail && validator.New().Var(email, "required,email") != nil {
		return fmt.Errorf("sync admin credentials: ADMIN_EMAIL must be a valid email address")
	}
	if email == "" {
		email = normalizeEmailLookupValue(cfg.Default.AdminEmail)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin admin credential sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := ent.NewTxContext(ctx, tx)
	txClient := tx.Client()

	// Serialize concurrent starts, and share the email uniqueness lock used by
	// normal user updates so differently cased addresses cannot collide.
	release, err := lockRepositoryScopedKeys(txCtx, txClient,
		txAwareSQLExecutor(txCtx, nil, txClient),
		"users:admin-credential-bootstrap", normalizedEmailUniquenessLockKey(email))
	if err != nil {
		return fmt.Errorf("lock admin credential sync: %w", err)
	}
	defer release()
	savedAdminID, err := loadBootstrapAdminUserID(txCtx, txClient)
	if err != nil {
		return err
	}

	query := txClient.User.Query().Where(user.DeletedAtIsNil(), user.Or(
		user.RoleEQ(service.RoleAdmin), userEmailLookupPredicate(email),
	)).Order(ent.Asc(user.FieldID))
	if txClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	candidates, err := query.All(txCtx)
	if err != nil {
		return fmt.Errorf("find administrator for credential sync: %w", err)
	}
	var selected *ent.User
	var savedAdmin *ent.User
	var admins []*ent.User
	for _, candidate := range candidates {
		if candidate.Role == service.RoleAdmin {
			admins = append(admins, candidate)
			if candidate.ID == savedAdminID {
				savedAdmin = candidate
			}
		}
		if (updateEmail || savedAdminID == 0) && email != "" && normalizeEmailLookupValue(candidate.Email) == email {
			if selected != nil {
				return fmt.Errorf("sync admin credentials: ADMIN_EMAIL matches multiple users")
			}
			selected = candidate
		}
	}
	if !updateEmail && savedAdminID != 0 && savedAdmin == nil {
		return fmt.Errorf("sync admin credentials: saved super administrator no longer exists; set ADMIN_EMAIL to an existing super administrator")
	}
	if selected != nil && selected.Role != service.RoleAdmin {
		return fmt.Errorf("sync admin credentials: ADMIN_EMAIL is already used by a non-super-administrator account")
	}
	// Password-only changes address the saved identity, not a stale email in
	// config.yaml. A new explicit email can also rename that known identity.
	if savedAdmin != nil && (!updateEmail || selected == nil) {
		selected = savedAdmin
	}
	if selected == nil {
		switch {
		case len(admins) == 0:
			return fmt.Errorf("sync admin credentials: no existing super administrator; complete initial setup first")
		case email == "":
			// Match AuthService's legacy fallback when no email was ever saved.
			for _, admin := range admins {
				if admin.Status == service.StatusActive {
					selected = admin
					break
				}
			}
			if selected == nil {
				return fmt.Errorf("sync admin credentials: no active legacy super administrator")
			}
		case len(admins) == 1:
			selected = admins[0]
		default:
			return fmt.Errorf("sync admin credentials: ADMIN_EMAIL does not match an existing administrator and multiple super administrators exist; use the current administrator email first")
		}
	}

	update := txClient.User.UpdateOneID(selected.ID)
	emailChanged := updateEmail && email != selected.Email
	if emailChanged {
		update.SetEmail(email)
	}
	admin := &service.User{PasswordHash: selected.PasswordHash}
	passwordChanged := password != "" && !admin.CheckPassword(password)
	if passwordChanged {
		if err := admin.SetPassword(password); err != nil {
			return fmt.Errorf("hash ADMIN_PASSWORD: %w", err)
		}
		update.SetPasswordHash(admin.PasswordHash)
	}
	// bcrypt uses a random salt. Rehashing an unchanged password would revoke
	// all sessions on every restart because token versions include the hash.
	if emailChanged || passwordChanged {
		if err := update.Exec(txCtx); err != nil {
			return fmt.Errorf("sync admin credentials: %w", err)
		}
	}
	if savedAdminID != selected.ID {
		if err := txClient.Setting.Create().SetKey(bootstrapAdminUserIDKey).
			SetValue(strconv.FormatInt(selected.ID, 10)).
			OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(txCtx); err != nil {
			return fmt.Errorf("save super administrator identity: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin credential sync: %w", err)
	}
	if emailChanged {
		cfg.Default.AdminEmail = email
	} else {
		cfg.Default.AdminEmail = selected.Email
	}
	if emailChanged || passwordChanged {
		slog.Info("administrator credentials synchronized from environment",
			"user_id", selected.ID, "email_changed", emailChanged, "password_changed", passwordChanged)
	}
	return nil
}

func loadBootstrapAdminUserID(ctx context.Context, client *ent.Client) (int64, error) {
	stored, err := client.Setting.Query().Where(setting.KeyEQ(bootstrapAdminUserIDKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load saved super administrator identity: %w", err)
	}
	id, err := strconv.ParseInt(stored.Value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid saved super administrator identity")
	}
	return id, nil
}
