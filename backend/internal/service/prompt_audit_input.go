package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// ExtractLatestUserPrompt returns only the newest user-authored text carried by
// an inbound request. It deliberately preserves text whitespace and excludes
// assistant, tool, image, file, system, and developer content.
func ExtractLatestUserPrompt(protocol string, body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}

	switch protocol {
	case ContentModerationProtocolOpenAIResponses:
		return latestResponsesUserPrompt(gjson.GetBytes(body, "input"))
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages:
		return latestRoleUserPrompt(gjson.GetBytes(body, "messages"))
	case ContentModerationProtocolGemini:
		return latestGeminiUserPrompt(gjson.GetBytes(body, "contents"))
	case ContentModerationProtocolOpenAIImages:
		return nonBlankAuditText(gjson.GetBytes(body, "prompt").String())
	default:
		return ""
	}
}

func latestResponsesUserPrompt(input gjson.Result) string {
	if !input.Exists() {
		return ""
	}
	if input.Type == gjson.String {
		return nonBlankAuditText(input.String())
	}

	var item gjson.Result
	if input.IsArray() {
		items := input.Array()
		if len(items) == 0 {
			return ""
		}
		item = items[len(items)-1]
	} else if input.IsObject() {
		item = input
	} else {
		return ""
	}

	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	if role != "user" && !(role == "" && (typ == "message" || typ == "input_text")) {
		return ""
	}

	parts := auditTextParts(item.Get("content"))
	if typ == "input_text" {
		parts = appendAuditText(parts, item.Get("text"))
	}
	return joinAuditTextParts(parts)
}

func latestRoleUserPrompt(messages gjson.Result) string {
	if !messages.IsArray() {
		return ""
	}
	items := messages.Array()
	if len(items) == 0 {
		return ""
	}
	last := items[len(items)-1]
	if strings.ToLower(strings.TrimSpace(last.Get("role").String())) != "user" {
		return ""
	}
	return joinAuditTextParts(auditTextParts(last.Get("content")))
}

func latestGeminiUserPrompt(contents gjson.Result) string {
	if !contents.IsArray() {
		return ""
	}
	items := contents.Array()
	if len(items) == 0 {
		return ""
	}
	last := items[len(items)-1]
	if strings.ToLower(strings.TrimSpace(last.Get("role").String())) != "user" {
		return ""
	}

	partsResult := last.Get("parts")
	if !partsResult.IsArray() {
		return ""
	}
	parts := make([]string, 0, len(partsResult.Array()))
	for _, part := range partsResult.Array() {
		parts = appendAuditText(parts, part.Get("text"))
	}
	return joinAuditTextParts(parts)
}

func auditTextParts(content gjson.Result) []string {
	if !content.Exists() {
		return nil
	}
	if content.Type == gjson.String {
		return appendAuditText(nil, content)
	}
	if !content.IsArray() {
		return nil
	}

	parts := make([]string, 0, len(content.Array()))
	for _, block := range content.Array() {
		if block.Type == gjson.String {
			parts = appendAuditText(parts, block)
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(block.Get("type").String()))
		if typ == "text" || typ == "input_text" {
			parts = appendAuditText(parts, block.Get("text"))
		}
	}
	return parts
}

func appendAuditText(parts []string, value gjson.Result) []string {
	if value.Type != gjson.String {
		return parts
	}
	text := value.String()
	if strings.TrimSpace(text) == "" {
		return parts
	}
	return append(parts, text)
}

func joinAuditTextParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func nonBlankAuditText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}
