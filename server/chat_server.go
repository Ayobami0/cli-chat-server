package server

import (
	"context"
	"log"
	"time"

	"github.com/Ayobami0/cli-chat-server/pb"
	"github.com/Ayobami0/cli-chat-server/server/store"
	"github.com/Ayobami0/cli-chat-server/server/store/models"
	"github.com/Ayobami0/cli-chat-server/server/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	pb.ChatServiceServer
	Store store.MongoDBStorage
}

func NewChatServer() *Server {
	var dbStore store.MongoDBStorage

	err := dbStore.Init("chat_db")
	if err != nil {
		log.Fatalf(err.Error())
	}
	return &Server{Store: dbStore}
}

func (s *Server) CreateNewAccount(c context.Context, r *pb.UserRequest) (*pb.UserCreatedResponse, error) {
	uName, pWord := r.Username, r.Password

	filter := bson.D{{Key: "username", Value: uName}}

	if err := s.Store.DB.Collection("users").FindOne(c, filter).Err(); err == nil {
		if err != mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.AlreadyExists, "User with username '%s' already exist.", uName)
		}
	}

	hashedPWord, err := utils.HashPassword(pWord)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "An error occured")
	}

	user := models.User{Username: uName, PasswordHash: hashedPWord, ID: primitive.NewObjectID()}

	_, err = s.Store.DB.Collection("users").InsertOne(c, &user)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, err.Error())
	}

	return &pb.UserCreatedResponse{User: r.Username}, nil
}

func (s *Server) LogIntoAccount(c context.Context, r *pb.UserRequest) (*pb.UserAuthenticatedResponse, error) {
	uName, pWord := r.Username, r.Password

	filter := bson.D{{Key: "username", Value: uName}}
	var user models.User

	err := s.Store.DB.Collection("users").FindOne(c, filter).Decode(&user)

	if err == mongo.ErrNoDocuments {
		return nil, status.Errorf(codes.NotFound, "User with username %s does not exist.", uName)
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

func (s *Server) ChatStream(pb.ChatService_ChatStreamServer) error {
	return nil
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

	err = s.Store.DB.Collection("direct_chat_requests").FindOneAndDelete(c, filter).Decode(&directChatRequest)

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
		_, err := s.Store.DB.Collection("chats").InsertOne(c, &newChat)

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
	cursor, err := s.Store.DB.Collection("direct_chat_requests").Find(c, filter)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	var results []models.DirectChatRequest
	err = cursor.All(c, &results)

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

	matchStage := bson.D{
		{
			Key: "$match",
			Value: bson.D{
				{
					Key:   "members",
					Value: user,
				},
			},
		},
	}
	coll := s.Store.DB.Collection("chats")
	cursor, err := coll.Aggregate(c, mongo.Pipeline{matchStage})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	var results []models.Chat
	err = cursor.All(c, &results)

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
	receiverName := r.Receiver.Username
	sender := c.Value(utils.USERNAME_HEADER).(models.User)

	recvFilter := bson.D{{Key: "username", Value: receiverName}}
	var receiver models.User

	if receiverName == sender.Username {
		return nil, status.Errorf(codes.InvalidArgument, "Cannot send request to self")
	}

	err := s.Store.DB.Collection("users").FindOne(c, recvFilter).Decode(&receiver)
	if err == mongo.ErrNoDocuments {
		return nil, status.Errorf(codes.NotFound, "User with username %s does not exist.", receiverName)
	}

	existingFilter := bson.D{{Key: "sender", Value: sender}, {Key: "receiver", Value: receiver}}
	if s.Store.DB.Collection("direct_chat_requests").FindOne(c, existingFilter).Err() == nil {
		return nil, status.Errorf(codes.Aborted, "Cannot send multiple request to same user")
	}

	chatRequest := models.DirectChatRequest{
		Sender:    models.User{Username: sender.Username, ID: sender.ID},
		Receiver:  models.User{Username: receiverName, ID: receiver.ID},
		CreatedAt: time.Now(),
		ID:        primitive.NewObjectID(),
	}

	s.Store.DB.Collection("direct_chat_requests").InsertOne(c, &chatRequest)

	return &pb.JoinDirectChatResponse{Id: chatRequest.ID.Hex()}, nil
}

func (s *Server) JoinGroupChat(c context.Context, r *pb.GroupChatRequest) (*pb.ChatResponse, error) {
	user := c.Value(utils.USERNAME_HEADER).(models.User)

	filter := bson.D{
		{Key: "name", Value: r.GroupName},
		{Key: "passkey", Value: r.GroupPasskey},
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
	s.Store.DB.Collection("chats").Aggregate(c, mongo.Pipeline{
		filter,
	})
	if err := s.Store.DB.Collection("chats").FindOne(c, filterPresense).Err(); err != mongo.ErrNoDocuments {
		if err == nil {
			return nil, status.Errorf(codes.AlreadyExists, "User is already a member of the group")
		}
		return nil, status.Errorf(codes.Unknown, err.Error())
	}

	var chat models.Chat
	err := s.Store.DB.Collection("chats").FindOneAndUpdate(c, filter, update).Decode(&chat)
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

	if err := s.Store.DB.Collection("chats").FindOne(c, filter).Err(); err != mongo.ErrNoDocuments {
		if err == nil {
			return nil, status.Errorf(codes.AlreadyExists, "Group name '%s' already exist.", r.GroupName)
		}
		return nil, status.Errorf(codes.Unknown, err.Error())
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
	_, err := s.Store.DB.Collection("chats").InsertOne(c, &chat)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	return utils.ConvertToProtoChats(chat), nil
}

func WithServerUnaryInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(utils.UnaryInterceptor)
}
