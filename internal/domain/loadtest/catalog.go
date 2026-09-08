package loadtest

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Catalog is the parsed, load-tester view of an on_search body.
type Catalog struct {
	Providers []Provider
}

type Provider struct {
	ID             string
	Locations      []Location
	Items          []Item
	Fulfillments   []Fulfillment
	Serviceability []Serviceability
}

type Location struct {
	ID       string
	GPS      string // "lat,lng"
	AreaCode string
}

type Item struct {
	ID              string
	PriceValue      string
	Currency        string
	LocationID      string
	FulfillmentID   string
	ParentItemID    string
	Tags            json.RawMessage // preserved verbatim (keeps customization structure)
	IsCustomization bool            // true for "base:cust" ids / type=customization items
}

type Fulfillment struct {
	ID   string
	Type string
}

// Serviceability captures one ONDC SSN serviceability rule for a location.
//   type 10 = hyperlocal circle (Radius km around store)
//   type 11 = intercity (pincode list)
//   type 12 = PAN-India
//   type 13 = polygon (GeoJSON)
type Serviceability struct {
	LocationID string
	Type       int
	Radius     float64    // km, type 10
	Pincodes   []string   // type 11
	Polygon    [][]LngLat // type 13 (rings; ring[0] is outer)
}

type LngLat struct{ Lng, Lat float64 }

// ParseCatalog extracts providers/items/locations/serviceability from an on_search body.
func ParseCatalog(onSearch []byte) (*Catalog, error) {
	var env struct {
		Message struct {
			Catalog struct {
				Providers []struct {
					ID        string `json:"id"`
					Locations []struct {
						ID      string `json:"id"`
						GPS     string `json:"gps"`
						Address struct {
							AreaCode string `json:"area_code"`
						} `json:"address"`
					} `json:"locations"`
					Fulfillments []struct {
						ID   string `json:"id"`
						Type string `json:"type"`
					} `json:"fulfillments"`
					Items []struct {
						ID            string          `json:"id"`
						LocationID    string          `json:"location_id"`
						FulfillmentID string          `json:"fulfillment_id"`
						ParentItemID  string          `json:"parent_item_id"`
						Tags          json.RawMessage `json:"tags"`
						Price         struct {
							Value    string `json:"value"`
							Currency string `json:"currency"`
						} `json:"price"`
					} `json:"items"`
					Tags json.RawMessage `json:"tags"`
				} `json:"bpp/providers"`
			} `json:"catalog"`
		} `json:"message"`
	}
	if err := json.Unmarshal(onSearch, &env); err != nil {
		return nil, fmt.Errorf("parse on_search: %w", err)
	}
	cat := &Catalog{}
	for _, p := range env.Message.Catalog.Providers {
		prov := Provider{ID: p.ID}
		for _, l := range p.Locations {
			prov.Locations = append(prov.Locations, Location{ID: l.ID, GPS: l.GPS, AreaCode: l.Address.AreaCode})
		}
		for _, f := range p.Fulfillments {
			prov.Fulfillments = append(prov.Fulfillments, Fulfillment{ID: f.ID, Type: f.Type})
		}
		for _, it := range p.Items {
			prov.Items = append(prov.Items, Item{
				ID: it.ID, PriceValue: it.Price.Value, Currency: it.Price.Currency,
				LocationID: it.LocationID, FulfillmentID: it.FulfillmentID,
				ParentItemID: it.ParentItemID, Tags: it.Tags,
				IsCustomization: strings.Contains(it.ID, ":") || itemTaggedCustomization(it.Tags),
			})
		}
		prov.Serviceability = parseServiceability(p.Tags)
		cat.Providers = append(cat.Providers, prov)
	}
	if len(cat.Providers) == 0 {
		return nil, fmt.Errorf("on_search has no bpp/providers")
	}
	return cat, nil
}

