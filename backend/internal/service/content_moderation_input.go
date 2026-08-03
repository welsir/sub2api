package service

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
)

const (
	contentModerationProjectionMaxDepth = 64
	contentModerationProjectionMaxNodes = 100_000
)

func ExtractContentModerationText(protocol string, body []byte) string {
	return ExtractContentModerationInput(protocol, body).Text
}

func ExtractContentModerationInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 {
		return ContentModerationInput{}
	}
	budget := contentModerationProjectionBudget{}
	if err := scanContentModerationRawProjectionBounds(&budget, body); err != nil {
		return ContentModerationInput{ProjectionFailed: true, ProjectionError: err.Error()}
	}
	if !gjson.ValidBytes(body) {
		return ContentModerationInput{ProjectionFailed: true, ProjectionError: "invalid JSON moderation payload"}
	}
	root := gjson.ParseBytes(body)
	if err := validateContentModerationEmbeddedProjectionBounds(protocol, root, &budget); err != nil {
		return ContentModerationInput{ProjectionFailed: true, ProjectionError: err.Error()}
	}
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
		for _, attachmentPath := range []string{"attachments", "images"} {
			if attachments := root.Get(attachmentPath); attachments.IsArray() {
				var candidate []string
				attachments.ForEach(func(_, attachment gjson.Result) bool {
					addModerationAttachment(&candidate, attachment)
					return true
				})
				appendModerationSegment(&parts, "user", candidate)
			}
		}
	default:
		collectResponsesModerationContext(root, &parts, &images)
		collectOpenAIChatModerationContext(root, &parts, &images)
		collectGeminiModerationContext(root, &parts, &images)
	}
	out := ContentModerationInput{
		Text: normalizeContentModerationText(strings.Join(parts, "\n")),
	}
	out.Normalize()
	return out
}

func collectOpenAIChatModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	appendModerationRootProjection(root, parts, "instructions", "tools", "functions", "response_format")
	messages := root.Get("messages")
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		role := normalizeModerationRole(message.Get("role").String(), "user")
		var candidate []string
		if role == "tool" && message.Get("content").IsObject() {
			addModerationResult(&candidate, message.Get("content"))
		} else {
			collectContentValue(message.Get("content"), &candidate, images)
		}
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
	appendModerationRootProjection(root, parts, "instructions", "tools", "output_config.format")

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
				if block.Get("content").IsObject() {
					addModerationResult(&candidate, block.Get("content"))
				} else {
					collectAnthropicContentValue(block.Get("content"), &candidate, images)
				}
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
		addSafeModerationText(parts, value.String())
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
		case "image_url", "input_image", "image", "document", "input_file", "file":
			addModerationAttachment(parts, value)
		default:
			addModerationResult(parts, value)
		}
	}
}

func collectResponsesModerationContext(root gjson.Result, parts *[]string, images *[]string) {
	appendModerationSegment(parts, "instructions", []string{root.Get("instructions").String()})
	appendModerationRootProjection(root, parts, "instructions", "tools", "text.format", "response_format")
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
	appendModerationRootProjection(root, parts, "instructions", "tools", "generationConfig.responseSchema", "generation_config.response_schema")
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
		var candidate []string
		hasStructuredAction := false
		addSafeModerationText(&candidate, part.Get("text").String())
		addGeminiModerationAttachments(&candidate, part)
		appendModerationSegment(parts, role, candidate)
		if call := part.Get("functionCall"); call.Exists() {
			hasStructuredAction = true
			var toolCall []string
			addModerationText(&toolCall, call.Get("name").String())
			addModerationResult(&toolCall, call.Get("args"))
			appendModerationSegment(parts, "assistant", toolCall)
		}
		if call := part.Get("function_call"); call.Exists() {
			hasStructuredAction = true
			var toolCall []string
			addModerationText(&toolCall, call.Get("name").String())
			addModerationResult(&toolCall, call.Get("args"))
			appendModerationSegment(parts, "assistant", toolCall)
		}
		for _, path := range []string{"functionResponse", "function_response"} {
			if response := part.Get(path); response.Exists() {
				hasStructuredAction = true
				var toolOutput []string
				addModerationText(&toolOutput, response.Get("name").String())
				addModerationResult(&toolOutput, response.Get("response"))
				appendModerationSegment(parts, "tool", toolOutput)
			}
		}
		if len(candidate) == 0 && !hasStructuredAction {
			addModerationResult(&candidate, part)
			appendModerationSegment(parts, role, candidate)
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

func appendModerationRootProjection(root gjson.Result, parts *[]string, role string, paths ...string) {
	var candidate []string
	for _, itemPath := range paths {
		addModerationResult(&candidate, root.Get(itemPath))
	}
	appendModerationSegment(parts, role, candidate)
}

func addModerationResult(parts *[]string, value gjson.Result) {
	if !value.Exists() {
		return
	}
	if value.Type == gjson.String {
		text := strings.TrimSpace(value.String())
		if gjson.Valid(text) {
			addModerationResult(parts, gjson.Parse(text))
			return
		}
		addSafeModerationText(parts, text)
		return
	}
	projected, ok := projectModerationResult(value, "")
	if !ok {
		return
	}
	raw, err := json.Marshal(projected)
	if err == nil {
		addModerationText(parts, string(raw))
	}
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
		addSafeModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentValue(item, parts, images)
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
				collectContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image", "input_file", "file", "document":
			addModerationAttachment(parts, value)
		default:
			addModerationResult(parts, value)
		}
	}
}

