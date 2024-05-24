package server

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/Ayobami0/cli-chat-server/pb"
	"github.com/Ayobami0/cli-chat-server/server/models"
	"github.com/Ayobami0/cli-chat-server/server/store"
	"github.com/Ayobami0/cli-chat-server/server/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	DATABASE_NAME = "chat_db"

	USERS_COLLECTION                = "users"
	DIRECT_CHAT_REQUESTS_COLLECTION = "direct_chat_requests"
	CHATS_COLLECTION                = "chats"
)

type Server struct {
	pb.ChatServiceServer
	Store store.Storage
}

func NewChatServer(storage store.Storage) *Server {

	err := storage.Init(DATABASE_NAME)
	if err != nil {
		log.Fatalf(err.Error())
	}
	return &Server{Store: storage}
}

func (s *Server) CreateNewAccount(c context.Context, r *pb.UserRequest) (*pb.UserCreatedResponse, error) {
	uName, pWord := r.Username, r.Password

	filter := bson.D{{Key: "username", Value: uName}}

	exist, err := s.Store.Exists(c, USERS_COLLECTION, filter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if exist {
		return nil, status.Errorf(codes.AlreadyExists, "User with username '%s' already exist.", uName)

	}

	hashedPWord, err := utils.HashPassword(pWord)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "Invalid password")
	}

	user := models.User{Username: uName, PasswordHash: hashedPWord, ID: primitive.NewObjectID()}

	err = s.Store.Add(c, USERS_COLLECTION, &user)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	return &pb.UserCreatedResponse{User: r.Username}, nil
}

func (s *Server) LogIntoAccount(c context.Context, r *pb.UserRequest) (*pb.UserAuthenticatedResponse, error) {
	uName, pWord := r.Username, r.Password

	filter := bson.D{{Key: "username", Value: uName}}
	var user models.User

	err := s.Store.Get(c, USERS_COLLECTION, &user, filter)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "User with username %s does not exist.", uName)
		}
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	if !utils.CheckPasswordHash(pWord, user.PasswordHash) {
		return nil, status.Errorf(codes.Unauthenticated, "Password is incorrect")
	}

	token, err := utils.GenerateToken(user.Username, user.ID.Hex())
	if err != nil {
		return nil, err
	}

	return &pb.UserAuthenticatedResponse{User: uName, Token: token}, nil
}

func (s *Server) ChatStream(stream pb.ChatService_ChatStreamServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}

		chatId, err := primitive.ObjectIDFromHex(in.ChatId)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "Id is not an hexadecimal string")
		}
		userId, err := primitive.ObjectIDFromHex(in.Message.Sender.Id)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "Id is not an hexadecimal string")
		}
		sender := models.User{ID: userId, Username: in.Message.Sender.Username}

		chatFilter := bson.D{{Key: "_id", Value: chatId}}
		userFilter := bson.D{{Key: "username", Value: sender.Username}}
		presenseFilter := bson.D{
			chatFilter[0],
			{Key: "members", Value: sender},
		}

		// Checks if user exist
		userExist, err := s.Store.Exists(context.TODO(), USERS_COLLECTION, userFilter)
		if err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}
		if !userExist {
			return status.Errorf(codes.NotFound, "User with name '%s' not found", sender.Username)
		}

		// Checks if chat exist
		chatExist, err := s.Store.Exists(context.TODO(), CHATS_COLLECTION, chatFilter)
		if err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}
		if !chatExist {
			return status.Errorf(codes.NotFound, "Chat with id '%s' not found", in.ChatId)
		}

		// Checks if user is a member of the chat exist
		userInChat, err := s.Store.Exists(context.TODO(), CHATS_COLLECTION, presenseFilter)
		if err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}
		if !userInChat {
		}

		message := &models.Message{
			ID:        primitive.NewObjectID(),
			Sender:    sender,
			Content:   in.Message.Content,
			Type:      in.Message.Type,
			CreatedAt: in.Message.SentAt.AsTime(),
		}

		update := bson.D{
			{
				Key: "$push",
				Value: bson.D{{
					Key:   "messages",
					Value: message,
				}},
			},
		}

		err = s.Store.Update(context.TODO(), CHATS_COLLECTION, chatFilter, update)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				return status.Errorf(codes.NotFound, "User is not a member part of the chat")
			}
			return status.Errorf(codes.Unknown, err.Error())
		}

		if err := stream.Send(&pb.MessageStream{
			ChatId: in.ChatId,
			Message: &pb.Message{
				Id:      message.ID.Hex(),
				Sender:  in.Message.Sender,
				Type:    message.Type,
				Content: in.Message.Content,
				SentAt:  in.Message.SentAt,
			},
		}); err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}

	}
}

func (s *Server) DirectChatRequestAction(c context.Context, r *pb.DirectChatAction) (*emptypb.Empty, error) {
	id, err := primitive.ObjectIDFromHex(r.Id)

	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Id is not an hexadecimal string")
	}

	filter := bson.D{{
		Key:   "_id",
		Value: id,
	}}

	var directChatRequest models.DirectChatRequest

	err = s.Store.GetAndDelete(c, DIRECT_CHAT_REQUESTS_COLLECTION, &directChatRequest, filter)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "Request with id '%s' does not exist", r.Id)
		}
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	switch r.Action {
	case pb.DirectChatAction_ACTION_ACCEPT:
		newChat := models.Chat{
			CreatedAt: time.Now(),
			Members: []models.User{
				directChatRequest.Sender,
				{Username: directChatRequest.Receiver.Username, ID: directChatRequest.Receiver.ID},
			},
			Type: pb.ChatType_CHAT_TYPE_DIRECT,
			ID:   primitive.NewObjectID(),
		}
		err := s.Store.Add(c, CHATS_COLLECTION, &newChat)

		if err != nil {
			return nil, status.Errorf(codes.Unknown, err.Error())
		}
	case pb.DirectChatAction_ACTION_REJECT:
		return &emptypb.Empty{}, nil
	default:
		return nil, status.Errorf(codes.InvalidArgument, "Action is not valid")
	}

	return &emptypb.Empty{}, nil
}

