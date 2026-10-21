// Package selfupdate finds newer Kit releases on GitHub and installs the
// terminal executable from them (ADR 0048).
//
// It never touches a server: callers decide whether a local server should be
// restarted after an executable is installed.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Repository is the GitHub repository Kit releases are published in.
const Repository = "akonwi/kit"

// DefaultAPIURL is GitHub's REST API origin.
const DefaultAPIURL = "https://api.github.com"

// ReleasesURL is the page listing every Kit release.
const ReleasesURL = "https://github.com/" + Repository + "/releases"

// CheckInterval is how often a running client checks for a newer release.
const CheckInterval = 6 * time.Hour

// maxReleaseListBytes bounds the release list a check reads.
const maxReleaseListBytes = 8 << 20

// Artifact selects which of Kit's independently versioned releases a check
// considers: the terminal executable's `vX.Y.Z` tags or the macOS app's
// `macos-vX.Y.Z` tags.
type Artifact string

const (
	// ArtifactCLI is the `kit` executable, tagged vX.Y.Z.
	ArtifactCLI Artifact = "v"
	// ArtifactMacOS is the macOS app, tagged macos-vX.Y.Z.
	ArtifactMacOS Artifact = "macos-v"
)

// Version is a release version, compared numerically.
type Version struct {
	Major, Minor, Patch int
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ParseVersion parses a release version such as "0.48.0". A leading "v" is
// accepted. Prerelease and build suffixes are not release versions.
func ParseVersion(value string) (Version, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimPrefix(value, "v"))
	if match == nil {
		return Version{}, false
	}
	var parts [3]int
	for index := range parts {
		number, err := strconv.Atoi(match[index+1])
		if err != nil {
			return Version{}, false
		}
		parts[index] = number
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, true
}

// Compare returns -1, 0, or 1 as v is older than, equal to, or newer than other.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}
	return 0
}

// String formats v as "X.Y.Z".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Release is a published Kit release.
type Release struct {
	// Tag is the release's Git tag, such as "v0.49.0".
	Tag     string
	Version Version
	// Notes is the release's Markdown body.
	Notes string
	// URL is the release's page on GitHub.
	URL    string
	Assets []Asset
}

// Asset is a file attached to a release.
type Asset struct {
	Name string
	// URL downloads the asset.
	URL  string
	Size int64
	// SHA256 is the lowercase hex digest GitHub reports for the asset, or
	// empty when GitHub reports none.
	SHA256 string
}

// Asset returns the release's asset named name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

// Summary is the first paragraph of the release notes after their title, as
// plain text joined onto one line: the release's one-paragraph overview.
func (r Release) Summary() string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(r.Notes, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" && len(lines) > 0:
			return strings.Join(lines, " ")
		case trimmed == "", strings.HasPrefix(trimmed, "#"):
			continue
		default:
			lines = append(lines, printable(trimmed))
		}
	}
	return strings.Join(lines, " ")
}

// printable replaces control characters, such as terminal escapes, with
// spaces so text from GitHub cannot steer the terminal it is printed to.
func printable(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

// ReleaseURL is the GitHub page of the release with tag, or the releases list
// when tag is empty.
func ReleaseURL(tag string) string {
	if tag == "" {
		return ReleasesURL
	}
	return ReleasesURL + "/tag/" + tag
}

// CurrentReleaseURL is the GitHub page of the running release of artifact, or
// the releases list for a build without a release version.
func CurrentReleaseURL(artifact Artifact, current, channel string) string {
	parsed, ok := ParseVersion(current)
	if !ok || channel != "" {
		return ReleasesURL
	}
	return ReleaseURL(string(artifact) + parsed.String())
}

// Checker reads Kit's releases from GitHub.
type Checker struct {
	// HTTP performs requests; nil uses a client with a 30 second timeout.
	HTTP *http.Client
	// APIURL is the GitHub API origin; empty uses DefaultAPIURL.
	APIURL string
}

// ErrNotRelease reports a build without a release version, which never
// updates: a development build or one bound to a channel.
var ErrNotRelease = errors.New("this build has no release version")

// Current returns the running build's release version, or ErrNotRelease for
// a development or channel-bound build.
func Current(version, channel string) (Version, error) {
	parsed, ok := ParseVersion(version)
	if !ok || channel != "" {
		return Version{}, ErrNotRelease
	}
	return parsed, nil
}

// Latest returns the newest published release of artifact. Drafts,
// prereleases, and the other artifact's tags are ignored.
func (c Checker) Latest(ctx context.Context, artifact Artifact) (Release, error) {
	origin := c.APIURL
	if origin == "" {
		origin = DefaultAPIURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(origin, "/")+"/repos/"+Repository+"/releases?per_page=50", nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := c.client().Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("list releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("list releases: GitHub responded %s", response.Status)
	}
	var listed []githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReleaseListBytes)).Decode(&listed); err != nil {
		return Release{}, fmt.Errorf("decode releases: %w", err)
	}
	var newest Release
	found := false
	for _, candidate := range listed {
		if candidate.Draft || candidate.Prerelease {
			continue
		}
		parsed, ok := artifact.version(candidate.TagName)
		if !ok {
			continue
		}
		if found && parsed.Compare(newest.Version) <= 0 {
			continue
		}
		newest = candidate.release(parsed)
		found = true
	}
	if !found {
		return Release{}, fmt.Errorf("no published %s release", artifact.describe())
	}
	return newest, nil
}

// Newer returns the newest release of artifact when it is newer than
// current, or false when current is up to date.
func (c Checker) Newer(ctx context.Context, artifact Artifact, current Version) (Release, bool, error) {
	latest, err := c.Latest(ctx, artifact)
	if err != nil {
		return Release{}, false, err
	}
	return latest, latest.Version.Compare(current) > 0, nil
}

func (c Checker) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (a Artifact) version(tag string) (Version, bool) {
	rest, ok := strings.CutPrefix(tag, string(a))
	if !ok || strings.HasPrefix(rest, "v") {
		return Version{}, false
	}
	return ParseVersion(rest)
}

func (a Artifact) describe() string {
	if a == ArtifactMacOS {
		return "macOS app"
	}
	return "Kit"
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Body       string        `json:"body"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

func (r githubRelease) release(parsed Version) Release {
	// The page is built from the tag rather than taken from the response, so
	// a client only ever opens Kit's own release pages.
	release := Release{Tag: r.TagName, Version: parsed, Notes: r.Body, URL: ReleaseURL(r.TagName)}
	for _, asset := range r.Assets {
		digest := ""
		if hex, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok {
			digest = strings.ToLower(hex)
		}
		release.Assets = append(release.Assets, Asset{
			Name: asset.Name, URL: asset.BrowserDownloadURL, Size: asset.Size, SHA256: digest,
		})
	}
	return release
}