func addGeminiModerationAttachments(parts *[]string, part gjson.Result) {
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		addModerationText(parts, buildModerationAttachmentMarker(kindFromMIME(inlineData.Get("mime_type").String(), "file"), inlineData.Get("mime_type").String(), "inline", ""))
		addModerationAttachmentSemanticSiblings(parts, inlineData)
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		addModerationText(parts, buildModerationAttachmentMarker(kindFromMIME(inlineData.Get("mimeType").String(), "file"), inlineData.Get("mimeType").String(), "inline", ""))
		addModerationAttachmentSemanticSiblings(parts, inlineData)
	}
	if fileData := part.Get("file_data"); fileData.IsObject() {
		mimeType := fileData.Get("mime_type").String()
		addModerationText(parts, buildModerationAttachmentMarker(kindFromMIME(mimeType, "file"), mimeType, "remote", safeModerationReferenceExtension(fileData.Get("file_uri").String())))
		addModerationAttachmentSemanticSiblings(parts, fileData)
	}
	if fileData := part.Get("fileData"); fileData.IsObject() {
		mimeType := fileData.Get("mimeType").String()
		addModerationText(parts, buildModerationAttachmentMarker(kindFromMIME(mimeType, "file"), mimeType, "remote", safeModerationReferenceExtension(fileData.Get("fileUri").String())))
		addModerationAttachmentSemanticSiblings(parts, fileData)
	}
}

func addModerationAttachment(parts *[]string, value gjson.Result) {
	marker, ok := moderationAttachmentMarker(value)
	if ok {
		addModerationText(parts, marker)
		addModerationAttachmentSemanticSiblings(parts, value)
	}
}

func addModerationAttachmentSemanticSiblings(parts *[]string, value gjson.Result) {
	projected := projectModerationAttachmentSemanticSiblings(value)
	if len(projected) == 0 {
		return
	}
	raw, err := json.Marshal(projected)
	if err == nil {
		addModerationText(parts, string(raw))
	}
}

func projectModerationAttachmentSemanticSiblings(value gjson.Result) map[string]any {
	out := map[string]any{}
	value.ForEach(func(itemKey, itemValue gjson.Result) bool {
		name := itemKey.String()
		lower := normalizeModerationObjectKey(name)
		if isTransportSecretModerationKey(lower) {
			return true
		}
		if isAttachmentTransportModerationKey(lower) {
			if projected, ok := projectNestedModerationAttachmentSemantics(itemValue); ok {
				out[name] = projected
			}
			return true
		}
		projected, ok := projectModerationResult(itemValue, lower)
		if ok {
			out[name] = projected
		}
		return true
	})
	return out
}

func projectNestedModerationAttachmentSemantics(value gjson.Result) (any, bool) {
	if value.IsObject() {
		projected := projectModerationAttachmentSemanticSiblings(value)
		return projected, len(projected) > 0
	}
	if value.IsArray() {
		projected := make([]any, 0)
		value.ForEach(func(_, item gjson.Result) bool {
			if nested, ok := projectNestedModerationAttachmentSemantics(item); ok {
				projected = append(projected, nested)
			}
			return true
		})
		return projected, len(projected) > 0
	}
	return nil, false
}

