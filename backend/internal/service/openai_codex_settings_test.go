package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type codexHeaderSettingRepoStub struct {
	values  map[string]string
	updates map[string]string
}

type blockingCodexHeaderGetRepo struct {
	*codexHeaderSettingRepoStub
	blockedKey string
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (r *blockingCodexHeaderGetRepo) GetValue(ctx context.Context, key string) (string, error) {
	value, err := r.codexHeaderSettingRepoStub.GetValue(ctx, key)
	if key != r.blockedKey {
		return value, err
	}
	r.once.Do(func() { close(r.started) })
	select {
	case <-r.release:
		return value, err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *codexHeaderSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *codexHeaderSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *codexHeaderSettingRepoStub) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *codexHeaderSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *codexHeaderSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.updates = make(map[string]string, len(settings))
	for key, value := range settings {
		r.values[key] = value
		r.updates[key] = value
	}
	return nil
}

func (r *codexHeaderSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *codexHeaderSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func TestSettingServiceOpenAICodexOriginatorSetting(t *testing.T) {
	t.Run("parse trims safe override and ignores unsafe stored value", func(t *testing.T) {
		safe := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexOriginator: "  codex-tui  ",
		}}, &config.Config{})
		settings, err := safe.GetAllSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, "codex-tui", settings.OpenAICodexOriginator)

		unsafe := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexOriginator: "bad/originator",
		}}, &config.Config{})
		settings, err = unsafe.GetAllSettings(context.Background())
		require.NoError(t, err)
		require.Empty(t, settings.OpenAICodexOriginator)
	})

	t.Run("persist normalizes and immediately refreshes runtime cache", func(t *testing.T) {
		accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
		t.Cleanup(func() {
			SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
		})

		repo := &codexHeaderSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexOriginator: "old-client",
		}}
		svc := NewSettingService(repo, &config.Config{})
		require.Equal(t, "old-client", svc.GetOpenAICodexOriginator(context.Background()))

		err := svc.UpdateSettings(context.Background(), &SystemSettings{
			OpenAICodexOriginator: "  Codex Desktop  ",
		})
		require.NoError(t, err)
		require.Equal(t, "Codex Desktop", repo.updates[SettingKeyOpenAICodexOriginator])
		require.Equal(t, "Codex Desktop", svc.GetOpenAICodexOriginator(context.Background()))
	})
}

func TestSettingServiceRejectsUnsafeOpenAICodexHeaders(t *testing.T) {
	tests := []struct {
		name     string
		settings SystemSettings
		key      string
	}{
		{name: "originator slash", settings: SystemSettings{OpenAICodexOriginator: "bad/originator"}, key: SettingKeyOpenAICodexOriginator},
		{name: "originator newline", settings: SystemSettings{OpenAICodexOriginator: "bad\noriginator"}, key: SettingKeyOpenAICodexOriginator},
		{name: "originator non ascii", settings: SystemSettings{OpenAICodexOriginator: "Codex 桌面"}, key: SettingKeyOpenAICodexOriginator},
		{name: "originator trailing newline", settings: SystemSettings{OpenAICodexOriginator: "Codex Desktop\n"}, key: SettingKeyOpenAICodexOriginator},
		{name: "user agent CRLF", settings: SystemSettings{OpenAICodexUserAgent: "Codex Desktop/0.150.1\r\nX-Injected: true"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent control byte", settings: SystemSettings{OpenAICodexUserAgent: "codex-tui/0.150.1\x00suffix"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent non ascii", settings: SystemSettings{OpenAICodexUserAgent: "Codex 桌面/0.150.1"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent leading tab", settings: SystemSettings{OpenAICodexUserAgent: "\tCodex Desktop/0.150.1"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent trailing newline", settings: SystemSettings{OpenAICodexUserAgent: "Codex Desktop/0.150.1\n"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent over length whitespace", settings: SystemSettings{OpenAICodexUserAgent: strings.Repeat(" ", codexAccountLocalUserAgentMaxLen+1)}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent missing client version shape", settings: SystemSettings{OpenAICodexUserAgent: "my-gateway"}, key: SettingKeyOpenAICodexUserAgent},
		{name: "user agent invalid version", settings: SystemSettings{OpenAICodexUserAgent: "my-gateway/latest"}, key: SettingKeyOpenAICodexUserAgent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &codexHeaderSettingRepoStub{values: map[string]string{}}
			svc := NewSettingService(repo, &config.Config{})

			err := svc.UpdateSettings(context.Background(), &tt.settings)
			require.ErrorContains(t, err, tt.key)
			require.Nil(t, repo.updates)
		})
	}
}

func TestOpenAICodexHeaderSaveWinsAgainstInflightCacheLoad(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		oldValue    string
		newValue    string
		newSettings SystemSettings
		get         func(*SettingService) string
	}{
		{
			name:        "originator",
			key:         SettingKeyOpenAICodexOriginator,
			oldValue:    "old-client",
			newValue:    "new-client",
			newSettings: SystemSettings{OpenAICodexOriginator: "new-client"},
			get: func(svc *SettingService) string {
				return svc.GetOpenAICodexOriginator(context.Background())
			},
		},
		{
			name:        "user agent",
			key:         SettingKeyOpenAICodexUserAgent,
			oldValue:    "old-client/0.150.1 (Linux; x86_64)",
			newValue:    "new-client/0.150.1 (Linux; arm64)",
			newSettings: SystemSettings{OpenAICodexUserAgent: "new-client/0.150.1 (Linux; arm64)"},
			get: func(svc *SettingService) string {
				return svc.GetOpenAICodexUserAgent(context.Background())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
			t.Cleanup(func() {
				SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
			})

			repo := &blockingCodexHeaderGetRepo{
				codexHeaderSettingRepoStub: &codexHeaderSettingRepoStub{values: map[string]string{
					tt.key: tt.oldValue,
				}},
				blockedKey: tt.key,
				started:    make(chan struct{}),
				release:    make(chan struct{}),
			}
			svc := NewSettingService(repo, &config.Config{})
			loaded := make(chan string, 1)
			go func() { loaded <- tt.get(svc) }()

			select {
			case <-repo.started:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for stale header load to begin")
			}
			tt.newSettings.EnableOpenAIAccountLocalDeviceIdentity = accountIdentityEnabled
			svc.refreshCachedSettings(&tt.newSettings)
			close(repo.release)

			select {
			case value := <-loaded:
				require.Equal(t, tt.newValue, value)
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for header load to retry")
			}
			require.Equal(t, tt.newValue, tt.get(svc))
		})
	}
}
