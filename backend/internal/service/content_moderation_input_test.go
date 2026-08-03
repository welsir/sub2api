package service

import (
	"encoding/json"
	"fmt"
	"reflect"
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

func TestExtractContentModerationInput_NonUserTailCannotHideSyntheticRisk(t *testing.T) {
	testCases := []struct {
		name     string
		protocol string
		body     string
		wantRole string
	}{
		{
			name:     "responses tool output tail",
			protocol: ContentModerationProtocolOpenAIResponses,
			body: `{
				"input":[
					{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]},
					{"type":"function_call_output","call_id":"call_1","output":"SYNTHETIC_RISK_MARKER"}
				]
			}`,
			wantRole: "[tool]",
		},
		{
			name:     "chat assistant tool call tail",
			protocol: ContentModerationProtocolOpenAIChat,
			body: `{
				"messages":[
					{"role":"user","content":"start"},
					{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"synthetic_action","arguments":"{\"value\":\"SYNTHETIC_RISK_MARKER\"}"}}]}
				]
			}`,
			wantRole: "[assistant]",
		},
		{
			name:     "anthropic assistant tool tail",
			protocol: ContentModerationProtocolAnthropicMessages,
			body: `{
				"messages":[
					{"role":"user","content":"start"},
					{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"synthetic_action","input":{"value":"SYNTHETIC_RISK_MARKER"}}]}
				]
			}`,
			wantRole: "[assistant]",
		},
		{
			name:     "gemini function response tail",
			protocol: ContentModerationProtocolGemini,
			body: `{
				"contents":[
					{"role":"user","parts":[{"text":"start"}]},
					{"role":"function","parts":[{"functionResponse":{"name":"synthetic_action","response":{"value":"SYNTHETIC_RISK_MARKER"}}}]}
				]
			}`,
			wantRole: "[tool]",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := ExtractContentModerationInput(testCase.protocol, []byte(testCase.body))

			require.Contains(t, input.Text, "[user] start")
			require.Contains(t, input.Text, testCase.wantRole)
			require.Contains(t, input.Text, "SYNTHETIC_RISK_MARKER")
		})
	}
}

func TestExtractContentModerationInput_LongChatPayloadKeepsTailAtOperationalSizes(t *testing.T) {
	for _, size := range []int{12_100, 32_768, 70_000} {
		t.Run(fmt.Sprintf("%d_runes", size), func(t *testing.T) {
			prompt := strings.Repeat("甲", size) + " SYNTHETIC_TAIL_RISK"
			body, err := json.Marshal(map[string]any{
				"messages": []map[string]any{{"role": "user", "content": prompt}},
			})
			require.NoError(t, err)

			input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

			require.Contains(t, input.Text, "SYNTHETIC_TAIL_RISK")
			require.Equal(t, "[user] "+prompt, input.Text)
		})
	}
}

