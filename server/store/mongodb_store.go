package store

import (
	"context"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBStorage struct {
	client   *mongo.Client
	database string
}

func (m *MongoDBStorage) Init(dbName string) error {
	var pass, user string
	switch os.Getenv("ENVIRONMENT") {
	case "development":
		pass = "root"
		user = "root"
	case "production":
		pass = os.Getenv("MONGO_PASSWORD")
		user = os.Getenv("MONGO_USERNAME")
	default:
		return fmt.Errorf("Invalid value for 'ENVIRONMENT'")
	}

	uri := fmt.Sprintf("mongodb://%s:%s@mongodb", user, pass)

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}

	m.client = client
	m.database = dbName
	return nil
}

func (m *MongoDBStorage) Get(ctx context.Context, collection string, data, filter any) error {
	if err := m.client.Database(m.database).Collection(collection).FindOne(ctx, filter).Decode(data); err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) Exists(ctx context.Context, collection string, filter any) (bool, error) {
	if err := m.client.Database(m.database).Collection(collection).FindOne(ctx, filter).Err(); err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *MongoDBStorage) GetAll(ctx context.Context, collection string, data, filter any) error {
	cursor, err := m.client.Database(m.database).Collection(collection).Find(ctx, filter)
	if err != nil {
		return err
	}

	err = cursor.All(ctx, data)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) GetAndUpdate(ctx context.Context, collection string, data, filter, update any) error {
	err := m.client.Database(m.database).Collection(collection).FindOneAndUpdate(ctx, filter, update).Decode(&data)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) GetAndDelete(ctx context.Context, collection string, data, filter any) error {
	err := m.client.Database(m.database).Collection(collection).FindOneAndDelete(ctx, filter).Decode(data)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) Add(ctx context.Context, collection string, data any) error {
	_, err := m.client.Database(m.database).Collection(collection).InsertOne(ctx, data)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) Update(ctx context.Context, collection string, filter, update any) error {
	_, err := m.client.Database(m.database).Collection(collection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDBStorage) Delete(ctx context.Context, collection string, filter any) error {
	_, err := m.client.Database(m.database).Collection(collection).DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	return nil
}
