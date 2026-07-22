package pool

import "testing"

func TestDiscoveryFiltersPseudoNodes(t *testing.T) {
	names := []string{
		"香港01", "新加坡03", "日本02",
		"剩余流量：500GB", "距离下次重置：12天", "套餐到期：2026-12-31",
	}

	nodes := DiscoverRealNodes("wgetcloud", names)
	if len(nodes) != 3 {
		t.Fatalf("real nodes = %+v", nodes)
	}
	if nodes[0].Key != "wgetcloud/香港01" || nodes[2].Key != "wgetcloud/日本02" {
		t.Fatalf("unexpected node keys: %+v", nodes)
	}
}

func TestDiscoveryFiltersProviderNotices(t *testing.T) {
	for _, name := range []string{"官网：www.example.com", "订阅即将到期", "订阅获取时间：2026-07-21 15:43", "流量用尽请续费"} {
		if !IsPseudoNode(name) {
			t.Errorf("IsPseudoNode(%q) = false", name)
		}
	}
	if IsPseudoNode("台湾01") {
		t.Fatal("real node was classified as pseudo")
	}
}
