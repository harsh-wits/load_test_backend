package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"

	"seller_app_load_tester/internal/config"
)

// LoadSearchPayload reads the base search template from fixtures and patches
// only the BAP/env-driven context fields, preserving all other fields
// (transaction_id, message_id, timestamp, ttl, etc.) for later patching.
func LoadSearchPayload(cfg *config.Config) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join("fixtures", "search", "search.json"))
	if err != nil {
		return nil, err
	}

	var full map[string]any
	if err := json.Unmarshal(raw, &full); err != nil {
		// If the template shape changes unexpectedly, fall back to raw bytes
		// rather than breaking at runtime.
		return raw, nil
	}

	ctx, _ := full["context"].(map[string]any)
	if ctx == nil {
		ctx = map[string]any{}
		full["context"] = ctx
	}

	if cfg != nil {
		if cfg.BAPID != "" {
			ctx["bap_id"] = cfg.BAPID
		}
		if cfg.BAPURI != "" {
			ctx["bap_uri"] = cfg.BAPURI
		}
		if cfg.CoreVersion != "" {
			ctx["core_version"] = cfg.CoreVersion
		}
		if cfg.CountryCode != "" {
			ctx["country"] = cfg.CountryCode
		}
		if cfg.CityCode != "" {
			ctx["city"] = cfg.CityCode
		}
		if cfg.Domain != "" {
			ctx["domain"] = cfg.Domain
		}
	}

	return json.Marshal(full)
}

