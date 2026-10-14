package repository

import (
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/th3ee9ine/qqq2api/internal/config"
)

// BenchmarkHTTPUpstreamClientCache exercises the actual cache lookup instead
// of only assigning a previously returned client. ColdMiss keeps the resident
// account count stable so it measures TTL maintenance below the capacity limit.
func BenchmarkHTTPUpstreamClientCache(b *testing.B) {
	for _, cardinality := range []int{128, 5000} {
		b.Run(fmt.Sprintf("ColdMiss/accounts=%d", cardinality), func(b *testing.B) {
			svc := newBenchmarkHTTPUpstreamCache(b, cardinality)
			accountID := int64(cardinality + 1)
			cacheKey := buildCacheKey(config.ConnectionPoolIsolationAccountProxy, directProxyKey, accountID, upstreamProtocolModeDefault)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				entry, err := svc.acquireClient("", accountID, 8)
				if err != nil {
					b.Fatal(err)
				}
				atomic.AddInt64(&entry.inFlight, -1)
				// Discard only the new entry to reproduce another cold miss without
				// introducing a full-cache capacity eviction into this benchmark.
				svc.mu.Lock()
				svc.removeClientLocked(cacheKey, entry)
				svc.mu.Unlock()
				entry.client.CloseIdleConnections()
			}
		})
	}
	b.Run("ParallelHit/accounts=5000", func(b *testing.B) {
		svc := newBenchmarkHTTPUpstreamCache(b, 5000)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			accountID := int64(0)
			for pb.Next() {
				accountID = accountID%5000 + 1
				entry, err := svc.acquireClient("", accountID, 8)
				if err != nil {
					b.Error(err)
					return
				}
				atomic.AddInt64(&entry.inFlight, -1)
			}
		})
	})
}

func newBenchmarkHTTPUpstreamCache(b *testing.B, cardinality int) *httpUpstreamService {
	b.Helper()
	svc := NewHTTPUpstream(&config.Config{Gateway: config.GatewayConfig{
		ConnectionPoolIsolation: config.ConnectionPoolIsolationAccountProxy,
		MaxUpstreamClients:      cardinality + 1,
	}}).(*httpUpstreamService)
	for id := 1; id <= cardinality; id++ {
		if _, err := svc.getOrCreateClient("", int64(id), 8); err != nil {
			b.Fatal(err)
		}
	}
	return svc
}

// httpClientSink 用于防止编译器优化掉基准测试中的赋值操作
// 这是 Go 基准测试的常见模式，确保测试结果准确
var httpClientSink *http.Client

// BenchmarkHTTPUpstreamProxyClient 对比重复创建与复用代理客户端的开销
//
// 测试目的：
// - 验证连接池复用相比每次新建的性能提升
// - 量化内存分配差异
//
// 预期结果：
// - "复用" 子测试应显著快于 "新建"
// - "复用" 子测试应零内存分配
func BenchmarkHTTPUpstreamProxyClient(b *testing.B) {
	// 创建测试配置
	cfg := &config.Config{
		Gateway: config.GatewayConfig{ResponseHeaderTimeout: 300},
	}
	upstream := NewHTTPUpstream(cfg)
	svc, ok := upstream.(*httpUpstreamService)
	if !ok {
		b.Fatalf("类型断言失败，无法获取 httpUpstreamService")
	}

	proxyURL := "http://127.0.0.1:8080"
	b.ReportAllocs() // 报告内存分配统计

	// 子测试：每次新建客户端
	// 模拟未优化前的行为，每次请求都创建新的 http.Client
	b.Run("新建", func(b *testing.B) {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			b.Fatalf("解析代理地址失败: %v", err)
		}
		settings := defaultPoolSettings(cfg)
		for i := 0; i < b.N; i++ {
			// 每次迭代都创建新客户端，包含 Transport 分配
			transport, err := buildUpstreamTransport(settings, parsedProxy, upstreamProtocolModeDefault)
			if err != nil {
				b.Fatalf("创建 Transport 失败: %v", err)
			}
			httpClientSink = &http.Client{
				Transport: transport,
			}
		}
	})

	// 子测试：复用已缓存的客户端
	// 模拟优化后的行为，从缓存获取客户端
	b.Run("复用", func(b *testing.B) {
		// 预热：确保客户端已缓存
		entry, err := svc.getOrCreateClient(proxyURL, 1, 1)
		if err != nil {
			b.Fatalf("getOrCreateClient: %v", err)
		}
		client := entry.client
		b.ResetTimer() // 重置计时器，排除预热时间
		for i := 0; i < b.N; i++ {
			// 直接使用缓存的客户端，无内存分配
			httpClientSink = client
		}
	})
}