func moderationAttachmentMarker(value gjson.Result) (string, bool) {
	return moderationAttachmentMarkerWithHint(value, "")
}

func moderationAttachmentMarkerWithHint(value gjson.Result, hint string) (string, bool) {
	typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
	if typ == "" {
		typ = strings.ToLower(strings.TrimSpace(value.Get("kind").String()))
	}
	kind := kindFromReferenceKey(hint)
	switch typ {
	case "image_url", "input_image", "image":
		kind = "image"
	case "document":
		kind = "document"
	case "file", "input_file":
		kind = "file"
	}
	if typ == "" && value.Get("image_url").Exists() {
		kind = "image"
	}
	mimeType := firstModerationValue(value, "mime", "mime_type", "mimeType", "media_type", "mediaType", "source.media_type", "source.mediaType", "file.mime_type", "file.mimeType")
	if kind == "file" {
		kind = kindFromMIME(mimeType, kind)
	}
	filename := firstModerationValue(value, "filename", "file.filename", "basename", "name")
	source := ""
	if sourceValue := value.Get("source"); sourceValue.Type == gjson.String {
		source = strings.ToLower(strings.TrimSpace(sourceValue.String()))
	}
	reference := ""
	if firstModerationValue(value, "file_id", "fileId", "file.file_id", "file.fileId") != "" {
		source = "file_id"
	}
	if source == "" {
		reference = firstModerationValue(value, "image_url.url", "imageUrl.url", "image_url", "imageUrl", "url", "file_url", "fileUrl", "file_uri", "fileUri", "source.url")
		if hasModerationDataURIPrefix(reference) {
			source = "inline"
			if mimeType == "" {
				mimeType = dataURIMIME(reference)
			}
		} else if reference != "" {
			source = "remote"
		}
	}
	if source == "" && firstModerationValue(value, "file_data", "fileData", "file.file_data", "file.fileData", "data", "base64", "source.data") != "" {
		source = "inline"
	}
	if source == "" {
		sourceType := strings.ToLower(firstModerationValue(value, "source.type"))
		switch sourceType {
		case "base64", "inline":
			source = "inline"
		case "url", "remote":
			source = "remote"
		}
	}
	if source == "" && typ == "" && strings.TrimSpace(hint) == "" {
		return "", false
	}
	extension := safeAttachmentExtension(firstModerationValue(value, "extension"))
	if extension == "" {
		extension = safeAttachmentExtension(filename)
	}
	if extension == "" {
		extension = safeModerationReferenceExtension(reference)
	}
	return buildModerationAttachmentMarker(kind, mimeType, source, extension), true
}

func firstModerationValue(value gjson.Result, paths ...string) string {
	for _, itemPath := range paths {
		if candidate := strings.TrimSpace(value.Get(itemPath).String()); candidate != "" {
			return candidate
		}
	}
	return ""
}

func firstModerationStringValue(value gjson.Result, paths ...string) string {
	for _, itemPath := range paths {
		candidate := value.Get(itemPath)
		if candidate.Type != gjson.String {
			continue
		}
		if text := strings.TrimSpace(candidate.String()); text != "" {
			return text
		}
	}
	return ""
}

func buildModerationAttachmentMarker(kind string, mimeType string, source string, extension string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "image" && kind != "document" {
		kind = "file"
	}
	fields := []string{"kind=" + kind}
	if mimeType = normalizeModerationMIME(mimeType); mimeType != "" {
		fields = append(fields, "mime="+mimeType)
	}
	if source == "inline" || source == "remote" || source == "file_id" || source == "upload" {
		fields = append(fields, "source="+source)
	}
	if extension != "" {
		fields = append(fields, "extension="+extension)
	}
	return "[attachment " + strings.Join(fields, " ") + "]"
}

func normalizeModerationMIME(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	if value == "image/jpg" {
		value = "image/jpeg"
	}
	if value == "" || !strings.Contains(value, "/") {
		return ""
	}
	for _, r := range value {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$&^_.+-/", r)) {
			return ""
		}
	}
	if !allowedModerationMIME[value] {
		return ""
	}
	return value
}

