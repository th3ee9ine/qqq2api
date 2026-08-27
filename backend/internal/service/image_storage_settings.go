package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const settingKeyImageStorageConfig = "image_storage_config"

// ErrSecretEncryptionKeyNotConfigured prevents persisting an object-storage
// secret with an ephemeral key that would become undecryptable after restart.
var ErrSecretEncryptionKeyNotConfigured = infraerrors.BadRequest(
	"SECRET_ENCRYPTION_KEY_NOT_CONFIGURED",
	"cannot store the S3 secret access key: no fixed secret encryption key is configured, so the auto-generated key would change on every restart and make the stored secret undecryptable after a restart or upgrade. Set a fixed TOTP_ENCRYPTION_KEY (e.g. generate one with `openssl rand -hex 32`) and try again",
)

// ErrImageStorageIncomplete 表示开关已打开但凭证不全，无法启用异步生图。
var ErrImageStorageIncomplete = errors.New("image storage is enabled but bucket/access_key_id/secret_access_key are incomplete")

// ImageStorageFactory 由 repository 层提供，把配置变成一个可用的对象存储实现，
// 避免 service 反向依赖 repository。
type ImageStorageFactory func(ctx context.Context, cfg *config.ImageStorageConfig) (ImageStorage, error)

// ImageStorageSettings 是异步生图对象存储配置的持久化表示。
type ImageStorageSettings struct {
	Enabled bool `json:"enabled"`

	Bucket           string `json:"bucket"`
	Prefix           string `json:"prefix"`
	PublicBaseURL    string `json:"public_base_url"`
	PresignExpiry    int    `json:"presign_expiry_hours"`
	MaxDownloadBytes int64  `json:"max_download_bytes"`

	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"` //nolint:revive // field name follows AWS convention
	ForcePathStyle  bool   `json:"force_path_style"`
}

// ImageStorageSettingService 读写持久化设置，并把结果解析成一个可直接使用的 uploader。
//
// 解析结果带缓存：网关每次请求都要判断功能是否开启，不能每次都查库。设置变更时调用
// Invalidate 清缓存，下一次请求即重建客户端，无需重启进程。
type ImageStorageSettingService struct {
	settingRepo             SettingRepository
	encryptor               SecretEncryptor
	factory                 ImageStorageFactory
	encryptionKeyConfigured bool

	// fallback 是 config.yaml 里的配置。后台从未保存过设置时沿用它，
	// 保证升级前已用配置文件开启该功能的部署不被打断。
	fallback config.ImageStorageConfig

	mu       sync.Mutex
	resolved bool
	uploader *ImageResultUploader
	enabled  bool
}

func NewImageStorageSettingService(
	settingRepo SettingRepository,
	encryptor SecretEncryptor,
	factory ImageStorageFactory,
	fallback config.ImageStorageConfig,
	encryptionKeyConfigured bool,
) *ImageStorageSettingService {
	return &ImageStorageSettingService{
		settingRepo:             settingRepo,
		encryptor:               encryptor,
		factory:                 factory,
		encryptionKeyConfigured: encryptionKeyConfigured,
		fallback:                fallback,
	}
}

// Resolver 返回可注入 ImageTaskService 的解析函数。
func (s *ImageStorageSettingService) Resolver() ImageStorageResolver {
	return func() (*ImageResultUploader, bool) {
		return s.resolve()
	}
}

func (s *ImageStorageSettingService) resolve() (*ImageResultUploader, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved {
		return s.uploader, s.enabled
	}

	ctx := context.Background()
	s.resolved = true
	s.uploader, s.enabled = nil, false

	cfg, err := s.effectiveConfig(ctx)
	if err != nil {
		logger.L().Warn("image_storage.settings_load_failed; async image tasks stay disabled", zap.Error(err))
		return nil, false
	}
	if !cfg.Enabled {
		return nil, false
	}
	if !cfg.IsConfigured() {
		logger.L().Warn("image_storage is enabled but not fully configured; async image tasks are disabled",
			zap.Strings("missing_keys", cfg.MissingCredentialKeys()))
		return nil, false
	}

	storage, err := s.factory(ctx, cfg)
	if err != nil {
		logger.L().Error("image_storage.client_build_failed; async image tasks stay disabled", zap.Error(err))
		return nil, false
	}
	s.uploader = NewImageResultUploader(storage, cfg.Prefix, cfg.MaxDownloadByte, nil)
	s.enabled = true
	return s.uploader, true
}

// Invalidate 丢弃缓存，使下一次请求按最新设置重新解析。
func (s *ImageStorageSettingService) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.resolved = false
	s.uploader = nil
	s.enabled = false
	s.mu.Unlock()
}

// Get 返回后台设置（SecretAccessKey 已脱敏）。从未保存过时返回 config.yaml 的等价值。
func (s *ImageStorageSettingService) Get(ctx context.Context) (*ImageStorageSettings, error) {
	settings, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = settingsFromConfig(s.fallback)
	}
	settings.SecretAccessKey = ""
	return settings, nil
}

// SecretConfigured 供前端展示"已配置"占位符。
func (s *ImageStorageSettingService) SecretConfigured(ctx context.Context) bool {
	settings, err := s.load(ctx)
	if err != nil || settings == nil {
		return s.fallback.SecretAccessKey != ""
	}
	return settings.SecretAccessKey != "" || s.fallback.SecretAccessKey != ""
}

