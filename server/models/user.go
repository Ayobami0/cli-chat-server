package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID           primitive.ObjectID `bson:"_id"`
	Username     string             `bson:"username"`
	PasswordHash string             `bson:"password_hash,omitempty"`
}

func (u User) GetID() string {
	return u.ID.Hex()
}