var allowedModerationMIME = map[string]bool{
	"application/json":              true,
	"application/msword":            true,
	"application/octet-stream":      true,
	"application/pdf":               true,
	"application/rtf":               true,
	"application/vnd.ms-excel":      true,
	"application/vnd.ms-powerpoint": true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/xml": true,
	"application/zip": true,
	"audio/mpeg":      true,
	"audio/ogg":       true,
	"audio/wav":       true,
	"image/gif":       true,
	"image/heic":      true,
	"image/heif":      true,
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"text/csv":        true,
	"text/html":       true,
	"text/markdown":   true,
	"text/plain":      true,
	"text/xml":        true,
	"video/mp4":       true,
	"video/mpeg":      true,
	"video/webm":      true,
}

func kindFromMIME(mimeType string, fallback string) string {
	if strings.HasPrefix(normalizeModerationMIME(mimeType), "image/") {
		return "image"
	}
	return fallback
}

func safeAttachmentExtension(reference string) string {
	reference = strings.TrimSpace(reference)
	if parsed, err := url.Parse(reference); err == nil && parsed.Path != "" {
		reference = parsed.Path
	} else if index := strings.IndexAny(reference, "?#"); index >= 0 {
		reference = reference[:index]
	}
	extension := strings.ToLower(path.Ext(reference))
	if len(extension) < 2 || len(extension) > 17 {
		return ""
	}
	for _, r := range extension[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return extension
}

func safeModerationReferenceExtension(reference string) string {
	if isRemoteModerationReference(reference) || isURLLikeModerationValue(reference) && !hasModerationReferenceScheme(reference) {
		return safeAttachmentExtension(reference)
	}
	return ""
}

func dataURIMIME(value string) string {
	mimeType, _ := parseModerationDataURI(value)
	return mimeType
}

func addSafeModerationText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if mimeType, ok := parseModerationDataURI(text); ok {
		addModerationText(parts, buildModerationAttachmentMarker(kindFromMIME(mimeType, "file"), mimeType, "inline", ""))
		return
	}
	if looksLikeModerationDataURIEnvelope(text) {
		addModerationText(parts, buildModerationAttachmentMarker("file", "", "inline", ""))
		return
	}
	addModerationText(parts, text)
}

func looksLikeLongBase64(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 128 || len(value)%4 != 0 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if !isBase64AlphabetByte(char) && char != '=' {
			return false
		}
	}
	return validBase64Syntax(value)
}

