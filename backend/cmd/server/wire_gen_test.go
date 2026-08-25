package main

import (
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWireIncludesStandaloneImageStorageHandler(t *testing.T) {
	source, err := os.ReadFile("wire_gen.go")
	require.NoError(t, err)
	require.Contains(t, string(source), "admin.NewImageStorageHandler(imageStorageSettingService)")
	require.Contains(t, string(source), "accountHandler, imageStorageHandler, oAuthHandler")
	require.NotContains(t, string(source), "admin.NewBackupHandler")
	require.NotContains(t, string(source), "service.NewSubscriptionService")
	require.NotContains(t, string(source), "service.ProvidePaymentService")
	require.NotContains(t, string(source), "service.ProvideUserPlatformQuotaUsageFlusher")
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
		nil, // promptAudit
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}
