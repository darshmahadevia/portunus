package gateway_test

import (
	"github.com/darshmahadevia/portunus/internal/gateway"
	"strings"
	"testing"
)

func TestConfigEnvironmentAndValidation(t *testing.T) {
	t.Setenv("PORTUNUS_TEST_CLIENT", "private-key")
	t.Setenv("PORTUNUS_TEST_PROVIDER", "provider-private-key")
	body := `{"providers":[{"id":"configured","kind":"openaicompat","url":"https://example.invalid/v1/chat/completions","model":"configured-model","apiKeyEnv":"PORTUNUS_TEST_PROVIDER","inputMicrosPerMillion":1,"outputMicrosPerMillion":2}],"routes":{"general":["configured"]},"clients":[{"id":"private","keyEnv":"PORTUNUS_TEST_CLIENT","rpm":1,"burst":1,"budgetMicros":100}]}`
	c, err := gateway.LoadConfig(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	e, err := gateway.New(c)
	if err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().Clients[0].ID != "private" {
		t.Fatal("configured client not loaded")
	}
	t.Setenv("PORTUNUS_TEST_PROVIDER", "")
	if _, err = gateway.New(c); err == nil {
		t.Fatal("missing provider environment variable accepted")
	}
	for _, bad := range []string{`{"extra":1}`, `{} {}`, `{`, `{}` + strings.Repeat(" ", 1<<20)} {
		if _, err = gateway.LoadConfig(strings.NewReader(bad)); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
