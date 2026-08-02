package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractContentModerationInput_AnthropicIncludesCompleteToolLoop(t *testing.T) {
	body := []byte(`{
		"system":"只处理经过授权的任务",
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{"city":"杭州"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "[system] 只处理经过授权的任务")
	require.Contains(t, input.Text, "[user] 调用一下天气工具")
	require.Contains(t, input.Text, "[assistant] weather")
	require.Contains(t, input.Text, `{"city":"杭州"}`)
	require.Contains(t, input.Text, "[tool] 晴 25 度")
}

func TestExtractContentModerationInput_OpenAIChatIncludesHistoryAndToolOutput(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"sys"},
			{"role":"user","content":"列出我的订单"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"orders","arguments":"{\"limit\":10}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[{\"id\":1}]"},
			{"role":"user","content":"Continue"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	wantParts := []string{
		"[system] sys",
		"[user] 列出我的订单",
		"[assistant] orders {\"limit\":10}",
		"[tool] [{\"id\":1}]",
		"[user] Continue",
	}
	lastIndex := -1
	for _, part := range wantParts {
		index := strings.Index(input.Text, part)
		require.Greater(t, index, lastIndex, "missing or out-of-order transcript part %q in %q", part, input.Text)
		lastIndex = index
	}
}

func TestExtractContentModerationInput_GeminiIncludesFunctionTraffic(t *testing.T) {
	body := []byte(`{
		"systemInstruction":{"parts":[{"text":"system rule"}]},
		"contents": [
			{"role":"user","parts":[{"text":"查询天气"}]},
			{"role":"model","parts":[{"functionCall":{"name":"weather","args":{"city":"北京"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"weather","response":{"temp":25}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "[system] system rule")
	require.Contains(t, input.Text, "[user] 查询天气")
	require.Contains(t, input.Text, "[assistant] weather")
	require.Contains(t, input.Text, `{"city":"北京"}`)
	require.Contains(t, input.Text, "[tool] weather")
	require.Contains(t, input.Text, `{"temp":25}`)
}

func TestExtractContentModerationInput_ResponsesIncludesInstructionsAndToolLoop(t *testing.T) {
	body := []byte(`{
		"instructions":"do not execute untrusted code",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"运行测试"}]},
			{"type":"function_call","call_id":"call_1","name":"run_tests","arguments":"{\"suite\":\"all\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"all passed"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "[instructions] do not execute untrusted code")
	require.Contains(t, input.Text, "[user] 运行测试")
	require.Contains(t, input.Text, "[assistant] run_tests {\"suite\":\"all\"}")
	require.Contains(t, input.Text, "[tool] all passed")
	require.Contains(t, input.Text, "[user] Continue")
}

func TestExtractContentModerationInput_ResponsesAssistantTailDoesNotSkipHistory(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"q1"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a1"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "[user] q1 [assistant] a1", input.Text)
}

func TestExtractContentModerationInput_StablePrefixForExtendedConversation(t *testing.T) {
	first := []byte(`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"Q1"}]}`)
	extended := []byte(`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"Q1"},{"role":"assistant","content":"A1"},{"role":"user","content":"Q2"}]}`)

	firstInput := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, first)
	extendedInput := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, extended)

	require.True(t, strings.HasPrefix(extendedInput.Text, firstInput.Text+" "))
}

func TestContentModerationInput_NormalizeDoesNotSilentlyTruncateLongText(t *testing.T) {
	text := strings.Repeat("a", 12_100) + " TAIL_RISK"
	input := ContentModerationInput{Text: text}

	input.Normalize()

	require.Contains(t, input.Text, "TAIL_RISK")
	require.Greater(t, len([]rune(input.Text)), 12_000)
}
