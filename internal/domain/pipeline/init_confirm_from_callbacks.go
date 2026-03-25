package pipeline

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"seller_app_load_tester/internal/shared/runlog"
)

func BuildInitBatchFromOnSelect(store runlog.Store, runID string, transactionIDs []string) ([]InitPayload, error) {
	raw, err := buildBatchFromCallbacks(store, runID, "on_select", "init", transactionIDs)
	if err != nil {
		return nil, err
	}
	out := make([]InitPayload, len(raw))
	for i := range raw {
		out[i] = InitPayload(raw[i])
	}
	return out, nil
}

func BuildConfirmBatchFromOnInit(store runlog.Store, runID string, transactionIDs []string) ([]ConfirmPayload, error) {
	raw, err := buildBatchFromCallbacks(store, runID, "on_init", "confirm", transactionIDs)
	if err != nil {
		return nil, err
	}
	out := make([]ConfirmPayload, len(raw))
	for i := range raw {
		out[i] = ConfirmPayload(raw[i])
	}
	return out, nil
}

func buildBatchFromCallbacks(
	store runlog.Store,
	runID, sourceAction, targetAction string,
	transactionIDs []string,
) ([][]byte, error) {
	if runID == "" {
		return nil, fmt.Errorf("runID required")
	}
	if len(transactionIDs) == 0 {
		return nil, nil
	}

	out := make([][]byte, 0, len(transactionIDs))

	for _, txnID := range transactionIDs {
		if txnID == "" {
			continue
		}
		body, err := store.Get(runID, "pipeline_b", sourceAction, txnID)
		if err != nil || body == nil {
			continue
		}

		var full map[string]any
		if err := json.Unmarshal(body, &full); err != nil {
			continue
		}

		if msg, _ := full["message"].(map[string]any); msg != nil {
			if ack, _ := msg["ack"].(map[string]any); ack != nil {
				if status, _ := ack["status"].(string); status != "" && status != "ACK" {
					continue
				}
			}
		}

		ctxMap, _ := full["context"].(map[string]any)
		if ctxMap == nil {
			continue
		}

		ctxMap["action"] = targetAction
		ctxMap["message_id"] = uuid.NewString()
		ctxMap["timestamp"] = time.Now().UTC().Format(ondcTimestampLayout)
		if _, ok := ctxMap["ttl"]; !ok {
			ctxMap["ttl"] = "PT30S"
		}

		if targetAction == "init" {
			enrichInit(full, store, runID, txnID)
		} else if targetAction == "confirm" {
			enrichConfirm(full, ctxMap)
		}

		payload, err := json.Marshal(full)
		if err != nil {
			continue
		}
		out = append(out, payload)
	}

	return out, nil
}

