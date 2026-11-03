package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type frameWorld struct {
	id       runtime.ExecutionContextID
	uniqueID string
}

type frameWorldsKey struct{}

/*
frameWorlds is the frame-world registry of one session: the page JavaScript context of each frame
document, as the session announces them.  It belongs to the renderer attachment, not to any one frame
handle.

The event hub keeps one for every session, from the session's first message, and updates it before
any listener runs (see eventHub.dispatch).  So it sees every context the session announces, the ones
Chrome reports when chromedp enables Runtime during its attach included, without asking Chrome to
announce them again.
*/
type frameWorlds struct {
	mu      sync.RWMutex
	byFrame map[cdp.FrameID]frameWorld
}

func newFrameWorlds() *frameWorlds {
	return &frameWorlds{byFrame: map[cdp.FrameID]frameWorld{}}
}

// frameWorldEvent reports whether a method changes the frame-world registry.
func frameWorldEvent(method string) bool {
	switch method {
	case runtime.ExecutionContextCreated.Method, runtime.ExecutionContextDestroyed.Method, runtime.ExecutionContextsCleared.Method:
		return true
	}
	return false
}

// observe applies one context event, from its undecoded params, to the registry.
func (w *frameWorlds) observe(method string, params jsonv2.Value) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch method {
	case runtime.ExecutionContextCreated.Method:
		var created struct {
			Context struct {
				ID       runtime.ExecutionContextID `json:"id"`
				UniqueID string                     `json:"uniqueId"`
				AuxData  struct {
					FrameID   cdp.FrameID `json:"frameId"`
					IsDefault bool        `json:"isDefault"`
				} `json:"auxData"`
			} `json:"context"`
		}
		if jsonv2.Unmarshal(params, &created, chromedp.DefaultUnmarshalOptions) != nil {
			return
		}
		if aux := created.Context.AuxData; aux.IsDefault && aux.FrameID != "" {
			w.byFrame[aux.FrameID] = frameWorld{id: created.Context.ID, uniqueID: created.Context.UniqueID}
		}
	case runtime.ExecutionContextDestroyed.Method:
		var destroyed struct {
			UniqueID string `json:"executionContextUniqueId"`
		}
		if jsonv2.Unmarshal(params, &destroyed, chromedp.DefaultUnmarshalOptions) != nil {
			return
		}
		for id, world := range w.byFrame {
			if world.uniqueID == destroyed.UniqueID {
				delete(w.byFrame, id)
			}
		}
	case runtime.ExecutionContextsCleared.Method:
		clear(w.byFrame)
	}
}

// trackFrameWorlds gives an attached target's context its session's frame-world registry, which the
// event hub has kept since the session's first message.  It sends no command.  Call it after the
// first Run on ctx, on the goroutine that ran it or after it, since that Run sets the context's
// target.
func trackFrameWorlds(ctx context.Context) (context.Context, error) {
	hub, c, err := hubFor(ctx)
	if err != nil {
		return nil, err
	}
	if c.Target == nil {
		return nil, errors.New("tracking frame worlds needs an attached target: run the context once first")
	}
	return context.WithValue(ctx, frameWorldsKey{}, hub.worldsFor(c.Target.SessionID)), nil
}

func mainFrameWorld(ctx context.Context, id cdp.FrameID) (frameWorld, error) {
	worlds, _ := ctx.Value(frameWorldsKey{}).(*frameWorlds)
	if worlds != nil {
		worlds.mu.RLock()
		world, found := worlds.byFrame[id]
		worlds.mu.RUnlock()
		if found && world.id != 0 && world.uniqueID != "" {
			return world, nil
		}
	}
	// Creation/navigation can briefly expose a frame before its context exists.
	// Never fall back to the target's default context, which could be the parent.
	return frameWorld{}, &Error{Code: CodeConditionNotMet, Operation: "attach frame", Message: "frame's page execution context is not available yet"}
}

// frameWorldCurrent compares document identity using the registry maintained by CDP events.
// It does not send a command to a potentially busy renderer.
func frameWorldCurrent(ctx context.Context, id cdp.FrameID, expected frameWorld) bool {
	world, err := mainFrameWorld(ctx, id)
	return ctx.Err() == nil && expected.uniqueID != "" && err == nil && world.uniqueID == expected.uniqueID
}

// Chrome can reject evaluation before it announces context destruction. Restrict
// this classification to protocol errors; page-thrown exceptions keep their meaning.
func frameContextGone(err error) bool {
	var protocolErr *cdproto.Error
	if !errors.As(err, &protocolErr) {
		return false
	}
	switch {
	case protocolErr.Code == -32000:
		switch protocolErr.Message {
		case "Inspected target navigated or closed", "Cannot find context with specified id", "Execution context was destroyed.":
			return true
		}
	case protocolErr.Code == -32602:
		// An evaluation pinned by uniqueContextId to a document that has since been replaced.
		return protocolErr.Message == "uniqueContextId not found"
	}
	return false
}