func TestExtractContentModerationInput_AttachmentProjectionIsTextOnlyAndBounded(t *testing.T) {
	longBase64 := strings.Repeat("QUJD", 80)
	testCases := []struct {
		name       string
		protocol   string
		body       string
		want       []string
		notWant    []string
		attachment int
	}{
		{
			name:     "responses image and file",
			protocol: ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":[
				{"type":"input_text","text":"inspect the attachments"},
				{"type":"input_image","image_url":"https://assets.example.invalid/photo.PNG?token=synthetic-secret#preview"},
				{"type":"input_file","file_id":"file-synthetic-opaque","filename":"private-manual.PDF"}
			]}]}`,
			want: []string{
				"[user] inspect the attachments",
				"[attachment kind=image source=remote extension=.png]",
				"[attachment kind=file source=file_id extension=.pdf]",
			},
			notWant:    []string{"token=", "synthetic-secret", "file-synthetic-opaque", "private-manual", "assets.example.invalid"},
			attachment: 2,
		},
		{
			name:     "chat image url and file",
			protocol: ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"user","content":[
				{"type":"text","text":"compare these inputs"},
				{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,` + longBase64 + `"}},
				{"type":"file","file":{"filename":"customer-records.CSV","file_data":"` + longBase64 + `"}}
			]}]}`,
			want: []string{
				"[user] compare these inputs",
				"[attachment kind=image mime=image/jpeg source=inline]",
				"[attachment kind=file source=inline extension=.csv]",
			},
			notWant:    []string{"data:image", longBase64, "customer-records"},
			attachment: 2,
		},
		{
			name:     "anthropic image and document",
			protocol: ContentModerationProtocolAnthropicMessages,
			body: `{"messages":[{"role":"user","content":[
				{"type":"text","text":"review the supplied material"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + longBase64 + `"}},
				{"type":"document","source":{"type":"url","url":"https://docs.example.invalid/guide.pdf?signature=synthetic-secret"}}
			]}]}`,
			want: []string{
				"[user] review the supplied material",
				"[attachment kind=image mime=image/png source=inline]",
				"[attachment kind=document source=remote extension=.pdf]",
			},
			notWant:    []string{longBase64, "signature=", "synthetic-secret", "docs.example.invalid"},
			attachment: 2,
		},
		{
			name:     "gemini inline and remote data",
			protocol: ContentModerationProtocolGemini,
			body: `{"contents":[{"role":"user","parts":[
				{"text":"understand the inputs"},
				{"inlineData":{"mimeType":"image/webp","data":"` + longBase64 + `"}},
				{"fileData":{"mimeType":"application/pdf","fileUri":"gs://synthetic-private/report.pdf?generation=9"}}
			]}]}`,
			want: []string{
				"[user] understand the inputs",
				"[attachment kind=image mime=image/webp source=inline]",
				"[attachment kind=file mime=application/pdf source=remote extension=.pdf]",
			},
			notWant:    []string{longBase64, "synthetic-private", "generation=9", "gs://"},
			attachment: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := ExtractContentModerationInput(testCase.protocol, []byte(testCase.body))

			for _, want := range testCase.want {
				require.Contains(t, input.Text, want)
			}
			for _, notWant := range testCase.notWant {
				require.NotContains(t, input.Text, notWant)
			}
			require.Empty(t, input.Images)
			require.IsType(t, "", input.ModerationInput())
			require.Equal(t, testCase.attachment, strings.Count(input.Text, "[attachment "))
		})
	}
}

func TestExtractContentModerationInput_AttachmentOnlyStillRequiresTextModeration(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_image","file_id":"file-synthetic-opaque"}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "[user] [attachment kind=image source=file_id]", input.Text)
	require.False(t, input.IsEmpty())
	require.Empty(t, input.Images)
	require.Equal(t, input.Text, input.ModerationInput())
}

func TestExtractContentModerationInput_ToolTrafficRedactsMediaAndTransportSecrets(t *testing.T) {
	longBase64 := strings.Repeat("QUJD", 80)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"type": "function",
					"function": map[string]any{
						"name": "inspect_artifact",
						"arguments": map[string]any{
							"page_url":  "https://example.invalid/help?session=synthetic-secret#part",
							"image_url": "https://cdn.example.invalid/sample.png?signature=synthetic-secret",
							"file_id":   "file-synthetic-opaque",
							"headers":   map[string]string{"Authorization": "Bearer synthetic-secret"},
							"blob":      longBase64,
						},
					},
				}},
			},
			{
				"role": "tool",
				"content": map[string]any{
					"status":   "ready",
					"data_uri": "data:application/pdf;base64," + longBase64,
				},
			},
		},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "inspect_artifact")
	require.Contains(t, input.Text, "[attachment kind=file source=remote]")
	require.Contains(t, input.Text, `"status":"ready"`)
	require.Contains(t, input.Text, "[attachment kind=image source=remote extension=.png]")
	require.Contains(t, input.Text, "[attachment kind=file source=file_id]")
	require.Contains(t, input.Text, "[attachment kind=file mime=application/pdf source=inline]")
	require.Contains(t, input.Text, "[omitted]")
	for _, secret := range []string{"session=", "signature=", "synthetic-secret", "file-synthetic-opaque", longBase64, "data:application/pdf", "Authorization", "Bearer"} {
		require.NotContains(t, input.Text, secret)
	}
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_AnthropicToolObjectUsesSafeProjection(t *testing.T) {
	longBase64 := strings.Repeat("QUJD", 80)
	body, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": "tool_1",
				"content": map[string]any{
					"status":   "ready",
					"data_uri": "data:application/pdf;base64," + longBase64,
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, `"status":"ready"`)
	require.Contains(t, input.Text, "[attachment kind=file mime=application/pdf source=inline]")
	require.NotContains(t, input.Text, longBase64)
	require.NotContains(t, input.Text, "data:application/pdf")
}

func TestExtractContentModerationInput_PlainTextStartingWithDataIsPreserved(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"data: 这里仍有 SYNTHETIC_RISK"}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "[user] data: 这里仍有 SYNTHETIC_RISK", input.Text)
}

func TestExtractContentModerationInput_NestedToolDataUsesStructuredSafeProjection(t *testing.T) {
	longBase64 := strings.Repeat("QUJD", 80)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "assistant",
			"tool_calls": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name": "inspect_nested_artifact",
					"arguments": map[string]any{
						"nested": map[string]any{
							"image_url":     map[string]any{"url": "https://cdn.example.invalid/private.png?signature=synthetic-secret"},
							"fileId":        "file-synthetic-secret",
							"previewBase64": "QUJD",
							"bytes":         []int{137, 80, 78, 71},
							"unknown_uri":   "data:image/webp;base64,QUJD",
							"unknown_blob":  longBase64,
							"headers":       map[string]string{"Authorization": "Bearer synthetic-secret"},
							"cookie":        "session=synthetic-secret",
							"semantic":      "SYNTHETIC_DANGEROUS_TEXT must remain",
							"ordinary":      "QUJD is a short ordinary token",
						},
					},
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "inspect_nested_artifact")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT must remain")
	require.Contains(t, input.Text, "QUJD is a short ordinary token")
	require.Contains(t, input.Text, "[attachment kind=image source=remote extension=.png]")
	require.Contains(t, input.Text, "[attachment kind=file source=file_id]")
	require.Contains(t, input.Text, "[attachment kind=image mime=image/webp source=inline]")
	require.Contains(t, input.Text, "[omitted]")
	for _, forbidden := range []string{
		"cdn.example.invalid", "signature=", "synthetic-secret", "file-synthetic-secret",
		"data:image", longBase64, "previewBase64", `"bytes":[137`, "Authorization", "Bearer", "session=",
	} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_NestedAttachmentObjectsCollapseToSafeMarkers(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "assistant",
			"tool_calls": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name": "inspect_attachments",
					"arguments": map[string]any{
						"attachment": map[string]any{
							"type":     "file",
							"filename": "private.pdf",
							"file_id":  "file-sensitive-id",
							"hash":     "sensitive-file-hash",
						},
						"image": map[string]any{
							"url":      "https://cdn.example.invalid/private.png?signature=sensitive-signature",
							"basename": "private-image-name.png",
						},
						"semantic": "SYNTHETIC_DANGEROUS_TEXT remains visible",
					},
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	require.Contains(t, input.Text, "[attachment kind=file source=file_id extension=.pdf]")
	require.Contains(t, input.Text, "[attachment kind=image source=remote extension=.png]")
	for _, forbidden := range []string{
		"private.pdf", "file-sensitive-id", "sensitive-file-hash", "cdn.example.invalid",
		"sensitive-signature", "private-image-name", `"hash"`, `"basename"`,
	} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_DataBodyAndPayloadKeepSemanticText(t *testing.T) {
	longBase64 := strings.Repeat("QUJD", 80)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "tool",
			"content": map[string]any{
				"body":    "SYNTHETIC_DANGEROUS_TEXT in body",
				"data":    map[string]any{"message": "SYNTHETIC_DANGEROUS_TEXT in data"},
				"payload": "ordinary semantic payload",
				"bytes":   []int{137, 80, 78, 71},
				"encoded": longBase64,
			},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT in body")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT in data")
	require.Contains(t, input.Text, "ordinary semantic payload")
	require.Contains(t, input.Text, "[omitted]")
	require.NotContains(t, input.Text, longBase64)
	require.NotContains(t, input.Text, `"bytes":[137`)
}

func TestExtractContentModerationInput_TransportSecretsAreRemovedWithoutDroppingCounters(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "tool",
			"content": map[string]any{
				"api_key":      "credential-api-key",
				"apiKey":       "credential-api-key-camel",
				"access_token": "credential-access-token",
				"accessToken":  "credential-access-token-camel",
				"token":        "credential-token",
				"secret":       "credential-secret",
				"x-api-key":    "credential-x-api-key",
				"bearer":       "credential-bearer",
				"token_count":  42,
				"message":      "SYNTHETIC_DANGEROUS_TEXT remains visible",
			},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, `"token_count":42`)
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	for _, forbidden := range []string{
		"credential-api-key", "credential-api-key-camel", "credential-access-token",
		"credential-access-token-camel", "credential-token", "credential-secret",
		"credential-x-api-key", "credential-bearer",
	} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_RelativeURLFieldsBecomeMarkersWithoutTouchingFreeText(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "tool",
			"content": map[string]any{
				"protocol_relative": map[string]any{"url": "//cdn.invalid/private.pdf?token=sensitive-token"},
				"relative":          map[string]any{"href": "/download/private.pdf?signature=sensitive-signature"},
				"semantic":          "ordinary free text keeps /path and SYNTHETIC_DANGEROUS_TEXT",
			},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Equal(t, 2, strings.Count(input.Text, "[attachment kind=file source=remote extension=.pdf]"))
	require.Contains(t, input.Text, "ordinary free text keeps /path and SYNTHETIC_DANGEROUS_TEXT")
	for _, forbidden := range []string{"cdn.invalid", "private.pdf", "sensitive-token", "sensitive-signature", "token=", "signature="} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_ExtendedCredentialFieldsAreRemoved(t *testing.T) {
	secretFields := map[string]any{
		"password":            "credential-password",
		"passwd":              "credential-passwd",
		"pwd":                 "credential-pwd",
		"credential":          "credential-single",
		"credentials":         "credential-plural",
		"session_id":          "credential-session-id",
		"sessionId":           "credential-session-id-camel",
		"private_key":         "credential-private-key",
		"privateKey":          "credential-private-key-camel",
		"client_secret":       "credential-client-secret",
		"refresh_token":       "credential-refresh-token",
		"id_token":            "credential-id-token",
		"auth_token":          "credential-auth-token",
		"proxy_authorization": "credential-proxy-authorization",
		"token_count":         17,
		"message":             "SYNTHETIC_DANGEROUS_TEXT remains visible",
	}
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "tool", "content": secretFields}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, `"token_count":17`)
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	for key, value := range secretFields {
		if key == "token_count" || key == "message" {
			continue
		}
		require.NotContains(t, input.Text, value.(string))
	}
}

func TestExtractContentModerationInput_InputFileAndImageObjectsCollapseToMarkers(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "assistant",
			"tool_calls": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name": "inspect_inputs",
					"arguments": map[string]any{
						"input_file": map[string]any{
							"filename":  "private.pdf",
							"basename":  "sensitive-file-name.pdf",
							"file_data": "QUJD",
						},
						"input_image": map[string]any{
							"basename":  "sensitive-image-name.png",
							"mime_type": "image/png",
							"data":      "QUJD",
						},
						"semantic": "SYNTHETIC_DANGEROUS_TEXT remains visible",
					},
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "[attachment kind=file source=inline extension=.pdf]")
	require.Contains(t, input.Text, "[attachment kind=image mime=image/png source=inline extension=.png]")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	for _, forbidden := range []string{"private.pdf", "sensitive-file-name", "sensitive-image-name", `"file_data"`, `"basename"`} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_LongByteArraysAreOmittedButShortNumericArraysRemain(t *testing.T) {
	longBytes := make([]int, 64)
	for index := range longBytes {
		longBytes[index] = index
	}
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "tool",
			"content": map[string]any{
				"body":          longBytes,
				"payload":       longBytes,
				"short_numbers": []int{1, 2, 3},
				"message":       "SYNTHETIC_DANGEROUS_TEXT remains visible",
			},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, `"body":"[omitted]"`)
	require.Contains(t, input.Text, `"payload":"[omitted]"`)
	require.Contains(t, input.Text, `"short_numbers":[1,2,3]`)
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	require.NotContains(t, input.Text, `"body":[0,1,2`)
}

func TestExtractContentModerationInput_URLKeyKeepsNonURLSemanticText(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","content":{"url":"SYNTHETIC_DANGEROUS_TEXT must remain"}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, `"url":"SYNTHETIC_DANGEROUS_TEXT must remain"`)
	require.NotContains(t, input.Text, "[attachment ")
}

func TestExtractContentModerationInput_BareRelativeURLFieldBecomesMarker(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"tool","content":{
		"relative":{"url":"download/private.pdf?signature=sensitive-secret"},
		"ordinary":{"url":"SYNTHETIC_DANGEROUS_TOKEN"}
	}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "[attachment kind=file source=remote extension=.pdf]")
	require.Contains(t, input.Text, `"url":"SYNTHETIC_DANGEROUS_TOKEN"`)
	for _, forbidden := range []string{"download/private.pdf", "signature=", "sensitive-secret"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_CamelCaseInputAttachmentsCollapseToMarkers(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "assistant",
			"tool_calls": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name": "inspect_camel_inputs",
					"arguments": map[string]any{
						"inputFile": map[string]any{
							"filename": "private.pdf",
							"fileData": "QUJD",
						},
						"inputImage": map[string]any{
							"basename": "private.png",
							"imageUrl": "//cdn.invalid/private.png?signature=sensitive-signature",
						},
						"semantic": "SYNTHETIC_DANGEROUS_TEXT remains visible",
					},
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "[attachment kind=file source=inline extension=.pdf]")
	require.Contains(t, input.Text, "[attachment kind=image source=remote extension=.png]")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT remains visible")
	for _, forbidden := range []string{"private.pdf", "private.png", "cdn.invalid", "sensitive-signature", `"fileData"`, `"imageUrl"`} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_GenericImageAndFileObjectsKeepSemanticFields(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"tool","content":{
		"image":{"mime_type":"image/png","filename":"generic.png","description":"SYNTHETIC_DANGEROUS_TEXT in image description"},
		"file":{"mime_type":"application/pdf","filename":"generic.pdf","notes":"SYNTHETIC_DANGEROUS_TEXT in file notes"}
	}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT in image description")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_TEXT in file notes")
	require.Contains(t, input.Text, "[attachment kind=image mime=image/png extension=.png]")
	require.Contains(t, input.Text, "[attachment kind=file mime=application/pdf extension=.pdf]")
	require.NotContains(t, input.Text, "generic.png")
	require.NotContains(t, input.Text, "generic.pdf")
}

func TestExtractContentModerationInput_UnknownSchemeImageURLWrapperNeverLeaksReference(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","content":{
		"image_url":{"url":"dangerous://user:password@host/private/SYNTHETIC_RISK.exe?token=secret#fragment"},
		"direct_image_url":"dangerous://user:password@host/private/DIRECT_RISK.exe?token=secret#fragment"
	}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "[attachment kind=image source=remote]")
	for _, forbidden := range []string{"dangerous://", "user", "password", "host", "private", "SYNTHETIC_RISK", "token", "secret", "fragment", ".exe"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_ReferenceWrappersKeepSemanticSiblings(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","content":{
		"image_url":{"url":"custom://user:password@host/private.png?token=secret","description":"IMAGE_WRAPPER_DANGEROUS_TEXT"},
		"security":{"url":"custom://user:password@host/private.bin?token=secret","instruction":"SECURITY_OBJECT_DANGEROUS_TEXT"}
	}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "IMAGE_WRAPPER_DANGEROUS_TEXT")
	require.Contains(t, input.Text, "SECURITY_OBJECT_DANGEROUS_TEXT")
	require.Contains(t, input.Text, "[attachment kind=image source=remote]")
	require.Contains(t, input.Text, "[attachment kind=file source=remote]")
	for _, forbidden := range []string{"custom://", "user:password", "host/private", "token=secret"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_NestedAttachmentTransportWrappersKeepSemanticSiblings(t *testing.T) {
	chatBody := []byte(`{"messages":[{"role":"tool","content":{
		"attachment":{"file":{"file_id":"file-private","instruction":"NESTED_FILE_DANGEROUS_TEXT"},"caption":"safe"},
		"typed":{"type":"image_url","image_url":{"url":"https://secret.invalid/private.png?token=secret","description":"NESTED_IMAGE_DANGEROUS_TEXT"}}
	}}]}`)
	geminiBody := []byte(`{"contents":[{"role":"user","parts":[{
		"inlineData":{"mimeType":"image/png","data":"QUJD","instruction":"GEMINI_INLINE_DANGEROUS_TEXT"}
	}]}]}`)

	chatInput := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, chatBody)
	geminiInput := ExtractContentModerationInput(ContentModerationProtocolGemini, geminiBody)

	for _, want := range []string{"NESTED_FILE_DANGEROUS_TEXT", "NESTED_IMAGE_DANGEROUS_TEXT"} {
		require.Contains(t, chatInput.Text, want)
	}
	require.Contains(t, geminiInput.Text, "GEMINI_INLINE_DANGEROUS_TEXT")
	for _, transcript := range []string{chatInput.Text, geminiInput.Text} {
		for _, forbidden := range []string{"file-private", "secret.invalid", "token=secret", "QUJD"} {
			require.NotContains(t, transcript, forbidden)
		}
	}
}

func TestExtractContentModerationInput_UnknownSchemeIsRedactedOnlyForReferenceKeys(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","content":{
		"value":"dangerous:SYNTHETIC_DANGEROUS_TEXT",
		"url":"dangerous:SYNTHETIC_DANGEROUS_URL_TEXT",
		"absolute_url":"dangerous://user:password@host/private/path?token=secret"
	}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "dangerous:SYNTHETIC_DANGEROUS_TEXT")
	require.Equal(t, 2, strings.Count(input.Text, "[attachment kind=file source=remote]"))
	for _, forbidden := range []string{"SYNTHETIC_DANGEROUS_URL_TEXT", "user", "password", "host", "private", "token", "secret"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_AttachmentProjectionKeepsSemanticSiblingsAndDropsTransportMetadata(t *testing.T) {
	lowEntropyBase64 := strings.Repeat("A", 256)
	payload, err := json.Marshal(map[string]any{
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type":        "image",
				"mime_type":   "application/run-malware",
				"filename":    "private-malware.exe",
				"basename":    "private-malware-copy.exe",
				"url":         "custom://user:password@host/private/run.exe?token=secret",
				"file_id":     "file-sensitive-id",
				"source":      map[string]any{"url": "https://secret.invalid/private", "data": lowEntropyBase64},
				"base64":      lowEntropyBase64,
				"bytes":       []int{1, 2, 3, 4},
				"headers":     map[string]string{"Authorization": "Bearer secret"},
				"cookies":     "session=secret",
				"credentials": map[string]string{"password": "secret"},
				"description": "SYNTHETIC_DANGEROUS_DESCRIPTION remains visible",
				"instruction": "SYNTHETIC_DANGEROUS_INSTRUCTION remains visible",
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, payload)

	require.Contains(t, input.Text, "[attachment kind=image source=file_id extension=.exe]")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_DESCRIPTION remains visible")
	require.Contains(t, input.Text, "SYNTHETIC_DANGEROUS_INSTRUCTION remains visible")
	for _, forbidden := range []string{
		"application/run-malware", "private-malware", "custom://", "user:password", "host/private",
		"file-sensitive-id", lowEntropyBase64, "Authorization", "Bearer", "session=", "credentials", "secret.invalid",
	} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_LowEntropyBase64IsOmittedOnlyUnderBinarySemanticKeys(t *testing.T) {
	lowEntropyBase64 := strings.Repeat("A", 256)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": lowEntropyBase64},
			{"role": "tool", "content": map[string]any{"encoded": lowEntropyBase64, "base64": lowEntropyBase64}},
		},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.Contains(t, input.Text, "[user] "+lowEntropyBase64)
	require.NotContains(t, input.Text, `"encoded":"`+lowEntropyBase64+`"`)
	require.NotContains(t, input.Text, `"base64":"`+lowEntropyBase64+`"`)
	require.Contains(t, input.Text, "[omitted]")
}

func TestExtractContentModerationInput_StructuredDataBase64IsOmitted(t *testing.T) {
	lowEntropyBase64 := strings.Repeat("A", 128)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role":    "tool",
			"content": map[string]any{"data": lowEntropyBase64, "instruction": "DATA_SIBLING_DANGEROUS_TEXT"},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.NotContains(t, input.Text, lowEntropyBase64)
	require.Contains(t, input.Text, "[omitted]")
	require.Contains(t, input.Text, "DATA_SIBLING_DANGEROUS_TEXT")
}

func TestExtractContentModerationInput_InputAudioBase64IsOmitted(t *testing.T) {
	lowEntropyBase64 := strings.Repeat("A", 128)
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{{
				"type": "input_audio",
				"input_audio": map[string]any{
					"data":        lowEntropyBase64,
					"format":      "wav",
					"description": "AUDIO_SIBLING_DANGEROUS_TEXT",
				},
			}},
		}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, payload)

	require.NotContains(t, input.Text, lowEntropyBase64)
	require.Contains(t, input.Text, "[omitted]")
	require.Contains(t, input.Text, "AUDIO_SIBLING_DANGEROUS_TEXT")
}

func TestExtractContentModerationInput_TopLevelSemanticSchemasAreAuditedThroughSafeProjection(t *testing.T) {
	testCases := []struct {
		name     string
		protocol string
		body     string
		want     []string
	}{
		{
			name:     "chat tools functions and response format",
			protocol: ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"user","content":"safe"}],
				"tools":[{"type":"function","function":{"name":"tool_a","description":"CHAT_TOOL_RISK","parameters":{"properties":{"instruction":{"description":"CHAT_SCHEMA_RISK"},"callback_url":{"default":"custom://user:password@host/private?token=secret"}}}}}],
				"functions":[{"name":"legacy_a","description":"CHAT_FUNCTION_RISK","parameters":{"description":"CHAT_FUNCTION_SCHEMA_RISK"}}],
				"response_format":{"type":"json_schema","json_schema":{"name":"result","description":"CHAT_FORMAT_RISK","schema":{"description":"CHAT_FORMAT_SCHEMA_RISK"}}}}`,
			want: []string{"CHAT_TOOL_RISK", "CHAT_SCHEMA_RISK", "CHAT_FUNCTION_RISK", "CHAT_FUNCTION_SCHEMA_RISK", "CHAT_FORMAT_RISK", "CHAT_FORMAT_SCHEMA_RISK"},
		},
		{
			name:     "responses tools and text format",
			protocol: ContentModerationProtocolOpenAIResponses,
			body: `{"input":"safe",
				"tools":[{"type":"function","name":"tool_b","description":"RESPONSES_TOOL_RISK","parameters":{"description":"RESPONSES_SCHEMA_RISK"}}],
				"text":{"format":{"type":"json_schema","name":"result","description":"RESPONSES_FORMAT_RISK","schema":{"description":"RESPONSES_FORMAT_SCHEMA_RISK"}}}}`,
			want: []string{"RESPONSES_TOOL_RISK", "RESPONSES_SCHEMA_RISK", "RESPONSES_FORMAT_RISK", "RESPONSES_FORMAT_SCHEMA_RISK"},
		},
		{
			name:     "anthropic tool declarations",
			protocol: ContentModerationProtocolAnthropicMessages,
			body: `{"messages":[{"role":"user","content":"safe"}],
				"tools":[{"name":"tool_c","description":"ANTHROPIC_TOOL_RISK","input_schema":{"description":"ANTHROPIC_SCHEMA_RISK"}}],
				"output_config":{"format":{"type":"json_schema","schema":{"description":"ANTHROPIC_FORMAT_RISK"}}}}`,
			want: []string{"ANTHROPIC_TOOL_RISK", "ANTHROPIC_SCHEMA_RISK", "ANTHROPIC_FORMAT_RISK"},
		},
		{
			name:     "gemini tool declarations",
			protocol: ContentModerationProtocolGemini,
			body: `{"contents":[{"role":"user","parts":[{"text":"safe"}]}],
				"tools":[{"functionDeclarations":[{"name":"tool_d","description":"GEMINI_TOOL_RISK","parameters":{"description":"GEMINI_SCHEMA_RISK"}}]}],
				"generationConfig":{"responseSchema":{"type":"object","description":"GEMINI_FORMAT_SCHEMA_RISK"}}}`,
			want: []string{"GEMINI_TOOL_RISK", "GEMINI_SCHEMA_RISK", "GEMINI_FORMAT_SCHEMA_RISK"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := ExtractContentModerationInput(testCase.protocol, []byte(testCase.body))
			for _, want := range testCase.want {
				require.Contains(t, input.Text, want)
			}
			for _, forbidden := range []string{"custom://", "user:password", "host/private", "token=secret"} {
				require.NotContains(t, input.Text, forbidden)
			}
		})
	}
}

