package footer

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

// shown joins spans into the footer text and its linked text.
func shown(spans []Span) (text, linked string) {
	for _, span := range spans {
		text += span.Text
		if span.URL != "" {
			linked += span.Text
		}
	}
	return text, linked
}

func items(texts ...string) protocol.PluginFooter {
	footer := protocol.PluginFooter{}
	for _, text := range texts {
		footer.Items = append(footer.Items, protocol.PluginFooterItem{Content: []protocol.PluginFooterSegment{{Text: text}}})
	}
	return footer
}

func TestVCSLocation(t *testing.T) {
	const oid = "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name   string
		status *protocol.VCSStatus
		want   string
		link   string
	}{
		{name: "unavailable", want: "~/repo"},
		{name: "clean branch", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}}, want: "~/repo (main)"},
		{name: "dirty branch", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "feature"}, Dirty: true}, want: "~/repo (feature*)"},
		{name: "unborn", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadUnborn, Name: "main"}, Dirty: true}, want: "~/repo (main*)"},
		{name: "detached", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadDetached, OID: oid}}, want: "~/repo (detached@0123456)"},
		{name: "branch with pull request", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 123, URL: "https://github.com/akonwi/kit/pull/123"},
		}, want: "~/repo (main · PR #123)", link: "https://github.com/akonwi/kit/pull/123"},
		{name: "dirty branch with pull request", status: &protocol.VCSStatus{
			Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "feature"}, Dirty: true,
			PullRequest: &protocol.GitHubPullRequest{Number: 7, URL: "http://github.internal/pull/7"},
		}, want: "~/repo (feature* · PR #7)", link: "http://github.internal/pull/7"},
		{name: "non-positive pull request number", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 0, URL: "https://github.com/akonwi/kit/pull/5"},
		}, want: "~/repo (main)"},
		{name: "detached head suppresses pull request", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadDetached, OID: oid},
			PullRequest: &protocol.GitHubPullRequest{Number: 9, URL: "https://github.com/akonwi/kit/pull/9"},
		}, want: "~/repo (detached@0123456)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			location := VCSLocation("~/repo", test.status)
			if location.Text != test.want || location.Base != "~/repo" || location.URL != test.link {
				t.Fatalf("location = %+v, want %q linking %q", location, test.want, test.link)
			}
		})
	}
}

func TestVCSLocationOmitsUnsafePullRequests(t *testing.T) {
	for _, unsafe := range []string{
		"",
		"/pull/42",
		"javascript:alert(1)",
		"mailto:pr@example.com",
		"https://user:secret@github.com/pull/42",
		"https://github.com/pull/42\n",
		"https://github.com/pull/\x1b42",
	} {
		location := VCSLocation("~/repo", &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 42, URL: unsafe},
		})
		if location.Text != "~/repo (main)" || location.URL != "" || location.LinkText != "" {
			t.Errorf("URL %q: location = %+v", unsafe, location)
		}
	}
}

func TestItemsSitBesideTheLocationKeepingTheirStyle(t *testing.T) {
	footer := protocol.PluginFooter{Items: []protocol.PluginFooterItem{{ID: "demo.status", Content: []protocol.PluginFooterSegment{{Text: "Ready", Style: protocol.PluginFooterStyle{Bold: true, FG: "toolText"}}}}}}
	location := Location{Text: "~/project"}
	spans := Spans(footer, location, 40)
	if text, _ := shown(spans); text != "~/project · Ready" {
		t.Fatalf("footer = %q", text)
	}
	last := spans[len(spans)-1]
	if !last.Item || !last.Style.Bold || last.Style.FG != "toolText" || spans[0].Item {
		t.Fatalf("spans = %+v", spans)
	}
	footer.LocationHidden = true
	if text, _ := shown(Spans(footer, location, 40)); text != "Ready" {
		t.Fatalf("replacement = %q", text)
	}
	if text, _ := shown(Spans(protocol.PluginFooter{}, location, 40)); text != "~/project" {
		t.Fatalf("restored = %q", text)
	}
	if spans := Spans(protocol.PluginFooter{LocationHidden: true}, location, 40); len(spans) != 0 {
		t.Fatalf("hidden location without items = %+v", spans)
	}
}

func TestItemsThatDoNotFitAreCounted(t *testing.T) {
	footer := items(strings.Repeat("long", 10), "Second")
	footer.LocationHidden = true
	if text, _ := shown(Spans(footer, Location{}, 20)); text != "… 2 more" {
		t.Fatalf("overflow = %q", text)
	}
	if text, _ := shown(Spans(items(strings.Repeat("x", 20)), Location{Text: "~/very-long-project"}, 16)); text != "…ject · … 1 more" {
		t.Fatalf("reserved overflow label = %q", text)
	}
}