func enrichInit(full map[string]any, store runlog.Store, runID, txnID string) {
	ctx, _ := full["context"].(map[string]any)
	domain, _ := ctx["domain"].(string)

	msg, _ := full["message"].(map[string]any)
	if msg == nil {
		msg = map[string]any{}
		full["message"] = msg
	}
	order, _ := msg["order"].(map[string]any)
	if order == nil {
		order = map[string]any{}
		msg["order"] = order
	}

	itemCounts := map[string]int{}
	if quote, _ := order["quote"].(map[string]any); quote != nil {
		if breakup, _ := quote["breakup"].([]any); breakup != nil {
			for _, b := range breakup {
				bm, _ := b.(map[string]any)
				if bm == nil {
					continue
				}
				id, _ := bm["@ondc/org/item_id"].(string)
				if id == "" {
					continue
				}
				qtyMap, _ := bm["@ondc/org/item_quantity"].(map[string]any)
				if qtyMap == nil {
					continue
				}
				switch v := qtyMap["count"].(type) {
				case float64:
					if v > 0 {
						itemCounts[id] = int(v)
					}
				case int:
					if v > 0 {
						itemCounts[id] = v
					}
				}
			}
		}
	}

	if items, _ := order["items"].([]any); items != nil {
		for idx, it := range items {
			im, _ := it.(map[string]any)
			if im == nil {
				continue
			}
			id, _ := im["id"].(string)
			if id == "" {
				continue
			}
			qtyMap, _ := im["quantity"].(map[string]any)
			if qtyMap == nil {
				qtyMap = map[string]any{}
			}
			if _, ok := qtyMap["count"]; !ok {
				if c, ok := itemCounts[id]; ok && c > 0 {
					qtyMap["count"] = c
				} else {
					qtyMap["count"] = 1
				}
			}
			im["quantity"] = qtyMap
			items[idx] = im
		}
		order["items"] = items
	}

	billing, hasBilling := order["billing"].(map[string]any)
	if !hasBilling || billing == nil {
		now := time.Now().UTC().Format(ondcTimestampLayout)
		billing = map[string]any{
			"name": "Test Buyer", "phone": "9999999999", "email": "test@example.com",
			"address": map[string]any{
				"name": "Test Buyer", "building": "Test Building", "locality": "MG Road",
				"city": "Bengaluru", "state": "KA", "country": "IND", "area_code": "560001",
			},
			"created_at": now, "updated_at": now,
		}
		order["billing"] = billing
	}

	var billingAddr map[string]any
	if ba, _ := billing["address"].(map[string]any); ba != nil {
		billingAddr = ba
	}

	var selectGPS, selectAreaCode string
	if selectData, err := store.Get(runID, "pipeline_b", "select", txnID); err == nil && selectData != nil {
		type selectEnv struct {
			Message struct {
				Order struct {
					Fulfillments []struct {
						End struct {
							Location struct {
								GPS     string `json:"gps"`
								Address struct {
									AreaCode string `json:"area_code"`
								} `json:"address"`
							} `json:"location"`
						} `json:"end"`
					} `json:"fulfillments"`
				} `json:"order"`
			} `json:"message"`
		}
		var sel selectEnv
		if json.Unmarshal(selectData, &sel) == nil && len(sel.Message.Order.Fulfillments) > 0 {
			loc := sel.Message.Order.Fulfillments[0].End.Location
			selectGPS = loc.GPS
			selectAreaCode = loc.Address.AreaCode
		}
	}

	if fulf, _ := order["fulfillments"].([]any); fulf != nil {
		for i, f := range fulf {
			fm, _ := f.(map[string]any)
			if fm == nil {
				continue
			}
			end, _ := fm["end"].(map[string]any)
			if end == nil {
				end = map[string]any{}
			}
			loc, _ := end["location"].(map[string]any)
			if loc == nil {
				loc = map[string]any{}
			}
			if _, ok := loc["gps"].(string); !ok {
				if selectGPS != "" {
					loc["gps"] = selectGPS
				} else {
					loc["gps"] = "12.9716,77.5946"
				}
			}
			addr, _ := loc["address"].(map[string]any)
			if addr == nil {
				addr = map[string]any{}
			}
			if _, ok := addr["area_code"].(string); !ok {
				if selectAreaCode != "" {
					addr["area_code"] = selectAreaCode
				} else {
					addr["area_code"] = "560001"
				}
			}

			if billingAddr != nil {
				if v, _ := billingAddr["name"].(string); v != "" {
					if _, ok := addr["name"].(string); !ok {
						addr["name"] = v
					}
				}
				if v, _ := billingAddr["building"].(string); v != "" {
					if _, ok := addr["building"].(string); !ok {
						addr["building"] = v
					}
				}
				if v, _ := billingAddr["locality"].(string); v != "" {
					if _, ok := addr["locality"].(string); !ok {
						addr["locality"] = v
					}
				}
				if v, _ := billingAddr["city"].(string); v != "" {
					if _, ok := addr["city"].(string); !ok {
						addr["city"] = v
					}
				}
				if v, _ := billingAddr["state"].(string); v != "" {
					if _, ok := addr["state"].(string); !ok {
						addr["state"] = v
					}
				}
				if v, _ := billingAddr["country"].(string); v != "" {
					if _, ok := addr["country"].(string); !ok {
						addr["country"] = v
					}
				}
			}

			// Fallback defaults if still missing
			if _, ok := addr["name"].(string); !ok {
				addr["name"] = "Test Buyer"
			}
			if _, ok := addr["building"].(string); !ok {
				addr["building"] = "Test Building"
			}
			if _, ok := addr["locality"].(string); !ok {
				addr["locality"] = "MG Road"
			}
			if _, ok := addr["city"].(string); !ok {
				addr["city"] = "Bengaluru"
			}
			if _, ok := addr["state"].(string); !ok {
				addr["state"] = "KA"
			}
			if _, ok := addr["country"].(string); !ok {
				addr["country"] = "IND"
			}
			loc["address"] = addr
			end["location"] = loc

			contact, _ := end["contact"].(map[string]any)
			if contact == nil {
				contact = map[string]any{}
			}
			if _, ok := contact["phone"].(string); !ok {
				contact["phone"] = "9999999999"
			}
			end["contact"] = contact
			fm["end"] = end

			// RET13-specific: remove @ondc/org/category on init fulfillments
			// to satisfy schemas that disallow this additional property.
			if domain == "ONDC:RET13" {
				delete(fm, "@ondc/org/category")
			}

			fulf[i] = fm
		}
		order["fulfillments"] = fulf
	}
}