func TestExtractContentModerationInput_UnknownContentBlockUsesGenericSafeProjection(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{
		"type":"future_semantic_block",
		"description":"UNKNOWN_BLOCK_RISK",
		"payload":{"instruction":"UNKNOWN_BLOCK_INSTRUCTION","url":"custom://user:password@host/private?q=secret","headers":{"Authorization":"secret"}}
	}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "UNKNOWN_BLOCK_RISK")
	require.Contains(t, input.Text, "UNKNOWN_BLOCK_INSTRUCTION")
	require.Contains(t, input.Text, "[attachment kind=file source=remote]")
	for _, forbidden := range []string{"custom://", "user:password", "host/private", "q=secret", "Authorization"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_GeminiUnknownPartUsesGenericSafeProjection(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{
		"futureSemanticBlock":{"description":"GEMINI_UNKNOWN_BLOCK_RISK","instruction":"GEMINI_UNKNOWN_BLOCK_INSTRUCTION","uri":"custom://user:password@host/private?q=secret"}
	}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "GEMINI_UNKNOWN_BLOCK_RISK")
	require.Contains(t, input.Text, "GEMINI_UNKNOWN_BLOCK_INSTRUCTION")
	require.Contains(t, input.Text, "[attachment kind=file source=remote]")
	for _, forbidden := range []string{"custom://", "user:password", "host/private", "q=secret"} {
		require.NotContains(t, input.Text, forbidden)
	}
}

