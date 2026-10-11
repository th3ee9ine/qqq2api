package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestSubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	testUsageRecordTaskCopiesRequestContext(t, func(pool *service.UsageRecordWorkerPool, mandatory bool) func(context.Context, service.UsageRecordTask) {
		h := &GatewayHandler{usageRecordWorkerPool: pool}
		if mandatory {
			return h.submitMandatoryUsageRecordTask
		}
		return h.submitUsageRecordTask
	})
}

func TestOpenAISubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	testUsageRecordTaskCopiesRequestContext(t, func(pool *service.UsageRecordWorkerPool, mandatory bool) func(context.Context, service.UsageRecordTask) {
		h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}
		if mandatory {
			return h.submitMandatoryUsageRecordTask
		}
		return h.submitUsageRecordTask
	})
}

func testUsageRecordTaskCopiesRequestContext(t *testing.T, submitter func(*service.UsageRecordWorkerPool, bool) func(context.Context, service.UsageRecordTask)) {
	t.Helper()
	for _, mode := range []string{"without_pool", "worker_pool", "stopped_pool"} {
		for _, mandatory := range []bool{false, true} {
			name := mode + "/normal"
			if mandatory {
				name = mode + "/mandatory"
			}
			t.Run(name, func(t *testing.T) {
				var pool *service.UsageRecordWorkerPool
				switch mode {
				case "worker_pool":
					pool = newUsageRecordTestPool(t)
				case "stopped_pool":
					pool = newStoppedUsageRecordPoolForTest()
				}
				parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-request-123")
				parent = context.WithValue(parent, ctxkey.RequestID, "request-456")
				parent, cancel := context.WithCancel(parent)
				cancel()
				type observedContext struct {
					clientRequestID string
					requestID       string
					err             error
					hasDeadline     bool
				}
				observed := make(chan observedContext, 1)
				submitter(pool, mandatory)(parent, func(ctx context.Context) {
					clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string)
					requestID, _ := ctx.Value(ctxkey.RequestID).(string)
					_, hasDeadline := ctx.Deadline()
					observed <- observedContext{clientRequestID, requestID, ctx.Err(), hasDeadline}
				})
				select {
				case got := <-observed:
					require.Equal(t, "client-request-123", got.clientRequestID)
					require.Equal(t, "request-456", got.requestID)
					require.NoError(t, got.err, "client cancellation must not cancel usage recording")
					require.True(t, got.hasDeadline, "usage recording must retain its own bounded lifetime")
				case <-time.After(time.Second):
					t.Fatal("usage recording task was not executed")
				}
			})
		}
	}
}
