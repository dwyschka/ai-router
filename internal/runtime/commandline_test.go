package runtime

import (
	"reflect"
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		command string
		args    []string
		wantErr bool
	}{
		{name: "einfaches Kommando", line: "claude", command: "claude"},
		{name: "mit Argumenten", line: "ollama launch claude", command: "ollama", args: []string{"launch", "claude"}},
		{name: "mehrfacher Leerraum", line: "  ollama   launch\tclaude ", command: "ollama", args: []string{"launch", "claude"}},
		{name: "doppelte Anführungszeichen", line: `tool --pfad "/mit leerzeichen/hier"`, command: "tool", args: []string{"--pfad", "/mit leerzeichen/hier"}},
		{name: "einfache Anführungszeichen", line: `tool 'a b' c`, command: "tool", args: []string{"a b", "c"}},
		{name: "maskiertes Leerzeichen", line: `tool a\ b`, command: "tool", args: []string{"a b"}},
		{name: "leeres Argument", line: `tool ""`, command: "tool", args: []string{""}},
		{name: "leer", line: "   ", wantErr: true},
		{name: "offenes Anführungszeichen", line: `tool "unfertig`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, args, err := ParseCommandLine(tc.line)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("erwartet Fehler, bekam %q %v", command, args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if command != tc.command {
				t.Errorf("Kommando = %q, erwartet %q", command, tc.command)
			}
			if len(args) != len(tc.args) || (len(args) > 0 && !reflect.DeepEqual(args, tc.args)) {
				t.Errorf("Argumente = %#v, erwartet %#v", args, tc.args)
			}
		})
	}
}

func TestParseCommandLineInterpretiertKeineShell(t *testing.T) {
	// Shell-Metazeichen bleiben Bestandteil der Token, sie werden nie ausgeführt.
	command, args, err := ParseCommandLine(`tool $HOME; rm -rf / | cat &`)
	if err != nil {
		t.Fatal(err)
	}
	if command != "tool" {
		t.Fatalf("Kommando = %q", command)
	}
	want := []string{"$HOME;", "rm", "-rf", "/", "|", "cat", "&"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("Argumente = %#v, erwartet %#v", args, want)
	}
}

func TestFormatCommandLineIstUmkehrbar(t *testing.T) {
	for _, line := range []string{"claude", "ollama launch claude", `tool "a b" c`} {
		command, args, err := ParseCommandLine(line)
		if err != nil {
			t.Fatal(err)
		}
		erneut, erneutArgs, err := ParseCommandLine(FormatCommandLine(command, args))
		if err != nil {
			t.Fatal(err)
		}
		if erneut != command || !reflect.DeepEqual(erneutArgs, args) {
			t.Errorf("%q → %q %v, erwartet %q %v", line, erneut, erneutArgs, command, args)
		}
	}
}
