package session

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	domainSession "seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/shared/mongo"
)

type MongoStore struct {
	sessions *mongodriver.Collection
	runs     *mongodriver.Collection
	catalogs *mongodriver.Collection
}

func NewMongoStore(client *mongo.Client) *MongoStore {
	return &MongoStore{
		sessions: client.Collection("sessions"),
		runs:     client.Collection("runs"),
		catalogs: client.Collection("catalogs"),
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
