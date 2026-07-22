package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/mihomo"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
	poolruntime "github.com/welsir/sub2api-v2-proxy-pool/internal/runtime"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/state"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check the local controller API")
	flag.Parse()
	if *healthcheck {
		if err := runHealthcheck(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := pool.LoadConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load proxy pool config: %w", err)
	}
	if !cfg.Enabled {
		return fmt.Errorf("proxy pool is disabled")
	}
	internalSecret := strings.TrimSpace(os.Getenv("PROXY_POOL_INTERNAL_SECRET"))
	if len(internalSecret) < 16 {
		return fmt.Errorf("PROXY_POOL_INTERNAL_SECRET must be at least 16 characters")
	}
	adminEmail := strings.TrimSpace(os.Getenv("PROXY_POOL_ADMIN_EMAIL"))
	adminPassword := os.Getenv("PROXY_POOL_ADMIN_PASSWORD")
	if adminEmail == "" || adminPassword == "" {
		return fmt.Errorf("V2 admin credentials are required")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	v2Client, err := sub2api.NewClient(cfg.Sub2APIBaseURL, cfg.InstanceID, httpClient)
	if err != nil {
		return err
	}
	loginContext, cancelLogin := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLogin()
	if err := v2Client.Login(loginContext, adminEmail, adminPassword); err != nil {
		return fmt.Errorf("authenticate to Sub2API V2: %w", err)
	}

	mihomoClient := mihomo.NewClient(cfg.MihomoBaseURL, cfg.MihomoSecret, httpClient)
	engine := poolruntime.NewEngine(cfg, mihomoClient, networkProbe)
	stateDir := envOrDefault("PROXY_POOL_STATE_DIR", "/var/lib/proxy-pool")
	store := state.NewStore(stateDir)
	cycle := func(ctx context.Context) (pool.CycleResult, error) {
		result, err := engine.Cycle(ctx)
		audit := state.AuditEvent{Action: "observe_cycle", Result: "success"}
		if err != nil {
			audit.Result = "failed"
			audit.Detail = err.Error()
		}
		_ = store.AppendAudit(audit)
		return result, err
	}
	controller := pool.NewController(cfg, cycle)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	controller.Start(ctx)

	server := &http.Server{
		Addr:              envOrDefault("PROXY_POOL_LISTEN", "0.0.0.0:9080"),
		Handler:           controller.Handler(internalSecret),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func networkProbe(ctx context.Context, group, _ string) pool.ProbeResult {
	prefix := "V2-PROBE-"
	index, err := strconv.Atoi(strings.TrimPrefix(group, prefix))
	if err != nil || index < 1 || index > 3 {
		return pool.ProbeResult{Failure: pool.FailureTransport, Message: "invalid probe group"}
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://mihomo:%d", 19100+index))
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(proxyURL),
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}
	return pool.ProbeHTTP(ctx, client, "https://chatgpt.com/backend-api/codex/responses")
}

func runHealthcheck() error {
	secret := strings.TrimSpace(os.Getenv("PROXY_POOL_INTERNAL_SECRET"))
	listen := envOrDefault("PROXY_POOL_LISTEN", "0.0.0.0:9080")
	_, port, found := strings.Cut(listen, ":")
	if !found {
		return fmt.Errorf("invalid PROXY_POOL_LISTEN")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/status", nil)
	req.Header.Set("X-Proxy-Pool-Secret", secret)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
