package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type frameWorld struct {
	id       runtime.ExecutionContextID
	uniqueID string
}

type frameWorldsKey struct{}

type frameWorlds struct {
	mu      sync.RWMutex
	byFrame map[cdp.FrameID]frameWorld
}

// trackFrameWorlds must run before the target's first chromedp.Run: Runtime.enable
// announces existing contexts then, including documents loaded before attachment.
// This registry belongs to the renderer attachment, not to any one frame handle.
func trackFrameWorlds(ctx context.Context) context.Context {
	worlds := &frameWorlds{byFrame: map[cdp.FrameID]frameWorld{}}
	chromedp.ListenTarget(ctx, func(event any) {
		switch event.(type) {
		case *runtime.EventExecutionContextCreated, *runtime.EventExecutionContextDestroyed, *runtime.EventExecutionContextsCleared:
		default:
			return
		}
		worlds.mu.Lock()
		defer worlds.mu.Unlock()
		switch event := event.(type) {
		case *runtime.EventExecutionContextCreated:
			var aux struct {
				FrameID   cdp.FrameID `json:"frameId"`
				IsDefault bool        `json:"isDefault"`
			}
			if json.Unmarshal(event.Context.AuxData, &aux) == nil && aux.IsDefault && aux.FrameID != "" {
				worlds.byFrame[aux.FrameID] = frameWorld{id: event.Context.ID, uniqueID: event.Context.UniqueID}
			}
		case *runtime.EventExecutionContextDestroyed:
			for id, world := range worlds.byFrame {
				if world.uniqueID == event.ExecutionContextUniqueID {
					delete(worlds.byFrame, id)
				}
			}
		case *runtime.EventExecutionContextsCleared:
			clear(worlds.byFrame)
		}
	})
	return context.WithValue(ctx, frameWorldsKey{}, worlds)
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

// Chrome can reject evaluation before it announces context destruction. Restrict
// this classification to protocol errors; page-thrown exceptions keep their meaning.
func frameContextGone(err error) bool {
	var protocolErr *cdproto.Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != -32000 {
		return false
	}
	switch protocolErr.Message {
	case "Inspected target navigated or closed", "Cannot find context with specified id", "Execution context was destroyed.":
		return true
	}
	return false
}
