package biloba

import (
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// These specs live in package biloba (not biloba_test) because they reflect over Biloba's unexported
// fields.  Ginkgo and Gomega are imported under their package names rather than dot-imported, which
// is all it takes to avoid colliding with biloba's own exported names.
//
// WHAT THEY GUARD.  Every lightweight view of a tab - Realistic(), WithTimeout(), WithPolling(),
// WithContext(), Immediate() - is a shallow COPY of the Biloba struct (`nb := *b`).  So a field on
// Biloba is shared with the views of that tab only if its TYPE is a reference: a pointer, map,
// channel, func, or interface.  A slice, bool, string, int, or struct field is COPIED, and then both
// directions break silently:
//
//   - a write through a view lands on the copy, so a handler registered through a view is registered
//     nowhere and never fires;
//   - a read through a view is frozen at the instant the view was made, so a list read through one
//     goes stale as the tab moves on.
//
// Neither shows up as a failure anywhere near the cause, and go vet cannot help: its copylocks check
// is what would otherwise object to `nb := *b`, and `lock` is a *sync.Mutex, which keeps it quiet.
//
// WHY THEY EXIST.  This class has been discovered four separate times.  colorSchemeEmulated (a
// *bool), probes, and occlusions were each turned into pointers on their own, with nothing at the
// call site saying why.  The fourth discovery came from outside - a consumer reporting that
// b.WithTimeout(d).HoldResponse(url) intercepted nothing while Await burned exactly the tuned
// deadline - and covered nine entry points at once: the five network registrars, the four dialog
// ones, plus stale reads and a stale bilobaIsInstalled across a navigation.
//
// The reason three local patches did not add up to a rule is worth stating, because it is what makes
// a guard the only thing that closes a class like this: each local fix REMOVES ITS OWN EVIDENCE.
// Once colorSchemeEmulated is a *bool it no longer looks like anything, and nothing marks it as an
// instance of a class - so the fourth person to hit the class starts from scratch, and the class
// outlives every repair made to it.  Written-down-and-enforced is the only form that accumulates.
//
// HOW THEY BEHAVE.  Add a field to Biloba whose type is copied and they fail until you classify it.
// That is the point: copying is occasionally right and always a decision.

// Reasons a copied field on Biloba is safe.  These are the only three, and each is a claim the next
// reader can check.
const (
	// reasonIdentity: written once, before any view of the tab can exist - during construction, or by
	// ConnectToChrome applying its options - and never again.  Every copy is therefore always equal to
	// the original, so copying it cannot diverge.
	reasonIdentity = "identity/config, set before any view exists"
	// reasonViewFlag: the field IS a view's difference from its tab.  Copying it is the whole point,
	// and sharing it would break the feature.
	reasonViewFlag = "view flag: differing per view is the point"
	// reasonRootOwned: the field lives on the ROOT tab and every access goes through b.root, which is
	// a pointer - so a view already reaches the one instance without the field itself being shared.
	reasonRootOwned = "root-owned: reached through b.root, a pointer"
)

// copiedBilobaFields classifies every Biloba field whose type a view copies rather than shares.
// Anything not listed here - anything that changes while a spec runs - belongs in tabState, which
// lives behind a pointer and so is shared with every view.
var copiedBilobaFields = map[string]string{
	"ChromeConnection":               reasonIdentity,
	"targetID":                       reasonIdentity,
	"browserContextID":               reasonIdentity,
	"downloadDir":                    reasonIdentity,
	"debugLogging":                   reasonIdentity,
	"failureScreenshots":             reasonIdentity,
	"failureOutlines":                reasonIdentity,
	"failureOutlinesSet":             reasonIdentity,
	"progressReportScreenshots":      reasonIdentity,
	"inlineScreenshots":              reasonIdentity,
	"inlineScreenshotsSet":           reasonIdentity,
	"failureScreenshotWidth":         reasonIdentity,
	"failureScreenshotHeight":        reasonIdentity,
	"progressReportScreenshotWidth":  reasonIdentity,
	"progressReportScreenshotHeight": reasonIdentity,
	"screenshotsDir":                 reasonIdentity,
	"baselinesDir":                   reasonIdentity,
	"screenshotTolerance":            reasonIdentity,
	"updateScreenshots":              reasonIdentity,
	"pollTrajectory":                 reasonIdentity,

	"realistic": reasonViewFlag,
	"immediate": reasonViewFlag,
}

// sharedWithEveryView reports whether a field of this type is reached, rather than copied, through a
// shallow copy of the struct that holds it.  Note that a SLICE is not: `append` through a copy updates
// the copy's own length, and a read through a copy sees the length frozen when the copy was made.
func sharedWithEveryView(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
		return true
	}
	return false
}