// Update 保存设置并立即生效。SecretAccessKey 留空表示沿用已保存的值。
func (s *ImageStorageSettingService) Update(ctx context.Context, in ImageStorageSettings) (*ImageStorageSettings, error) {
	normalizeImageStorageSettings(&in)

	if in.SecretAccessKey == "" {
		if old, err := s.load(ctx); err == nil && old != nil {
			in.SecretAccessKey = old.SecretAccessKey
		}
	} else {
		// 拒绝用自动生成的临时密钥加密：重启后密文无法解密（#4524）。
		if !s.encryptionKeyConfigured {
			return nil, ErrSecretEncryptionKeyNotConfigured
		}
		encrypted, err := s.encryptor.Encrypt(in.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt secret: %w", err)
		}
		in.SecretAccessKey = encrypted
	}

	data, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal image storage settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, settingKeyImageStorageConfig, string(data)); err != nil {
		return nil, fmt.Errorf("save image storage settings: %w", err)
	}
	s.Invalidate()

	in.SecretAccessKey = ""
	return &in, nil
}

// TestConnection 用给定设置试建一次客户端，用于后台的"测试连接"按钮。
// 与 Update 一样支持留空 SecretAccessKey 表示沿用已保存的值。
func (s *ImageStorageSettingService) TestConnection(ctx context.Context, in ImageStorageSettings) error {
	normalizeImageStorageSettings(&in)
	if in.SecretAccessKey == "" {
		old, err := s.load(ctx)
		if err == nil && old != nil {
			in.SecretAccessKey = old.SecretAccessKey
		}
		if in.SecretAccessKey == "" {
			in.SecretAccessKey = s.fallback.SecretAccessKey
		}
	}
	cfg, err := s.toImageStorageConfig(&in)
	if err != nil {
		return err
	}
	if !cfg.IsConfigured() {
		return ErrImageStorageIncomplete
	}
	if _, err := s.factory(ctx, cfg); err != nil {
		return err
	}
	return nil
}

// effectiveConfig 把后台设置（或 config.yaml 回落）解析成运行时配置。
func (s *ImageStorageSettingService) effectiveConfig(ctx context.Context) (*config.ImageStorageConfig, error) {
	settings, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		fallback := s.fallback
		return &fallback, nil
	}
	cfg, err := s.toImageStorageConfig(settings)
	if err != nil {
		return nil, err
	}
	// A blank secret in the database means "keep the existing secret". This
	// includes deployments that still source that credential from config.yaml:
	// saving another field in the admin UI must not silently disable uploads.
	if cfg.SecretAccessKey == "" {
		cfg.SecretAccessKey = s.fallback.SecretAccessKey
	}
	return cfg, nil
}

func (s *ImageStorageSettingService) toImageStorageConfig(in *ImageStorageSettings) (*config.ImageStorageConfig, error) {
	cfg := &config.ImageStorageConfig{
		Enabled:         in.Enabled,
		Bucket:          in.Bucket,
		Prefix:          in.Prefix,
		PublicBaseURL:   in.PublicBaseURL,
		PresignExpiry:   in.PresignExpiry,
		MaxDownloadByte: in.MaxDownloadBytes,
		Endpoint:        in.Endpoint,
		Region:          in.Region,
		AccessKeyID:     in.AccessKeyID,
		SecretAccessKey: in.SecretAccessKey,
		ForcePathStyle:  in.ForcePathStyle,
	}

	if cfg.SecretAccessKey != "" {
		decrypted, err := s.encryptor.Decrypt(cfg.SecretAccessKey)
		if err != nil {
			// 兼容历史版本留下的明文配置。
			logger.L().Warn("image_storage secret decrypt failed; treating the stored value as plaintext", zap.Error(err))
		} else {
			cfg.SecretAccessKey = decrypted
		}
	}
	return cfg, nil
}

// load 读出后台设置；从未保存过时返回 nil。
func (s *ImageStorageSettingService) load(ctx context.Context) (*ImageStorageSettings, error) {
	if s.settingRepo == nil {
		return nil, nil //nolint:nilnil // no repository means no stored settings
	}
	raw, err := s.settingRepo.GetValue(ctx, settingKeyImageStorageConfig)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, nil //nolint:nilnil // never configured is a valid state
	}
	var settings ImageStorageSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("parse image storage settings: %w", err)
	}
	return &settings, nil
}

func settingsFromConfig(cfg config.ImageStorageConfig) *ImageStorageSettings {
	return &ImageStorageSettings{
		Enabled:          cfg.Enabled,
		Bucket:           cfg.Bucket,
		Prefix:           cfg.Prefix,
		PublicBaseURL:    cfg.PublicBaseURL,
		PresignExpiry:    cfg.PresignExpiry,
		MaxDownloadBytes: cfg.MaxDownloadByte,
		Endpoint:         cfg.Endpoint,
		Region:           cfg.Region,
		AccessKeyID:      cfg.AccessKeyID,
		SecretAccessKey:  cfg.SecretAccessKey,
		ForcePathStyle:   cfg.ForcePathStyle,
	}
}

func normalizeImageStorageSettings(in *ImageStorageSettings) {
	in.Bucket = strings.TrimSpace(in.Bucket)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.Region = strings.TrimSpace(in.Region)
	in.AccessKeyID = strings.TrimSpace(in.AccessKeyID)
	in.SecretAccessKey = strings.TrimSpace(in.SecretAccessKey)
	in.PublicBaseURL = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in.PublicBaseURL), "/"))

	in.Prefix = strings.TrimSpace(in.Prefix)
	if in.Prefix == "" {
		in.Prefix = "images/"
	}
	if !strings.HasSuffix(in.Prefix, "/") {
		in.Prefix += "/"
	}
	if in.Region == "" {
		in.Region = "auto"
	}
	if in.PresignExpiry <= 0 {
		in.PresignExpiry = 24
	}
	if in.MaxDownloadBytes <= 0 {
		in.MaxDownloadBytes = defaultImageMaxDownloadBytes
	}
}
