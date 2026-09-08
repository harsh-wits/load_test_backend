package loadtest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const ondcTimestampLayout = "2006-01-02T15:04:05.000Z"

// GenContext holds the ONDC context fields the generator stamps onto every payload.
type GenContext struct {
	Domain      string
	Country     string
	City        string
	CoreVersion string
	BAPID       string
	BAPURI      string
	BPPID       string
	BPPURI      string
}

// Generator produces ONDC payloads from a parsed catalog. Not safe for
// concurrent use (the RNG is single-threaded); call Generate sequentially.
type Generator struct {
	cat *Catalog
	ctx GenContext
	rnd RandomizeConfig
	rng RandSource
}

func NewGenerator(cat *Catalog, ctx GenContext, rnd RandomizeConfig, rng RandSource) *Generator {
	return &Generator{cat: cat, ctx: ctx, rnd: rnd, rng: rng}
}

type itemSel struct {
	id, parentItemID, fulfillmentID string
	priceValue, currency            string
	qty                             int
	tags                            json.RawMessage
}

// Generate returns a marshaled payload plus its transaction and message ids.
func (g *Generator) Generate(action Action) (payload []byte, txnID, msgID string, err error) {
	txnID = uuid.NewString()
	msgID = uuid.NewString()
	ctx := g.buildContext(string(action), txnID, msgID)

	var message map[string]any
	switch action {
	case ActionSearch:
		message = g.buildSearchMessage()
	case ActionSelect, ActionInit, ActionConfirm:
		order, err2 := g.buildOrder(action)
		if err2 != nil {
			return nil, "", "", err2
		}
		message = map[string]any{"order": order}
	default:
		return nil, "", "", fmt.Errorf("unsupported action %q", action)
	}

	env := map[string]any{"context": ctx, "message": message}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal %s: %w", action, err)
	}
	return b, txnID, msgID, nil
}

func (g *Generator) buildContext(action, txnID, msgID string) map[string]any {
	return map[string]any{
		"domain":       g.ctx.Domain,
		"country":      g.ctx.Country,
		"city":         g.ctx.City,
		"action":       action,
		"core_version": g.ctx.CoreVersion,
		"bap_id":       g.ctx.BAPID,
		"bap_uri":      g.ctx.BAPURI,
		"bpp_id":       g.ctx.BPPID,
		"bpp_uri":      g.ctx.BPPURI,
		"transaction_id": txnID,
		"message_id":     msgID,
		"timestamp":      time.Now().UTC().Format(ondcTimestampLayout),
		"ttl":            "PT30S",
	}
}

func (g *Generator) chooseProvider() *Provider {
	if len(g.cat.Providers) == 0 {
		return nil
	}
	idx := 0
	if g.rnd.Provider == "random" && len(g.cat.Providers) > 1 {
		idx = g.rng.Intn(len(g.cat.Providers))
	}
	return &g.cat.Providers[idx]
}

func (g *Generator) chooseLocation(p *Provider) Location {
	if len(p.Locations) == 0 {
		return Location{ID: p.ID}
	}
	idx := 0
	if g.rnd.Location == "random" && len(p.Locations) > 1 {
		idx = g.rng.Intn(len(p.Locations))
	}
	return p.Locations[idx]
}

func (g *Generator) chooseItems(p *Provider, locID string) []itemSel {
	if len(p.Items) == 0 {
		return nil
	}
	// Order only base items. Customization items (id "base:cust" or
	// type=customization) are not standalone-orderable — selecting one without
	// its parent produces an invalid select that sellers reject or hang on.
	pool := make([]int, 0, len(p.Items))
	for i := range p.Items {
		if !p.Items[i].IsCustomization {
			pool = append(pool, i)
		}
	}
	if len(pool) == 0 {
		for i := range p.Items {
			pool = append(pool, i)
		}
	}

	maxN := 3
	if len(pool) < maxN {
		maxN = len(pool)
	}
	n := g.rnd.ItemCount.pick(g.rng, 1+g.rng.Intn(maxN))
	if n < 1 {
		n = 1
	}
	if n > len(pool) {
		n = len(pool)
	}
	sel := g.rng.Perm(len(pool))[:n]

	fulfillmentID := ""
	if len(p.Fulfillments) > 0 {
		fulfillmentID = p.Fulfillments[0].ID
	}
	out := make([]itemSel, 0, n)
	for _, k := range sel {
		it := p.Items[pool[k]]
		fid := it.FulfillmentID
		if fid == "" {
			fid = fulfillmentID
		}
		out = append(out, itemSel{
			id: it.ID, parentItemID: it.ParentItemID, fulfillmentID: fid,
			priceValue: it.PriceValue, currency: it.Currency,
			qty:  g.rnd.Quantity.pick(g.rng, 1),
			tags: it.Tags,
		})
	}
	return out
}

