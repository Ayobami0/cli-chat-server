package utils

import (
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	CLAIM_EXPIRY_MINS = 30
)

type UserClaim struct {
	jwt.RegisteredClaims
	Username string
	UserID   string
}

func ValidateToken(tokenString string) (string, string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaim{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(os.Getenv("SECRET_KEY")), nil
	})

	if err != nil {
		return "", "", status.Errorf(codes.Unknown, err.Error())
	}
	if !token.Valid {
		return "", "", ErrInvalidToken
	}
	claim, ok := token.Claims.(*UserClaim)
	if !ok {
		return "", "", ErrInvalidClaim
	}
	return claim.Username, claim.UserID, nil
}

func GenerateToken(username, userId string) (string, error) {
	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		UserClaim{
			Username: username,
			UserID:   userId,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute * CLAIM_EXPIRY_MINS)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		},
	)
	skey := os.Getenv("SECRET_KEY")
	tokenString, err := token.SignedString([]byte(skey))

	if err != nil {
		log.Println(err)
		return "", ErrTokenGeneration
	}

	return tokenString, nil
}
