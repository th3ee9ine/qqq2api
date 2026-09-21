package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// ProxyExpiryService 周期扫描到期代理并把绑定账号改投备用/直连。
type ProxyExpiryService struct {
	proxyRepo      ProxyRepository
	runtimeBlocker AccountRuntimeBlocker
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
}

func NewProxyExpiryService(proxyRepo ProxyRepository, interval time.Duration, runtimeBlocker ...AccountRuntimeBlocker) *ProxyExpiryService {
	service := &ProxyExpiryService{proxyRepo: proxyRepo, interval: interval, stopCh: make(chan struct{})}
	if len(runtimeBlocker) > 0 {
		service.runtimeBlocker = runtimeBlocker[0]
	}
	return service
}

func (s *ProxyExpiryService) Start() {
	if s == nil || s.proxyRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *ProxyExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *ProxyExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sweepAt := time.Now()

	var runtimeAccounts []ProxyAccountSummary
	if _, ok := s.runtimeBlocker.(OpenAIAccountRuntimeStateInvalidator); ok {
		proxies, err := s.proxyRepo.ListAllForFallback(ctx)
		if err != nil {
			log.Printf("[ProxyExpiry] list proxies for runtime invalidation failed: %v", err)
			return
		}
		for i := range proxies {
			proxy := &proxies[i]
			if proxy.Status != StatusActive || !proxy.IsExpired(sweepAt) {
				continue
			}
			accounts, err := snapshotOpenAIProxyRuntimeAccounts(s.proxyRepo, proxy.ID)
			if err != nil {
				log.Printf("[ProxyExpiry] snapshot proxy %d accounts for runtime invalidation failed: %v", proxy.ID, err)
				return
			}
			runtimeAccounts = append(runtimeAccounts, accounts...)
		}
	}

	changed, err := s.proxyRepo.SweepExpiredProxies(ctx, sweepAt)
	if err != nil {
		log.Printf("[ProxyExpiry] sweep expired proxies failed: %v", err)
		return
	}
	invalidateOpenAIProxyRuntimeAccounts(s.runtimeBlocker, runtimeAccounts)
	if changed > 0 {
		log.Printf("[ProxyExpiry] re-routed %d accounts off expired proxies", changed)
	}
}
