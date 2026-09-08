package loadtest

import (
	"encoding/json"
	"math/rand"
	"testing"
)

const sampleOnSearch = `{
  "context": {"domain":"ONDC:RET11","country":"IND","city":"std:080","core_version":"1.2.0"},
  "message": {"catalog": {"bpp/providers":[{
    "id":"P1",
    "locations":[{"id":"L1","gps":"12.90,77.60","address":{"area_code":"560103"}}],
    "fulfillments":[{"id":"F1","type":"Delivery"}],
    "tags":[{"code":"serviceability","list":[{"code":"location","value":"L1"},{"code":"type","value":"10"},{"code":"val","value":"5"},{"code":"unit","value":"km"}]}],
    "items":[
      {"id":"I1","price":{"value":"100.00","currency":"INR"},"location_id":"L1","fulfillment_id":"F1"},
      {"id":"I2","price":{"value":"50.00","currency":"INR"},"location_id":"L1","fulfillment_id":"F1"}
    ]
  }]}}
}`

func genCtx() GenContext {
	return GenContext{Domain: "ONDC:RET11", Country: "IND", City: "std:080", CoreVersion: "1.2.0",
		BAPID: "bap.example.org", BAPURI: "https://bap.example.org", BPPID: "bpp.example.in", BPPURI: "https://bpp.example.in/v2"}
}

func fixed(n int) *RangeInt { return &RangeInt{Fixed: &n} }
func rng() RandSource       { return rand.New(rand.NewSource(1)) }

