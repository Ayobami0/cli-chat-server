package store

import (
	"context"
)

type Storage interface {
	Init(string) error

	Get(context.Context, string, any, any) error
	Add(context.Context, string, any) error

	GetAndDelete(context.Context, string, any, any) error
	GetAndUpdate(context.Context, string, any, any, any) error

	Delete(context.Context, string, any) error
	Update(context.Context, string, any, any) error

	GetAll(context.Context, string, any, any) error
	Exists(context.Context, string, any) (bool, error)
}