// parseServiceability reads ONDC SSN serviceability entries from provider tags.
func parseServiceability(raw json.RawMessage) []Serviceability {
	if len(raw) == 0 {
		return nil
	}
	var tags []struct {
		Code string `json:"code"`
		List []struct {
			Code  string `json:"code"`
			Value string `json:"value"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &tags); err != nil {
		return nil
	}
	var out []Serviceability
	for _, t := range tags {
		if t.Code != "serviceability" {
			continue
		}
		s := Serviceability{}
		var typ, val, unit string
		for _, kv := range t.List {
			switch kv.Code {
			case "location":
				s.LocationID = kv.Value
			case "type":
				typ = kv.Value
			case "val":
				val = kv.Value
			case "unit":
				unit = kv.Value
			}
		}
		s.Type, _ = strconv.Atoi(strings.TrimSpace(typ))
		switch s.Type {
		case 10:
			r, _ := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if strings.EqualFold(unit, "km") || unit == "" {
				s.Radius = r
			} else {
				s.Radius = r // treat as km best-effort
			}
		case 11:
			for _, p := range strings.Split(val, ",") {
				if p = strings.TrimSpace(p); p != "" {
					s.Pincodes = append(s.Pincodes, p)
				}
			}
		case 13:
			s.Polygon = parseGeoJSONPolygon(val)
		}
		out = append(out, s)
	}
	return out
}

// parseGeoJSONPolygon accepts a GeoJSON Polygon (or a raw coordinates array)
// string and returns its rings as [lng,lat] points.
func parseGeoJSONPolygon(val string) [][]LngLat {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	// Try full GeoJSON geometry first.
	var geo struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	var coordsRaw json.RawMessage
	if err := json.Unmarshal([]byte(val), &geo); err == nil && len(geo.Coordinates) > 0 {
		coordsRaw = geo.Coordinates
	} else {
		coordsRaw = json.RawMessage(val)
	}
	// Polygon coordinates: [][][2]float64 (rings of [lng,lat]).
	var rings [][][]float64
	if err := json.Unmarshal(coordsRaw, &rings); err != nil {
		return nil
	}
	out := make([][]LngLat, 0, len(rings))
	for _, ring := range rings {
		pts := make([]LngLat, 0, len(ring))
		for _, c := range ring {
			if len(c) >= 2 {
				pts = append(pts, LngLat{Lng: c[0], Lat: c[1]})
			}
		}
		if len(pts) >= 3 {
			out = append(out, pts)
		}
	}
	return out
}

// itemTaggedCustomization reports whether an item's tags declare type=customization.
func itemTaggedCustomization(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var tags []struct {
		Code string `json:"code"`
		List []struct {
			Code  string `json:"code"`
			Value string `json:"value"`
		} `json:"list"`
	}
	if json.Unmarshal(raw, &tags) != nil {
		return false
	}
	for _, t := range tags {
		if t.Code != "type" {
			continue
		}
		for _, kv := range t.List {
			if kv.Code == "type" && kv.Value == "customization" {
				return true
			}
		}
	}
	return false
}

func parseGPS(s string) (lat, lng float64, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	la, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	ln, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return la, ln, true
}

func formatGPS(lat, lng float64) string {
	return fmt.Sprintf("%.6f,%.6f", lat, lng)
}

// deliveryPoint chooses a serviceable delivery gps/area_code for a store location.
// Default is the store's own coordinates (trivially in-zone). When randomize is
// set, it jitters within the location's serviceable area (circle or polygon).
func deliveryPoint(loc Location, svcs []Serviceability, randomize bool, rng RandSource) (gps, areaCode string) {
	gps, areaCode = loc.GPS, loc.AreaCode
	baseLat, baseLng, ok := parseGPS(loc.GPS)
	if !randomize || !ok {
		return gps, areaCode
	}
	svc := serviceabilityFor(loc.ID, svcs)
	if svc == nil {
		return gps, areaCode
	}
	switch svc.Type {
	case 10:
		if svc.Radius > 0 {
			lat, lng := randomPointInRadius(baseLat, baseLng, svc.Radius, rng)
			return formatGPS(lat, lng), areaCode
		}
	case 13:
		if lat, lng, ok := randomPointInPolygon(svc.Polygon, rng); ok {
			return formatGPS(lat, lng), areaCode
		}
	}
	return gps, areaCode
}

func serviceabilityFor(locID string, svcs []Serviceability) *Serviceability {
	var any *Serviceability
	for i := range svcs {
		if svcs[i].LocationID == locID {
			return &svcs[i]
		}
		if any == nil {
			any = &svcs[i]
		}
	}
	return any
}

// randomPointInRadius returns a uniformly-random point within radiusKm of (lat,lng).
func randomPointInRadius(lat, lng, radiusKm float64, rng RandSource) (float64, float64) {
	u := rng.Float64()
	r := radiusKm * math.Sqrt(u)
	theta := 2 * math.Pi * rng.Float64()
	dLat := (r * math.Cos(theta)) / 111.0
	cosLat := math.Cos(lat * math.Pi / 180.0)
	if cosLat == 0 {
		cosLat = 1e-6
	}
	dLng := (r * math.Sin(theta)) / (111.0 * cosLat)
	return lat + dLat, lng + dLng
}

// randomPointInPolygon rejection-samples a point inside the outer ring.
func randomPointInPolygon(rings [][]LngLat, rng RandSource) (lat, lng float64, ok bool) {
	if len(rings) == 0 || len(rings[0]) < 3 {
		return 0, 0, false
	}
	outer := rings[0]
	minLng, minLat := math.Inf(1), math.Inf(1)
	maxLng, maxLat := math.Inf(-1), math.Inf(-1)
	for _, p := range outer {
		minLng, maxLng = math.Min(minLng, p.Lng), math.Max(maxLng, p.Lng)
		minLat, maxLat = math.Min(minLat, p.Lat), math.Max(maxLat, p.Lat)
	}
	for attempt := 0; attempt < 40; attempt++ {
		la := minLat + rng.Float64()*(maxLat-minLat)
		ln := minLng + rng.Float64()*(maxLng-minLng)
		if pointInRing(ln, la, outer) {
			return la, ln, true
		}
	}
	// Fallback: centroid of the outer ring.
	var sumLat, sumLng float64
	for _, p := range outer {
		sumLat += p.Lat
		sumLng += p.Lng
	}
	n := float64(len(outer))
	return sumLat / n, sumLng / n, true
}

// pointInRing uses ray casting; ring points are [lng,lat].
func pointInRing(lng, lat float64, ring []LngLat) bool {
	inside := false
	n := len(ring)
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := ring[i].Lng, ring[i].Lat
		xj, yj := ring[j].Lng, ring[j].Lat
		if ((yi > lat) != (yj > lat)) &&
			(lng < (xj-xi)*(lat-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}