func projectModerationResult(value gjson.Result, key string) (any, bool) {
	if value.IsObject() {
		if marker, ok := moderationStructuredAttachmentMarker(value, key); ok {
			semantic := projectModerationAttachmentSemanticSiblings(value)
			if len(semantic) == 0 {
				return marker, true
			}
			semantic["attachment"] = marker
			return semantic, true
		}
		if marker, ok := moderationAttachmentReferenceMarker(value, key); ok {
			semantic := projectModerationAttachmentSemanticSiblings(value)
			if len(semantic) == 0 {
				return marker, true
			}
			semantic["attachment"] = marker
			return semantic, true
		}
		out := map[string]any{}
		value.ForEach(func(itemKey, itemValue gjson.Result) bool {
			name := itemKey.String()
			lower := normalizeModerationObjectKey(name)
			if isTransportSecretModerationKey(lower) {
				return true
			}
			if isBinaryLikeModerationKey(lower) {
				out["omitted"] = "[omitted]"
				return true
			}
			projectionKey := lower
			if isExplicitModerationURLKey(key) && isModerationSchemaValueKey(lower) {
				projectionKey = key
			}
			projected, ok := projectModerationResult(itemValue, projectionKey)
			if ok {
				out[name] = projected
			}
			return true
		})
		return out, len(out) > 0
	}
	if value.IsArray() {
		if isBinaryLikeModerationKey(key) {
			return "[omitted]", true
		}
		if looksLikeModerationByteArray(value, key) {
			return "[omitted]", true
		}
		out := make([]any, 0)
		value.ForEach(func(_, item gjson.Result) bool {
			if projected, ok := projectModerationResult(item, key); ok {
				out = append(out, projected)
			}
			return true
		})
		return out, len(out) > 0
	}
	if value.Type == gjson.String {
		text := strings.TrimSpace(value.String())
		normalizedKey := normalizeModerationObjectKey(key)
		if normalizedKey == "data" && looksLikeLongBase64(text) {
			return "[omitted]", true
		}
		switch normalizedKey {
		case "imageurl", "imageuri":
			marker := buildModerationAttachmentMarker("image", dataURIMIME(text), moderationReferenceSource(text), safeModerationReferenceExtension(text))
			return marker, true
		case "fileid":
			return buildModerationAttachmentMarker("file", "", "file_id", ""), true
		case "filedata", "datauri":
			if mimeType, ok := parseModerationDataURI(text); ok {
				return buildModerationAttachmentMarker(kindFromMIME(mimeType, "file"), mimeType, "inline", ""), true
			}
			return "[omitted]", true
		}
		if isExplicitModerationURLKey(normalizedKey) {
			if mimeType, ok := parseModerationDataURI(text); ok {
				return buildModerationAttachmentMarker(kindFromMIME(mimeType, kindFromReferenceKey(normalizedKey)), mimeType, "inline", ""), true
			}
			if looksLikeModerationDataURIEnvelope(text) {
				return buildModerationAttachmentMarker(kindFromReferenceKey(normalizedKey), "", "inline", ""), true
			}
			if isURLLikeModerationValue(text) || hasModerationReferenceScheme(text) {
				return buildModerationAttachmentMarker(kindFromReferenceKey(normalizedKey), "", "remote", safeModerationReferenceExtension(text)), true
			}
			return text, text != ""
		}
		if mimeType, ok := parseModerationDataURI(text); ok {
			return buildModerationAttachmentMarker(kindFromMIME(mimeType, "file"), mimeType, "inline", ""), true
		}
		if looksLikeModerationDataURIEnvelope(text) {
			return buildModerationAttachmentMarker(kindFromReferenceKey(normalizedKey), "", "inline", ""), true
		}
		if isBinaryLikeModerationKey(normalizedKey) {
			return "[omitted]", true
		}
		if isRemoteModerationReference(text) {
			return buildModerationAttachmentMarker(kindFromReferenceKey(normalizedKey), "", "remote", safeModerationReferenceExtension(text)), true
		}
		return text, text != ""
	}
	switch value.Type {
	case gjson.Number:
		return value.Num, true
	case gjson.True:
		return true, true
	case gjson.False:
		return false, true
	case gjson.Null:
		return nil, true
	default:
		return value.Value(), true
	}
}

func isModerationSchemaValueKey(key string) bool {
	switch normalizeModerationObjectKey(key) {
	case "default", "const", "example", "value":
		return true
	default:
		return false
	}
}

func isBinaryLikeModerationKey(key string) bool {
	key = normalizeModerationObjectKey(key)
	switch key {
	case "blob", "bytes", "binary", "base64", "encoded", "filebytes", "contentbytes":
		return true
	default:
		return strings.HasSuffix(key, "base64") || strings.HasSuffix(key, "blob") || strings.HasSuffix(key, "bytes") || strings.HasSuffix(key, "encoded")
	}
}

func isAttachmentTransportModerationKey(key string) bool {
	key = normalizeModerationObjectKey(key)
	if isBinaryLikeModerationKey(key) || isExplicitModerationURLKey(key) {
		return true
	}
	switch key {
	case "type", "kind", "name", "filename", "basename", "extension", "mime", "mimetype", "mediatype",
		"source", "rawsource", "file", "fileid", "filedata", "data", "datauri", "hash", "checksum":
		return true
	default:
		return false
	}
}

func moderationReferenceSource(value string) string {
	if _, ok := parseModerationDataURI(value); ok || looksLikeModerationDataURIEnvelope(value) {
		return "inline"
	}
	if isRemoteModerationReference(value) || hasModerationReferenceScheme(value) {
		return "remote"
	}
	return ""
}

func parseModerationDataURI(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !hasModerationDataURIPrefix(value) {
		return "", false
	}
	comma := strings.IndexByte(value, ',')
	if comma < len("data:") {
		return "", false
	}
	metadata := value[len("data:"):comma]
	if strings.IndexFunc(metadata, unicode.IsSpace) >= 0 {
		return "", false
	}
	segments := strings.Split(metadata, ";")
	mimeType := ""
	if len(segments) > 0 && segments[0] != "" {
		mimeType = normalizeModerationMIME(segments[0])
		if mimeType == "" {
			return "", false
		}
	}
	isBase64 := false
	for _, segment := range segments[1:] {
		if strings.EqualFold(segment, "base64") {
			isBase64 = true
		}
	}
	if isBase64 {
		payload := value[comma+1:]
		if !validBase64Syntax(payload) {
			return "", false
		}
	}
	return mimeType, true
}

