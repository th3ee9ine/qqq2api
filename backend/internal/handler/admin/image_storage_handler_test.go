package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageStorageHandlerSettingRepo struct {
	values map[string]string
}

func (r *imageStorageHandlerSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, nil
}

func (r *imageStorageHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *imageStorageHandlerSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *imageStorageHandlerSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *imageStorageHandlerSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (r *imageStorageHandlerSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *imageStorageHandlerSettingRepo) Delete(context.Context, string) error { return nil }

type imageStorageHandlerEncryptor struct{}

func (imageStorageHandlerEncryptor) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func (imageStorageHandlerEncryptor) Decrypt(ciphertext string) (string, error) {
	plaintext, ok := strings.CutPrefix(ciphertext, "encrypted:")
	if !ok {
		return "", errors.New("not encrypted")
	}
	return plaintext, nil
}

type imageStorageHandlerStore struct{}

func (imageStorageHandlerStore) Save(context.Context, string, string, []byte) (string, error) {
	return "https://cdn.example.com/image.png", nil
}

func newImageStorageHandlerTestRouter(t *testing.T) (*gin.Engine, *imageStorageHandlerSettingRepo, *[]config.ImageStorageConfig) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &imageStorageHandlerSettingRepo{values: map[string]string{}}
	built := make([]config.ImageStorageConfig, 0)
	factory := func(_ context.Context, cfg *config.ImageStorageConfig) (service.ImageStorage, error) {
		built = append(built, *cfg)
		return imageStorageHandlerStore{}, nil
	}
	svc := service.NewImageStorageSettingService(
		repo,
		imageStorageHandlerEncryptor{},
		factory,
		config.ImageStorageConfig{},
		true,
	)
	h := NewImageStorageHandler(svc)
	router := gin.New()
	router.GET("/image-storage", h.Get)
	router.PUT("/image-storage", h.Update)
	router.POST("/image-storage/test", h.TestConnection)
	return router, repo, &built
}

func TestImageStorageHandlerUpdateGetAndTestConnection(t *testing.T) {
	router, repo, built := newImageStorageHandlerTestRouter(t)
	body := `{
		"enabled":true,
		"endpoint":"https://r2.example.com",
		"region":"auto",
		"bucket":"images",
		"prefix":"openai",
		"access_key_id":"access",
		"secret_access_key":"secret",
		"force_path_style":true
	}`

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/image-storage", strings.NewReader(body))
	updateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(updateRecorder, updateRequest)
	require.Equal(t, http.StatusOK, updateRecorder.Code)
	require.Equal(t, float64(0), gjson.Get(updateRecorder.Body.String(), "code").Float())
	require.True(t, gjson.Get(updateRecorder.Body.String(), "data.enabled").Bool())
	require.Equal(t, "openai/", gjson.Get(updateRecorder.Body.String(), "data.prefix").String())
	require.Empty(t, gjson.Get(updateRecorder.Body.String(), "data.secret_access_key").String())

	var stored string
	for _, value := range repo.values {
		stored = value
	}
	require.NotEmpty(t, stored)
	require.NotContains(t, stored, `"secret_access_key":"secret"`)
	require.Contains(t, stored, "encrypted:secret")

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/image-storage", nil))
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.True(t, gjson.Get(getRecorder.Body.String(), "data.secret_configured").Bool())
	require.Equal(t, "images", gjson.Get(getRecorder.Body.String(), "data.config.bucket").String())
	require.Empty(t, gjson.Get(getRecorder.Body.String(), "data.config.secret_access_key").String())

	// An empty secret reuses the stored encrypted credential for a connection test.
	testBody := `{"enabled":true,"endpoint":"https://r2.example.com","bucket":"images","access_key_id":"access"}`
	testRecorder := httptest.NewRecorder()
	testRequest := httptest.NewRequest(http.MethodPost, "/image-storage/test", strings.NewReader(testBody))
	testRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(testRecorder, testRequest)
	require.Equal(t, http.StatusOK, testRecorder.Code)
	require.True(t, gjson.Get(testRecorder.Body.String(), "data.ok").Bool())
	require.Equal(t, "connection successful", gjson.Get(testRecorder.Body.String(), "data.message").String())
	require.Len(t, *built, 1)
	require.Equal(t, "secret", (*built)[0].SecretAccessKey)
}

func TestImageStorageHandlerTestConnectionReportsValidationFailure(t *testing.T) {
	router, _, _ := newImageStorageHandlerTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/image-storage/test", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, gjson.Get(recorder.Body.String(), "data.ok").Bool())
	require.Contains(t, gjson.Get(recorder.Body.String(), "data.message").String(), "incomplete")
}
