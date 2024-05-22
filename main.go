package main

import (
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	"github.com/Ayobami0/cli-chat-server/pb"
	"github.com/Ayobami0/cli-chat-server/server"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("No .env file found")
	}

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 5000))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(server.WithServerUnaryInterceptor())
	pb.RegisterChatServiceServer(grpcServer, server.NewChatServer())
	grpcServer.Serve(lis)
}
