package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
)

type openAILegacySessionHashContextKey struct{}

type openAIStickySessionWriteSuppressedContextKey struct{}

var openAILegacySessionHashKey = openAILegacySessionHashContextKey{}

func withOpenAIStickySessionWriteSuppressed(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIStickySessionWriteSuppressedContextKey{}, true)
}

func openAIStickySessionWriteSuppressed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	suppressed, _ := ctx.Value(openAIStickySessionWriteSuppressedContextKey{}).(bool)
	return suppressed
}

var (
	openAIStickyLegacyReadFallbackTotal atomic.Int64
	openAIStickyLegacyReadFallbackHit   atomic.Int64
	openAIStickyLegacyDualWriteTotal    atomic.Int64
)

// openAIStickySessionAtomicCache is an optional cache capability used by the
// scheduler's first-writer-wins binding path. The base GatewayCache interface
// remains unchanged for existing in-memory and test implementations.
type openAIStickySessionAtomicCache interface {
	ClaimSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) (bool, error)
	CompareAndDeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string, expectedAccountID int64) (bool, error)
}

type openAIStickySessionCompareSetCache interface {
	CompareAndSetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, expectedAccountID, accountID int64, ttl time.Duration) (bool, error)
}

var ErrStickySessionClaimConflict = errors.New("sticky session binding claimed by another account")

func openAIStickyCompatStats() (legacyReadFallbackTotal, legacyReadFallbackHit, legacyDualWriteTotal int64) {
	return openAIStickyLegacyReadFallbackTotal.Load(),
		openAIStickyLegacyReadFallbackHit.Load(),
		openAIStickyLegacyDualWriteTotal.Load()
}

// DeriveSessionHashFromSeed computes the current-format sticky-session hash
// from an arbitrary seed string.
func DeriveSessionHashFromSeed(seed string) string {
	currentHash, _ := deriveOpenAISessionHashes(seed)
	return currentHash
}

func deriveOpenAISessionHashes(sessionID string) (currentHash string, legacyHash string) {
	normalized := strings.TrimSpace(sessionID)
	if normalized == "" {
		return "", ""
	}

	currentHash = fmt.Sprintf("%016x", xxhash.Sum64String(normalized))
	sum := sha256.Sum256([]byte(normalized))
	legacyHash = hex.EncodeToString(sum[:])
	return currentHash, legacyHash
}

func withOpenAILegacySessionHash(ctx context.Context, legacyHash string) context.Context {
	if ctx == nil {
		return nil
	}
	trimmed := strings.TrimSpace(legacyHash)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, openAILegacySessionHashKey, trimmed)
}

func openAILegacySessionHashFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(openAILegacySessionHashKey).(string)
	return strings.TrimSpace(value)
}

func attachOpenAILegacySessionHashToGin(c *gin.Context, legacyHash string) {
	if c == nil || c.Request == nil {
		return
	}
	c.Request = c.Request.WithContext(withOpenAILegacySessionHash(c.Request.Context(), legacyHash))
}

func (s *OpenAIGatewayService) openAISessionHashReadOldFallbackEnabled() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAIWS.SessionHashReadOldFallback
}

func (s *OpenAIGatewayService) openAISessionHashDualWriteOldEnabled() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAIWS.SessionHashDualWriteOld
}

func (s *OpenAIGatewayService) openAISessionCacheKey(sessionHash string) string {
	normalized := strings.TrimSpace(sessionHash)
	if normalized == "" {
		return ""
	}
	return "openai:" + normalized
}

func (s *OpenAIGatewayService) openAILegacySessionCacheKey(ctx context.Context, sessionHash string) string {
	legacyHash := openAILegacySessionHashFromContext(ctx)
	if legacyHash == "" {
		return ""
	}
	legacyKey := "openai:" + legacyHash
	if legacyKey == s.openAISessionCacheKey(sessionHash) {
		return ""
	}
	return legacyKey
}

func (s *OpenAIGatewayService) openAIStickyLegacyTTL(ttl time.Duration) time.Duration {
	legacyTTL := ttl
	if legacyTTL <= 0 {
		legacyTTL = openaiStickySessionTTL
	}
	if legacyTTL > 10*time.Minute {
		return 10 * time.Minute
	}
	return legacyTTL
}

func (s *OpenAIGatewayService) getStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, nil
	}

	primaryKey := s.openAISessionCacheKey(sessionHash)
	if primaryKey == "" {
		return 0, nil
	}

	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), primaryKey)
	if err == nil && accountID > 0 {
		return accountID, nil
	}
	if !s.openAISessionHashReadOldFallbackEnabled() {
		return accountID, err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey == "" {
		return accountID, err
	}

	openAIStickyLegacyReadFallbackTotal.Add(1)
	legacyAccountID, legacyErr := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), legacyKey)
	if legacyErr == nil && legacyAccountID > 0 {
		openAIStickyLegacyReadFallbackHit.Add(1)
		return legacyAccountID, nil
	}
	return accountID, err
}

func (s *OpenAIGatewayService) setStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if s == nil || s.cache == nil || accountID <= 0 || strings.TrimSpace(sessionHash) == "" || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	primaryKey := s.openAISessionCacheKey(sessionHash)
	if primaryKey == "" {
		return nil
	}

	if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), primaryKey, accountID, ttl); err != nil {
		return err
	}

	if !s.openAISessionHashDualWriteOldEnabled() {
		return nil
	}
	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey == "" {
		return nil
	}
	if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), legacyKey, accountID, s.openAIStickyLegacyTTL(ttl)); err != nil {
		return err
	}
	openAIStickyLegacyDualWriteTotal.Add(1)
	return nil
}

