package loadtest

import "encoding/json"

// Action is an ONDC BPP endpoint the load tester can target.
type Action string

const (
	ActionSearch  Action = "search"
	ActionSelect  Action = "select"
	ActionInit    Action = "init"
	ActionConfirm Action = "confirm"
)

func (a Action) valid() bool {
	switch a {
	case ActionSearch, ActionSelect, ActionInit, ActionConfirm:
		return true
	}
	return false
}

// RangeInt expresses either a fixed value or an inclusive [Min,Max] range.
type RangeInt struct {
	Fixed *int `json:"fixed,omitempty"`
	Min   *int `json:"min,omitempty"`
	Max   *int `json:"max,omitempty"`
}

// pick returns a value honoring Fixed, then [Min,Max], else def.
func (r *RangeInt) pick(rng RandSource, def int) int {
	if r == nil {
		return def
	}
	if r.Fixed != nil {
		return *r.Fixed
	}
	lo, hi := def, def
	if r.Min != nil {
		lo = *r.Min
	}
	if r.Max != nil {
		hi = *r.Max
	}
	if hi < lo {
		hi = lo
	}
	if hi == lo {
		return lo
	}
	return lo + rng.Intn(hi-lo+1)
}

// RandomizeConfig controls how payloads vary across the generated load.
type RandomizeConfig struct {
	ItemCount *RangeInt `json:"item_count,omitempty"` // items per order (default 1..3)
	Quantity  *RangeInt `json:"quantity,omitempty"`   // per-item count (default fixed 1)
	Provider  string    `json:"provider,omitempty"`   // "first" | "random"
	Location  string    `json:"location,omitempty"`   // "first" | "random"
	Delivery  string    `json:"delivery,omitempty"`   // "store" | "random" (in serviceable zone)
	Seed      *int64    `json:"seed,omitempty"`       // reproducible runs
}

// StartRequest is the POST /loadtest body.
type StartRequest struct {
	Action      Action          `json:"action"`
	BPPID       string          `json:"bpp_id"`
	BPPURI      string          `json:"bpp_uri"`
	Count       int             `json:"count"`
	QPS         int             `json:"qps"`
	DurationSec int             `json:"duration_sec"`
	Sign        bool            `json:"sign"`
	OnSearch    json.RawMessage `json:"on_search"`
	Randomize   RandomizeConfig `json:"randomize"`
}

// LedgerRow is one dispatched request, for seller-log reconciliation.
type LedgerRow struct {
	TransactionID string `json:"transaction_id"`
	MessageID     string `json:"message_id"`
	Action        string `json:"action"`
	SentAtUnixMs  int64  `json:"sent_at_unix_ms"`
	SentAtUTC     string `json:"sent_at_utc"`
	HTTPStatus    int    `json:"http_status"`
	AckStatus     string `json:"ack_status"` // ACK | NACK | none
	LatencyMs     int64  `json:"client_latency_ms"`
	Error         string `json:"error,omitempty"`
	ResponseBody  string `json:"response_body,omitempty"` // captured for non-ACK responses
}

// RandSource is the minimal RNG surface the generator needs.
type RandSource interface {
	Intn(n int) int
	Float64() float64
	Perm(n int) []int
}
