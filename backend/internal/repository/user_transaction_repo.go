package repository

import (
	"context"
	"database/sql"

	"hash/fnv"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"entgo.io/ent/dialect"

	dbent "github.com/th3ee9ine/qqq2api/ent"
)

type sqlQueryExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

var repositoryScopedKeyLocks = newScopedKeyLockRegistry()

type scopedKeyLockRegistry struct {
	mu    sync.Mutex
	locks map[string]*scopedKeyLockEntry
}

type scopedKeyLockEntry struct {
	mu   sync.Mutex
	refs int
}

func newScopedKeyLockRegistry() *scopedKeyLockRegistry {
	return &scopedKeyLockRegistry{
		locks: make(map[string]*scopedKeyLockEntry),
	}
}

func (r *scopedKeyLockRegistry) lock(keys ...string) func() {
	normalized := normalizeLockKeys(keys...)
	if len(normalized) == 0 {
		return func() {}
	}

	entries := make([]*scopedKeyLockEntry, 0, len(normalized))
	r.mu.Lock()
	for _, key := range normalized {
		entry := r.locks[key]
		if entry == nil {
			entry = &scopedKeyLockEntry{}
			r.locks[key] = entry
		}
		entry.refs++
		entries = append(entries, entry)
	}
	r.mu.Unlock()

	for _, entry := range entries {
		entry.mu.Lock()
	}

	return func() {
		for i := len(entries) - 1; i >= 0; i-- {
			entries[i].mu.Unlock()
		}

		r.mu.Lock()
		defer r.mu.Unlock()
		for idx, key := range normalized {
			entry := entries[idx]
			entry.refs--
			if entry.refs == 0 {
				delete(r.locks, key)
			}
		}
	}
}

func normalizeLockKeys(keys ...string) []string {
	if len(keys) == 0 {
		return nil
	}

	deduped := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		deduped[trimmed] = struct{}{}
	}
	if len(deduped) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(deduped))
	for key := range deduped {
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized
}

func advisoryLockHash(key string) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key))
	return int64(hasher.Sum64())
}

func lockRepositoryScopedKeys(ctx context.Context, client *dbent.Client, exec sqlQueryExecutor, keys ...string) (func(), error) {
	release := repositoryScopedKeyLocks.lock(keys...)
	normalized := normalizeLockKeys(keys...)
	if len(normalized) == 0 || client == nil || exec == nil || client.Driver().Dialect() != dialect.Postgres {
		return release, nil
	}

	for _, key := range normalized {
		rows, err := exec.QueryContext(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockHash(key))
		if err != nil {
			release()
			return nil, err
		}
		_ = rows.Close()
	}
	return release, nil
}

func (r *userRepository) WithUserAdminTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *userRepository) UpdateUserLastActiveAt(ctx context.Context, userID int64, activeAt time.Time) error {
	_, err := clientFromContext(ctx, r.client).User.UpdateOneID(userID).
		SetLastActiveAt(activeAt).
		Save(ctx)
	return err
}

func txAwareSQLExecutor(ctx context.Context, fallback sqlExecutor, client *dbent.Client) sqlQueryExecutor {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		if exec := sqlExecutorFromEntClient(tx.Client()); exec != nil {
			return exec
		}
	}
	if fallback != nil {
		return fallback
	}
	return sqlExecutorFromEntClient(client)
}

func sqlExecutorFromEntClient(client *dbent.Client) sqlQueryExecutor {
	if client == nil {
		return nil
	}

	clientValue := reflect.ValueOf(client).Elem()
	configValue := clientValue.FieldByName("config")
	driverValue := configValue.FieldByName("driver")
	if !driverValue.IsValid() {
		return nil
	}

	driver := reflect.NewAt(driverValue.Type(), unsafe.Pointer(driverValue.UnsafeAddr())).Elem().Interface()
	exec, ok := driver.(sqlQueryExecutor)
	if !ok {
		return nil
	}
	return exec
}
