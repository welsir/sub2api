package pool

import "strings"

var pseudoNodeMarkers = []string{
	"剩余流量",
	"流量用尽",
	"流量：",
	"流量:",
	"重置",
	"套餐到期",
	"订阅即将到期",
	"订阅获取时间",
	"官网",
	"请续费",
}

func IsPseudoNode(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return true
	}
	for _, marker := range pseudoNodeMarkers {
		if strings.Contains(normalized, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func DiscoverRealNodes(provider string, names []string) []Node {
	nodes := make([]Node, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if IsPseudoNode(name) {
			continue
		}
		nodes = append(nodes, Node{
			Key:      provider + "/" + name,
			Provider: provider,
			Name:     name,
			State:    NodeUnverified,
		})
	}
	return nodes
}
