package models

import (
	"time"

	"github.com/Ayobami0/cli-chat-server/pb"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Message struct {
	ID        primitive.ObjectID     `bson:"_id"`
	Sender    User                   `bson:"sender"`
	Content   string                 `bson:"content"`
	Type      pb.Message_MessageType `bson:"type"`
	CreatedAt time.Time              `bson:"created_at"`
}

func (m Message) GetID() string {
	return m.ID.Hex()
}
