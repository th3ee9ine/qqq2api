package admin

import (
	"context"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

type settingHandlerRepoStub struct {
	values      map[string]string
	lastUpdates map[string]string
}

func (s *settingHandlerRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingHandlerRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values != nil {
		if value, ok := s.values[key]; ok {
			return value, nil
		}
	}
	return "", nil
}

func (s *settingHandlerRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingHandlerRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *settingHandlerRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	s.lastUpdates = make(map[string]string, len(settings))
	for key, value := range settings {
		s.lastUpdates[key] = value
		if s.values == nil {
			s.values = map[string]string{}
		}
		s.values[key] = value
	}
	return nil
}

func (s *settingHandlerRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *settingHandlerRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}
