package pluginmessage

import "testing"

func TestSummaryIsTheFirstVisibleLineAsPlainText(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ text, want string }{
		"plain":            {"Continue the experiment loop.\n\nLog each result.", "Continue the experiment loop."},
		"inline markup":    {"Read `autoresearch.md`, then **log** the _result_.", "Read autoresearch.md, then log the result."},
		"link label":       {"See [the plan](https://example.com/plan) first.", "See the plan first."},
		"leading heading":  {"\n\n## Next experiment\n\nTry parallel builds.", "Next experiment"},
		"wrapped line":     {"Continue the loop\nwith the next idea.", "Continue the loop"},
		"list item":        {"- [ ] Run the benchmark\n- [ ] Log it", "Run the benchmark"},
		"quote":            {"> Keep the baseline.", "Keep the baseline."},
		"fenced code":      {"```sh\ngo test ./...\n```", "go test ./..."},
		"table header row": {"| Experiment | Result |\n| --- | --- |\n| 3 | kept |", "Experiment · Result"},
		"rule then text":   {"---\n\nAfter the rule.", "After the rule."},
	} {
		if got := Summary(test.text); got != test.want {
			t.Errorf("%s: Summary(%q) = %q, want %q", name, test.text, got, test.want)
		}
	}
}
