package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"seller_app_load_tester/internal/config"
	"seller_app_load_tester/internal/domain/session"
)

// LoadSearchPayload reads the base search template from fixtures and patches
// only the BAP/env-driven context fields, preserving all other fields
// (transaction_id, message_id, timestamp, ttl, etc.) for later patching.
func LoadSearchPayload(cfg *config.Config, sess *session.Session) ([]byte, error) {
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
		if cfg.CountryCode != "" {
			ctx["country"] = cfg.CountryCode
		}
		if cfg.CityCode != "" {
			ctx["city"] = cfg.CityCode
		}
	}

	if sess != nil {
		if sess.CoreVersion != "" {
			ctx["core_version"] = sess.CoreVersion
		}
		if sess.Domain != "" {
			ctx["domain"] = sess.Domain
		}
	}

	// RET13-specific search intent tags for bap_terms.
	if sess != nil && sess.Domain == "ONDC:RET13" {
		msg, _ := full["message"].(map[string]any)
		if msg == nil {
			msg = map[string]any{}
			full["message"] = msg
		}
		intent, _ := msg["intent"].(map[string]any)
		if intent == nil {
			intent = map[string]any{}
			msg["intent"] = intent
		}

		tags, _ := intent["tags"].([]any)
		const targetCode = "bap_terms"
		const staticTermsURL = "https://github.com/ONDC-Official/NP-Static-Terms/buyerNP_BNP/1.0/tc.pdf"

		var bapTerms map[string]any
		for _, t := range tags {
			if tm, _ := t.(map[string]any); tm != nil {
				if c, _ := tm["code"].(string); c == targetCode {
					bapTerms = tm
					break
				}
			}
		}
		if bapTerms == nil {
			bapTerms = map[string]any{"code": targetCode}
			tags = append(tags, bapTerms)
		}

		list, _ := bapTerms["list"].([]any)
		upsert := func(code, value string) {
			for i, e := range list {
				if em, _ := e.(map[string]any); em != nil {
					if c, _ := em["code"].(string); c == code {
						em["value"] = value
						list[i] = em
						return
					}
				}
			}
			list = append(list, map[string]any{"code": code, "value": value})
		}

		// static_terms: just needs to be present; reuse the same URL.
		upsert("static_terms", staticTermsURL)
		// static_terms_new: must be exactly the allowed URL.
		upsert("static_terms_new", staticTermsURL)
		// effective_date: must match strict timestamp regex with trailing Z.
		effective := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		upsert("effective_date", effective)

		bapTerms["list"] = list
		intent["tags"] = tags
	}

	return json.Marshal(full)
}