func mustGen(t *testing.T, action Action, rc RandomizeConfig) map[string]any {
	t.Helper()
	cat, err := ParseCatalog([]byte(sampleOnSearch))
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	g := NewGenerator(cat, genCtx(), rc, rng())
	b, _, _, err := g.Generate(action)
	if err != nil {
		t.Fatalf("Generate(%s): %v", action, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestParseCatalogReadsServiceability(t *testing.T) {
	cat, err := ParseCatalog([]byte(sampleOnSearch))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Providers) != 1 || len(cat.Providers[0].Items) != 2 {
		t.Fatalf("unexpected providers/items: %+v", cat.Providers)
	}
	svc := cat.Providers[0].Serviceability
	if len(svc) != 1 || svc[0].Type != 10 || svc[0].Radius != 5 || svc[0].LocationID != "L1" {
		t.Fatalf("serviceability not parsed: %+v", svc)
	}
	if cat.Providers[0].Locations[0].GPS != "12.90,77.60" {
		t.Fatalf("location gps not parsed: %+v", cat.Providers[0].Locations[0])
	}
}

func TestGenerateSelectUsesStoreDeliveryAndItems(t *testing.T) {
	env := mustGen(t, ActionSelect, RandomizeConfig{ItemCount: fixed(2), Quantity: fixed(2), Delivery: "store"})
	order := env["message"].(map[string]any)["order"].(map[string]any)
	items := order["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	fulf := order["fulfillments"].([]any)[0].(map[string]any)
	end := fulf["end"].(map[string]any)["location"].(map[string]any)
	if end["gps"] != "12.90,77.60" {
		t.Fatalf("delivery should default to store gps, got %v", end["gps"])
	}
	if end["address"].(map[string]any)["area_code"] != "560103" {
		t.Fatalf("area_code wrong: %v", end["address"])
	}
	ctx := env["context"].(map[string]any)
	if ctx["action"] != "select" || ctx["bpp_id"] != "bpp.example.in" {
		t.Fatalf("bad context: %v", ctx)
	}
}

func TestGenerateInitSynthesizesQuoteFromCatalogPrices(t *testing.T) {
	// Both items, qty 2 => (100+50)*2 = 300.00
	env := mustGen(t, ActionInit, RandomizeConfig{ItemCount: fixed(2), Quantity: fixed(2), Delivery: "store"})
	order := env["message"].(map[string]any)["order"].(map[string]any)
	quote, ok := order["quote"].(map[string]any)
	if !ok {
		t.Fatal("init missing quote")
	}
	price := quote["price"].(map[string]any)
	if price["value"] != "300.00" {
		t.Fatalf("quote total = %v, want 300.00", price["value"])
	}
	if _, ok := order["billing"].(map[string]any); !ok {
		t.Fatal("init missing billing")
	}
	if len(quote["breakup"].([]any)) != 2 {
		t.Fatalf("expected 2 breakup lines")
	}
}

func TestGenerateConfirmAddsPaymentAndState(t *testing.T) {
	env := mustGen(t, ActionConfirm, RandomizeConfig{ItemCount: fixed(1), Quantity: fixed(1), Delivery: "store"})
	order := env["message"].(map[string]any)["order"].(map[string]any)
	if order["state"] != "Created" {
		t.Fatalf("confirm state = %v", order["state"])
	}
	pay, ok := order["payment"].(map[string]any)
	if !ok || pay["status"] != "PAID" {
		t.Fatalf("confirm payment missing/invalid: %v", order["payment"])
	}
}

func TestDeliveryRandomStaysWithinRadius(t *testing.T) {
	loc := Location{ID: "L1", GPS: "12.90,77.60", AreaCode: "560103"}
	svcs := []Serviceability{{LocationID: "L1", Type: 10, Radius: 5}}
	r := rng()
	for i := 0; i < 200; i++ {
		gps, _ := deliveryPoint(loc, svcs, true, r)
		lat, lng, ok := parseGPS(gps)
		if !ok {
			t.Fatalf("bad gps %q", gps)
		}
		// ~ each degree lat = 111km; 5km radius ⇒ well under 0.1 deg.
		if lat < 12.85 || lat > 12.95 || lng < 77.55 || lng > 77.65 {
			t.Fatalf("point outside ~5km box: %v,%v", lat, lng)
		}
	}
}

func TestGenerateSkipsCustomizationItems(t *testing.T) {
	// Catalog with one base item and one customization item (":" id).
	os := `{"context":{"domain":"ONDC:RET11"},"message":{"catalog":{"bpp/providers":[{
      "id":"P1","locations":[{"id":"L1","gps":"12.9,77.6","address":{"area_code":"560001"}}],
      "fulfillments":[{"id":"F1","type":"Delivery"}],
      "items":[
        {"id":"BASE","price":{"value":"100"},"location_id":"L1"},
        {"id":"BASE:CUST","price":{"value":"10"},"location_id":"L1"}
      ]}]}}}`
	cat, err := ParseCatalog([]byte(os))
	if err != nil {
		t.Fatal(err)
	}
	if !cat.Providers[0].Items[1].IsCustomization || cat.Providers[0].Items[0].IsCustomization {
		t.Fatalf("customization classification wrong: %+v", cat.Providers[0].Items)
	}
	// Over many generations, only BASE must ever be selected.
	g := NewGenerator(cat, genCtx(), RandomizeConfig{ItemCount: fixed(1), Quantity: fixed(1)}, rng())
	for i := 0; i < 100; i++ {
		b, _, _, err := g.Generate(ActionSelect)
		if err != nil {
			t.Fatal(err)
		}
		var env map[string]any
		_ = json.Unmarshal(b, &env)
		items := env["message"].(map[string]any)["order"].(map[string]any)["items"].([]any)
		for _, it := range items {
			if id := it.(map[string]any)["id"].(string); id != "BASE" {
				t.Fatalf("selected non-base item %q", id)
			}
		}
	}
}

func TestPointInPolygon(t *testing.T) {
	square := [][]LngLat{{{77.0, 12.0}, {78.0, 12.0}, {78.0, 13.0}, {77.0, 13.0}, {77.0, 12.0}}}
	if !pointInRing(77.5, 12.5, square[0]) {
		t.Fatal("center should be inside")
	}
	if pointInRing(76.0, 12.5, square[0]) {
		t.Fatal("west point should be outside")
	}
	if _, _, ok := randomPointInPolygon(square, rng()); !ok {
		t.Fatal("should sample a point in polygon")
	}
}