func (s *Server) GetDirectChatRequests(c context.Context, r *emptypb.Empty) (*pb.JoinDirectChatResponses, error) {
	user := c.Value(utils.USERNAME_HEADER).(models.User)

	filter := bson.D{{
		Key:   "receiver",
		Value: user,
	}}
	var results []models.DirectChatRequest
	err := s.Store.GetAll(c, DIRECT_CHAT_REQUESTS_COLLECTION, &results, filter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	var requests []*pb.JoinDirectChatResponse

	for _, v := range results {
		requests = append(requests, &pb.JoinDirectChatResponse{Id: v.ID.Hex(), Sender: &pb.User{Id: v.Sender.ID.Hex(), Username: v.Sender.Username}})
	}

	return &pb.JoinDirectChatResponses{
		Requests: requests,
	}, nil
}

func (s *Server) GetChats(c context.Context, r *emptypb.Empty) (*pb.ChatsResponse, error) {
	user := c.Value(utils.USERNAME_HEADER).(models.User)

	filter := bson.D{
		{
			Key:   "members",
			Value: user,
		},
	}
	var results []models.Chat
	err := s.Store.GetAll(c, CHATS_COLLECTION, &results, filter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	var chats []*pb.ChatResponse

	for _, v := range results {
		chats = append(chats, utils.ConvertToProtoChats(v))
	}

	return &pb.ChatsResponse{Chats: chats}, nil

}

func (s *Server) JoinDirectChat(c context.Context, r *pb.JoinDirectChatRequest) (*pb.JoinDirectChatResponse, error) {
	sender := c.Value(utils.USERNAME_HEADER).(models.User)
	receiverId, err := primitive.ObjectIDFromHex(r.Receiver.Id)

	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Id is not an hexadecimal string")
	}

	receiver := models.User{
		Username: r.Receiver.Username,
		ID:       receiverId,
	}
	recvFilter := bson.D{{Key: "username", Value: receiver.Username}}

	if receiver.Username == sender.Username {
		return nil, status.Errorf(codes.InvalidArgument, "Cannot send request to self")
	}

	exist, err := s.Store.Exists(c, USERS_COLLECTION, recvFilter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if !exist {
		return nil, status.Errorf(codes.NotFound, "User with username %s does not exist.", receiver.Username)
	}

	existingFilter := bson.D{{Key: "sender", Value: sender}, {Key: "receiver", Value: receiver}}
	match, err := s.Store.Exists(c, DIRECT_CHAT_REQUESTS_COLLECTION, existingFilter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if match {
		return nil, status.Errorf(codes.Aborted, "Cannot send multiple request to same user")
	}

	chatRequest := models.DirectChatRequest{
		Sender:    sender,
		Receiver:  receiver,
		CreatedAt: time.Now(),
		ID:        primitive.NewObjectID(),
	}

	s.Store.Add(c, DIRECT_CHAT_REQUESTS_COLLECTION, &chatRequest)

	return &pb.JoinDirectChatResponse{Id: chatRequest.ID.Hex(), Sender: &pb.User{Username: sender.Username, Id: sender.ID.Hex()}}, nil
}

func (s *Server) JoinGroupChat(c context.Context, r *pb.GroupChatRequest) (*pb.ChatResponse, error) {
	user := c.Value(utils.USERNAME_HEADER).(models.User)

	filter := bson.D{
		{Key: "name", Value: r.GroupName},
		{Key: "passkey", Value: r.GroupPasskey},
		{Key: "type", Value: pb.ChatType_CHAT_TYPE_GROUP},
	}
	filterPresense := bson.D{
		{Key: "members", Value: user},
	}
	update := bson.D{
		{
			Key: "$push",
			Value: bson.D{{
				Key:   "members",
				Value: user,
			}},
		},
	}
	exist, err := s.Store.Exists(c, CHATS_COLLECTION, filterPresense)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if exist {
		return nil, status.Errorf(codes.AlreadyExists, "User is already a member of the group")
	}

	var chat models.Chat
	err = s.Store.GetAndUpdate(c, CHATS_COLLECTION, &chat, filter, update)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "Group name or passkey incorrect.")
		}
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	return utils.ConvertToProtoChats(chat), nil
}

func (s *Server) CreateGroupChat(c context.Context, r *pb.GroupChatRequest) (*pb.ChatResponse, error) {
	user := c.Value(utils.USERNAME_HEADER).(models.User)

	filter := bson.D{{Key: "name", Value: r.GroupName}}

	exist, err := s.Store.Exists(c, CHATS_COLLECTION, filter)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if exist {
		return nil, status.Errorf(codes.AlreadyExists, "Group name '%s' already exist.", r.GroupName)
	}

	chat := models.Chat{
		Members:   []models.User{{Username: user.Username, ID: user.ID}},
		Messages:  []models.Message{},
		Type:      pb.ChatType_CHAT_TYPE_GROUP,
		ID:        primitive.NewObjectID(),
		CreatedAt: time.Now(),
		PassKey:   r.GroupPasskey,
		Name:      r.GroupName,
	}
	err = s.Store.Add(c, CHATS_COLLECTION, &chat)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	return utils.ConvertToProtoChats(chat), nil
}

func WithServerUnaryInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(utils.UnaryInterceptor)
}