func (g *Generator) buildOrder(action Action) (map[string]any, error) {
	prov := g.chooseProvider()
	if prov == nil {
		return nil, fmt.Errorf("catalog has no provider")
	}
	loc := g.chooseLocation(prov)
	items := g.chooseItems(prov, loc.ID)
	if len(items) == 0 {
		return nil, fmt.Errorf("catalog provider %q has no items", prov.ID)
	}

	fulfillmentID := "F1"
	fulfillmentType := "Delivery"
	if len(prov.Fulfillments) > 0 {
		if prov.Fulfillments[0].ID != "" {
			fulfillmentID = prov.Fulfillments[0].ID
		}
		if prov.Fulfillments[0].Type != "" {
			fulfillmentType = prov.Fulfillments[0].Type
		}
	}

	orderItems := make([]any, 0, len(items))
	for _, it := range items {
		im := map[string]any{
			"id":          it.id,
			"quantity":    map[string]any{"count": it.qty},
			"location_id": loc.ID,
		}
		if it.fulfillmentID != "" {
			im["fulfillment_id"] = it.fulfillmentID
		}
		if it.parentItemID != "" {
			im["parent_item_id"] = it.parentItemID
		}
		if len(it.tags) > 0 {
			var t any
			if json.Unmarshal(it.tags, &t) == nil && t != nil {
				im["tags"] = t
			}
		}
		if _, ok := im["tags"]; !ok {
			im["tags"] = []any{map[string]any{"code": "type", "list": []any{map[string]any{"code": "type", "value": "item"}}}}
		}
		orderItems = append(orderItems, im)
	}

	randomizeDelivery := g.rnd.Delivery == "random"
	gps, areaCode := deliveryPoint(loc, prov.Serviceability, randomizeDelivery, g.rng)
	if gps == "" {
		gps = "12.9716,77.5946"
	}
	if areaCode == "" {
		areaCode = "560001"
	}

	fulfillment := map[string]any{
		"id":    fulfillmentID,
		"type":  fulfillmentType,
		"state": map[string]any{"descriptor": map[string]any{"code": "Serviceable"}},
		"end": map[string]any{
			"location": map[string]any{
				"gps":     gps,
				"address": map[string]any{"area_code": areaCode},
			},
		},
	}

	order := map[string]any{
		"provider":     map[string]any{"id": prov.ID, "locations": []any{map[string]any{"id": loc.ID}}},
		"items":        orderItems,
		"fulfillments": []any{fulfillment},
	}

	if action == ActionInit || action == ActionConfirm {
		g.enrichBuyerFields(order, items, gps, areaCode)
	}
	if action == ActionConfirm {
		g.enrichPayment(order, items)
	}
	return order, nil
}

