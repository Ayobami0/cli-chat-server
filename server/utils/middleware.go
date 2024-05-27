package utils

import (
	"context"
	"strings"

	"github.com/Ayobami0/cli-chat-server/server/models"
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

// wrappedStream wraps around the embedded grpc.ServerStream, and intercepts the RecvMsg and
// SendMsg method call.
type wrappedStream struct {
	grpc.ServerStream
}

func (w *wrappedStream) RecvMsg(m any) error {
	return w.ServerStream.RecvMsg(m)
}

func (w *wrappedStream) SendMsg(m any) error {
	return w.ServerStream.SendMsg(m)
}

func newWrappedStream(s grpc.ServerStream) grpc.ServerStream {
	return &wrappedStream{s}
}

func StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// authentication (token verification)
	meta, ok := metadata.FromIncomingContext(ss.Context())
	if !ok {
		return ErrMissingMetadata
	}
	if AuthRequired(info.FullMethod) {

		bearerToken := meta["authorization"]

		if len(bearerToken) == 0 {
			return status.Errorf(codes.Unauthenticated, "authorization token is missing")
		}

		bearer := strings.Fields(bearerToken[0])

		if len(bearer) != 2 || bearer[0] != "Bearer" {
			return status.Errorf(codes.InvalidArgument, "invalid authorization scheme. need Bearer <credential>")
		}

		_, userIdHex, err := ValidateToken(bearer[1])
		if err != nil {
			return err
		}
		_, err = primitive.ObjectIDFromHex(userIdHex)

		if err != nil {
			return status.Errorf(codes.InvalidArgument, "invalid format for hex")
		}
	}

	err := handler(srv, newWrappedStream(ss))
	if err != nil {
		return status.Errorf(codes.Unknown, err.Error())
	}
	return err
}
