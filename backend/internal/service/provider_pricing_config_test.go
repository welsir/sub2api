package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProviderPricingConfig(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		enabled   bool
		groupName string
		models    []string
		wantError string
	}{
		{name: "disabled allows empty config", platform: PlatformAnthropic},
		{name: "valid openai publication", platform: PlatformOpenAI, enabled: true, groupName: "gpt01", models: []string{"gpt-5.6"}},
		{name: "rejects non openai group", platform: PlatformAnthropic, enabled: true, groupName: "gpt01", models: []string{"gpt-5.6"}, wantError: "only supported for openai"},
		{name: "rejects internal display name", platform: PlatformOpenAI, enabled: true, groupName: "pro号池", models: []string{"gpt-5.6"}, wantError: "must match gptNN"},
		{name: "rejects short number", platform: PlatformOpenAI, enabled: true, groupName: "gpt1", models: []string{"gpt-5.6"}, wantError: "must match gptNN"},
		{name: "rejects empty models", platform: PlatformOpenAI, enabled: true, groupName: "gpt01", wantError: "at least one model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProviderPricingConfig(tt.platform, tt.enabled, tt.groupName, tt.models)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestNormalizeProviderPricingModels(t *testing.T) {
	require.Equal(t, []string{"gpt-5.6", "gpt-5.4"}, NormalizeProviderPricingModels([]string{
		" gpt-5.6 ", "", "gpt-5.4", "gpt-5.6",
	}))
}
