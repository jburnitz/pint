package checks_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/cloudflare/pint/internal/checks"
	"github.com/cloudflare/pint/internal/discovery"
	"github.com/cloudflare/pint/internal/promapi"
)

func textDependencyRule(nr int) string {
	return fmt.Sprintf("Metric generated by this rule is used by %d other rule(s).", nr)
}

func detailsDependencyRule(name, broken string) string {
	return fmt.Sprintf(
		"If you remove the recording rule generating `%s`, and there is no other source of this metric, then any other rule depending on it will break.\nList of found rules that are using `%s`:\n\n%s",
		name, name, broken,
	)
}

func TestRuleDependencyCheck(t *testing.T) {
	parseWithState := func(input string, state discovery.ChangeType, sp, rp string) []discovery.Entry {
		entries := mustParseContent(input)
		for i := range entries {
			entries[i].State = state
			entries[i].SourcePath = sp
			entries[i].ReportedPath = rp

		}
		return entries
	}

	testCases := []checkTest{
		{
			description: "ignores alerting rules",
			content:     "- alert: foo\n  expr: up == 0\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
		},
		{
			description: "ignores rules with syntax errors",
			content:     "- record: foo\n  expr: sum(foo) without(\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
		},
		{
			description: "ignores alerts with expr errors",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
			entries: []discovery.Entry{
				parseWithState("- record: foo\n  expr: sum(foo)\n", discovery.Removed, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: foo\n  expr: foo ==\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
			},
		},
		{
			description: "ignores alerts without dependencies",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
			entries: []discovery.Entry{
				parseWithState("- record: foo\n  expr: sum(foo)\n", discovery.Removed, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: foo\n  expr: up == 0\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
			},
		},
		{
			description: "includes alerts on other prometheus servers",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: func(uri string) *promapi.FailoverGroup {
				return promapi.NewFailoverGroup(
					"prom",
					uri,
					[]*promapi.Prometheus{
						promapi.NewPrometheus("prom", uri, "", map[string]string{"X-Debug": "1"}, time.Second, 16, 1000, nil),
					},
					true,
					"up",
					[]*regexp.Regexp{},
					[]*regexp.Regexp{regexp.MustCompile("excluded.yml")},
					[]string{},
				)
			},
			problems: func(s string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "record: foo",
						Anchor:   checks.AnchorBefore,
						Lines:    []int{1, 2},
						Reporter: checks.RuleDependencyCheckName,
						Text:     textDependencyRule(1),
						Details:  detailsDependencyRule("foo", "- `alert` at `excluded.yaml:2`\n"),
						Severity: checks.Warning,
					},
				}
			},
			entries: []discovery.Entry{
				parseWithState("- record: foo\n  expr: sum(foo)\n", discovery.Removed, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: alert\n  expr: foo == 0\n", discovery.Noop, "excluded.yaml", "excluded.yaml")[0],
			},
		},
		{
			description: "warns about removed dependency",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems: func(s string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "record: foo",
						Anchor:   checks.AnchorBefore,
						Lines:    []int{1, 2},
						Reporter: checks.RuleDependencyCheckName,
						Text:     textDependencyRule(1),
						Details:  detailsDependencyRule("foo", "- `alert` at `foo.yaml:2`\n"),
						Severity: checks.Warning,
					},
				}
			},
			entries: []discovery.Entry{
				parseWithState("- record: foo\n  expr: sum(foo)\n", discovery.Removed, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: alert\n  expr: foo == 0\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
			},
		},
		{
			description: "ignores unparsable files",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
			entries: []discovery.Entry{
				{
					ReportedPath: "broken.yaml",
					SourcePath:   "broken.yaml",
					PathError:    errors.New("bad file"),
				},
				parseWithState("- alert: foo\n  expr: up == 0\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
			},
		},
		{
			description: "ignores rules with errors",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems:   noProblems,
			entries: []discovery.Entry{
				parseWithState("- recordx: foo\n  expr: sum(foo)\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: foo\n  expr: up == 0\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
			},
		},
		{
			description: "deduplicates affected files",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleDependencyCheck()
			},
			prometheus: newSimpleProm,
			problems: func(s string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "record: foo",
						Anchor:   checks.AnchorBefore,
						Lines:    []int{1, 2},
						Reporter: checks.RuleDependencyCheckName,
						Text:     textDependencyRule(5),
						Details:  detailsDependencyRule("foo", "- `alert` at `alice.yaml:4`\n- `alert` at `alice.yaml:6`\n- `alert` at `bar.yaml:2`\n- `xxx` at `bar.yaml:2`\n- `alert` at `foo.yaml:2`\n"),
						Severity: checks.Warning,
					},
				}
			},
			entries: []discovery.Entry{
				parseWithState("\n\n- alert: alert\n  expr: (foo / foo) == 0\n- alert: alert\n  expr: (foo / foo) == 0\n", discovery.Noop, "alice.yaml", "alice.yaml")[1],
				parseWithState("\n\n- alert: alert\n  expr: (foo / foo) == 0\n- alert: alert\n  expr: (foo / foo) == 0\n", discovery.Noop, "alice.yaml", "alice.yaml")[0],
				parseWithState("- alert: alert\n  expr: (foo / foo) == 0\n", discovery.Noop, "symlink3.yaml", "bar.yaml")[0],
				parseWithState("- record: foo\n  expr: sum(foo)\n", discovery.Removed, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: alert\n  expr: foo == 0\n", discovery.Noop, "foo.yaml", "foo.yaml")[0],
				parseWithState("- alert: xxx\n  expr: (foo / foo) == 0\n", discovery.Noop, "bar.yaml", "bar.yaml")[0],
				parseWithState("- alert: alert\n  expr: (foo / foo) == 0\n", discovery.Noop, "bar.yaml", "bar.yaml")[0],
				parseWithState("- alert: alert\n  expr: foo == 0\n", discovery.Noop, "symlink1.yaml", "foo.yaml")[0],
				parseWithState("- alert: alert\n  expr: foo == 0\n", discovery.Noop, "symlink2.yaml", "foo.yaml")[0],
			},
		},
	}

	runTests(t, testCases)
}
