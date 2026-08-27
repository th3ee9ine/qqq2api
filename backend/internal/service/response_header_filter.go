package service

import (
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/util/responseheaders"
)

func compileResponseHeaderFilter(cfg *config.Config) *responseheaders.CompiledHeaderFilter {
	if cfg == nil {
		return nil
	}
	return responseheaders.CompileHeaderFilter(cfg.Security.ResponseHeaders)
}
