package service

import "testing"

func TestExtractLatestUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
	}{
		{
			name:     "responses stores latest user message",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"second\nline"}]}]}`,
			want:     "second\nline",
		},
		{
			name:     "responses string input preserves whitespace",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":"  keep  internal\nspacing  "}`,
			want:     "  keep  internal\nspacing  ",
		},
		{
			name:     "responses joins ordered text blocks",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"},{"type":"input_text","text":"beta"}]}]}`,
			want:     "alpha\nbeta",
		},
		{
			name:     "chat stores latest user message",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"second"}]}`,
			want:     "second",
		},
		{
			name:     "anthropic stores latest user message",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"messages":[{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"text","text":"anthropic prompt"}]}]}`,
			want:     "anthropic prompt",
		},
		{
			name:     "gemini stores latest user content",
			protocol: ContentModerationProtocolGemini,
			body:     `{"contents":[{"role":"model","parts":[{"text":"answer"}]},{"role":"user","parts":[{"text":"gemini prompt"},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`,
			want:     "gemini prompt",
		},
		{
			name:     "tool continuation is skipped",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":[{"type":"function_call_output","call_id":"call_1","output":"result"}]}`,
			want:     "",
		},
		{
			name:     "assistant last message is skipped",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"user","content":"prompt"},{"role":"assistant","content":"answer"}]}`,
			want:     "",
		},
		{
			name:     "image stores prompt but not image bytes",
			protocol: ContentModerationProtocolOpenAIImages,
			body:     `{"prompt":"draw a moon","image":"data:image/png;base64,AAAA"}`,
			want:     "draw a moon",
		},
		{
			name:     "invalid json is skipped",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":`,
			want:     "",
		},
		{
			name:     "empty user text is skipped",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"user","content":"   "}]}`,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractLatestUserPrompt(tt.protocol, []byte(tt.body)); got != tt.want {
				t.Fatalf("ExtractLatestUserPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}
