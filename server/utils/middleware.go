package utils

import (
	"context"
	"strings"

	"github.com/Ayobami0/cli-chat-server/server/store/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	USERNAME_HEADER = "username"
)

func UnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	meta, ok := metadata.FromIncomingContext(ctx)

	if !ok {
		return nil, ErrMissingMetadata
	}

	if AuthRequired(info.FullMethod) {

		bearerToken := meta["authorization"]

		if len(bearerToken) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "authorization token is missing")
		}

		bearer := strings.Fields(bearerToken[0])

		if len(bearer) != 2 || bearer[0] != "Bearer" {
			return nil, status.Errorf(codes.InvalidArgument, "invalid authorization scheme. need Bearer <credential>")
		}

		username, userIdHex, err := ValidateToken(bearer[1])
		if err != nil {
			return nil, err
		}
		userId, err := primitive.ObjectIDFromHex(userIdHex)

		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid format for hex")
		}
		ctx = context.WithValue(ctx, USERNAME_HEADER, models.User{Username: username, ID: userId})
	}

	m, err := handler(ctx, req)

	if err != nil {
		return nil, status.Errorf(codes.Unknown, err.Error())
	}
	return m, nil
}