func hasModerationDataURIPrefix(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= len("data:") && strings.EqualFold(value[:len("data:")], "data:")
}

func looksLikeModerationDataURIEnvelope(value string) bool {
	value = strings.TrimSpace(value)
	if !hasModerationDataURIPrefix(value) {
		return false
	}
	comma := strings.IndexByte(value, ',')
	return comma >= len("data:") && strings.IndexFunc(value[len("data:"):comma], unicode.IsSpace) < 0
}

func normalizeModerationObjectKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.NewReplacer("_", "", "-", "", ".", "").Replace(key)
}

func isTransportSecretModerationKey(key string) bool {
	key = normalizeModerationObjectKey(key)
	switch key {
	case "header", "headers", "cookie", "cookies", "authorization", "authheader", "setcookie",
		"apikey", "accesstoken", "token", "secret", "xapikey", "bearer",
		"password", "passwd", "pwd", "credential", "credentials", "sessionid", "privatekey", "proxyauthorization":
		return true
	default:
		return strings.HasSuffix(key, "apikey") || strings.HasSuffix(key, "token") || strings.HasSuffix(key, "secret")
	}
}

func moderationStructuredAttachmentMarker(value gjson.Result, key string) (string, bool) {
	key = normalizeModerationObjectKey(key)
	typ := normalizeModerationObjectKey(firstModerationStringValue(value, "type", "kind"))
	parentIsAttachment := key == "attachment" || key == "image" || key == "file" || key == "document" || key == "inputfile" || key == "inputimage"
	typeIsAttachment := typ == "image" || typ == "imageurl" || typ == "inputimage" || typ == "file" || typ == "inputfile" || typ == "document"
	hasAttachmentIdentity := firstModerationStringValue(value, "file_id", "fileId", "file.file_id", "file.fileId") != ""
	hasAttachmentSource := firstModerationStringValue(value, "image_url", "imageUrl", "url", "file_url", "fileUrl", "file_uri", "fileUri", "file_data", "fileData", "data", "base64", "source.data", "source.url") != ""
	hasAttachmentMetadata := firstModerationStringValue(value, "filename", "basename", "mime", "mime_type", "mimeType", "media_type", "mediaType") != ""
	highConfidenceAttachment := parentIsAttachment && (hasAttachmentIdentity || hasAttachmentSource || hasAttachmentMetadata) ||
		typeIsAttachment && (hasAttachmentIdentity || hasAttachmentSource || hasAttachmentMetadata) ||
		hasAttachmentMetadata && (hasAttachmentIdentity || hasAttachmentSource)
	if !highConfidenceAttachment {
		return "", false
	}
	return moderationAttachmentMarkerWithHint(value, key)
}

func moderationAttachmentReferenceMarker(value gjson.Result, key string) (string, bool) {
	key = normalizeModerationObjectKey(key)
	if !isExplicitModerationURLKey(key) {
		return "", false
	}
	reference := firstModerationValue(value, "url", "uri", "image_url", "imageUrl", "file_url", "fileUrl")
	if reference == "" {
		return "", false
	}
	if mimeType, ok := parseModerationDataURI(reference); ok {
		return buildModerationAttachmentMarker(kindFromReferenceKey(key), mimeType, "inline", ""), true
	}
	if looksLikeModerationDataURIEnvelope(reference) {
		return buildModerationAttachmentMarker(kindFromReferenceKey(key), "", "inline", ""), true
	}
	return buildModerationAttachmentMarker(kindFromReferenceKey(key), "", "remote", safeModerationReferenceExtension(reference)), true
}

func kindFromReferenceKey(key string) string {
	key = normalizeModerationObjectKey(key)
	if strings.Contains(key, "image") {
		return "image"
	}
	if strings.Contains(key, "document") {
		return "document"
	}
	return "file"
}

func isExplicitModerationURLKey(key string) bool {
	key = normalizeModerationObjectKey(key)
	if key == "curl" {
		return false
	}
	return key == "url" || key == "uri" || key == "link" || key == "href" ||
		strings.HasSuffix(key, "url") || strings.HasSuffix(key, "uri") ||
		strings.HasSuffix(key, "link") || strings.HasSuffix(key, "href")
}

