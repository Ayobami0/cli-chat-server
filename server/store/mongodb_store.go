package store

import (
	"context"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBStorage struct {
	DB *mongo.Database
}

func (m *MongoDBStorage) Init(dbName string) error {
	var uri string

	switch os.Getenv("ENVIRONMENT") {
	case "dev":
		uri = os.Getenv("MONGODB_URI_DEV")
	case "prod":
		uri = os.Getenv("MONGODB_URI_PROD")
	default:
		return fmt.Errorf("Invalid value for 'ENVIRONMENT'")
	}

	if uri == "" {
		return fmt.Errorf("'MONGODB_URI' environment variable not set. " +
			"See: " +
			"www.mongodb.com/docs/drivers/go/current/usage-examples/#environment-variable")
	}
	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}

	m.DB = client.Database(dbName)
	return nil
}
