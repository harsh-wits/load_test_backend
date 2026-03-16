package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

type SelectPayload []byte

func ExtractTransactionIDs(selects []SelectPayload) []string {
	out := make([]string, 0, len(selects))
	for _, p := range selects {
		var env struct {
			Context struct {
				TransactionID string `json:"transaction_id"`
			} `json:"context"`
		}
		if err := json.Unmarshal(p, &env); err != nil {
			continue
		}
		if env.Context.TransactionID != "" {
			out = append(out, env.Context.TransactionID)
		}
	}
	return out
}

type SelectBatchService interface {
	GenerateBatchFromExample(examplePath string, batchSize int) ([]SelectPayload, error)
	GenerateBatchFromOnSearch(onSearch OnSearchPayload, examplePath string, batchSize int) ([]SelectPayload, error)
	GenerateBatchFromOnSearchWithTxnID(onSearch OnSearchPayload, examplePath string, batchSize int, txnID string) ([]SelectPayload, error)
}

type selectBatchService struct{}

func NewSelectBatchService() SelectBatchService {
	return &selectBatchService{}
}

const ondcTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

func nowONDCTimestamp() string {
	return time.Now().UTC().Format(ondcTimestampLayout)
}

type ondcContext struct {
	Domain        string `json:"domain"`
	Country       string `json:"country"`
	City          string `json:"city"`
	Action        string `json:"action"`
	CoreVersion   string `json:"core_version"`
	BAPID         string `json:"bap_id"`
	BAPURI        string `json:"bap_uri"`
	BPPURI        string `json:"bpp_uri"`
	TransactionID string `json:"transaction_id"`
	MessageID     string `json:"message_id"`
	Timestamp     string `json:"timestamp"`
	BPPID         string `json:"bpp_id"`
	TTL           string `json:"ttl"`
}

type ondcEnvelope struct {
	Context ondcContext     `json:"context"`
	Message json.RawMessage `json:"message"`
}

func (s *selectBatchService) GenerateBatchFromExample(examplePath string, batchSize int) ([]SelectPayload, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	raw, err := os.ReadFile(examplePath)
	if err != nil {
		return nil, fmt.Errorf("read example select: %w", err)
	}

	var env ondcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("unmarshal example select: %w", err)
	}

	const selectTTL = "PT30S"
	out := make([]SelectPayload, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		envCopy := env
		envCopy.Context.TransactionID = uuid.NewString()
		envCopy.Context.MessageID = uuid.NewString()
		envCopy.Context.Action = "select"
		envCopy.Context.Timestamp = nowONDCTimestamp()
		if envCopy.Context.TTL == "" {
			envCopy.Context.TTL = selectTTL
		}

		payload, err := json.Marshal(envCopy)
		if err != nil {
			return nil, fmt.Errorf("marshal generated select: %w", err)
		}
		payload, err = normalizeSelectPayload(payload)
		if err != nil {
			return nil, fmt.Errorf("normalize select: %w", err)
		}
		out = append(out, SelectPayload(payload))
	}

	return out, nil
}

func normalizeSelectPayload(payload []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	if ctx, _ := m["context"].(map[string]any); ctx != nil {
		if ttl, _ := ctx["ttl"].(string); ttl == "" {
			ctx["ttl"] = "PT30S"
		}
	}
	msg, _ := m["message"].(map[string]any)
	if msg == nil {
		return json.Marshal(m)
	}
	order, _ := msg["order"].(map[string]any)
	if order == nil {
		return json.Marshal(m)
	}
	if fulfillments, ok := order["fulfillments"].([]any); ok {
		for _, f := range fulfillments {
			if fm, _ := f.(map[string]any); fm != nil && fm["type"] == nil {
				fm["type"] = "Delivery"
			}
		}
	}
	if items, ok := order["items"].([]any); ok {
		for _, it := range items {
			if im, _ := it.(map[string]any); im != nil && im["fulfillment_id"] == nil {
				im["fulfillment_id"] = "F1"
			}
		}
	}
	return json.Marshal(m)
}

func (s *selectBatchService) GenerateBatchFromOnSearch(onSearch OnSearchPayload, examplePath string, batchSize int) ([]SelectPayload, error) {
	return s.generateFromOnSearch(onSearch, examplePath, batchSize, "")
}

func (s *selectBatchService) GenerateBatchFromOnSearchWithTxnID(onSearch OnSearchPayload, examplePath string, batchSize int, txnID string) ([]SelectPayload, error) {
	return s.generateFromOnSearch(onSearch, examplePath, batchSize, txnID)
}

func (s *selectBatchService) generateFromOnSearch(onSearch OnSearchPayload, examplePath string, batchSize int, fixedTxnID string) ([]SelectPayload, error) {
	if batchSize <= 0 || len(onSearch) == 0 {
		return nil, nil
	}

	var onEnv ondcEnvelope
	if err := json.Unmarshal(onSearch, &onEnv); err != nil {
		return nil, fmt.Errorf("unmarshal on_search: %w", err)
	}

	raw, err := os.ReadFile(examplePath)
	if err != nil {
		return nil, fmt.Errorf("read example select: %w", err)
	}

	var tmpl ondcEnvelope
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		return nil, fmt.Errorf("unmarshal example select: %w", err)
	}

	var orderWrapper struct {
		Order map[string]any `json:"order"`
	}
	if err := json.Unmarshal(tmpl.Message, &orderWrapper); err != nil {
		return nil, fmt.Errorf("unmarshal example select order: %w", err)
	}

	const distinctCount = 12
	orders, err := buildDistinctOrdersFromOnSearch(onSearch, orderWrapper.Order, distinctCount)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		orders = []map[string]any{orderWrapper.Order}
	} else {
		saveSelectPool(onSearch, orders)
	}

	return buildSelectPayloads(tmpl, onEnv.Context, orders, batchSize, fixedTxnID)
}

