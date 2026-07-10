package service

import (
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	maxWorkingDirScanBytes = 256 * 1024
	maxWorkingDirLen       = 1024
	maxPromptTextDepth     = 8
)

var (
	claudeWorkingDirRe = regexp.MustCompile(`(?i)working directory:[ \t]*([^\r\n]+)`)
	codexCwdRe         = regexp.MustCompile(`(?i)<cwd>\s*([^<]+?)\s*</cwd>`)
)

// ExtractWorkingDirectory best-effort extracts a cwd without affecting request handling.
func ExtractWorkingDirectory(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}

	var text strings.Builder
	root := gjson.ParseBytes(body)
	collectWorkingDirPromptText(root.Get("system"), &text, 0)
	collectWorkingDirPromptText(root.Get("instructions"), &text, 0)
	collectWorkingDirPromptText(root.Get("input"), &text, 0)
	root.Get("messages").ForEach(func(_, message gjson.Result) bool {
		collectWorkingDirPromptText(message.Get("content"), &text, 0)
		return text.Len() < maxWorkingDirScanBytes
	})

	joined := text.String()
	if match := codexCwdRe.FindStringSubmatch(joined); match != nil {
		if dir := sanitizeWorkingDir(match[1]); dir != "" {
			return dir
		}
	}
	if match := claudeWorkingDirRe.FindStringSubmatch(joined); match != nil {
		return sanitizeWorkingDir(match[1])
	}
	return ""
}

func collectWorkingDirPromptText(result gjson.Result, dst *strings.Builder, depth int) {
	if depth > maxPromptTextDepth || dst.Len() >= maxWorkingDirScanBytes || !result.Exists() {
		return
	}
	switch {
	case result.IsArray():
		result.ForEach(func(_, item gjson.Result) bool {
			collectWorkingDirPromptText(item, dst, depth+1)
			return dst.Len() < maxWorkingDirScanBytes
		})
	case result.IsObject():
		if value := result.Get("text"); value.Exists() && value.Type == gjson.String {
			writeWorkingDirPromptText(dst, value.String())
		}
		if content := result.Get("content"); content.Exists() {
			collectWorkingDirPromptText(content, dst, depth+1)
		}
	case result.Type == gjson.String:
		writeWorkingDirPromptText(dst, result.String())
	}
}

func writeWorkingDirPromptText(dst *strings.Builder, value string) {
	remaining := maxWorkingDirScanBytes - dst.Len()
	if value == "" || remaining <= 0 {
		return
	}
	if len(value) > remaining {
		value = value[:remaining]
	}
	dst.WriteString(value)
	dst.WriteByte('\n')
}

func sanitizeWorkingDir(raw string) string {
	dir := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "`\"'"))
	if dir == "" || !looksLikeAbsolutePath(dir) {
		return ""
	}
	if len(dir) > maxWorkingDirLen {
		dir = dir[:maxWorkingDirLen]
	}
	return dir
}

func looksLikeAbsolutePath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") {
		return true
	}
	if len(value) < 3 || value[1] != ':' || (value[2] != '\\' && value[2] != '/') {
		return false
	}
	return (value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')
}
