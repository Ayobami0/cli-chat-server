package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/Ayobami0/cli-chat-server/pb"
	"github.com/Ayobami0/cli-chat-server/server"
	"github.com/Ayobami0/cli-chat-server/server/store"
)

var STORAGE = map[string]store.Storage{
	"mongodb": &store.MongoDBStorage{},
}

func main() {
	f, err := os.OpenFile("server.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	var addr string
	switch os.Getenv("ENVIRONMENT") {
	case "development":
		addr = fmt.Sprintf("0.0.0.0:%d", 5000)
	case "production":
		host := os.Getenv("HOST")
		port := os.Getenv("PORT")

		addr = fmt.Sprintf("%s:%s", host, port)
	default:
		log.Fatalf("Invalid value for 'ENVIRONMENT'")
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Println("gRPC Server started on " + addr)

	storage, ok := STORAGE[os.Getenv("STORAGE")]
	if !ok {
		log.Fatalf("Invalid storage type")
	}

	grpcServer := grpc.NewServer(server.WithServerUnaryInterceptor(), server.WithServerStreamInterceptor())
	pb.RegisterChatServiceServer(grpcServer, server.NewChatServer(storage))
	grpcServer.Serve(lis)
}
