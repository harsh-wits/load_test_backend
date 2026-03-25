package session

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"seller_app_load_tester/internal/domain/latency"
	domainSession "seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/shared/mongo"
)

type MongoStore struct {
	sessions *mongodriver.Collection
	runs     *mongodriver.Collection
	catalogs *mongodriver.Collection
	payloads *mongodriver.Collection
	latencyEvents     *mongodriver.Collection
	latencySummaries  *mongodriver.Collection
}

func NewMongoStore(client *mongo.Client) *MongoStore {
	s := &MongoStore{
		sessions: client.Collection("sessions"),
		runs:     client.Collection("runs"),
		catalogs: client.Collection("catalogs"),
		payloads: client.Collection("run_payloads"),
		latencyEvents:    client.Collection("run_latency_events"),
		latencySummaries: client.Collection("run_latency_summary"),
	}

	s.ensureLatencyIndexes()
	return s
}

func (s *MongoStore) ensureLatencyIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.latencyEvents.Indexes().CreateOne(ctx, mongodriver.IndexModel{
		Keys: bson.M{"run_id": 1, "stage": 1, "txn_id": 1},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		log.Printf("[mongo] latencyEvents index create FAILED error=%v", err)
	}

	_, err = s.latencySummaries.Indexes().CreateOne(ctx, mongodriver.IndexModel{
		Keys: bson.M{"run_id": 1, "stage": 1},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		log.Printf("[mongo] latencySummaries index create FAILED error=%v", err)
	}
}

func (s *MongoStore) SaveSession(ctx context.Context, sess *domainSession.Session) error {
	opts := options.Update().SetUpsert(true)
	_, err := s.sessions.UpdateByID(ctx, sess.ID, bson.M{"$set": sess}, opts)
	return err
}

func (s *MongoStore) SaveRun(ctx context.Context, r *domainSession.Run) error {
	opts := options.Update().SetUpsert(true)
	_, err := s.runs.UpdateByID(ctx, r.ID, bson.M{"$set": r}, opts)
	return err
}

func (s *MongoStore) SaveCatalog(ctx context.Context, sessionID string, payload []byte) error {
	doc := bson.M{
		"_id":        sessionID,
		"payload":    payload,
		"updated_at": time.Now().UTC(),
	}
	opts := options.Update().SetUpsert(true)
	_, err := s.catalogs.UpdateByID(ctx, sessionID, bson.M{"$set": doc}, opts)
	return err
}

func (s *MongoStore) GetCatalog(ctx context.Context, sessionID string) ([]byte, error) {
	var doc struct {
		Payload []byte `bson:"payload"`
	}
	err := s.catalogs.FindOne(ctx, bson.M{"_id": sessionID}).Decode(&doc)
	if err != nil {
		return nil, fmt.Errorf("catalog not found")
	}
	return doc.Payload, nil
}

func (s *MongoStore) GetRunHistory(ctx context.Context, sessionID string) ([]*domainSession.Run, error) {
	opts := options.Find().SetSort(bson.M{"started_at": -1})
	cursor, err := s.runs.Find(ctx, bson.M{"session_id": sessionID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var runs []*domainSession.Run
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func (s *MongoStore) GetRunByID(ctx context.Context, runID string) (*domainSession.Run, error) {
	var r domainSession.Run
	err := s.runs.FindOne(ctx, bson.M{"_id": runID}).Decode(&r)
	if err != nil {
		return nil, fmt.Errorf("run not found")
	}
	return &r, nil
}

func (s *MongoStore) GetSessionsByBPP(ctx context.Context, bppID string, page, pageSize int) ([]*domainSession.Session, int64, error) {
	filter := bson.M{"bpp_id": bppID}
	total, err := s.sessions.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	skip := int64((page - 1) * pageSize)
	opts := options.Find().SetSort(bson.M{"created_at": -1}).SetSkip(skip).SetLimit(int64(pageSize))
	cursor, err := s.sessions.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var sessions []*domainSession.Session
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

func (s *MongoStore) GetSessionByID(ctx context.Context, id string) (*domainSession.Session, error) {
	var sess domainSession.Session
	err := s.sessions.FindOne(ctx, bson.M{"_id": id}).Decode(&sess)
	if err != nil {
		return nil, fmt.Errorf("session not found")
	}
	return &sess, nil
}

func (s *MongoStore) SaveRunPayload(ctx context.Context, p *domainSession.RunPayload) error {
	if p == nil {
		return nil
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Timestamp.IsZero() {
		p.Timestamp = time.Now().UTC()
	}
	_, err := s.payloads.InsertOne(ctx, p)
	return err
}

func (s *MongoStore) GetRunPayloads(ctx context.Context, runID string) ([]*domainSession.RunPayload, error) {
	opts := options.Find().SetSort(bson.M{"timestamp": 1})
	cursor, err := s.payloads.Find(ctx, bson.M{"run_id": runID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var payloads []*domainSession.RunPayload
	if err := cursor.All(ctx, &payloads); err != nil {
		return nil, err
	}
	return payloads, nil
}

func (s *MongoStore) UpsertRunLatencyEvent(ctx context.Context, e *latency.RunLatencyEvent) error {
	if e == nil {
		return nil
	}

	filter := bson.M{
		"run_id": e.RunID,
		"stage":  e.Stage,
		"txn_id": e.TxnID,
	}

	// `$setOnInsert` ensures "late callbacks" cannot overwrite already-classified outcomes.
	doc := bson.M{
		"session_id":    e.SessionID,
		"run_id":        e.RunID,
		"stage":         e.Stage,
		"txn_id":        e.TxnID,
		"sent_at":       e.SentAt,
		"received_at":  e.ReceivedAt,
		"latency_ms":   e.LatencyMS,
		"outcome":      e.Outcome,
		"recorded_at":  e.RecordedAt,
		"timeout_cause": e.TimeoutCause,
	}

	opts := options.Update().SetUpsert(true)
	update := bson.M{"$setOnInsert": doc}
	_, err := s.latencyEvents.UpdateOne(ctx, filter, update, opts)
	return err
}

func (s *MongoStore) UpsertRunLatencySummary(ctx context.Context, sum *latency.RunLatencySummary) error {
	if sum == nil {
		return nil
	}

	filter := bson.M{
		"run_id": sum.RunID,
		"stage":  sum.Stage,
	}

	doc := bson.M{
		"session_id":           sum.SessionID,
		"run_id":               sum.RunID,
		"stage":                sum.Stage,
		"timeout_threshold_ms": sum.TimeoutThresholdMS,
		"cutoff_at":           sum.CutoffAt,
		"total":                sum.Total,
		"success_count":       sum.SuccessCount,
		"failure_count":       sum.FailureCount,
		"timeout_count":       sum.TimeoutCount,
		"avg_ms":               sum.AvgMS,
		"p90_ms":               sum.P90MS,
		"p95_ms":               sum.P95MS,
		"p99_ms":               sum.P99MS,
		"computed_at":         sum.ComputedAt,
	}

	opts := options.Update().SetUpsert(true)
	update := bson.M{"$set": doc}
	_, err := s.latencySummaries.UpdateOne(ctx, filter, update, opts)
	return err
}

func (s *MongoStore) GetRunLatencyEvents(ctx context.Context, runID string, stage latency.Stage, txnIDs []string) (map[string]*latency.RunLatencyEvent, error) {
	out := map[string]*latency.RunLatencyEvent{}
	if len(txnIDs) == 0 {
		return out, nil
	}

	filter := bson.M{
		"run_id": runID,
		"stage":  stage,
		"txn_id": bson.M{"$in": txnIDs},
	}

	cursor, err := s.latencyEvents.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var e latency.RunLatencyEvent
		if err := cursor.Decode(&e); err != nil {
			return nil, err
		}
		ev := e
		out[ev.TxnID] = &ev
	}
	return out, nil
}

func (s *MongoStore) GetRunLatencySummaries(ctx context.Context, runID string) (map[latency.Stage]*latency.RunLatencySummary, error) {
	out := map[latency.Stage]*latency.RunLatencySummary{}
	cursor, err := s.latencySummaries.Find(ctx, bson.M{"run_id": runID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var sum latency.RunLatencySummary
		if err := cursor.Decode(&sum); err != nil {
			return nil, err
		}
		s := sum
		out[s.Stage] = &s
	}
	return out, nil
}

func (s *MongoStore) ExpireSessionsByBPP(ctx context.Context, bppID string) error {
	_, err := s.sessions.UpdateMany(ctx,
		bson.M{"bpp_id": bppID, "status": string(domainSession.SessionActive)},
		bson.M{"$set": bson.M{"status": string(domainSession.SessionExpired)}},
	)
	return err
}

func (s *MongoStore) HardDeleteSession(ctx context.Context, id string) error {
	_, _ = s.runs.DeleteMany(ctx, bson.M{"session_id": id})
	_, _ = s.catalogs.DeleteOne(ctx, bson.M{"_id": id})
	_, err := s.sessions.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *MongoStore) HardDeleteSessionsByBPP(ctx context.Context, bppID string) (int64, error) {
	cursor, err := s.sessions.Find(ctx, bson.M{"bpp_id": bppID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var ids []string
	for cursor.Next(ctx) {
		var doc struct{ ID string `bson:"_id"` }
		if err := cursor.Decode(&doc); err == nil {
			ids = append(ids, doc.ID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}

	idFilter := bson.M{"$in": ids}
	_, _ = s.runs.DeleteMany(ctx, bson.M{"session_id": idFilter})
	_, _ = s.catalogs.DeleteMany(ctx, bson.M{"_id": idFilter})
	res, err := s.sessions.DeleteMany(ctx, bson.M{"_id": idFilter})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
