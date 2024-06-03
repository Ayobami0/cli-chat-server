package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	USERS_COLLECTION                = "users"
	DIRECT_CHAT_REQUESTS_COLLECTION = "direct_chat_requests"
	CHATS_COLLECTION                = "chats"

	CHAT_STREAM_HANDSHAKE_KEY_ID       = "stream_chat_id"
	CHAT_STREAM_HANDSHAKE_KEY_USERNAME = "stream_username"
)

type Server struct {
	pb.ChatServiceServer
	Store store.Storage

	mu          sync.RWMutex
	connections map[string]map[string]pb.ChatService_ChatStreamServer
}

func NewChatServer(storage store.Storage) *Server {
	databaseName := os.Getenv("MONGO_NAME")

	if databaseName == "" {
		log.Fatalf("MONGO_NAME variable is required")
	}
	err := storage.Init(databaseName)
	if err != nil {
		log.Fatalf(err.Error())
	}
	log.Println("Connected To Database")
	return &Server{Store: storage, connections: map[string]map[string]pb.ChatService_ChatStreamServer{}, mu: sync.RWMutex{}}
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

	return &pb.UserAuthenticatedResponse{User: &pb.User{Username: user.Username, Id: user.ID.Hex()}, Token: token}, nil
}

func (s *Server) isSession(key string) bool {
	_, ok := s.connections[key]

	return ok
}

func (s *Server) broadcast(key string, message *pb.MessageStream) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isSession(key) {
		return errors.New("Invalid session")
	}

	for _, s := range s.connections[key] {
		if err := s.Send(message); err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}
	}

	return nil
}

func (s *Server) addStream(key string, user string, stream pb.ChatService_ChatStreamServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isSession(key) {
		s.connections[key] = map[string]pb.ChatService_ChatStreamServer{}
	}
	s.connections[key][user] = stream
	return nil
}

func (s *Server) removeSession(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isSession(key) {
		return errors.New("Invalid session")
	}
	if len(s.connections[key]) != 0 {
		return errors.New("Cannot remove session. Active streams present")
	}
	delete(s.connections, key)
	return nil
}

func (s *Server) removeStream(key, user string, stream pb.ChatService_ChatStreamServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn := make([]pb.ChatService_ChatStreamServer, 0)

	if !s.isSession(key) {
		return errors.New("Invalid session")
	}

	for _, v := range s.connections[key] {
		if v != stream {
			conn = append(conn, v)
		}
	}

	delete(s.connections[key], user)

	if len(s.connections[key]) == 0 {
		s.removeSession(key)
	}

	return nil
}

func (s *Server) ChatStream(stream pb.ChatService_ChatStreamServer) error {
	// STREAM HANDSHAKE
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return utils.ErrMissingMetadata
	}

	cID := md.Get(CHAT_STREAM_HANDSHAKE_KEY_ID)
	cUsername := md.Get(CHAT_STREAM_HANDSHAKE_KEY_USERNAME)

	if len(cID) == 0 {
		return status.Errorf(codes.InvalidArgument, "Invalid metadata value '%s'", CHAT_STREAM_HANDSHAKE_KEY_ID)
	}
	if len(cUsername) == 0 {
		return status.Errorf(codes.InvalidArgument, "Invalid metadata value '%s'", CHAT_STREAM_HANDSHAKE_KEY_USERNAME)
	}

	chatId, err := primitive.ObjectIDFromHex(cID[0])
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Id is not an hexadecimal string")
	}
	// Checks if chat exist
	chatFilter := bson.D{{Key: "_id", Value: chatId}}

	chatExist, err := s.Store.Exists(context.TODO(), CHATS_COLLECTION, chatFilter)
	if err != nil {
		return status.Errorf(codes.Unknown, err.Error())
	}
	if !chatExist {
		return status.Errorf(codes.NotFound, "Chat with id '%s' not found", cID[0])
	}
	userFilter := bson.D{{Key: "username", Value: cUsername[0]}}

	// Checks if user exist
	var user models.User
	err = s.Store.Get(context.TODO(), USERS_COLLECTION, &user, userFilter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return status.Errorf(codes.NotFound, "User with username %s does not exist.", cUsername[0])
		}
		return status.Errorf(codes.Unknown, err.Error())
	}

	presenseFilter := bson.D{
		chatFilter[0],
		{Key: "members", Value: models.User{Username: user.Username, ID: user.ID}},
	}
	// Checks if user is a member of the chat
	userInChat, err := s.Store.Exists(context.TODO(), CHATS_COLLECTION, presenseFilter)
	if err != nil {
		return status.Errorf(codes.Unknown, err.Error())
	}
	if !userInChat {
		return status.Errorf(codes.NotFound, "User '%s' is not a member of this chat", cUsername[0])
	}

	s.addStream(cID[0], cUsername[0], stream)

	s.broadcast(cID[0], &pb.MessageStream{
		Message: &pb.Message{
			Content: fmt.Sprintf("User [%s] has joined. Say Hi.", user.Username),
			Type:    pb.Message_MESSAGE_TYPE_NOTIFICATION,
			SentAt:  timestamppb.Now(),
		},
		ChatId: cID[0],
	})

	// POST HANDSHAKE
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			s.removeStream(cID[0], cUsername[0], stream)
			s.broadcast(cID[0], &pb.MessageStream{
				Message: &pb.Message{
					Content: fmt.Sprintf("User [%s] has just left.", user.Username),
					Type:    pb.Message_MESSAGE_TYPE_NOTIFICATION,
					SentAt:  timestamppb.Now(),
				},
				ChatId: cID[0],
			})
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, err.Error())
		}

		sender := models.User{Username: in.Message.Sender.Username}

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

		s.broadcast(cID[0], &pb.MessageStream{
			ChatId: in.ChatId,
			Message: &pb.Message{
				Id:      message.ID.Hex(),
				Sender:  in.Message.Sender,
				Type:    message.Type,
				Content: in.Message.Content,
				SentAt:  in.Message.SentAt,
			}})

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
			Type:     pb.ChatType_CHAT_TYPE_DIRECT,
			Messages: make([]models.Message, 0),
			ID:       primitive.NewObjectID(),
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

	receiver := models.User{
		Username: r.Receiver.Username,
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
	existingChatFilter := bson.D{
		{Key: "type", Value: pb.ChatType_CHAT_TYPE_DIRECT},
		{Key: "members.username", Value: bson.D{
			{
				Key:   "$all",
				Value: bson.A{sender.Username, receiver.Username},
			}},
		},
	}

	match, err := s.Store.Exists(c, CHATS_COLLECTION, existingChatFilter)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	if match {
		return nil, status.Errorf(codes.Aborted, "Cannot send request to an already existing chat")
	}

	existingRequestFilter := bson.D{{Key: "sender", Value: sender}, {Key: "receiver", Value: receiver}}
	match, err = s.Store.Exists(c, DIRECT_CHAT_REQUESTS_COLLECTION, existingRequestFilter)

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
		filter[0],
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

func WithServerStreamInterceptor() grpc.ServerOption {
	return grpc.StreamInterceptor(utils.StreamInterceptor)
}