func enrichConfirm(full map[string]any, ctxMap map[string]any) {
	msg, _ := full["message"].(map[string]any)
	if msg == nil {
		msg = map[string]any{}
		full["message"] = msg
	}
	order, _ := msg["order"].(map[string]any)
	if order == nil {
		order = map[string]any{}
		msg["order"] = order
	}

	now := time.Now().UTC().Format(ondcTimestampLayout)

	if id, _ := order["id"].(string); id == "" {
		if txn, _ := ctxMap["transaction_id"].(string); txn != "" {
			order["id"] = txn
		} else {
			order["id"] = uuid.NewString()
		}
	}
	if state, _ := order["state"].(string); state != "Created" && state != "Accepted" && state != "Cancelled" {
		order["state"] = "Created"
	}
	if _, ok := order["created_at"].(string); !ok {
		order["created_at"] = now
	}
	if _, ok := order["updated_at"].(string); !ok {
		order["updated_at"] = now
	}

	var personName string
	if billing, _ := order["billing"].(map[string]any); billing != nil {
		if n, _ := billing["name"].(string); n != "" {
			personName = n
		}
	}
	if personName == "" {
		personName = "Test Buyer"
	}
	if fulf, _ := order["fulfillments"].([]any); fulf != nil {
		for i, f := range fulf {
			fm, _ := f.(map[string]any)
			if fm == nil {
				continue
			}
			end, _ := fm["end"].(map[string]any)
			if end == nil {
				end = map[string]any{}
			}
			person, _ := end["person"].(map[string]any)
			if person == nil {
				person = map[string]any{}
			}
			if _, ok := person["name"].(string); !ok {
				person["name"] = personName
			}
			end["person"] = person
			fm["end"] = end

			// Ensure a valid @ondc/org/TAT on confirm fulfillments.
			if _, ok := fm["@ondc/org/TAT"].(string); !ok {
				fm["@ondc/org/TAT"] = "P2D"
			}

			fulf[i] = fm
		}
		order["fulfillments"] = fulf
	}

	payment, _ := order["payment"].(map[string]any)
	if payment == nil {
		payment = map[string]any{}
	}
	if _, ok := payment["uri"].(string); !ok {
		payment["uri"] = "https://example.com/payment"
	}
	if _, ok := payment["tl_method"].(string); !ok {
		payment["tl_method"] = "http/get"
	}

	var amount, currency string
	if quote, _ := order["quote"].(map[string]any); quote != nil {
		if price, _ := quote["price"].(map[string]any); price != nil {
			if v, _ := price["value"].(string); v != "" {
				amount = v
			}
			if cur, _ := price["currency"].(string); cur != "" {
				currency = cur
			}
		}
	}
	if amount == "" {
		amount = "0"
	}
	if currency == "" {
		currency = "INR"
	}

	params, _ := payment["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["amount"].(string); !ok {
		params["amount"] = amount
	}
	if _, ok := params["currency"].(string); !ok {
		params["currency"] = currency
	}
	if _, ok := params["transaction_id"].(string); !ok {
		params["transaction_id"] = uuid.NewString()
	}
	payment["params"] = params

	if _, ok := payment["status"].(string); !ok {
		payment["status"] = "PAID"
	}
	if _, ok := payment["type"].(string); !ok {
		payment["type"] = "ON-ORDER"
	}
	if _, ok := payment["collected_by"].(string); !ok {
		payment["collected_by"] = "BAP"
	}
	order["payment"] = payment
}