func buildSelectPayloads(tmpl ondcEnvelope, baseCtx ondcContext, orders []map[string]any, batchSize int, fixedTxnID string) ([]SelectPayload, error) {
	const selectTTL = "PT30S"
	out := make([]SelectPayload, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		envCopy := tmpl
		envCopy.Context = baseCtx
		envCopy.Context.Action = "select"
		if fixedTxnID != "" {
			envCopy.Context.TransactionID = fixedTxnID
		} else {
			envCopy.Context.TransactionID = uuid.NewString()
		}
		envCopy.Context.MessageID = uuid.NewString()
		envCopy.Context.Timestamp = nowONDCTimestamp()
		if envCopy.Context.TTL == "" {
			envCopy.Context.TTL = selectTTL
		}
		order := orders[i%len(orders)]
		msgBytes, err := json.Marshal(map[string]any{
			"order": order,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal select order: %w", err)
		}
		envCopy.Message = msgBytes

		payload, err := json.Marshal(envCopy)
		if err != nil {
			return nil, fmt.Errorf("marshal generated select from on_search: %w", err)
		}
		payload, err = normalizeSelectPayload(payload)
		if err != nil {
			return nil, fmt.Errorf("normalize select: %w", err)
		}
		out = append(out, SelectPayload(payload))
	}
	return out, nil
}

// onSearchCatalog is a minimal view of the on_search catalog needed
// for building varied select orders.
type onSearchCatalog struct {
	Message struct {
		Catalog struct {
			Providers []struct {
				ID        string `json:"id"`
				Locations []struct {
					ID string `json:"id"`
				} `json:"locations"`
				Items []struct {
					ID           string `json:"id"`
					LocationID   string `json:"location_id"`
					ParentItemID string `json:"parent_item_id"`
					Tags         []struct {
						Code string `json:"code"`
						List []struct {
							Code  string `json:"code"`
							Value string `json:"value"`
						} `json:"list"`
					} `json:"tags"`
				} `json:"items"`
			} `json:"bpp/providers"`
		} `json:"catalog"`
	} `json:"message"`
}

func buildDistinctOrdersFromOnSearch(onSearch []byte, baseOrder map[string]any, distinctCount int) ([]map[string]any, error) {
	if distinctCount <= 0 {
		return nil, nil
	}

	var cat onSearchCatalog
	if err := json.Unmarshal(onSearch, &cat); err != nil {
		// If the catalog shape is not as expected, fall back to the
		// example-based behaviour by signalling no orders.
		return nil, nil
	}
	if len(cat.Message.Catalog.Providers) == 0 {
		return nil, nil
	}

	prov := cat.Message.Catalog.Providers[0]
	if len(prov.Items) == 0 {
		return nil, nil
	}

	// Build a flat list of items belonging to this provider.
	type flatItem struct {
		ID           string
		LocationID   string
		ParentItemID string
		Tags         []map[string]any
	}

	items := make([]flatItem, 0, len(prov.Items))
	for _, it := range prov.Items {
		tagMaps := make([]map[string]any, 0, len(it.Tags))
		for _, t := range it.Tags {
			list := make([]map[string]any, 0, len(t.List))
			for _, v := range t.List {
				list = append(list, map[string]any{
					"code":  v.Code,
					"value": v.Value,
				})
			}
			tagMaps = append(tagMaps, map[string]any{
				"code": t.Code,
				"list": list,
			})
		}
		items = append(items, flatItem{
			ID:           it.ID,
			LocationID:   it.LocationID,
			ParentItemID: it.ParentItemID,
			Tags:         tagMaps,
		})
	}
	if len(items) == 0 {
		return nil, nil
	}

	if distinctCount > len(items) {
		distinctCount = len(items)
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	baseCopy := func() map[string]any {
		// Deep copy via JSON round-trip to avoid sharing nested maps.
		raw, _ := json.Marshal(baseOrder)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}

	maxItemsPerOrder := 5
	if len(items) < maxItemsPerOrder {
		maxItemsPerOrder = len(items)
	}
	if maxItemsPerOrder <= 0 {
		maxItemsPerOrder = 1
	}

	out := make([]map[string]any, 0, distinctCount)
	for i := 0; i < distinctCount; i++ {
		order := baseCopy()

		// Choose 1..maxItemsPerOrder distinct items for this order.
		n := 1 + r.Intn(maxItemsPerOrder)
		if n > len(items) {
			n = len(items)
		}

		perm := r.Perm(len(items))
		chosen := perm[:n]

		orderItems := make([]any, 0, n)
		for _, idx := range chosen {
			it := items[idx]
			itemMap := map[string]any{
				"id":          it.ID,
				"location_id": it.LocationID,
				"tags":        it.Tags,
				"quantity": map[string]any{
					"count": 2,
				},
			}
			if it.ParentItemID != "" {
				itemMap["parent_item_id"] = it.ParentItemID
			}
			orderItems = append(orderItems, itemMap)
		}

		// Replace items on the base order while retaining provider,
		// locations, fulfillments, etc.
		order["items"] = orderItems
		out = append(out, order)
	}

	return out, nil
}

func saveSelectPool(onSearch []byte, orders []map[string]any) {
	if len(orders) == 0 {
		return
	}

	sum := sha256.Sum256(onSearch)
	id := hex.EncodeToString(sum[:8])

	dir := filepath.Join("fixtures", "select_pools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	wrapped := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		wrapped = append(wrapped, map[string]any{
			"order": o,
		})
	}

	data, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644)
}
