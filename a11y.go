package biloba

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/chromedp"
)

/*
A11yOutline() returns the page's accessibility tree as indented text: one line per node,
showing each node's ARIA role and accessible name (e.g. `button "Submit"`).  Nodes that
are ignored for accessibility are omitted, while their meaningful descendants are kept - so
the result is the compact, role/name-oriented view of the page that a screen reader (and,
increasingly, a reasoning model) works from.

It complements [Biloba.Outline]: where Outline shows the raw DOM, A11yOutline elides
presentational noise and surfaces semantics.  The output is capped at ~32 KB.

Read https://onsi.github.io/biloba/#accessibility-outline for details.
*/
func (b *Biloba) A11yOutline() string {
	b.gt.Helper()
	b.guardConfig("A11yOutline")
	text, err := b.a11yOutline()
	if err != nil {
		b.gt.Fatalf("Failed to capture accessibility outline:\n%s", err.Error())
		return ""
	}
	return capOutline(text)
}

func (b *Biloba) a11yOutline() (string, error) {
	var nodes []*accessibility.Node
	err := chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		nodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	}))
	if err != nil {
		return "", err
	}
	return renderA11yTree(nodes), nil
}

func renderA11yTree(nodes []*accessibility.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	byID := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, n := range nodes {
		byID[n.NodeID] = n
	}
	// The root is the first node whose parent we don't have a record of (getFullAXTree
	// returns the tree root first, but we don't rely on ordering).
	var root *accessibility.Node
	for _, n := range nodes {
		if n.ParentID == "" || byID[n.ParentID] == nil {
			root = n
			break
		}
	}
	if root == nil {
		root = nodes[0]
	}

	out := &strings.Builder{}
	var walk func(n *accessibility.Node, depth int)
	walk = func(n *accessibility.Node, depth int) {
		// Ignored nodes (and InlineTextBox nodes, which just mirror their StaticText parent)
		// contribute no semantics, so skip the node itself but keep descending into its
		// children at the same depth - this flattens away presentational wrappers.
		nextDepth := depth
		if !n.Ignored && axValueString(n.Role) != "InlineTextBox" {
			fmt.Fprintf(out, "%s%s\n", strings.Repeat("  ", depth), a11yLine(n))
			nextDepth = depth + 1
		}
		for _, childID := range n.ChildIDs {
			if child := byID[childID]; child != nil {
				walk(child, nextDepth)
			}
		}
	}
	walk(root, 0)
	return out.String()
}

func a11yLine(n *accessibility.Node) string {
	role := axValueString(n.Role)
	if role == "" {
		role = "none"
	}
	line := role
	if name := axValueString(n.Name); name != "" {
		line += fmt.Sprintf(" %q", name)
	}
	if value := axValueString(n.Value); value != "" {
		line += fmt.Sprintf(" (value: %q)", value)
	}
	return line
}

func axValueString(v *accessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(v.Value), &decoded); err != nil {
		return ""
	}
	switch d := decoded.(type) {
	case nil:
		return ""
	case string:
		return d
	default:
		return fmt.Sprint(d)
	}
}
