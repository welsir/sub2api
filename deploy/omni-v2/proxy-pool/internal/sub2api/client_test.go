package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSub2APILoginListUpdateAndTestAccount(t *testing.T) {
	var updatedProxy int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/auth/login":
			_ = json.NewEncoder(w).Encode(envelope(map[string]any{"access_token": "admin-token"}))
		case "GET /api/v1/admin/accounts":
			if r.Header.Get("Authorization") != "Bearer admin-token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(envelope(map[string]any{
				"items": []map[string]any{
					{"id": 12, "name": "oauth-a", "platform": "openai", "type": "oauth", "status": "active", "proxy_id": 6},
					{"id": 13, "name": "disabled", "platform": "openai", "type": "oauth", "status": "inactive", "proxy_id": 6},
				},
				"total": 2,
			}))
		case "PUT /api/v1/admin/accounts/12":
			var body map[string]int64
			_ = json.NewDecoder(r.Body).Decode(&body)
			updatedProxy = body["proxy_id"]
			_ = json.NewEncoder(w).Encode(envelope(map[string]any{"id": 12, "proxy_id": updatedProxy}))
		case "POST /api/v1/admin/accounts/12/test":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"type\":\"response.completed\"}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "sub2api-v2", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(context.Background(), "admin@example.com", "password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	accounts, err := client.ListEligibleOpenAIOAuth(context.Background())
	if err != nil {
		t.Fatalf("ListEligibleOpenAIOAuth() error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != 12 {
		t.Fatalf("accounts = %+v", accounts)
	}
	if err := client.UpdateAccountProxy(context.Background(), 12, 21); err != nil {
		t.Fatalf("UpdateAccountProxy() error = %v", err)
	}
	if updatedProxy != 21 {
		t.Fatalf("updated proxy = %d", updatedProxy)
	}
	if err := client.TestAccount(context.Background(), 12, "gpt-5.4-mini", "hi"); err != nil {
		t.Fatalf("TestAccount() error = %v", err)
	}
}

func TestV2GuardRejectsPreviewBeforeSendingRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()

	for _, tc := range []struct {
		baseURL  string
		instance string
	}{
		{baseURL: server.URL, instance: "sub2api-preview"},
		{baseURL: "https://preview.example.com", instance: "sub2api-v2"},
		{baseURL: "https://api-v1.example.com", instance: "sub2api-v2"},
	} {
		if _, err := NewClient(tc.baseURL, tc.instance, server.Client()); err == nil {
			t.Fatalf("NewClient(%q, %q) succeeded", tc.baseURL, tc.instance)
		}
	}
	if requests != 0 {
		t.Fatalf("guard sent %d requests", requests)
	}
}

func TestSub2APITestAccountRequiresCompletedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"type\":\"response.in_progress\"}\n\n"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "sub2api-v2", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.token = "token"
	if err := client.TestAccount(context.Background(), 1, "gpt-5.4-mini", "hi"); err == nil || !strings.Contains(err.Error(), "response.completed") {
		t.Fatalf("TestAccount() error = %v", err)
	}
}

func envelope(data any) map[string]any {
	return map[string]any{"code": 0, "message": "success", "data": data}
}
