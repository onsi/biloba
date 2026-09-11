package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrEmptyUnreleased is returned by Prepare when `## Unreleased` has no entries: nothing to release.
var ErrEmptyUnreleased = errors.New("## Unreleased in CHANGELOG.md has no entries - nothing to release")

// The fresh section Prepare puts back at the top of the changelog.
const freshUnreleased = "## Unreleased\n\n### Features\n\n### Fixes\n\n"

var versionLine = regexp.MustCompile(`(?m)^const BILOBA_VERSION = "([^"]*)"$`)
var semver = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// CurrentVersion reads BILOBA_VERSION out of biloba.go's source.
func CurrentVersion(bilobaGo string) (string, error) {
	matches := versionLine.FindAllStringSubmatch(bilobaGo, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one `const BILOBA_VERSION = \"X.Y.Z\"` line in biloba.go, found %d", len(matches))
	}
	return matches[0][1], nil
}

// NextVersion bumps an X.Y.Z version: patch -> X.Y.(Z+1), minor -> X.(Y+1).0.
func NextVersion(current string, bump string) (string, error) {
	parts := semver.FindStringSubmatch(current)
	if parts == nil {
		return "", fmt.Errorf("BILOBA_VERSION %q is not a plain X.Y.Z version", current)
	}
	major, _ := strconv.Atoi(parts[1])
	minor, _ := strconv.Atoi(parts[2])
	patch, _ := strconv.Atoi(parts[3])
	switch bump {
	case "patch":
		return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), nil
	case "minor":
		return fmt.Sprintf("%d.%d.0", major, minor+1), nil
	}
	return "", fmt.Errorf("bump must be patch or minor, got %q", bump)
}

// SetVersion rewrites the BILOBA_VERSION line in biloba.go's source, leaving every other byte alone.
func SetVersion(bilobaGo string, version string) (string, error) {
	if _, err := CurrentVersion(bilobaGo); err != nil {
		return "", err
	}
	return versionLine.ReplaceAllLiteralString(bilobaGo, `const BILOBA_VERSION = "`+version+`"`), nil
}

// Prepare turns `## Unreleased` into `## <version>` and puts a fresh, empty `## Unreleased` above
// it.  Empty `###` subsections are dropped from the released section.  Everything before and after
// the Unreleased section is left byte-for-byte as it was.
func Prepare(changelog string, version string) (string, error) {
	before, body, after, err := splitSection(changelog, "Unreleased")
	if err != nil {
		return "", err
	}
	if isEmpty(body) {
		return "", ErrEmptyUnreleased
	}
	if _, _, _, err := splitSection(changelog, version); err == nil {
		return "", fmt.Errorf("CHANGELOG.md already has a ## %s section", version)
	}
	body = dropEmptySubsections(body)
	if !strings.HasSuffix(body, "\n\n") && after != "" {
		body = strings.TrimRight(body, "\n") + "\n\n"
	}
	return before + freshUnreleased + "## " + version + "\n" + body + after, nil
}

// Notes returns the body of the `## <version>` section, trimmed of surrounding blank lines - the
// GitHub release notes.
func Notes(changelog string, version string) (string, error) {
	_, body, _, err := splitSection(changelog, version)
	if err != nil {
		return "", err
	}
	notes := strings.Trim(body, "\n")
	if notes == "" {
		return "", fmt.Errorf("## %s in CHANGELOG.md is empty", version)
	}
	return notes + "\n", nil
}

// splitSection finds the `## <name>` heading and returns the text before the heading, the section
// body (the lines after the heading, up to the next `## ` heading or the end of the file), and the
// text from the next `## ` heading on.
func splitSection(changelog string, name string) (before string, body string, after string, err error) {
	lines := strings.SplitAfter(changelog, "\n")
	offset, start := 0, -1
	for _, line := range lines {
		if start == -1 && strings.TrimRight(line, "\n") == "## "+name {
			start = offset
			offset += len(line)
			continue
		}
		if start != -1 && strings.HasPrefix(line, "## ") {
			return changelog[:start], changelog[start+len("## "+name+"\n") : offset], changelog[offset:], nil
		}
		offset += len(line)
	}
	if start == -1 {
		return "", "", "", fmt.Errorf("CHANGELOG.md has no ## %s section", name)
	}
	bodyStart := min(start+len("## "+name+"\n"), len(changelog))
	return changelog[:start], changelog[bodyStart:], "", nil
}

// isEmpty reports whether a section body holds nothing but blank lines and `###` headings.
func isEmpty(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" && !isSubheading(line) {
			return false
		}
	}
	return true
}

func isSubheading(line string) bool {
	return strings.HasPrefix(line, "### ")
}

// dropEmptySubsections removes each `###` heading that is followed only by blank lines before the
// next `###` heading or the end of the body.
func dropEmptySubsections(body string) string {
	var chunks []string
	for _, line := range strings.SplitAfter(body, "\n") {
		if line == "" {
			continue
		}
		if isSubheading(line) || len(chunks) == 0 {
			chunks = append(chunks, line)
		} else {
			chunks[len(chunks)-1] += line
		}
	}
	var kept strings.Builder
	for _, chunk := range chunks {
		heading, rest, _ := strings.Cut(chunk, "\n")
		if isSubheading(heading) && strings.TrimSpace(rest) == "" {
			continue
		}
		kept.WriteString(chunk)
	}
	return kept.String()
}
