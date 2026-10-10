package githubpr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/pullrequest"
)

func TestParseRichPullRequestStatus(t *testing.T) {
	raw := []byte(`{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main","headRefOid":"0123456789abcdef0123456789abcdef01234567","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","reviewDecision":"APPROVED","statusCheckRollup":[{"__typename":"CheckRun","name":"test","workflowName":"CI","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"StatusContext","context":"lint","state":"SUCCESS"}],"latestReviews":[{"author":{"login":"reviewer"},"state":"APPROVED"}]}`)
	required := []byte(`[{"bucket":"pass","name":"test","state":"SUCCESS","workflow":"CI"}]`)
	got := parseObservation(raw, required, "main")
	if got == nil || got.Readiness != "ready" || got.RequiredChecks != "satisfied" || got.RequiredApprovals != "satisfied" || len(got.Checks) != 2 || !got.Checks[0].Required || got.Checks[0].Name != "test" || len(got.Reviews) != 1 {
		t.Fatalf("parsed=%+v", got)
	}
	pending := strings.Replace(string(raw), `"mergeStateStatus":"CLEAN"`, `"mergeStateStatus":"BLOCKED"`, 1)
	pending = strings.Replace(pending, `"reviewDecision":"APPROVED"`, `"reviewDecision":"REVIEW_REQUIRED"`, 1)
	got = parseObservation([]byte(pending), []byte(`[{"bucket":"pending","name":"test","state":"PENDING","workflow":"CI"}]`), "main")
	if got == nil || got.Readiness != "blocked" || got.RequiredChecks != "pending" || got.RequiredApprovals != "pending" {
		t.Fatalf("pending=%+v", got)
	}
}

func TestCanonicalCheckStates(t *testing.T) {
	cases := []struct {
		value viewCheck
		want  pullrequest.CheckState
	}{
		{viewCheck{Status: "QUEUED"}, pullrequest.CheckPending},
		{viewCheck{Status: "COMPLETED", Conclusion: "SUCCESS"}, pullrequest.CheckPassed},
		{viewCheck{Status: "COMPLETED", Conclusion: "FAILURE"}, pullrequest.CheckFailed},
		{viewCheck{Status: "COMPLETED", Conclusion: "NEUTRAL"}, pullrequest.CheckSkipped},
		{viewCheck{Status: "COMPLETED", Conclusion: "SKIPPED"}, pullrequest.CheckSkipped},
		{viewCheck{Status: "COMPLETED", Conclusion: "CANCELLED"}, pullrequest.CheckCanceled},
		{viewCheck{Status: "COMPLETED", Conclusion: ""}, pullrequest.CheckUnknown},
	}
	for _, test := range cases {
		if got := checkState(test.value); got != test.want {
			t.Errorf("checkState(%+v)=%q, want %q", test.value, got, test.want)
		}
	}
}

func TestReviewDecisionAndDismissalProjection(t *testing.T) {
	base := `{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main","state":"OPEN","mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":%q,"latestReviews":[{"author":{"login":"reviewer"},"state":%q}]}`
	for _, test := range []struct {
		decision, review string
		want             pullrequest.RequirementState
	}{
		{"APPROVED", "APPROVED", pullrequest.RequirementSatisfied},
		{"CHANGES_REQUESTED", "CHANGES_REQUESTED", pullrequest.RequirementUnsatisfied},
		{"REVIEW_REQUIRED", "DISMISSED", pullrequest.RequirementPending},
	} {
		got := parseObservation([]byte(fmt.Sprintf(base, test.decision, test.review)), []byte(`[]`), "main")
		if got == nil || got.RequiredApprovals != test.want || len(got.Reviews) != 1 || got.Reviews[0].State != test.review {
			t.Errorf("decision=%s review=%s parsed=%+v", test.decision, test.review, got)
		}
	}
	deletedAuthor := `{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main","state":"OPEN","latestReviews":[{"author":null,"state":"APPROVED"}]}`
	if got := parseObservation([]byte(deletedAuthor), nil, "main"); got == nil || len(got.Reviews) != 0 {
		t.Fatalf("deleted review author parsed=%+v", got)
	}
}

func TestParsePullRequest(t *testing.T) {
	good := `{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}`
	if got := parse([]byte(good), "main"); got == nil || got.Number != 42 || got.URL != "https://github.com/a/b/pull/42" {
		t.Fatalf("parsed=%v", got)
	}
	for _, raw := range []string{`null`, `{}`, good + `{}`, strings.Replace(good, `42,`, `0,`, 1), strings.Replace(good, `42,`, `1.5,`, 1), strings.Replace(good, `42,`, `9007199254740992,`, 1), strings.Replace(good, "main", "other", 1), strings.Replace(good, "https://github.com/a/b/pull/42", "file:///tmp/a", 1)} {
		if got := parse([]byte(raw), "main"); got != nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, target := range []string{"javascript:alert(1)", "https://", "https://user:pass@example.com/pr/1", "https://example.com/\n", "https://example.com/a b", "https://example.com\\evil", strings.Repeat("a", 8193)} {
		if ValidURL(target) {
			t.Fatalf("accepted URL %q", target)
		}
	}
}
func fakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func TestLookupUsesExplicitBranchAndIgnoresRepositoryOverrides(t *testing.T) {
	cwd := t.TempDir()
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("KIT_GH_ARGS", args)
	t.Setenv("GH_REPO", "wrong/repo")
	t.Setenv("GIT_DIR", "/wrong")
	t.Setenv("GH_TOKEN", "test-token")
	fakeGH(t, `[ -z "$GH_REPO" ] && [ -z "$GIT_DIR" ] && [ "$GH_TOKEN" = test-token ] && [ "$GH_PROMPT_DISABLED" = 1 ] || exit 1
printf '%s\n' "$PWD" "$@" >> "$KIT_GH_ARGS"
if [ "$2" = checks ]; then
  printf '%s' '[]'
else
  printf '%s' '{"number":12,"url":"https://github.com/a/b/pull/12","headRefName":"--branch"}'
fi
`)
	got := Lookup(t.Context(), cwd, "--branch")
	if got == nil || got.Number != 12 {
		t.Fatalf("lookup=%v", got)
	}
	raw, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	expected := canonical + "\npr\nview\n--json\nnumber,url,headRefName,headRefOid,state,isDraft,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup,latestReviews\n--\n--branch\n" +
		canonical + "\npr\nchecks\n12\n--required\n--json\nbucket,name,state,workflow\n"
	// Shell PWD may retain the original cwd spelling on platforms with /tmp symlinks.
	if string(raw) != expected && string(raw) != strings.Replace(expected, canonical, cwd, 1) {
		t.Fatalf("args=%q", raw)
	}
}
func TestLookupBoundsOutputAndCancellation(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		fakeGH(t, `printf '%65536s' ' '
printf '%s' '{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}'`)
		if got := Lookup(t.Context(), t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		fakeGH(t, "sleep 30\n")
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		if got := Lookup(ctx, t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
		if time.Since(start) > time.Second {
			t.Fatal("lookup ignored cancellation")
		}
	})
	t.Run("failure", func(t *testing.T) {
		fakeGH(t, "exit 1\n")
		if got := Lookup(t.Context(), t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
	})
}
