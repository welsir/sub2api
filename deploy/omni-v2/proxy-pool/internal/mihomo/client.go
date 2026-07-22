package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type ProviderNode struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Alive bool   `json:"alive"`
}

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    httpClient,
	}
}

func (c *Client) ListProvider(ctx context.Context, provider string) ([]ProviderNode, error) {
	var response struct {
		Proxies []ProviderNode `json:"proxies"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(provider), nil, &response); err != nil {
		return nil, err
	}
	return response.Proxies, nil
}

func (c *Client) Current(ctx context.Context, group string) (string, error) {
	var response struct {
		Now string `json:"now"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/proxies/"+url.PathEscape(group), nil, &response); err != nil {
		return "", err
	}
	if response.Now == "" {
		return "", fmt.Errorf("mihomo group %q returned an empty selection", group)
	}
	return response.Now, nil
}

func (c *Client) SelectAndConfirm(ctx context.Context, group, node string) (string, error) {
	body := map[string]string{"name": node}
	if err := c.doJSON(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), body, nil); err != nil {
		return "", err
	}
	actual, err := c.Current(ctx, group)
	if err != nil {
		return "", fmt.Errorf("read back mihomo group %q: %w", group, err)
	}
	if actual != node {
		return actual, fmt.Errorf("mihomo group %q read-back mismatch: wanted %q, got %q", group, node, actual)
	}
	return actual, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode mihomo request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create mihomo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mihomo request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mihomo request returned HTTP %d", resp.StatusCode)
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read mihomo response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("mihomo response exceeds %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode mihomo response: %w", err)
	}
	return nil
}
