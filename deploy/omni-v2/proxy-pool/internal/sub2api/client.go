package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 4 << 20

type Account struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	ProxyID  *int64 `json:"proxy_id"`
}

type Proxy struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type CreateProxyRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type ManagedTopology struct {
	Lanes  []Proxy `json:"lanes"`
	Probes []Proxy `json:"probes"`
	Canary Proxy   `json:"canary"`
}

type Client struct {
	baseURL string
	http    *http.Client
	token   string
}

func NewClient(baseURL, instanceID string, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(instanceID) != "sub2api-v2" {
		return nil, fmt.Errorf("refusing non-v2 instance %q", instanceID)
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("sub2api V2 base URL must be absolute")
	}
	lower := strings.ToLower(baseURL)
	if strings.Contains(lower, "preview") || strings.Contains(lower, "legacy") || strings.Contains(lower, "v1") {
		return nil, fmt.Errorf("refusing forbidden non-v2 URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}, nil
}

func (c *Client) Login(ctx context.Context, email, password string) error {
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.doEnvelope(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, &response); err != nil {
		return err
	}
	if response.AccessToken == "" {
		return fmt.Errorf("V2 login returned no access token; 2FA is not supported by the controller")
	}
	c.token = response.AccessToken
	return nil
}

func (c *Client) ListEligibleOpenAIOAuth(ctx context.Context) ([]Account, error) {
	var page struct {
		Items []Account `json:"items"`
	}
	path := "/api/v1/admin/accounts?page=1&page_size=1000&platform=openai&type=oauth&status=active"
	if err := c.doEnvelope(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	eligible := make([]Account, 0, len(page.Items))
	for _, account := range page.Items {
		if account.Platform == "openai" && account.Type == "oauth" && account.Status == "active" {
			eligible = append(eligible, account)
		}
	}
	return eligible, nil
}

func (c *Client) UpdateAccountProxy(ctx context.Context, accountID, proxyID int64) error {
	return c.doEnvelope(ctx, http.MethodPut, "/api/v1/admin/accounts/"+strconv.FormatInt(accountID, 10), map[string]int64{
		"proxy_id": proxyID,
	}, nil)
}

func (c *Client) ListProxies(ctx context.Context) ([]Proxy, error) {
	var proxies []Proxy
	if err := c.doEnvelope(ctx, http.MethodGet, "/api/v1/admin/proxies/all", nil, &proxies); err != nil {
		return nil, err
	}
	return proxies, nil
}

func (c *Client) CreateProxy(ctx context.Context, request CreateProxyRequest) (Proxy, error) {
	var proxy Proxy
	if err := c.doEnvelope(ctx, http.MethodPost, "/api/v1/admin/proxies", request, &proxy); err != nil {
		return Proxy{}, err
	}
	return proxy, nil
}

func (c *Client) EnsureManagedProxies(ctx context.Context, host string, laneCount int) (ManagedTopology, error) {
	proxies, err := c.ListProxies(ctx)
	if err != nil {
		return ManagedTopology{}, fmt.Errorf("list V2 proxies: %w", err)
	}
	byName := make(map[string]Proxy, len(proxies))
	for _, proxy := range proxies {
		byName[proxy.Name] = proxy
	}
	ensure := func(name string, port int) (Proxy, error) {
		if proxy, ok := byName[name]; ok {
			if proxy.Protocol != "http" || proxy.Host != host || proxy.Port != port {
				return Proxy{}, fmt.Errorf("managed proxy name collision for %q: got %s://%s:%d", name, proxy.Protocol, proxy.Host, proxy.Port)
			}
			return proxy, nil
		}
		proxy, err := c.CreateProxy(ctx, CreateProxyRequest{Name: name, Protocol: "http", Host: host, Port: port})
		if err != nil {
			return Proxy{}, fmt.Errorf("create managed proxy %q: %w", name, err)
		}
		byName[name] = proxy
		return proxy, nil
	}

	topology := ManagedTopology{Lanes: make([]Proxy, 0, laneCount)}
	for number := 1; number <= laneCount; number++ {
		proxy, err := ensure(fmt.Sprintf("v2-stable-lane-%d", number), 19080+number)
		if err != nil {
			return ManagedTopology{}, err
		}
		topology.Lanes = append(topology.Lanes, proxy)
	}
	for number := 1; number <= 3; number++ {
		proxy, err := ensure(fmt.Sprintf("v2-stable-probe-%d", number), 19100+number)
		if err != nil {
			return ManagedTopology{}, err
		}
		topology.Probes = append(topology.Probes, proxy)
	}
	topology.Canary, err = ensure("v2-stable-canary", 19201)
	if err != nil {
		return ManagedTopology{}, err
	}
	return topology, nil
}

func (c *Client) TestAccount(ctx context.Context, accountID int64, model, prompt string) error {
	body, err := json.Marshal(map[string]string{"model_id": model, "prompt": prompt, "mode": "responses"})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/admin/accounts/"+strconv.FormatInt(accountID, 10)+"/test", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("V2 account test request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("V2 account test returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read V2 account test SSE: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("V2 account test SSE exceeds limit")
	}
	if !bytes.Contains(data, []byte("response.completed")) {
		return fmt.Errorf("V2 account test SSE did not contain response.completed")
	}
	return nil
}

func (c *Client) doEnvelope(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("V2 admin request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("V2 admin request returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("V2 admin response exceeds limit")
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode V2 response envelope: %w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("V2 API error %d: %s", envelope.Code, envelope.Message)
	}
	if output != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("decode V2 response data: %w", err)
		}
	}
	return nil
}
