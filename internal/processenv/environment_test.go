package processenv

import "testing"

func TestWithoutSensitiveKeys(t *testing.T) {
	env := New([]string{
		"PATH=/usr/bin",
		"DEEPSEEK_API_KEY=sk-deepseek",
		"ATRIX_CLI_LOCAL_TOKEN=cli-token",
		"ATRIX_S3_SECRET_ACCESS_KEY=secret",
		"HTTP_PROXY=http://127.0.0.1:7890",
		"ATRIX_GO_BINARY=/usr/local/go/bin/go",
	})
	filtered := env.WithoutSensitiveKeys()
	for _, name := range []string{"DEEPSEEK_API_KEY", "ATRIX_CLI_LOCAL_TOKEN", "ATRIX_S3_SECRET_ACCESS_KEY"} {
		if _, ok := filtered.Lookup(name); ok {
			t.Fatalf("%s should have been removed from child process environment", name)
		}
	}
	for name, want := range map[string]string{
		"PATH":            "/usr/bin",
		"HTTP_PROXY":      "http://127.0.0.1:7890",
		"ATRIX_GO_BINARY": "/usr/local/go/bin/go",
	} {
		if got, ok := filtered.Lookup(name); !ok || got != want {
			t.Fatalf("%s = %q, %v; want %q, true", name, got, ok, want)
		}
	}
}

func TestSensitiveEnvName(t *testing.T) {
	cases := map[string]bool{
		"DEEPSEEK_API_KEY":           true,
		"ATRIX_CLI_LOCAL_TOKEN":      true,
		"ATRIX_S3_SECRET_ACCESS_KEY": true,
		"MY_PASSWORD":                true,
		"SSH_PRIVATE_KEY":            true,
		"PATH":                       false,
		"HTTP_PROXY":                 false,
		"ATRIX_GO_BINARY":            false,
	}
	for name, want := range cases {
		if got := SensitiveEnvName(name); got != want {
			t.Fatalf("SensitiveEnvName(%q) = %v, want %v", name, got, want)
		}
	}
}
