package repl

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseLineAcceptsHostAndCommand(t *testing.T) {
	req, err := ParseLine("[ 'edge01' ] ( 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if req.Target != "edge01" || req.Command != "show version" {
		t.Fatalf("request = %+v", req)
	}
	if want := []string{"show version"}; !reflect.DeepEqual(req.Commands, want) {
		t.Fatalf("Commands = %#v, want %#v", req.Commands, want)
	}
}

func TestParseLineAcceptsRepeatedHostTargets(t *testing.T) {
	req, err := ParseLine("[ 'edge01', 'edge02' ] ( 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if req.Target != "edge01" || req.Command != "show version" || req.Kind != TargetHost {
		t.Fatalf("request = %+v", req)
	}
	if want := []string{"edge01", "edge02"}; !reflect.DeepEqual(req.Targets, want) {
		t.Fatalf("Targets = %#v, want %#v", req.Targets, want)
	}
}

func TestParseLineAcceptsQuotedSingleCommand(t *testing.T) {
	req, err := ParseLine("[ 'edge01' ] ( 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if req.Command != "show version" {
		t.Fatalf("Command = %q, want show version", req.Command)
	}
	if want := []string{"show version"}; !reflect.DeepEqual(req.Commands, want) {
		t.Fatalf("Commands = %#v, want %#v", req.Commands, want)
	}
}

func TestParseLineAcceptsQuotedMultiCommand(t *testing.T) {
	req, err := ParseLine("[ 'edge01', 'edge02' ] ( 'show ip int brief', 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if want := []string{"edge01", "edge02"}; !reflect.DeepEqual(req.Targets, want) {
		t.Fatalf("Targets = %#v, want %#v", req.Targets, want)
	}
	if want := []string{"show ip int brief", "show version"}; !reflect.DeepEqual(req.Commands, want) {
		t.Fatalf("Commands = %#v, want %#v", req.Commands, want)
	}
	if req.Command != "show ip int brief" {
		t.Fatalf("Command = %q, want first command", req.Command)
	}
}

func TestParseLineUnescapesSingleQuoteInQuotedCommand(t *testing.T) {
	req, err := ParseLine(`[ 'edge01' ] ( 'show \'clock' )`)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if want := []string{"show 'clock"}; !reflect.DeepEqual(req.Commands, want) {
		t.Fatalf("Commands = %#v, want %#v", req.Commands, want)
	}
}

func TestParseLineRejectsMalformedQuotedCommands(t *testing.T) {
	for _, input := range []string{
		"[ 'edge01' ] ( 'show version )",
		"[ 'edge01' ] ( '' )",
		"[ 'edge01' ] ( 'show version', )",
		"[ 'edge01' ] ( 'show version' show hostname )",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseLine(input); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestParseLineAcceptsRawIPv6HostLiteral(t *testing.T) {
	req, err := ParseLine("[ '2001:db8::10' ] ( 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if req.Target != "2001:db8::10" || req.Kind != TargetHost {
		t.Fatalf("request = %+v", req)
	}
}

func TestParseLineAcceptsExplicitSelector(t *testing.T) {
	req, err := ParseLine("[ 'select:group:local/lab' ] ( 'show version' )")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if req.Target != "group:local/lab" || req.Kind != TargetSelector || req.Command != "show version" {
		t.Fatalf("request = %+v", req)
	}
}

func TestParseLineRejectsMissingTargetOrCommand(t *testing.T) {
	for _, input := range []string{"show version", "@edge01 show version", "[ '' ] ( 'show version' )", "[ 'edge01' ] ( '' )", "[ 'edge01' ]"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseLine(input); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestExpandHostPattern(t *testing.T) {
	got, err := ExpandHostPattern("acm-lab-agg-sw(1,2)")
	if err != nil {
		t.Fatalf("ExpandHostPattern: %v", err)
	}
	want := []string{"acm-lab-agg-sw1", "acm-lab-agg-sw2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandHostPattern = %#v, want %#v", got, want)
	}
}

func TestResolveTargetsUsesSelectorPrefixAndDedupes(t *testing.T) {
	resolver := fakeTargetResolver{
		hosts: map[string]string{
			"edge01": "edge01",
		},
		selectors: map[string][]string{
			"group:local/lab": {"edge02", "edge01", "edge02"},
		},
	}

	got, err := ResolveTargets(Request{Kind: TargetSelector, Target: "group:local/lab"}, resolver)
	if err != nil {
		t.Fatalf("ResolveTargets: %v", err)
	}
	want := []string{"edge02", "edge01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveTargets = %#v, want %#v", got, want)
	}
}

func TestResolveTargetsExpandsThenResolvesHosts(t *testing.T) {
	resolver := fakeTargetResolver{
		hosts: map[string]string{
			"sw1": "switch-one",
			"sw2": "switch-two",
		},
	}

	got, err := ResolveTargets(Request{Kind: TargetHost, Target: "sw(1,2)"}, resolver)
	if err != nil {
		t.Fatalf("ResolveTargets: %v", err)
	}
	want := []string{"switch-one", "switch-two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveTargets = %#v, want %#v", got, want)
	}
}

func TestResolveTargetsResolvesRepeatedHostTargets(t *testing.T) {
	resolver := fakeTargetResolver{
		hosts: map[string]string{
			"edge01": "switch-one",
			"edge02": "switch-two",
		},
	}

	got, err := ResolveTargets(Request{Kind: TargetHost, Target: "edge01", Targets: []string{"edge01", "edge02"}}, resolver)
	if err != nil {
		t.Fatalf("ResolveTargets: %v", err)
	}
	want := []string{"switch-one", "switch-two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveTargets = %#v, want %#v", got, want)
	}
}

type fakeTargetResolver struct {
	hosts       map[string]string
	selectors   map[string][]string
	suggestions []string
}

func (r fakeTargetResolver) ResolveHost(host string) (string, error) {
	if r.hosts == nil {
		return host, nil
	}
	if resolved, ok := r.hosts[host]; ok {
		return resolved, nil
	}
	return "", errors.New("missing host")
}

func (r fakeTargetResolver) SelectHosts(selector string) ([]string, error) {
	if hosts, ok := r.selectors[selector]; ok {
		return hosts, nil
	}
	return nil, nil
}

func (r fakeTargetResolver) SuggestHosts(prefix string) ([]string, error) {
	var matched []string
	for _, host := range r.suggestions {
		if prefix == "" || len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			matched = append(matched, host)
		}
	}
	return matched, nil
}