func TestOnlyThePullRequestLabelIsLinked(t *testing.T) {
	const target = "https://github.com/akonwi/kit/pull/123"
	location := Location{Text: "~/repo (main* · PR #123)", LinkText: "PR #123", URL: target}
	spans := Spans(protocol.PluginFooter{}, location, 40)
	if text, linked := shown(spans); text != location.Text || linked != "PR #123" {
		t.Fatalf("text %q linked %q", text, linked)
	}
	for _, span := range spans {
		if span.URL != "" && span.URL != target {
			t.Fatalf("span %+v links elsewhere", span)
		}
	}
	if _, linked := shown(Spans(protocol.PluginFooter{}, Location{Text: "~/repo (main)"}, 40)); linked != "" {
		t.Fatalf("a location without a pull request links %q", linked)
	}
	if spans := Spans(protocol.PluginFooter{LocationHidden: true}, location, 40); len(spans) != 0 {
		t.Fatalf("a hidden location shows %+v", spans)
	}
}

func TestThePullRequestLinkSitsBesideItems(t *testing.T) {
	location := Location{Text: "~/repo (main · PR #8)", LinkText: "PR #8", URL: "https://github.com/akonwi/kit/pull/8"}
	text, linked := shown(Spans(items("Ready"), location, 60))
	if text != "~/repo (main · PR #8) · Ready" || linked != "PR #8" {
		t.Fatalf("text %q linked %q", text, linked)
	}
}

func TestOnlyACompleteLabelIsLinked(t *testing.T) {
	location := Location{LinkText: "PR #123", URL: "https://github.com/a/b/pull/123"}
	for _, test := range []struct {
		fitted string
		linked bool
	}{
		{"…/目录 (main · PR #123)", true},
		{"… PR #123)", true},
		{"…R #123)", false},
		{"~/PR #123/repo (main)", false},
	} {
		text, linked := shown(locationSpans(test.fitted, location))
		want := ""
		if test.linked {
			want = "PR #123"
		}
		if text != test.fitted || linked != want {
			t.Fatalf("fitted=%q text=%q link=%q want=%q", test.fitted, text, linked, want)
		}
	}
}

func TestNarrowWidthsKeepCompleteGitStatus(t *testing.T) {
	const directory = "~/very/long/project"
	location := Location{Text: directory + " (feature* · PR #8)", Base: directory, LinkText: "PR #8", URL: "https://github.com/a/b/pull/8"}
	for _, test := range []struct {
		name   string
		width  int
		footer protocol.PluginFooter
		want   string
	}{
		{name: "no plugins", width: 28, want: "…/project (feature* · PR #8)"},
		{name: "one plugin", width: 32, footer: items("Ready"), want: "…ject (feature* · PR #8) · Ready"},
		{name: "overflow", width: 29, footer: items("Very long status", "Second"), want: "(feature* · PR #8) · … 2 more"},
		{name: "suffix only", width: 18, want: "(feature* · PR #8)"},
		{name: "PR only", width: 7, want: "(PR #8)"},
		{name: "too narrow for PR", width: 6, want: "…"},
		{name: "hidden", width: 32, footer: protocol.PluginFooter{LocationHidden: true}, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, linked := shown(Spans(test.footer, location, test.width))
			if text != test.want {
				t.Fatalf("footer = %q, want %q", text, test.want)
			}
			if strings.Contains(test.want, "PR #8") && linked != "PR #8" {
				t.Fatalf("linked = %q", linked)
			}
		})
	}
}

func TestWideDirectoriesAreShortenedByCellsBeforeGitStatus(t *testing.T) {
	const directory = "~/世界/研究/プロジェクト"
	location := Location{Text: directory + " (界界* · PR #42)", Base: directory, LinkText: "PR #42", URL: "https://github.com/a/b/pull/42"}
	if text, _ := shown(Spans(protocol.PluginFooter{}, location, 28)); text != "…ロジェクト (界界* · PR #42)" {
		t.Fatalf("footer = %q", text)
	}
}

func TestABranchWithoutPullRequestStaysWhole(t *testing.T) {
	const directory = "~/deep/path/to/worktree"
	location := Location{Text: directory + " (feature-branch*)", Base: directory}
	for _, test := range []struct {
		width int
		want  string
	}{
		{20, "…e (feature-branch*)"},
		{17, "(feature-branch*)"},
		{16, "…ath/to/worktree"},
	} {
		if text, _ := shown(Spans(protocol.PluginFooter{}, location, test.width)); text != test.want {
			t.Fatalf("width %d: footer = %q, want %q", test.width, text, test.want)
		}
	}
}
