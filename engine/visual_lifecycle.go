package engine

import (
	"context"
	"errors"
)

type visualLifecycle struct {
	color   bool
	frozen  bool
	emulate func(context.Context, string) error
	handler func(context.Context, string, string) (HandlerResponse, error)
}

func (s *Session) emulateVisualColor(ctx context.Context, scheme string) error {
	if s.visual.emulate != nil {
		return s.visual.emulate(ctx, scheme)
	}
	return EmulateColorSchemeContext(ctx, scheme)
}

func (s *Session) runVisualHandler(ctx context.Context, name, arg string) (HandlerResponse, error) {
	if s.visual.handler != nil {
		return s.visual.handler(ctx, name, arg)
	}
	return RunHandlerContext(ctx, name, arg)
}

func (s *Session) applyVisualColor(ctx context.Context, scheme string) error {
	s.visual.color = true
	return s.emulateVisualColor(ctx, scheme)
}

func (s *Session) clearVisualColor(ctx context.Context) error {
	if !s.visual.color {
		return nil
	}
	if err := s.emulateVisualColor(ctx, ""); err != nil {
		return err
	}
	s.visual.color = false
	return nil
}

func (s *Session) applyVisualFreeze(ctx context.Context) error {
	s.visual.frozen = true
	response, err := s.runVisualHandler(ctx, "freezeRendering", "")
	if err != nil || response.Err != "" || !response.Success {
		return handlerCallError("freeze rendering", response, err)
	}
	return nil
}

func (s *Session) clearVisualFreeze(ctx context.Context) error {
	if !s.visual.frozen {
		return nil
	}
	response, err := s.runVisualHandler(ctx, "unfreezeRendering", "")
	if err != nil || response.Err != "" || !response.Success {
		return handlerCallError("restore rendering freeze", response, err)
	}
	s.visual.frozen = false
	return nil
}

func (s *Session) cleanupVisualState(ctx context.Context, includeFreeze bool) error {
	colorErr := s.clearVisualColor(ctx)
	var freezeErr error
	if includeFreeze {
		freezeErr = s.clearVisualFreeze(ctx)
	}
	return errors.Join(colorErr, freezeErr)
}
