package main

import "testing"

func TestCommand(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantAction   string
		wantArgument int
		wantError    bool
	}{
		{name: "default up", wantAction: "up"},
		{name: "up", args: []string{"up"}, wantAction: "up"},
		{name: "down", args: []string{"down"}, wantAction: "down"},
		{name: "version", args: []string{"version"}, wantAction: "version"},
		{name: "forward steps", args: []string{"step", "2"}, wantAction: "step", wantArgument: 2},
		{name: "backward step", args: []string{"step", "-1"}, wantAction: "step", wantArgument: -1},
		{name: "unknown", args: []string{"reset"}, wantError: true},
		{name: "extra argument", args: []string{"up", "1"}, wantError: true},
		{name: "missing step", args: []string{"step"}, wantError: true},
		{name: "invalid step", args: []string{"step", "many"}, wantError: true},
		{name: "zero step", args: []string{"step", "0"}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action, argument, err := command(test.args)
			if test.wantError {
				if err == nil {
					t.Fatalf("command(%q) succeeded", test.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("command(%q): %v", test.args, err)
			}
			if action != test.wantAction || argument != test.wantArgument {
				t.Fatalf("command(%q) = %q, %d; want %q, %d", test.args, action, argument, test.wantAction, test.wantArgument)
			}
		})
	}
}