func isURLLikeModerationValue(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" {
		return isAllowedModerationURLScheme(parsed.Scheme)
	}
	if strings.HasPrefix(value, "//") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return true
	}
	return strings.IndexFunc(value, unicode.IsSpace) < 0 && (strings.Contains(value, "/") || strings.ContainsAny(value, "?#"))
}

func hasModerationReferenceScheme(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme != ""
}

func looksLikeModerationByteArray(value gjson.Result, key string) bool {
	key = normalizeModerationObjectKey(key)
	if key != "body" && key != "payload" && key != "data" && key != "content" {
		return false
	}
	count := 0
	valid := true
	value.ForEach(func(_, item gjson.Result) bool {
		count++
		if item.Type != gjson.Number || item.Num < 0 || item.Num > 255 || item.Num != math.Trunc(item.Num) {
			valid = false
			return false
		}
		return true
	})
	return valid && count >= 32
}

func isRemoteModerationReference(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.IsAbs() && isAllowedModerationURLScheme(parsed.Scheme) && (parsed.Host != "" || parsed.Opaque != "")
}

func isAllowedModerationURLScheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "https", "file", "gs", "s3", "blob", "ftp", "ftps", "ipfs", "az":
		return true
	default:
		return false
	}
}

func validBase64Syntax(value string) bool {
	if value == "" {
		return true
	}
	if len(value)%4 == 1 {
		return false
	}
	paddingStart := len(value)
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '=' {
			if paddingStart == len(value) {
				paddingStart = index
			}
			continue
		}
		if paddingStart != len(value) || !isBase64AlphabetByte(char) {
			return false
		}
	}
	padding := len(value) - paddingStart
	if padding > 2 {
		return false
	}
	return padding == 0 || len(value)%4 == 0
}

func isBase64AlphabetByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' || value == '+' || value == '/'
}

func validateContentModerationEmbeddedProjectionBounds(protocol string, root gjson.Result, budget *contentModerationProjectionBudget) error {
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		return validateAnthropicEmbeddedModerationResults(root, budget)
	case ContentModerationProtocolOpenAIChat:
		return validateOpenAIChatEmbeddedModerationResults(root, budget)
	case ContentModerationProtocolOpenAIResponses:
		return validateResponsesEmbeddedModerationResults(root, budget)
	case ContentModerationProtocolGemini:
		return validateGeminiEmbeddedModerationResults(root, budget)
	case ContentModerationProtocolOpenAIImages:
		return nil
	default:
		if err := validateResponsesEmbeddedModerationResults(root, budget); err != nil {
			return err
		}
		if err := validateOpenAIChatEmbeddedModerationResults(root, budget); err != nil {
			return err
		}
		return validateGeminiEmbeddedModerationResults(root, budget)
	}
}

func validateOpenAIChatEmbeddedModerationResults(root gjson.Result, budget *contentModerationProjectionBudget) error {
	var validationErr error
	root.Get("messages").ForEach(func(_, message gjson.Result) bool {
		if validationErr = validateStringifiedModerationResult(message.Get("function_call.arguments"), budget); validationErr != nil {
			return false
		}
		message.Get("tool_calls").ForEach(func(_, call gjson.Result) bool {
			validationErr = validateStringifiedModerationResult(call.Get("function.arguments"), budget)
			return validationErr == nil
		})
		return validationErr == nil
	})
	return validationErr
}

func validateResponsesEmbeddedModerationResults(root gjson.Result, budget *contentModerationProjectionBudget) error {
	validateItem := func(item gjson.Result) error {
		typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
		switch typ {
		case "function_call", "custom_tool_call", "computer_call", "web_search_call":
			if err := validateStringifiedModerationResult(item.Get("arguments"), budget); err != nil {
				return err
			}
			return validateStringifiedModerationResult(item.Get("action"), budget)
		case "function_call_output", "custom_tool_call_output", "computer_call_output", "web_search_call_output":
			return validateStringifiedModerationResult(item.Get("output"), budget)
		default:
			return validateStringifiedModerationResult(item.Get("output"), budget)
		}
	}

	input := root.Get("input")
	if input.IsArray() {
		var validationErr error
		input.ForEach(func(_, item gjson.Result) bool {
			validationErr = validateItem(item)
			return validationErr == nil
		})
		return validationErr
	}
	if input.IsObject() {
		return validateItem(input)
	}
	return nil
}

