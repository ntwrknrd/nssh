package sshargs

import (
	"slices"
	"testing"
)

func TestOptionValuesAreOpaque(t *testing.T) {
	for _, value := range []string{"--", "-lwrong", "-Oexit", "-Fwrong", "a,b", ""} {
		args := []string{"-vi", value, "-qp", "2222", "--", "echo", "-l", "remote"}
		var got []Option
		Walk(args, func(o Option) bool { got = append(got, o); return true })
		want := []Option{{Name: 'v'}, {Name: 'i', Value: value}, {Name: 'q'}, {Name: 'p', Value: "2222"}}
		if !slices.Equal(got, want) {
			t.Fatalf("%q: options=%+v", value, got)
		}
		opts, command := Split(args)
		if !slices.Equal(opts, args[:4]) || !slices.Equal(command, []string{"echo", "-l", "remote"}) {
			t.Fatalf("split: %q / %q", opts, command)
		}
	}
}

func TestNextAttachedAndMissingValues(t *testing.T) {
	options, n, err := Next([]string{"-qJjump1,jump2", "host"})
	if err != nil || n != 1 || !slices.Equal(options, []Option{{Name: 'q'}, {Name: 'J', Value: "jump1,jump2"}}) {
		t.Fatalf("next: %+v %d %v", options, n, err)
	}
	for _, args := range [][]string{{"-vJ"}, {"-B"}, {"-e"}, {"-P"}, {"-z"}} {
		if _, _, err := Next(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
