//go:build unit

package service

import "testing"

func TestExtractWorkingDirectory(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "claude code env block (system array)",
			body: `{"model":"claude","system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},{"type":"text","text":"<env>\nWorking directory: /Users/alice/work/company-repo\nIs directory a git repo: Yes\nPlatform: darwin\n</env>"}]}`,
			want: "/Users/alice/work/company-repo",
		},
		{
			name: "claude code newer 'Primary working directory' (system string)",
			body: `{"model":"claude","system":"# Environment\nYou have been invoked in the following environment:\n - Primary working directory: /Users/bob/personal/sideproject\n - Is a git repository: true\n"}`,
			want: "/Users/bob/personal/sideproject",
		},
		{
			name: "codex environment_context cwd (responses input array)",
			body: `{"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/home/carol/projects/api</cwd>\n  <shell>bash</shell>\n</environment_context>"}]}]}`,
			want: "/home/carol/projects/api",
		},
		{
			name: "codex cwd in instructions string",
			body: `{"model":"gpt-5","instructions":"<environment_context><cwd>/srv/build dir/with space</cwd></environment_context>"}`,
			want: "/srv/build dir/with space",
		},
		{
			name: "path with spaces captured to end of line",
			body: `{"system":"Working directory: /Users/dan/My Project\nIs directory a git repo: No"}`,
			want: "/Users/dan/My Project",
		},
		{
			name: "windows absolute path",
			body: `{"system":"Working directory: C:\\Users\\erin\\repo"}`,
			want: `C:\Users\erin\repo`,
		},
		{
			name: "no working directory present",
			body: `{"model":"claude","system":"You are a helpful assistant.","messages":[{"role":"user","content":"hello"}]}`,
			want: "",
		},
		{
			name: "non-path value is rejected (guard against prose match)",
			body: `{"system":"The working directory: should be set before running."}`,
			want: "",
		},
		{
			name: "empty body",
			body: ``,
			want: "",
		},
		{
			name: "invalid json",
			body: `{not json`,
			want: "",
		},
		{
			name: "cwd preferred over env line when both present",
			body: `{"instructions":"<environment_context><cwd>/codex/cwd</cwd></environment_context>","system":"Working directory: /claude/cwd"}`,
			want: "/codex/cwd",
		},
		{
			name: "chat completions message content (string)",
			body: `{"messages":[{"role":"system","content":"Working directory: /opt/app"}]}`,
			want: "/opt/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWorkingDirectory([]byte(tt.body))
			if got != tt.want {
				t.Errorf("ExtractWorkingDirectory() = %q, want %q", got, tt.want)
			}
		})
	}
}
