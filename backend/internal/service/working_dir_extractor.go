package service

import (
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// 工作目录提取：从客户端请求体里识别发起请求时的工作目录(cwd)，用于按目录归集花费、
// 发现员工是否将 AI 用于非公司项目。两类客户端会自动把 cwd 注入到 prompt 中：
//   - Claude Code：系统提示里的 env 块，形如 "Working directory: /path"
//     或较新版本的 "Primary working directory: /path"。
//   - Codex CLI：Responses 请求 input 里的 "<environment_context><cwd>/path</cwd>..."。
//
// 该提取是 best-effort：解析失败、格式变化、客户端未注入都只是返回空，绝不影响请求转发与计费。
// 运行在用量记录的后台路径（响应完成后），不影响首字延迟。

const (
	// maxWorkingDirScanBytes 限制参与扫描的文本量，避免超大 prompt 拖慢后台记录。
	// env / environment_context 块都出现在系统提示/指令的前部，足够覆盖。
	maxWorkingDirScanBytes = 256 * 1024
	// maxWorkingDirLen 与 usage_logs.working_directory 列长度(1024)对齐。
	maxWorkingDirLen = 1024
	// maxPromptTextDepth 限制递归层数，防御异常嵌套结构。
	maxPromptTextDepth = 8
)

var (
	// 匹配 "Working directory: /xxx" 与 "Primary working directory: /xxx"（大小写不敏感），
	// 捕获到行尾（路径可能含空格，如 "/Users/a/My Project"）。
	claudeWorkingDirRe = regexp.MustCompile(`(?i)working directory:[ \t]*([^\r\n]+)`)
	// 匹配 Codex 的 <cwd>/xxx</cwd>，取首个。
	codexCwdRe = regexp.MustCompile(`(?i)<cwd>\s*([^<]+?)\s*</cwd>`)
)

// ExtractWorkingDirectory 从请求体中尽力解析客户端工作目录；无法识别时返回 ""。
func ExtractWorkingDirectory(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}

	var sb strings.Builder
	root := gjson.ParseBytes(body)
	// Anthropic(Claude Code): system 字段（string 或 [{type,text}] 数组）。
	collectPromptText(root.Get("system"), &sb, 0)
	// Responses(Codex): instructions(string) + input(string 或 item 数组)。
	collectPromptText(root.Get("instructions"), &sb, 0)
	collectPromptText(root.Get("input"), &sb, 0)
	// Chat Completions: messages[].content（string 或 content part 数组）。
	root.Get("messages").ForEach(func(_, m gjson.Result) bool {
		collectPromptText(m.Get("content"), &sb, 0)
		return sb.Len() < maxWorkingDirScanBytes
	})

	text := sb.String()
	if text == "" {
		return ""
	}

	// Codex 的 <cwd> 优先（结构化、最精确），其次 Claude Code 的 env 行。
	if m := codexCwdRe.FindStringSubmatch(text); m != nil {
		if dir := sanitizeWorkingDir(m[1]); dir != "" {
			return dir
		}
	}
	if m := claudeWorkingDirRe.FindStringSubmatch(text); m != nil {
		if dir := sanitizeWorkingDir(m[1]); dir != "" {
			return dir
		}
	}
	return ""
}

// collectPromptText 递归收集 gjson 结果中的文本（string 值、对象的 .text、对象/数组的 .content），
// 累加到 sb，受总量与递归深度限制。
func collectPromptText(r gjson.Result, sb *strings.Builder, depth int) {
	if depth > maxPromptTextDepth || sb.Len() >= maxWorkingDirScanBytes || !r.Exists() {
		return
	}
	switch {
	case r.IsArray():
		r.ForEach(func(_, e gjson.Result) bool {
			collectPromptText(e, sb, depth+1)
			return sb.Len() < maxWorkingDirScanBytes
		})
	case r.IsObject():
		if t := r.Get("text"); t.Exists() && t.Type == gjson.String {
			writeCapped(sb, t.String())
		}
		if c := r.Get("content"); c.Exists() {
			collectPromptText(c, sb, depth+1)
		}
	case r.Type == gjson.String:
		writeCapped(sb, r.String())
	}
}

func writeCapped(sb *strings.Builder, s string) {
	if s == "" {
		return
	}
	if remaining := maxWorkingDirScanBytes - sb.Len(); remaining > 0 {
		if len(s) > remaining {
			s = s[:remaining]
		}
		sb.WriteString(s)
		sb.WriteByte('\n')
	}
}

// sanitizeWorkingDir 清洗并校验候选路径：去空白、截断超长、要求形似绝对路径，
// 以降低把普通正文里 "working directory:" 误识别为路径的风险。
func sanitizeWorkingDir(raw string) string {
	dir := strings.TrimSpace(raw)
	// 去掉可能的包裹引号/反引号。
	dir = strings.Trim(dir, "`\"'")
	dir = strings.TrimSpace(dir)
	if dir == "" || !looksLikeAbsolutePath(dir) {
		return ""
	}
	if len(dir) > maxWorkingDirLen {
		dir = dir[:maxWorkingDirLen]
	}
	return dir
}

// looksLikeAbsolutePath 判断字符串是否形似绝对路径（POSIX、~ 家目录或 Windows 盘符）。
func looksLikeAbsolutePath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") {
		return true
	}
	// Windows: C:\ 或 C:/
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		c := s[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}
