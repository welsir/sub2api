package proxypool

import (
	"os"
	"strings"
	"testing"
)

func TestProxyPoolComposeIsV2OnlyAndPrivate(t *testing.T) {
	compose := readFile(t, "docker-compose.proxy-pool.yml")
	for _, required := range []string{
		"sub2api-v2-mihomo", "sub2api-v2-proxy-controller", "v1.19.28",
		"mem_limit:", "max-size:", "v2-network", "WGETCLOUD_PROVIDER_FILE",
	} {
		if !strings.Contains(compose, required) {
			t.Errorf("compose missing %q", required)
		}
	}
	if strings.Contains(compose, "ports:") {
		t.Fatal("proxy pool compose exposes a host port")
	}
	for _, forbidden := range []string{"sub2api-preview", "18082", "/mnt/sub2api", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("compose contains forbidden V1/global-proxy marker %q", forbidden)
		}
	}
}

func TestMihomoTemplateDefinesFiveProductionThreeProbeOneCanary(t *testing.T) {
	template := readFile(t, "mihomo/config.yaml.tmpl")
	for _, name := range []string{
		"V2-LANE-1", "V2-LANE-2", "V2-LANE-3", "V2-LANE-4", "V2-LANE-5",
		"V2-PROBE-1", "V2-PROBE-2", "V2-PROBE-3", "V2-CANARY",
	} {
		if !strings.Contains(template, name) {
			t.Errorf("template missing %s", name)
		}
	}
	if got := strings.Count(template, "type: http"); got != 9 {
		t.Fatalf("HTTP type entries = %d, want 9", got)
	}
	if !strings.Contains(template, "type: file") || !strings.Contains(template, "path: ./providers/wgetcloud.yaml") || !strings.Contains(template, "__MIHOMO_SECRET__") {
		t.Fatal("template must use the mounted provider file and secret placeholder")
	}
	if strings.Contains(template, "WGETCLOUD_SUBSCRIPTION_URL") || strings.Contains(template, "url:") {
		t.Fatal("template must not embed a subscription URL")
	}
}

func TestBaseV2ComposeDoesNotSetGlobalProxyEnvironment(t *testing.T) {
	base := readFile(t, "../docker-compose.yml")
	for _, forbidden := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if strings.Contains(base, forbidden) {
			t.Errorf("base V2 compose contains %s", forbidden)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