var _ = ginkgo.Describe("what a view of a tab shares with it", ginkgo.Label("no-browser"), func() {
	bilobaType := reflect.TypeOf((*Biloba)(nil)).Elem()

	// copiedFields is every field a view gets its own copy of, by name.
	copiedFields := func() map[string]reflect.StructField {
		out := map[string]reflect.StructField{}
		for i := range bilobaType.NumField() {
			if field := bilobaType.Field(i); !sharedWithEveryView(field.Type) {
				out[field.Name] = field
			}
		}
		return out
	}

	ginkgo.It("classifies every field a view copies rather than shares", func() {
		for name, field := range copiedFields() {
			gomega.Expect(copiedBilobaFields).To(gomega.HaveKey(name), `Biloba.%s is a %s (%s), so every
lightweight view of a tab - Realistic(), WithTimeout(), Immediate(), ... - gets its own COPY of it.

If it is state that changes while a spec runs, move it into tabState.  Otherwise a write through a
view lands on the copy and is lost, and a read through a view is frozen at the moment the view was
made.  Both fail silently, and go vet cannot catch either.

If copying it really is correct, add it to copiedBilobaFields with the reason - reasonIdentity,
reasonViewFlag, or reasonRootOwned.  See the note at the top of this file.`,
				name, field.Type.Kind(), field.Type)
		}
	})

	ginkgo.It("does not classify a field that every view already shares", func() {
		copied := copiedFields()
		for name := range copiedBilobaFields {
			field, ok := bilobaType.FieldByName(name)
			gomega.Expect(ok).To(gomega.BeTrue(),
				"copiedBilobaFields classifies %q, but Biloba has no field by that name.  It was renamed, "+
					"removed, or moved into tabState - drop the entry.", name)
			gomega.Expect(copied).To(gomega.HaveKey(name),
				"copiedBilobaFields classifies Biloba.%s, but it is a %s, which every view already shares - "+
					"so it needs no classification.  Drop the entry.", name, field.Type.Kind())
		}
	})

	// The reasons are claims a reader can check, not decoration: an entry with an unrecognised reason
	// is one nobody has actually justified.
	ginkgo.It("gives every copied field one of the three recognised reasons", func() {
		for name, reason := range copiedBilobaFields {
			gomega.Expect(reason).To(gomega.BeElementOf(reasonIdentity, reasonViewFlag, reasonRootOwned),
				"Biloba.%s is classified with a reason that is not one of the three.  Use reasonIdentity, "+
					"reasonViewFlag, or reasonRootOwned - or, if none of them is true, the field belongs in "+
					"tabState.", name)
		}
	})

	// tabState is only shared because Biloba holds it by POINTER.  A refactor that made it a value
	// field would put every field it protects straight back into the copied category, without a single
	// call site changing - so nothing else would notice.
	ginkgo.It("holds the tab's own state by pointer", func() {
		field, ok := bilobaType.FieldByName("state")
		gomega.Expect(ok).To(gomega.BeTrue(), "Biloba should hold its tab state in a field named state")
		gomega.Expect(field.Type.Kind()).To(gomega.Equal(reflect.Pointer),
			"Biloba.state must be a *tabState.  As a value it would be copied by every view, which is "+
				"exactly what tabState exists to prevent.")
	})
})
