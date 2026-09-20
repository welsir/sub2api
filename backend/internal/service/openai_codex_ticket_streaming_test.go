package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The upstream deliberately withholds completion until the client sees a delta.
// A whole-response buffer deadlocks here and fails the bounded assertion.
func TestStrictCodexBridgeStreamsBeforeUpstreamCompletion(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "proxy", true: "direct"}[direct], func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			release := make(chan struct{})
			sent := make(chan struct{})
			writerDone := make(chan struct{})
			defer func() { close(release); reader.Close(); writer.Close(); <-writerDone }()
			go func() {
				defer close(writerDone)
				defer writer.Close()
				io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
				select {
				case <-sent:
				case <-release:
					return
				}
				io.WriteString(writer, ticketTestSSE("gpt-6-astra"))
			}()
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: direct}, upstream)
			svc.cfg.Gateway.MaxLineSize = defaultMaxLineSize
			account := ticketTestAccount(41)
			svc.storeOpenAICodexTicket(context.Background(), account, verifiedTestTicket(account, fakeCodexTicketState(292), ticketTestProxyURL))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"ping","stream":true}`)
			observed := make(chan struct{}, 1)
			done := make(chan error, 1)
			go func() {
				_, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "tok", payload, len(payload), "gpt-6-astra", "", "", "", "", 1, func(frame []byte) error {
					if strings.Contains(string(frame), "response.output_text.delta") {
						observed <- struct{}{}
					}
					return nil
				})
				done <- err
			}()
			select {
			case <-observed:
			case <-time.After(3 * time.Second):
				t.Fatal("delta withheld until upstream completion")
			}
			close(sent)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not finish")
			}
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestStrictCodexTransportRejectsUncollectedStateBeforeForwarding(t *testing.T) {
	for _, state := range []string{"", fakeCodexTicketState(292)} {
		upstream := &httpUpstreamRecorder{}
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
		req := httptest.NewRequest(http.MethodPost, chatgptCodexURL, nil)
		req.Header.Set(openAICodexTurnStateHeader, state)
		resp, err := svc.doOpenAIUpstream(req, "", ticketTestAccount(41))
		require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
		require.Nil(t, resp)
		require.Empty(t, upstream.requests)
	}
}