func validateAnthropicEmbeddedModerationResults(root gjson.Result, budget *contentModerationProjectionBudget) error {
	var validationErr error
	root.Get("messages").ForEach(func(_, message gjson.Result) bool {
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			typ := strings.ToLower(strings.TrimSpace(block.Get("type").String()))
			if typ == "tool_use" || typ == "server_tool_use" {
				validationErr = validateStringifiedModerationResult(block.Get("input"), budget)
			}
			return validationErr == nil
		})
		return validationErr == nil
	})
	return validationErr
}

func validateGeminiEmbeddedModerationResults(root gjson.Result, budget *contentModerationProjectionBudget) error {
	var validationErr error
	root.Get("contents").ForEach(func(_, content gjson.Result) bool {
		content.Get("parts").ForEach(func(_, part gjson.Result) bool {
			for _, resultPath := range []string{
				"functionCall.args", "function_call.args", "functionResponse.response", "function_response.response",
			} {
				if validationErr = validateStringifiedModerationResult(part.Get(resultPath), budget); validationErr != nil {
					return false
				}
			}
			return true
		})
		return validationErr == nil
	})
	return validationErr
}

func validateStringifiedModerationResult(value gjson.Result, budget *contentModerationProjectionBudget) error {
	if value.Type != gjson.String {
		return nil
	}
	text := strings.TrimSpace(value.String())
	if !looksLikeStringifiedJSONValue(text) {
		return nil
	}

	trial := *budget
	if trial.nodes > 0 {
		trial.nodes--
	}
	if err := scanContentModerationRawProjectionBounds(&trial, text); err != nil {
		return err
	}
	if !gjson.Valid(text) {
		return nil
	}
	*budget = trial
	parsed := gjson.Parse(text)
	if parsed.Type == gjson.String {
		return validateStringifiedModerationResult(parsed, budget)
	}
	return nil
}

func looksLikeStringifiedJSONValue(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case '{':
		return value[len(value)-1] == '}'
	case '[':
		return value[len(value)-1] == ']'
	case '"':
		return value[len(value)-1] == '"'
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 't', 'f', 'n':
		return true
	default:
		return false
	}
}

type contentModerationProjectionBudget struct {
	nodes int
}

func scanContentModerationRawProjectionBounds[T ~string | ~[]byte](budget *contentModerationProjectionBudget, body T) error {
	type container struct {
		kind              byte
		expectsArrayValue bool
	}

	var stack [contentModerationProjectionMaxDepth]container
	depth := 0
	rootStarted := false

	addNode := func() error {
		budget.nodes++
		if budget.nodes > contentModerationProjectionMaxNodes {
			return fmt.Errorf("content moderation projection nodes exceed %d", contentModerationProjectionMaxNodes)
		}
		return nil
	}
	beginValue := func() error {
		if depth == 0 {
			if rootStarted {
				return nil
			}
			rootStarted = true
			return addNode()
		}
		parent := &stack[depth-1]
		if parent.kind != '[' || !parent.expectsArrayValue {
			return nil
		}
		parent.expectsArrayValue = false
		return addNode()
	}

	for index := 0; index < len(body); {
		char := body[index]
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			index++
			continue
		}

		switch char {
		case '"':
			if err := beginValue(); err != nil {
				return err
			}
			index++
			for index < len(body) {
				switch body[index] {
				case '\\':
					index += 2
				case '"':
					index++
					goto stringComplete
				default:
					index++
				}
			}
		stringComplete:
			continue
		case '{', '[':
			if err := beginValue(); err != nil {
				return err
			}
			depth++
			if depth > contentModerationProjectionMaxDepth {
				return fmt.Errorf("content moderation projection depth exceeds %d", contentModerationProjectionMaxDepth)
			}
			stack[depth-1] = container{kind: char, expectsArrayValue: char == '['}
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth > 0 && stack[depth-1].kind == '{' {
				if err := addNode(); err != nil {
					return err
				}
			}
		case ',':
			if depth > 0 && stack[depth-1].kind == '[' {
				stack[depth-1].expectsArrayValue = true
			}
		default:
			if err := beginValue(); err != nil {
				return err
			}
		}
		index++
	}
	return nil
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
