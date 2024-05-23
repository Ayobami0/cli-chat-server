package utils

import (
	"strings"

	"github.com/Ayobami0/cli-chat-server/pb"
	"github.com/Ayobami0/cli-chat-server/server/models"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	return string(hashed), err
}

func CheckPasswordHash(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))

	if err != nil {
		return false
	}

	return true
}

func AuthRequired(method string) bool {
	prefix := "/chat.ChatService/"
	mtdName := strings.TrimPrefix(method, prefix)

	notRequired := []string{"CreateNewAccount", "LogIntoAccount"}

	for _, v := range notRequired {
		if v == mtdName {
			return false
		}
	}
	return true
}

func ConvertToProtoChats(mongoChat models.Chat) *pb.ChatResponse {
	var messages []*pb.Message
	var users []*pb.User

	for _, msg := range mongoChat.Messages {
		messages = append(messages, &pb.Message{
			Sender:  &pb.User{Username: msg.Sender.Username, Id: msg.Sender.ID.Hex()},
			Content: msg.Content,
			SentAt:  timestamppb.New(msg.CreatedAt),
			Type:    msg.Type,
			Id:      msg.ID.Hex(),
		})
	}
	for _, usr := range mongoChat.Members {
		users = append(users, &pb.User{
			Username: usr.Username,
			Id:       usr.ID.Hex(),
		})
	}
	return &pb.ChatResponse{
		Messages:  messages,
		Members:   users,
		Name:      &mongoChat.Name,
		Type:      mongoChat.Type,
		Id:        mongoChat.ID.Hex(),
		CreatedAt: timestamppb.New(mongoChat.CreatedAt),
	}
}