func TestExtractContentModerationInput_NonEmptyInvalidJSONFailsProjection(t *testing.T) {
	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(`{"input":`))

	require.True(t, input.ProjectionFailed)
	require.Contains(t, input.ProjectionError, "invalid JSON")
}

func TestLooksLikeLongBase64_DoesNotAllocateDecodedPayload(t *testing.T) {
	encoded := strings.Repeat("QUJD", 4096)
	require.True(t, looksLikeLongBase64(encoded))

	allocations := testing.AllocsPerRun(20, func() {
		if !looksLikeLongBase64(encoded) {
			t.Fatal("expected high-confidence base64")
		}
	})
	require.Zero(t, allocations)
}

func TestExtractContentModerationInput_ValidDoublePaddedDataURIKeepsMIME(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QQ=="}}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "[attachment kind=image mime=image/png source=inline]")
}

func TestParseModerationDataURI_LargePayloadDoesNotAllocatePayloadSizedCopy(t *testing.T) {
	dataURI := "DATA:image/png;base64," + strings.Repeat("QUJD", 16*1024)
	result := testing.Benchmark(func(b *testing.B) {
		for index := 0; index < b.N; index++ {
			mimeType, ok := parseModerationDataURI(dataURI)
			if !ok || mimeType != "image/png" {
				b.Fatal("expected valid image data URI")
			}
		}
	})

	require.Less(t, result.AllocedBytesPerOp(), int64(4096), "prefix detection must not lowercase-copy the complete payload")
}

