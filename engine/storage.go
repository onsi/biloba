package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

type StorageArea string

const (
	StorageLocal   StorageArea = "localStorage"
	StorageSession StorageArea = "sessionStorage"
)

type Storage struct {
	session *Session
	area    StorageArea
}

func (s *Session) Storage(area StorageArea) *Storage { return &Storage{session: s, area: area} }

func (s *Storage) valid() error {
	if s == nil || s.session == nil || (s.area != StorageLocal && s.area != StorageSession) {
		return &Error{Code: CodeInvalidArgument, Operation: "storage", Message: "area must be localStorage or sessionStorage"}
	}
	return nil
}

func (s *Storage) Set(ctx context.Context, key string, value any) error {
	if err := s.valid(); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return &Error{Code: CodeInvalidArgument, Operation: "storage set", Message: err.Error(), Cause: err}
	}
	args, _ := EncodeArgs(key, string(encoded))
	return s.session.serial(ctx, "storage set", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("window.%s.setItem(...%s)", s.area, args), false, nil)
	})
}

func (s *Storage) Get(ctx context.Context, key string) (any, bool, error) {
	if err := s.valid(); err != nil {
		return nil, false, err
	}
	args, _ := EncodeArgs(key)
	var raw *string
	err := s.session.serial(ctx, "storage get", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("window.%s.getItem(...%s)", s.area, args), false, &raw)
	})
	if err != nil || raw == nil {
		return nil, false, err
	}
	return decodeStoredValue(*raw), true, nil
}

func (s *Storage) GetAll(ctx context.Context) (map[string]any, error) {
	if err := s.valid(); err != nil {
		return nil, err
	}
	var raw map[string]string
	err := s.session.serial(ctx, "storage get all", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("({...window.%s})", s.area), false, &raw)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = decodeStoredValue(value)
	}
	return out, nil
}

func (s *Storage) Remove(ctx context.Context, key string) error {
	if err := s.valid(); err != nil {
		return err
	}
	args, _ := EncodeArgs(key)
	return s.session.serial(ctx, "storage remove", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("window.%s.removeItem(...%s)", s.area, args), false, nil)
	})
}

func (s *Storage) Clear(ctx context.Context) error {
	if err := s.valid(); err != nil {
		return err
	}
	return s.session.serial(ctx, "storage clear", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("window.%s.clear()", s.area), false, nil)
	})
}

func (s *Storage) Length(ctx context.Context) (int, error) {
	if err := s.valid(); err != nil {
		return 0, err
	}
	var length int
	err := s.session.serial(ctx, "storage length", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, fmt.Sprintf("window.%s.length", s.area), false, &length)
	})
	return length, err
}

func decodeStoredValue(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}
