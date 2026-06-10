package repl

import (
	"reflect"
	"testing"
)

func TestSuggestTargetInputsForHostPrefix(t *testing.T) {
	resolver := fakeTargetResolver{
		suggestions: []string{"edge01", "edge02", "spine01"},
	}

	got, err := SuggestTargetInputs("[ 'ed' ] ( '' )", resolver, 8)
	if err != nil {
		t.Fatalf("SuggestTargetInputs: %v", err)
	}
	want := []string{"edge01", "edge02"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v, want %#v", got, want)
	}
}

func TestSuggestTargetInputsOnlyBeforeCommandWhitespace(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"edge01"}}

	got, err := SuggestTargetInputs("[ 'edge01' ] ( 'show' )", resolver, 8)
	if err != nil {
		t.Fatalf("SuggestTargetInputs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("suggestions = %#v, want none", got)
	}
}

func TestSuggestTargetInputsSkipsSelectors(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"select-host"}}

	got, err := SuggestTargetInputs("[ 'select:g' ] ( '' )", resolver, 8)
	if err != nil {
		t.Fatalf("SuggestTargetInputs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("suggestions = %#v, want none", got)
	}
}

func TestSuggestTargetInputsLimitsAndDedupes(t *testing.T) {
	resolver := fakeTargetResolver{
		suggestions: []string{"edge01", "edge01", "edge02", "edge03"},
	}

	got, err := SuggestTargetInputs("[ '' ] ( '' )", resolver, 2)
	if err != nil {
		t.Fatalf("SuggestTargetInputs: %v", err)
	}
	want := []string(nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v, want %#v", got, want)
	}
}

func TestCompleteTargetInputSingleMatchCompletesWithTrailingSpace(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"edge01"}}

	got, err := CompleteTargetInput("[ 'ed' ] ( '' )", resolver)
	if err != nil {
		t.Fatalf("CompleteTargetInput: %v", err)
	}
	if got.Input != "[ 'edge01' ] ( '' )" || !got.Completed || !got.SingleMatch {
		t.Fatalf("completion = %+v", got)
	}
}

func TestCompleteTargetInputMultipleMatchesExtendsCommonPrefix(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"edge01", "edge02"}}

	got, err := CompleteTargetInput("[ 'ed' ] ( '' )", resolver)
	if err != nil {
		t.Fatalf("CompleteTargetInput: %v", err)
	}
	if got.Input != "[ 'edge0' ] ( '' )" || !got.Completed || got.SingleMatch {
		t.Fatalf("completion = %+v", got)
	}
}

func TestCompleteTargetInputAmbiguousWithoutProgressKeepsInput(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"edge01", "edge02"}}

	got, err := CompleteTargetInput("[ 'edge0' ] ( '' )", resolver)
	if err != nil {
		t.Fatalf("CompleteTargetInput: %v", err)
	}
	if got.Input != "[ 'edge0' ] ( '' )" || got.Completed || len(got.Matches) != 2 {
		t.Fatalf("completion = %+v", got)
	}
}

func TestCompleteTargetInputIgnoresCommandText(t *testing.T) {
	resolver := fakeTargetResolver{suggestions: []string{"edge01"}}

	got, err := CompleteTargetInput("[ 'edge01' ] ( 'show' )", resolver)
	if err != nil {
		t.Fatalf("CompleteTargetInput: %v", err)
	}
	if got.Input != "[ 'edge01' ] ( 'show' )" || got.Completed || len(got.Matches) != 0 {
		t.Fatalf("completion = %+v", got)
	}
}
