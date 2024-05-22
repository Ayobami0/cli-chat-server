package utils

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrMissingMetadata = status.Errorf(codes.InvalidArgument, "missing metadata")
	ErrInvalidToken    = status.Errorf(codes.Unauthenticated, "invalid token")
	ErrTokenGeneration = status.Errorf(codes.Unauthenticated, "unable to generate token")
	ErrInvalidClaim    = status.Errorf(codes.Unauthenticated, "invalid claim")
	ErrTokenNotFound   = status.Errorf(codes.InvalidArgument, "token not found")
)