func TestExtractContentModerationInput_RawProjectionDepthIsCheckedBeforeJSONValidation(t *testing.T) {
	body := []byte(strings.Repeat("[", contentModerationProjectionMaxDepth+1))

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.True(t, input.ProjectionFailed)
	require.Contains(t, input.ProjectionError, "depth")
}

func TestExtractContentModerationInput_RawProjectionScannerIgnoresBracketsInsideStrings(t *testing.T) {
	riskText := strings.Repeat(`[{\"escaped\":\"]}]`, 100) + " SYNTHETIC_RISK"
	body, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "tool", "content": riskText}},
	})
	require.NoError(t, err)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.False(t, input.ProjectionFailed)
	require.Contains(t, input.Text, "SYNTHETIC_RISK")
}

func TestExtractContentModerationInput_StringifiedJSONProjectionBoundsAreMarkedFailed(t *testing.T) {
	deep := any("SYNTHETIC_DEPTH_RISK")
	for index := 0; index < contentModerationProjectionMaxDepth+1; index++ {
		deep = map[string]any{"next": deep}
	}
	deepArguments, err := json.Marshal(deep)
	require.NoError(t, err)

	tooManyNodes := make([]int, contentModerationProjectionMaxNodes+1)
	nodeArguments, err := json.Marshal(tooManyNodes)
	require.NoError(t, err)

	for name, arguments := range map[string]string{
		"depth": string(deepArguments),
		"nodes": string(nodeArguments),
	} {
		t.Run(name, func(t *testing.T) {
			body, marshalErr := json.Marshal(map[string]any{
				"messages": []map[string]any{{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"type":     "function",
						"function": map[string]any{"name": "inspect", "arguments": arguments},
					}},
				}},
			})
			require.NoError(t, marshalErr)

			input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

			require.True(t, input.ProjectionFailed)
			require.Contains(t, input.ProjectionError, name)
		})
	}
}

