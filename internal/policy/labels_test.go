package policy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/policy"
)

func TestLabelGrammar(t *testing.T) {
	long := strings.Repeat("a", policy.MaxLabelLength)
	for _, tc := range []struct {
		text       string
		key, value bool
	}{
		{"web", true, true},
		{"storefront-api", true, true},
		{"v1.2_3", true, true},
		{"A9", true, true},
		{"x", true, true},
		{long, true, true},
		{long + "a", false, false},
		{"team/owner", true, false},
		{"", false, false},
		{"web env=lab", false, false},
		{"web env", false, false},
		{"a=b", false, false},
		{"a|b", false, false},
		{" web", false, false},
		{"web ", false, false},
		{"web\t", false, false},
		{"-web", false, false},
		{"web.", false, false},
		{"/team", false, false},
		{"wéb", false, false},
		{`"web"`, false, false},
	} {
		if got := policy.CheckLabelKey(tc.text) == nil; got != tc.key {
			t.Errorf("key %q admitted = %v, want %v", tc.text, got, tc.key)
		}
		if got := policy.CheckLabelValue(tc.text) == nil; got != tc.value {
			t.Errorf("value %q admitted = %v, want %v", tc.text, got, tc.value)
		}
	}
	if !errors.Is(policy.CheckLabelKey(""), policy.ErrEmptyLabelKey) || !errors.Is(policy.CheckLabelKey("a b"), policy.ErrBadLabelKey) {
		t.Fatal("key errors")
	}
}

func TestValidateLabelSet(t *testing.T) {
	if err := policy.ValidateLabelSet([]policy.LabelPair{{Key: "app", Value: "web"}, {Key: "env", Value: "lab"}}); err != nil {
		t.Fatal(err)
	}
	err := policy.ValidateLabelSet([]policy.LabelPair{{Key: "app", Value: "web env=lab"}, {Key: "env", Value: "lab"}, {Key: "env", Value: "prod"}, {Key: "", Value: ""}})
	f := policy.AsFindings(err)
	if f == nil || len(f.Errors) != 4 {
		t.Fatalf("findings = %v", err)
	}
	want := []struct {
		path, rule string
	}{{"labels[0]", "label-value"}, {"labels[2]", "label-key-duplicate"}, {"labels[3]", "label-key-required"}, {"labels[3]", "label-value"}}
	for i, w := range want {
		if f.Errors[i].Path != w.path || f.Errors[i].Rule() != w.rule {
			t.Fatalf("finding %d = %s %s, want %s %s", i, f.Errors[i].Path, f.Errors[i].Rule(), w.path, w.rule)
		}
	}
	// The offending value is quoted, so its whitespace shows.
	if !strings.Contains(f.Errors[0].Message(), `"web env=lab"`) {
		t.Fatalf("message = %q", f.Errors[0].Message())
	}
}

func TestSelectorAdmissionHoldsTheGrammar(t *testing.T) {
	err := policy.ValidateSelector(policy.Selector{"app": {"web", "web env=lab"}, "ro le": {"db"}})
	f := policy.AsFindings(err)
	if f == nil || len(f.Errors) != 2 || f.Errors[0].Rule() != "label-value" || f.Errors[0].Path != "[app]" || f.Errors[1].Rule() != "label-key" {
		t.Fatalf("findings = %v", err)
	}
	if err := policy.ValidateSelector(policy.Selector{"app": {"web", "api"}, "env": {"lab"}}); err != nil {
		t.Fatal(err)
	}
}
