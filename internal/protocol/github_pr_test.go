package protocol

import "testing"

func TestGitHubPullRequestWireValidation(t *testing.T) {
	valid := SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo", Status: &VCSStatus{Root: "/repo", Head: VCSHead{Kind: VCSHeadBranch, Name: "main"}, PullRequest: &GitHubPullRequest{Number: 42, URL: "https://github.com/a/b/pull/42"}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, pr := range []GitHubPullRequest{{Number: 0, URL: "https://example.com"}, {Number: -1, URL: "https://example.com"}, {Number: 1, URL: "file:///tmp/pr"}, {Number: 1, URL: "https://user:pass@example.com/pr"}, {Number: 1, URL: "https://example.com/pr\n"}, {Number: 1, URL: "https://example.com\\evil"}, {Number: 1, URL: "https://"}} {
		value := *valid.Status
		value.PullRequest = &pr
		result := valid
		result.Status = &value
		if err := result.Validate(); err == nil {
			t.Fatalf("accepted %+v", pr)
		}
	}
	for _, kind := range []VCSHeadKind{VCSHeadDetached, VCSHeadUnborn} {
		value := *valid.Status
		value.Head = VCSHead{Kind: kind, Name: "main"}
		result := valid
		result.Status = &value
		if err := result.Validate(); err == nil {
			t.Fatalf("accepted PR for %s", kind)
		}
	}
}