func TestExtractContentModerationInput_OrdinaryStringifiedJSONAndTextRemainModeratable(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[
		{"type":"function","function":{"name":"json","arguments":"{\"message\":\"SYNTHETIC_JSON_TEXT\"}"}},
		{"type":"function","function":{"name":"text","arguments":"SYNTHETIC_PLAIN_TEXT with [brackets"}}
	]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.False(t, input.ProjectionFailed)
	require.Contains(t, input.Text, "SYNTHETIC_JSON_TEXT")
	require.Contains(t, input.Text, "SYNTHETIC_PLAIN_TEXT with [brackets")
}

func TestExtractContentModerationInput_ProjectionBoundsAreMarkedFailed(t *testing.T) {
	deep := any("SYNTHETIC_DANGEROUS_TEXT")
	for index := 0; index < 70; index++ {
		deep = map[string]any{"next": deep}
	}
	deepBody, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "tool", "content": deep}}})
	require.NoError(t, err)

	tooManyNodes := make([]int, 100_100)
	nodeBody, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "tool", "content": map[string]any{"values": tooManyNodes}}}})
	require.NoError(t, err)

	for name, body := range map[string][]byte{"depth": deepBody, "nodes": nodeBody} {
		t.Run(name, func(t *testing.T) {
			input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)
			value := reflect.ValueOf(input)
			failed := value.FieldByName("ProjectionFailed")
			require.True(t, failed.IsValid(), "ContentModerationInput must expose ProjectionFailed")
			require.True(t, failed.Bool())
			projectionError := value.FieldByName("ProjectionError")
			require.True(t, projectionError.IsValid(), "ContentModerationInput must expose ProjectionError")
			require.NotEmpty(t, projectionError.String())
			require.False(t, input.IsEmpty(), "projection failure must never look like safe empty input")
		})
	}
}

