package biloba

/*
ArtifactKind names what sort of file Biloba wrote.  See [Artifact].
*/
type ArtifactKind string

const (
	// ScreenshotArtifact is a PNG written by [Biloba.CaptureScreenshotToFile], by the
	// on-failure screenshots, or by a progress report.
	ScreenshotArtifact ArtifactKind = "screenshot"
	// VisualActualArtifact is the <name>.actual.png a failed (or missing-baseline)
	// [Biloba.HaveScreenshot] wrote.
	VisualActualArtifact ArtifactKind = "visual-actual"
	// VisualDiffArtifact is the <name>.diff.png a failed [Biloba.HaveScreenshot] wrote.
	VisualDiffArtifact ArtifactKind = "visual-diff"
	// VisualBaselineArtifact is a baseline PNG written under BILOBA_UPDATE_SCREENSHOTS.
	VisualBaselineArtifact ArtifactKind = "visual-baseline"
)

/*
An Artifact is a file Biloba wrote to disk during the current spec.  Get them with [Biloba.Artifacts].

Read https://onsi.github.io/biloba/#collecting-the-files-biloba-wrote to learn more
*/
type Artifact struct {
	// Kind is what sort of file this is.
	Kind ArtifactKind
	// Path is the absolute path to the file.
	Path string
	// Label is the tab title for a failure screenshot and the baseline label for a visual
	// artifact.  It is empty for a screenshot you asked for by path.
	Label string
}

/*
Artifacts returns the files Biloba has written to disk so far during the current spec: failure screenshots, the actual/diff PNGs of a failed [Biloba.HaveScreenshot], baselines written under BILOBA_UPDATE_SCREENSHOTS, and any [Biloba.CaptureScreenshotToFile] you asked for.  Biloba also announces every one of these in the test output; Artifacts is the same information as data, for a reporter that wants to upload the files rather than print their paths.

The list is cleared by [Biloba.Prepare], so it always describes the current spec.  Note that the on-failure screenshots are written by a cleanup, so a complete list is only available after that cleanup has run - which in Ginkgo means a ReportAfterEach:

	ReportAfterEach(func(report SpecReport) {
		for _, artifact := range b.Artifacts() {
			uploadToCIArtifactStore(artifact.Path)
		}
	})

Artifacts is a snapshot: it does not poll, and it rejects the poll-config knobs.

Read https://onsi.github.io/biloba/#collecting-the-files-biloba-wrote to learn more
*/
func (b *Biloba) Artifacts() []Artifact {
	b.guardConfig("Artifacts")
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	out := make([]Artifact, len(root.artifacts))
	copy(out, root.artifacts)
	return out
}

// recordArtifact appends to the root tab's per-spec artifact list.  Every site that writes a file
// calls this, including the ones that write mid-spec from inside a matcher - a list that only knew
// about the failure cleanup would silently omit the visual-regression artifacts, whose filenames
// (unlike the failure screenshots') carry no spec component for a consumer to scan for.
func (b *Biloba) recordArtifact(kind ArtifactKind, path string, label string) {
	if path == "" {
		return
	}
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	root.artifacts = append(root.artifacts, Artifact{Kind: kind, Path: path, Label: label})
}

// resetArtifacts drops the previous spec's list.  Called by Prepare, and deliberately NOT by
// attachFailureArtifactsIfFailed: a consumer's read window is after that cleanup runs, and a
// cleanup that populated-then-cleared would hand back an empty list indistinguishable from a
// spec that wrote nothing.
func (b *Biloba) resetArtifacts() {
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	root.artifacts = nil
}