// enrichBuyerFields adds billing, a synthesized quote (from catalog prices),
// and fulfillment contact/person — the parts init/confirm need beyond select.
func (g *Generator) enrichBuyerFields(order map[string]any, items []itemSel, gps, areaCode string) {
	now := time.Now().UTC().Format(ondcTimestampLayout)
	order["billing"] = map[string]any{
		"name": "Test Buyer", "phone": "9999999999", "email": "test@example.com",
		"address": map[string]any{
			"name": "Test Buyer", "building": "Test Building", "locality": "MG Road",
			"city": "Bengaluru", "state": "KA", "country": "IND", "area_code": areaCode,
		},
		"created_at": now, "updated_at": now,
	}

	// Fulfillment end contact + person + address enrich.
	if fulf, ok := order["fulfillments"].([]any); ok {
		for _, f := range fulf {
			fm, _ := f.(map[string]any)
			if fm == nil {
				continue
			}
			end, _ := fm["end"].(map[string]any)
			if end == nil {
				end = map[string]any{}
				fm["end"] = end
			}
			end["contact"] = map[string]any{"phone": "9999999999", "email": "test@example.com"}
			end["person"] = map[string]any{"name": "Test Buyer"}
			loc, _ := end["location"].(map[string]any)
			if loc == nil {
				loc = map[string]any{"gps": gps}
				end["location"] = loc
			}
			addr, _ := loc["address"].(map[string]any)
			if addr == nil {
				addr = map[string]any{"area_code": areaCode}
			}
			addr["name"] = "Test Buyer"
			addr["building"] = "Test Building"
			addr["locality"] = "MG Road"
			addr["city"] = "Bengaluru"
			addr["state"] = "KA"
			addr["country"] = "IND"
			loc["address"] = addr
		}
	}

	// Synthesize quote from catalog item prices.
	currency := "INR"
	var total float64
	breakup := make([]any, 0, len(items))
	for _, it := range items {
		price, _ := strconv.ParseFloat(it.priceValue, 64)
		line := price * float64(it.qty)
		total += line
		if it.currency != "" {
			currency = it.currency
		}
		breakup = append(breakup, map[string]any{
			"@ondc/org/item_id":       it.id,
			"@ondc/org/item_quantity": map[string]any{"count": it.qty},
			"title":                   it.id,
			"@ondc/org/title_type":    "item",
			"price":                   map[string]any{"currency": currency, "value": formatAmount(line)},
		})
	}
	order["quote"] = map[string]any{
		"price":   map[string]any{"currency": currency, "value": formatAmount(total)},
		"breakup": breakup,
		"ttl":     "P1D",
	}
}

func (g *Generator) enrichPayment(order map[string]any, items []itemSel) {
	now := time.Now().UTC().Format(ondcTimestampLayout)
	if _, ok := order["id"].(string); !ok {
		order["id"] = uuid.NewString()
	}
	order["state"] = "Created"
	order["created_at"] = now
	order["updated_at"] = now

	amount, currency := "0", "INR"
	if quote, ok := order["quote"].(map[string]any); ok {
		if price, ok := quote["price"].(map[string]any); ok {
			if v, ok := price["value"].(string); ok && v != "" {
				amount = v
			}
			if c, ok := price["currency"].(string); ok && c != "" {
				currency = c
			}
		}
	}
	order["payment"] = map[string]any{
		"uri":       "https://ondc.transaction.com/payment",
		"tl_method": "http/get",
		"params": map[string]any{
			"amount": amount, "currency": currency, "transaction_id": uuid.NewString(),
		},
		"status":       "PAID",
		"type":         "ON-ORDER",
		"collected_by": "BAP",
		"@ondc/org/buyer_app_finder_fee_type":   "percent",
		"@ondc/org/buyer_app_finder_fee_amount": "3",
		"@ondc/org/settlement_basis":            "delivery",
		"@ondc/org/settlement_window":           "P1D",
	}
}

// buildSearchMessage produces a minimal browse intent using the first
// serviceable delivery point if a catalog is present.
func (g *Generator) buildSearchMessage() map[string]any {
	gps, areaCode := "12.9716,77.5946", "560001"
	if len(g.cat.Providers) > 0 && len(g.cat.Providers[0].Locations) > 0 {
		p := &g.cat.Providers[0]
		gps, areaCode = deliveryPoint(p.Locations[0], p.Serviceability, g.rnd.Delivery == "random", g.rng)
		if gps == "" {
			gps = "12.9716,77.5946"
		}
		if areaCode == "" {
			areaCode = "560001"
		}
	}
	return map[string]any{
		"intent": map[string]any{
			"fulfillment": map[string]any{
				"type": "Delivery",
				"end":  map[string]any{"location": map[string]any{"gps": gps, "address": map[string]any{"area_code": areaCode}}},
			},
			"payment": map[string]any{
				"@ondc/org/buyer_app_finder_fee_type":   "percent",
				"@ondc/org/buyer_app_finder_fee_amount": "3",
			},
		},
	}
}

func formatAmount(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
