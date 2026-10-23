package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
	"github.com/darshmahadevia/portunus/internal/provider"
)

type ProviderConfig struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	URL           string               `json:"url"`
	Model         string               `json:"model"`
	APIKeyEnv     string               `json:"apiKeyEnv"`
	InputPrice    int64                `json:"inputMicrosPerMillion"`
	OutputPrice   int64                `json:"outputMicrosPerMillion"`
	MaxConcurrent int                  `json:"maxConcurrent"`
	Fake          *fakeprovider.Server `json:"-"`
	Adapter       provider.Adapter     `json:"-"`
}
type ClientConfig struct {
	ID           string `json:"id"`
	Key          string `json:"key,omitempty"`
	KeyEnv       string `json:"keyEnv,omitempty"`
	RPM          int    `json:"rpm"`
	Burst        int    `json:"burst"`
	BudgetMicros int64  `json:"budgetMicros"`
}
type CacheConfig struct {
	MaxEntries int           `json:"maxEntries"`
	MaxBytes   int           `json:"maxBytes"`
	TTL        time.Duration `json:"ttl"`
}
type Config struct {
	Providers         []ProviderConfig    `json:"providers"`
	Clients           []ClientConfig      `json:"clients"`
	Routes            map[string][]string `json:"routes"`
	Revision          string              `json:"revision"`
	Timeout           time.Duration       `json:"timeout"`
	FirstChunkTimeout time.Duration       `json:"firstChunkTimeout"`
	WriteTimeout      time.Duration       `json:"writeTimeout"`
	MaxConcurrent     int                 `json:"maxConcurrent"`
	MaxAttempts       int                 `json:"maxAttempts"`
	CircuitThreshold  int                 `json:"circuitThreshold"`
	CircuitCooldown   time.Duration       `json:"circuitCooldown"`
	Cache             CacheConfig         `json:"cache"`
	MaxRecords        int                 `json:"maxRecords"`
	Dashboard         http.Handler        `json:"-"`
	Now               func() time.Time    `json:"-"`
}

func DefaultConfig() Config {
	return Config{Clients: []ClientConfig{{ID: "demo", Key: "demo-key", RPM: 600, Burst: 100, BudgetMicros: 10000000}, {ID: "limited", Key: "limited-key", RPM: 2, Burst: 2, BudgetMicros: 1000}}, Routes: map[string][]string{"general": {"primary", "backup"}}, Revision: "1", Timeout: 30 * time.Second, FirstChunkTimeout: 300 * time.Millisecond, MaxConcurrent: 64, MaxAttempts: 3, CircuitThreshold: 3, CircuitCooldown: 2 * time.Second, Cache: CacheConfig{128, 4 << 20, 60 * time.Second}, MaxRecords: 1000}
}
func LoadConfig(r io.Reader) (Config, error) {
	c := DefaultConfig()
	body, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return Config{}, err
	}
	if len(body) > 1<<20 {
		return Config{}, fmt.Errorf("config exceeds 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return Config{}, e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return Config{}, fmt.Errorf("expected one config object")
	}
	return c, nil
}
func prepare(c Config) (Config, error) {
	d := DefaultConfig()
	c.Providers = append([]ProviderConfig(nil), c.Providers...)
	if c.Clients != nil {
		c.Clients = append([]ClientConfig{}, c.Clients...)
	}
	if c.Routes != nil {
		routes := make(map[string][]string, len(c.Routes))
		for alias, targets := range c.Routes {
			routes[alias] = append([]string(nil), targets...)
		}
		c.Routes = routes
	}
	if c.Timeout == 0 {
		c.Timeout = d.Timeout
	}
	if c.FirstChunkTimeout == 0 {
		c.FirstChunkTimeout = d.FirstChunkTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = c.Timeout + 5*time.Second
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = d.MaxConcurrent
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = d.MaxAttempts
	}
	if c.CircuitThreshold == 0 {
		c.CircuitThreshold = d.CircuitThreshold
	}
	if c.CircuitCooldown == 0 {
		c.CircuitCooldown = d.CircuitCooldown
	}
	if c.MaxRecords == 0 {
		c.MaxRecords = d.MaxRecords
	}
	if c.Cache == (CacheConfig{}) {
		c.Cache = d.Cache
	}
	if c.Clients == nil {
		c.Clients = d.Clients
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Revision == "" {
		c.Revision = "1"
	}
	if c.Timeout <= 0 || c.Timeout > 24*time.Hour || c.FirstChunkTimeout <= 0 || c.FirstChunkTimeout > 24*time.Hour || c.WriteTimeout <= 0 || c.WriteTimeout > 24*time.Hour || c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.CircuitThreshold < 1 || c.CircuitCooldown < 0 || c.CircuitCooldown > 24*time.Hour || c.MaxRecords < 1 || c.MaxRecords > 100000 || c.Cache.MaxEntries < 1 || c.Cache.MaxEntries > 100000 || c.Cache.MaxBytes < 1 || c.Cache.MaxBytes > 1<<30 || c.Cache.TTL <= 0 {
		return c, fmt.Errorf("invalid gateway bounds")
	}
	if len(c.Clients) == 0 {
		return c, fmt.Errorf("at least one configured client key required")
	}
	if len(c.Providers) == 0 {
		return c, fmt.Errorf("at least one provider required")
	}
	seen := map[string]bool{}
	for i, p := range c.Providers {
		if p.ID == "" || seen[p.ID] || p.Model == "" || (p.Kind != "fake" && p.Kind != "openaicompat") || p.InputPrice < 0 || p.OutputPrice < 0 || p.InputPrice > 1000000000 || p.OutputPrice > 1000000000 || p.MaxConcurrent < 0 || p.MaxConcurrent > 65536 {
			return c, fmt.Errorf("invalid provider configuration")
		}
		seen[p.ID] = true
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.MaxConcurrent == 0 {
			p.MaxConcurrent = c.MaxConcurrent
		}
		if p.Adapter == nil {
			if e := provider.ValidateURL(p.URL); e != nil {
				return c, e
			}
			key := ""
			if p.APIKeyEnv != "" {
				key = os.Getenv(p.APIKeyEnv)
				if key == "" {
					return c, fmt.Errorf("provider credential environment variable is unset")
				}
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.DisableCompression = true
			transport.MaxResponseHeaderBytes = 64 << 10
			transport.MaxIdleConnsPerHost = p.MaxConcurrent
			p.Adapter = &provider.HTTP{URL: p.URL, Kind: p.Kind, APIKey: key, Client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
		}
		c.Providers[i] = p
	}
	if c.Routes == nil {
		c.Routes = map[string][]string{"general": {c.Providers[0].ID}}
	}
	for alias, targets := range c.Routes {
		if alias == "" || len(targets) == 0 {
			return c, fmt.Errorf("empty route")
		}
		used := map[string]bool{}
		for _, id := range targets {
			if !seen[id] || used[id] {
				return c, fmt.Errorf("invalid route target")
			}
			used[id] = true
		}
	}
	keys, ids := map[string]bool{}, map[string]bool{}
	for i, cl := range c.Clients {
		if cl.KeyEnv != "" {
			cl.Key = os.Getenv(cl.KeyEnv)
		}
		if cl.ID == "" || cl.Key == "" || keys[cl.Key] || ids[cl.ID] || cl.RPM < 1 || cl.Burst < 1 || cl.RPM > 1000000000 || cl.Burst > 1000000000 || cl.BudgetMicros < 0 {
			return c, fmt.Errorf("invalid client configuration")
		}
		keys[cl.Key] = true
		ids[cl.ID] = true
		c.Clients[i] = cl
	}
	return c, nil
}
