package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
)

func (s *Session) Title(ctx context.Context) (string, error) {
	var title string
	err := s.serial(ctx, "read title", func(opCtx context.Context) error {
		var readErr error
		title, readErr = TitleContext(opCtx)
		return readErr
	})
	return title, err
}

func (s *Session) WindowSize(ctx context.Context) (int, int, error) {
	var width, height int64
	err := s.serial(ctx, "read window size", func(opCtx context.Context) error {
		var readErr error
		width, height, readErr = ViewportDimensionsContext(opCtx)
		return readErr
	})
	return int(width), int(height), err
}

func (s *Session) Outline(ctx context.Context) (string, error) {
	var outline string
	err := s.serial(ctx, "capture DOM outline", func(opCtx context.Context) error {
		response, runErr := s.runHandler(opCtx, "outline", Selector{})
		if runErr != nil {
			return runErr
		}
		outline, _ = response.Result.(string)
		return nil
	})
	return capOutline(outline), err
}

func (s *Session) AccessibilityOutline(ctx context.Context) (string, error) {
	var nodes []*accessibility.Node
	err := s.serial(ctx, "capture accessibility outline", func(opCtx context.Context) error {
		var readErr error
		nodes, readErr = AccessibilityTreeContext(opCtx)
		return readErr
	})
	if err != nil {
		return "", err
	}
	return capOutline(renderAccessibilityTree(nodes)), nil
}

func renderAccessibilityTree(nodes []*accessibility.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	byID := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	root := nodes[0]
	for _, node := range nodes {
		if node.ParentID == "" || byID[node.ParentID] == nil {
			root = node
			break
		}
	}
	out := &strings.Builder{}
	var walk func(*accessibility.Node, int)
	walk = func(node *accessibility.Node, depth int) {
		nextDepth := depth
		role := accessibilityValue(node.Role)
		if !node.Ignored && role != "InlineTextBox" {
			if role == "" {
				role = "none"
			}
			fmt.Fprintf(out, "%s%s", strings.Repeat("  ", depth), role)
			if name := accessibilityValue(node.Name); name != "" {
				fmt.Fprintf(out, " %q", name)
			}
			if value := accessibilityValue(node.Value); value != "" {
				fmt.Fprintf(out, " (value: %q)", value)
			}
			out.WriteByte('\n')
			nextDepth++
		}
		for _, id := range node.ChildIDs {
			if child := byID[id]; child != nil {
				walk(child, nextDepth)
			}
		}
	}
	walk(root, 0)
	return out.String()
}

func accessibilityValue(value *accessibility.Value) string {
	if value == nil {
		return ""
	}
	var text string
	_ = json.Unmarshal(value.Value, &text)
	return text
}
