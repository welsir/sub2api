package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/tidwall/gjson"
)

func ExtractContentModerationText(protocol string, body []byte) string {
	return ExtractContentModerationInput(protocol, body).Text
}

func ExtractContentModerationInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ContentModerationInput{}
	}
	root := gjson.ParseBytes(body)
	var parts []string
	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectAnthropicModerationContext(root, &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		collectOpenAIChatModerationContext(root, &parts, &images)
	case ContentModerationProtocolOpenAIResponses:
		collectResponsesModerationContext(root, &parts, &images)
	case ContentModerationProtocolGemini:
		collectGeminiModerationContext(root, &parts, &images)
	case ContentModerationProtocolOpenAIImages:
		appendModerationSegment(&parts, "user", []string{root.Get("prompt").String()})
		var ignored []string
		collectContentValue(root.Get("images"), &ignored, &images)
	default:
		collectResponsesModerationContext(root, &parts, &images)
		collectOpenAIChatModerationContext(root, &parts, &images)
		collectGeminiModerationContext(root, &parts, &images)
	}
	out := ContentModerationInput{
		Text:   normalizeContentModerationText(strings.Join(parts, "\n")),
		Images: normalizeModerationImages(images),
	}
	out.Normalize()
	return out
}

func collectOpenAIChatModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	messages := root.Get("messages")
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		role := normalizeModerationRole(message.Get("role").String(), "user")
		var candidate []string
		collectContentValue(message.Get("content"), &candidate, images)
		collectFunctionCall(message.Get("function_call"), &candidate)
		if calls := message.Get("tool_calls"); calls.IsArray() {
			calls.ForEach(func(_, call gjson.Result) bool {
				collectFunctionCall(call.Get("function"), &candidate)
				return true
			})
		}
		appendModerationSegment(parts, role, candidate)
		return true
	})
}

func collectAnthropicModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	var system []string
	collectAnthropicContentValue(root.Get("system"), &system, images)
	appendModerationSegment(parts, "system", system)

	messages := root.Get("messages")
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		role := normalizeModerationRole(message.Get("role").String(), "user")
		content := message.Get("content")
		if !content.IsArray() {
			var candidate []string
			collectAnthropicContentValue(content, &candidate, images)
			appendModerationSegment(parts, role, candidate)
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			typ := strings.ToLower(strings.TrimSpace(block.Get("type").String()))
			var candidate []string
			switch typ {
			case "tool_use", "server_tool_use":
				addModerationText(&candidate, block.Get("name").String())
				addModerationResult(&candidate, block.Get("input"))
				appendModerationSegment(parts, "assistant", candidate)
			case "tool_result", "web_search_tool_result":
				collectAnthropicContentValue(block.Get("content"), &candidate, images)
				appendModerationSegment(parts, "tool", candidate)
			default:
				collectAnthropicContentValue(block, &candidate, images)
				appendModerationSegment(parts, role, candidate)
			}
			return true
		})
		return true
	})
}

func collectAnthropicContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectAnthropicContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "output_text", "message":
			if value.Get("text").Exists() {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectAnthropicContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
			var ignored []string
			collectContentValue(value, &ignored, images)
		}
	}
}

func collectResponsesModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	appendModerationSegment(parts, "instructions", []string{root.Get("instructions").String()})
	input := root.Get("input")
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		appendModerationSegment(parts, "user", []string{input.String()})
	case input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			collectResponsesItem(item, parts, images)
			return true
		})
	case input.IsObject():
		collectResponsesItem(input, parts, images)
	}
}

func collectResponsesItem(item gjson.Result, parts *[]string, images *[]string) {
	typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	role := normalizeModerationRole(item.Get("role").String(), "user")
	var candidate []string
	switch typ {
	case "function_call", "custom_tool_call", "computer_call", "web_search_call":
		addModerationText(&candidate, item.Get("name").String())
		addModerationResult(&candidate, item.Get("arguments"))
		addModerationResult(&candidate, item.Get("action"))
		appendModerationSegment(parts, "assistant", candidate)
	case "function_call_output", "custom_tool_call_output", "computer_call_output", "web_search_call_output":
		addModerationResult(&candidate, item.Get("output"))
		appendModerationSegment(parts, "tool", candidate)
	default:
		collectContentValue(item.Get("content"), &candidate, images)
		if item.Get("text").Exists() {
			addModerationText(&candidate, item.Get("text").String())
		}
		if item.Get("output").Exists() {
			addModerationResult(&candidate, item.Get("output"))
		}
		appendModerationSegment(parts, role, candidate)
	}
}

func collectGeminiModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	for _, path := range []string{"systemInstruction.parts", "system_instruction.parts"} {
		collectGeminiParts(root.Get(path), "system", parts, images)
	}
	contents := root.Get("contents")
	if !contents.IsArray() {
		return
	}
	contents.ForEach(func(_, content gjson.Result) bool {
		role := normalizeModerationRole(content.Get("role").String(), "user")
		collectGeminiParts(content.Get("parts"), role, parts, images)
		return true
	})
}

func collectGeminiParts(value gjson.Result, role string, parts *[]string, images *[]string) {
	if !value.IsArray() {
		return
	}
	value.ForEach(func(_, part gjson.Result) bool {
		appendModerationSegment(parts, role, []string{part.Get("text").String()})
		addGeminiModerationImage(images, part)
		if call := part.Get("functionCall"); call.Exists() {
			var toolCall []string
			addModerationText(&toolCall, call.Get("name").String())
			addModerationResult(&toolCall, call.Get("args"))
			appendModerationSegment(parts, "assistant", toolCall)
		}
		if call := part.Get("function_call"); call.Exists() {
			var toolCall []string
			addModerationText(&toolCall, call.Get("name").String())
			addModerationResult(&toolCall, call.Get("args"))
			appendModerationSegment(parts, "assistant", toolCall)
		}
		for _, path := range []string{"functionResponse", "function_response"} {
			if response := part.Get(path); response.Exists() {
				var toolOutput []string
				addModerationText(&toolOutput, response.Get("name").String())
				addModerationResult(&toolOutput, response.Get("response"))
				appendModerationSegment(parts, "tool", toolOutput)
			}
		}
		return true
	})
}

func collectFunctionCall(value gjson.Result, parts *[]string) {
	if !value.Exists() {
		return
	}
	addModerationText(parts, value.Get("name").String())
	addModerationResult(parts, value.Get("arguments"))
}

func addModerationResult(parts *[]string, value gjson.Result) {
	if !value.Exists() {
		return
	}
	if value.Type == gjson.String {
		addModerationText(parts, value.String())
		return
	}
	addModerationText(parts, value.Raw)
}

func appendModerationSegment(parts *[]string, role string, candidate []string) {
	text := normalizeContentModerationText(strings.Join(candidate, "\n"))
	if text == "" {
		return
	}
	role = normalizeModerationRole(role, "user")
	*parts = append(*parts, fmt.Sprintf("[%s] %s", role, text))
}

func normalizeModerationRole(role string, fallback string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "model":
		return "assistant"
	case "function":
		return "tool"
	case "system", "developer", "user", "assistant", "tool", "instructions":
		return role
	case "":
		return fallback
	default:
		return role
	}
}

func collectContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		addModerationImage(images, value.Get("image_url.url").String())
		addModerationImage(images, value.Get("image_url").String())
		addModerationImage(images, value.Get("url").String())
		addModerationImageData(images, value.Get("source.media_type").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("source.mediaType").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("media_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mime_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mimeType").String(), value.Get("data").String())
		addModerationImage(images, value.Get("source.data").String())
		addModerationImage(images, value.Get("data").String())
		addModerationImage(images, value.Get("base64").String())
		switch typ {
		case "", "text", "input_text", "output_text", "message":
			if value.Get("text").Exists() {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
		}
	}
}

func addGeminiModerationImage(images *[]string, part gjson.Result) {
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mime_type").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mimeType").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	addModerationImage(images, part.Get("file_data.file_uri").String())
	addModerationImage(images, part.Get("fileData.fileUri").String())
}

func addModerationImageData(images *[]string, mimeType string, data string) {
	mimeType = strings.TrimSpace(mimeType)
	data = strings.TrimSpace(data)
	if mimeType == "" || data == "" {
		return
	}
	addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
}

func addModerationImage(images *[]string, image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
}

func normalizeModerationImages(images []string) []string {
	out := make([]string, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		out = append(out, image)
	}
	return out
}

func limitContentModerationImages(images []string) []string {
	if len(images) <= maxContentModerationInputImages {
		return images
	}
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(images))))
	if err != nil {
		return images[:maxContentModerationInputImages]
	}
	return []string{images[int(idx.Int64())]}
}

func addModerationText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	*parts = append(*parts, text)
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
