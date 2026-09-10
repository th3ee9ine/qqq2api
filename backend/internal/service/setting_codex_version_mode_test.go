package service

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func TestOpenAICodexClientVersionModeSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"legacy missing", "", OpenAICodexClientVersionModeAuto},
		{"automatic", "auto", OpenAICodexClientVersionModeAuto},
		{"pinned", "pinned", OpenAICodexClientVersionModePinned},
		{"trimmed", " pinned ", OpenAICodexClientVersionModePinned},
		{"invalid stored value falls back", "latest", OpenAICodexClientVersionModeAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersionMode: tc.raw,
			}}, &config.Config{})
			settings, err := svc.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, settings.OpenAICodexClientVersionMode)
		})
	}
}

func TestSettingServiceValidatesMergedOpenAICodexVersionMode(t *testing.T) {
	identityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() { SetCodexAccountLocalDeviceIdentityEnabled(identityEnabled) })
	for _, tc := range []struct {
		name        string
		storedMode  string
		storedValue string
		mode        string
		version     string
		omitted     OmittedSettingKeys
		wantMode    string
		wantVersion string
		wantError   bool
	}{
		{name: "pinned historical version", mode: " pinned ", version: " 0.100.0 ", wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "legacy mode remains automatic", version: "0.100.0", wantMode: "auto", wantVersion: "0.100.0"},
		{name: "invalid mode", mode: "latest", version: "0.100.0", wantError: true},
		{name: "empty pinned version", mode: "pinned", wantError: true},
		{name: "prerelease pinned version", mode: "pinned", version: "0.100.0-alpha.1", wantError: true},
		{name: "invalid automatic version", mode: "auto", version: "latest", wantError: true},
		{name: "pin only uses stored version", storedMode: "auto", storedValue: "0.100.0", mode: "pinned", omitted: OmittedSettingKeys{SettingKeyOpenAICodexClientVersion: {}}, wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "pin only rejects missing stored version despite stale input", storedMode: "auto", mode: "pinned", version: "0.100.0", omitted: OmittedSettingKeys{SettingKeyOpenAICodexClientVersion: {}}, wantError: true},
		{name: "clear only rejects pinned stored mode despite stale auto snapshot", storedMode: "pinned", storedValue: "0.100.0", mode: "auto", omitted: OmittedSettingKeys{SettingKeyOpenAICodexClientVersionMode: {}}, wantError: true},
		{name: "explicitly unpin and clear", storedMode: "pinned", storedValue: "0.100.0", mode: "auto", wantMode: "auto"},
		{name: "unrelated partial save preserves pin", storedMode: "pinned", storedValue: "0.100.0", omitted: OmittedSettingKeys{SettingKeyOpenAICodexClientVersionMode: {}, SettingKeyOpenAICodexClientVersion: {}}, wantMode: "pinned", wantVersion: "0.100.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &codexHeaderSettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersionMode: tc.storedMode,
				SettingKeyOpenAICodexClientVersion:     tc.storedValue,
			}}
			svc := NewSettingService(repo, &config.Config{})
			err := svc.UpdateSettingsOmitting(context.Background(), &SystemSettings{
				OpenAICodexClientVersionMode: tc.mode,
				OpenAICodexClientVersion:     tc.version,
			}, tc.omitted)
			if tc.wantError {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Nil(t, repo.updates, "rejected updates must not partially persist")
				require.Equal(t, tc.storedMode, repo.values[SettingKeyOpenAICodexClientVersionMode])
				require.Equal(t, tc.storedValue, repo.values[SettingKeyOpenAICodexClientVersion])
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantMode, repo.values[SettingKeyOpenAICodexClientVersionMode])
			require.Equal(t, tc.wantVersion, repo.values[SettingKeyOpenAICodexClientVersion])
		})
	}
}

type codexModeBlockingReadRepo struct {
	*serializedSettingWriteRepo
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (r *codexModeBlockingReadRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyOpenAICodexClientVersion {
		r.once.Do(func() {
			close(r.entered)
			select {
			case <-r.release:
			case <-ctx.Done():
			}
		})
	}
	return r.serializedSettingWriteRepo.GetValue(ctx, key)
}

func TestSettingServiceCodexVersionModeValidationSerializesWithWrite(t *testing.T) {
	identityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() { SetCodexAccountLocalDeviceIdentityEnabled(identityEnabled) })
	repo := &codexModeBlockingReadRepo{
		serializedSettingWriteRepo: newSerializedSettingWriteRepo(),
		entered:                    make(chan struct{}),
		release:                    make(chan struct{}),
	}
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.100.0"
	repo.values[SettingKeyOpenAICodexClientVersionMode] = "auto"
	svc := NewSettingService(repo, &config.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() {
		first <- svc.UpdateSettingsOmitting(ctx, &SystemSettings{OpenAICodexClientVersionMode: "pinned"}, OmittedSettingKeys{SettingKeyOpenAICodexClientVersion: {}})
	}()
	select {
	case <-repo.entered:
	case <-ctx.Done():
		t.Fatal("timed out waiting for merged version validation")
	}
	second := make(chan error, 1)
	go func() {
		// This request was built before the pin committed. Its omitted mode
		// must be re-read after that commit, rather than assumed to be auto.
		second <- svc.UpdateSettingsOmitting(ctx, &SystemSettings{}, OmittedSettingKeys{SettingKeyOpenAICodexClientVersionMode: {}})
	}()
	close(repo.release)
	require.NoError(t, <-first)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(<-second))
	values, _ := repo.snapshot()
	require.Equal(t, "pinned", values[SettingKeyOpenAICodexClientVersionMode])
	require.Equal(t, "0.100.0", values[SettingKeyOpenAICodexClientVersion])
}
