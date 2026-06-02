package integlife

import "testing"

func TestLoadConfigUsesEnvTokenBeforeTokenFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveAPIToken("file-token"); err != nil {
		t.Fatalf("SaveAPIToken() error = %v", err)
	}
	t.Setenv("INTEGLIFE_API_TOKEN", "env-token")

	cfg := LoadConfig()
	if cfg.APIToken != "env-token" {
		t.Fatalf("APIToken = %q, want env-token", cfg.APIToken)
	}
}
