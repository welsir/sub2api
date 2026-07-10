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
			name: "claude code env block",
			body: `{"model":"claude","system":[{"type":"text","text":"<env>\nWorking directory: /Users/alice/work/company-repo\nPlatform: darwin\n</env>"}]}`,
			want: "/Users/alice/work/company-repo",
		},
		{
			name: "claude code primary working directory",
			body: `{"system":"Primary working directory: /Users/bob/personal/side project\nIs a git repository: true"}`,
			want: "/Users/bob/personal/side project",
		},
		{
			name: "codex environment context",
			body: `{"model":"gpt-5","input":[{"type":"message","content":[{"type":"input_text","text":"<environment_context><cwd>/home/carol/projects/api</cwd></environment_context>"}]}]}`,
			want: "/home/carol/projects/api",
		},
		{
			name: "windows path",
			body: `{"system":"Working directory: C:\\Users\\erin\\repo"}`,
			want: `C:\Users\erin\repo`,
		},
		{
			name: "structured cwd wins",
			body: `{"instructions":"<cwd>/codex/cwd</cwd>","system":"Working directory: /claude/cwd"}`,
			want: "/codex/cwd",
		},
		{
			name: "prose is rejected",
			body: `{"system":"The working directory: should be set before running."}`,
			want: "",
		},
		{name: "invalid json", body: `{bad`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractWorkingDirectory([]byte(tt.body)); got != tt.want {
				t.Fatalf("ExtractWorkingDirectory() = %q, want %q", got, tt.want)
			}
		})
	}
}