func (s *OpenAIGatewayService) refreshStickySessionTTL(ctx context.Context, groupID *int64, sessionHash string, ttl time.Duration) error {
	if s == nil || s.cache == nil || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	primaryKey := s.openAISessionCacheKey(sessionHash)
	if primaryKey == "" {
		return nil
	}

	err := s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), primaryKey, ttl)
	if !s.openAISessionHashReadOldFallbackEnabled() && !s.openAISessionHashDualWriteOldEnabled() {
		return err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey != "" {
		_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), legacyKey, s.openAIStickyLegacyTTL(ttl))
	}
	return err
}

func (s *OpenAIGatewayService) deleteStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string) error {
	if s == nil || s.cache == nil || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	primaryKey := s.openAISessionCacheKey(sessionHash)
	if primaryKey == "" {
		return nil
	}

	err := s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), primaryKey)
	if !s.openAISessionHashReadOldFallbackEnabled() && !s.openAISessionHashDualWriteOldEnabled() {
		return err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey != "" {
		_ = s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), legacyKey)
	}
	return err
}

// claimStickySessionAccountID uses the cache's atomic claim when available.
// Older cache implementations retain the historical overwrite behavior so
// this capability can be rolled out without breaking existing deployments.
func (s *OpenAIGatewayService) claimStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if s == nil || s.cache == nil || accountID <= 0 || strings.TrimSpace(sessionHash) == "" || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	if atomicCache, ok := s.cache.(openAIStickySessionAtomicCache); ok {
		claimed, err := atomicCache.ClaimSessionAccountID(ctx, derefGroupID(groupID), s.openAISessionCacheKey(sessionHash), accountID, ttl)
		if err != nil {
			return err
		}
		if !claimed {
			return ErrStickySessionClaimConflict
		}
		if s.openAISessionHashDualWriteOldEnabled() {
			legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
			if legacyKey != "" {
				if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), legacyKey, accountID, s.openAIStickyLegacyTTL(ttl)); err != nil {
					return err
				}
				openAIStickyLegacyDualWriteTotal.Add(1)
			}
		}
		return nil
	}
	return s.setStickySessionAccountID(ctx, groupID, sessionHash, accountID, ttl)
}

// replaceStickySessionAccountID conditionally migrates a weighted binding. A
// zero expected owner means the caller is claiming a missing key; an existing
// owner must match either the expected or the new owner. Older cache adapters
// retain their historical read/write behavior; production Redis uses Lua.
func (s *OpenAIGatewayService) replaceStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string, expectedAccountID, accountID int64, ttl time.Duration) error {
	if s == nil || s.cache == nil || accountID <= 0 || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	primaryKey := s.openAISessionCacheKey(sessionHash)
	if primaryKey == "" {
		return nil
	}
	if atomicCache, ok := s.cache.(openAIStickySessionCompareSetCache); ok {
		replaced, err := atomicCache.CompareAndSetSessionAccountID(ctx, derefGroupID(groupID), primaryKey, expectedAccountID, accountID, ttl)
		if err != nil {
			return err
		}
		if !replaced {
			return ErrStickySessionClaimConflict
		}
		if s.openAISessionHashDualWriteOldEnabled() {
			legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
			if legacyKey != "" {
				if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), legacyKey, accountID, s.openAIStickyLegacyTTL(ttl)); err != nil {
					return err
				}
				openAIStickyLegacyDualWriteTotal.Add(1)
			}
		}
		return nil
	}
	if expectedAccountID > 0 {
		current, err := s.getStickySessionAccountID(ctx, groupID, sessionHash)
		if err != nil && !errors.Is(err, ErrStickySessionNotFound) {
			return err
		}
		if current > 0 && current != expectedAccountID && current != accountID {
			return ErrStickySessionClaimConflict
		}
	}
	return s.setStickySessionAccountID(ctx, groupID, sessionHash, accountID, ttl)
}

// deleteStickySessionAccountIDIfOwner performs a compare-and-delete when the
// cache supports it, preserving the legacy delete fallback for old caches.
func (s *OpenAIGatewayService) deleteStickySessionAccountIDIfOwner(ctx context.Context, groupID *int64, sessionHash string, expectedAccountID int64) error {
	if s == nil || s.cache == nil || expectedAccountID <= 0 || openAIStickySessionWriteSuppressed(ctx) {
		return nil
	}
	if atomicCache, ok := s.cache.(openAIStickySessionAtomicCache); ok {
		_, err := atomicCache.CompareAndDeleteSessionAccountID(ctx, derefGroupID(groupID), s.openAISessionCacheKey(sessionHash), expectedAccountID)
		if err != nil {
			return err
		}
		if s.openAISessionHashReadOldFallbackEnabled() || s.openAISessionHashDualWriteOldEnabled() {
			legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
			if legacyKey != "" {
				_, _ = atomicCache.CompareAndDeleteSessionAccountID(ctx, derefGroupID(groupID), legacyKey, expectedAccountID)
			}
		}
		return nil
	}
	return s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
}
