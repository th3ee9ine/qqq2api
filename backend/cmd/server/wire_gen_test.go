package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGeneratedWireIncludesStandaloneImageStorageHandler(t *testing.T) {
	source, err := os.ReadFile("wire_gen.go")
	require.NoError(t, err)
	generated := string(source)

	for _, retained := range []string{
		"admin.NewModelPricingHandler(billingService)",
		"admin.NewAccountAdminHandler(adminService)",
		"admin.NewImageStorageHandler(imageStorageSettingService)",
		"accountHandler, accountAdminHandler, imageStorageHandler, oAuthHandler",
		"service.NewPluginManager(",
		"admin.NewPluginHandler(pluginManager)",
		"service.ProvideOpenAIQuotaAutoResetService(",
		"service.ProvideOllamaCloudUsageService(",
	} {
		require.Contains(t, generated, retained)
	}

	for _, retired := range []string{
		"admin.NewUserHandler",
		"admin.NewRedeemHandler",
		"admin.NewPromoCodeHandler",
		"admin.NewAnnouncementHandler",
		"admin.NewSubscriptionHandler",
		"admin.NewPaymentHandler",
		"admin.NewBackupHandler",
		"admin.NewDataManagementHandler",
		"admin.NewChannelHandler",
		"admin.NewChannelMonitorHandler",
		"admin.NewComplianceHandler",
		"admin.NewGeminiOAuthHandler",
		"admin.NewAntigravityOAuthHandler",
		"admin.NewGrokOAuthHandler",
		"admin.NewCNProviderHandler",
		"handler.NewModelPlazaHandler",
		"service.NewSubscriptionService",
		"service.ProvideSubscriptionExpiryService",
		"service.ProvidePaymentService",
		"service.NewModelPlazaService",
		"service.NewGeminiOAuthService",
		"service.NewAntigravityOAuthService",
		"service.NewGrokOAuthService",
		"service.ProvideUserPlatformQuotaUsageFlusher",
	} {
		require.NotContains(t, generated, retired)
	}
}

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	codexVersionSyncSvc := service.NewOpenAICodexVersionSyncService(nil, nil, nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		codexVersionSyncSvc,
		proxyExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		oauthSvc,
		openAIOAuthSvc,
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // upstreamBillingProbe
		nil, // ollamaCloudUsage
		nil, // auditLog
		nil, // openAIAutoReset
		nil, // promptAudit
		nil, // pluginManager
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}
