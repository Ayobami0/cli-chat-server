package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type DirectChatRequest struct {
	ID        primitive.ObjectID `bson:"_id"`
	Sender    User               `bson:"sender"`
	Receiver  User               `bson:"receiver"`
	CreatedAt time.Time          `bson:"created_at"`
}

func (d DirectChatRequest) GetID() string {
	return d.ID.Hex()
}
