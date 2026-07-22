package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMihomoListsProviderWithAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/providers/proxies/wgetcloud" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-value" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proxies": []map[string]any{{"name": "香港01", "type": "Shadowsocks", "alive": true}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-value", server.Client())
	nodes, err := client.ListProvider(context.Background(), "wgetcloud")
	if err != nil {
		t.Fatalf("ListProvider() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "香港01" || !nodes[0].Alive {
		t.Fatalf("nodes = %+v", nodes)
	}
}

func TestMihomoSelectReadsBackActualNode(t *testing.T) {
	selected := "新加坡03"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /proxies/V2-LANE-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "V2-LANE-1", "now": selected})
		case "PUT /proxies/V2-LANE-1":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-value", server.Client())
	actual, err := client.SelectAndConfirm(context.Background(), "V2-LANE-1", "香港02")
	if err != nil {
		t.Fatalf("SelectAndConfirm() error = %v", err)
	}
	if actual != "香港02" {
		t.Fatalf("actual = %q", actual)
	}
}

func TestMihomoSelectRejectsReadBackMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "V2-LANE-1", "now": "旧节点"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-value", server.Client())
	if _, err := client.SelectAndConfirm(context.Background(), "V2-LANE-1", "新节点"); err == nil {
		t.Fatal("SelectAndConfirm() succeeded despite read-back mismatch")
	}
}
