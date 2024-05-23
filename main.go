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
	"github.com/joho/godotenv"
)

var STORAGE = map[string]store.Storage{
	"mongodb": &store.MongoDBStorage{},
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("No .env file found")
	}

	var addr string
	switch os.Getenv("ENVIRONMENT") {
	case "dev":
		addr = fmt.Sprintf("localhost:%d", 5000)
	case "prod":
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

	storage, ok := STORAGE[os.Getenv("STORAGE")]
	if !ok {
		log.Fatalf("Invalid storage type")
	}

	grpcServer := grpc.NewServer(server.WithServerUnaryInterceptor())
	pb.RegisterChatServiceServer(grpcServer, server.NewChatServer(storage))
	grpcServer.Serve(lis)
}