func TestContentModerationInput_NormalizeCanonicalizesCompatibilityImages(t *testing.T) {
	first := ContentModerationInput{
		Text:   "[user] inspect",
		Images: []string{"https://one.invalid/private.png?token=first-secret"},
	}
	second := ContentModerationInput{
		Text:   "[user] inspect",
		Images: []string{"https://two.invalid/other.png?token=second-secret"},
	}

	first.Normalize()
	second.Normalize()

	require.Empty(t, first.Images)
	require.Empty(t, second.Images)
	require.Equal(t, "[user] inspect [attachment kind=image source=remote extension=.png]", first.Text)
	require.Equal(t, first.Text, second.Text)
	require.Equal(t, first.Hash(), second.Hash())
	require.Equal(t, first.Text, first.ModerationInput())
	require.False(t, first.IsEmpty())
	require.NotContains(t, first.Text, "one.invalid")
	require.NotContains(t, first.Text, "first-secret")

	attachmentOnly := ContentModerationInput{Images: []string{"data:image/jpeg;base64,QUJD"}}
	attachmentOnly.Normalize()
	require.Equal(t, "[attachment kind=image mime=image/jpeg source=inline]", attachmentOnly.Text)
	require.Empty(t, attachmentOnly.Images)
	require.False(t, attachmentOnly.IsEmpty())
}
