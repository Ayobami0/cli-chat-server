package models

import (
	"time"

	"github.com/Ayobami0/cli-chat-server/pb"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Chat struct {
	ID        primitive.ObjectID `bson:"_id"`
	PassKey   string             `bson:"passkey, omitempty"`
	Name      string             `bson:"name, omitempty"`
	Type      pb.ChatType        `bson:"type"`
	CreatedAt time.Time          `bson:"created_at"`
	Members   []User             `bson:"members"`
	Messages  []Message          `bson:"messages"`
}
