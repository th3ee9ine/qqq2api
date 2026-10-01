package dto

import (
	"reflect"
	"strings"
	"testing"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

// TestPublicSettingsInjectionPayload_SchemaDoesNotDrift guarantees the SSR
// injection struct exposes every JSON field consumed by the frontend.
//
// Why this test exists: before we extracted a named PublicSettingsInjectionPayload
// type, the inline struct was manually kept in sync with dto.PublicSettings and
// drifted — ChannelMonitorEnabled / AvailableChannelsEnabled were missing, which
// made the frontend read `undefined` on refresh and hide the "可用渠道" menu
// until the async /api/v1/settings/public round-trip finished.
//
// This test compares the two JSON-tag sets and fails if injection is missing
// any field that dto.PublicSettings exposes. Adding a new feature flag with
// only a DTO entry will fail this test until the injection struct is updated.
func TestPublicSettingsInjectionPayload_SchemaDoesNotDrift(t *testing.T) {
	injection := jsonTags(reflect.TypeOf(service.PublicSettingsInjectionPayload{}))
	dtoKeys := jsonTags(reflect.TypeOf(PublicSettings{}))

	var missing []string
	for key := range dtoKeys {
		if _, ok := injection[key]; ok {
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		t.Fatalf("service.PublicSettingsInjectionPayload is missing JSON fields present on dto.PublicSettings: %s\n"+
			"add the field to PublicSettingsInjectionPayload (and GetPublicSettingsForInjection).", strings.Join(missing, ", "))
	}
}

func jsonTags(t reflect.Type) map[string]struct{} {
	out := make(map[string]struct{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}
