package flagenv

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

type flags struct {
	accessKey string
	addr      string
	port      int
}

var defaultFlags = flags{
	accessKey: "defaultAccessKey",
	addr:      "defaultAddr",
	port:      42,
}

func newFlagSet() (*flag.FlagSet, *flags) {
	flags := defaultFlags
	fs := flag.NewFlagSet("flagenv", flag.ContinueOnError)
	fs.StringVar(&flags.addr, "addr", flags.addr, "usage addr")
	fs.IntVar(&flags.port, "port", flags.port, "usage port")
	fs.StringVar(&flags.accessKey, "access-key", flags.accessKey, "usage access-key")
	return fs, &flags
}

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		env       []string
		config    string
		envPrefix string
		wantFlag  string
		wantErr   string
	}{
		{
			args:     []string{},
			wantFlag: defaultFlags.accessKey,
		},
		{
			args:     []string{"-access-key", "asdf"},
			wantFlag: "asdf",
		},
		{
			args:     []string{"--", "-access-key", "asdf"},
			wantFlag: defaultFlags.accessKey,
		},
		{
			env:      []string{"ACCESS_KEY", "env"},
			wantFlag: "env",
		},
		{
			args:     []string{"--"},
			env:      []string{"ACCESS_KEY", "env"},
			wantFlag: "env",
		},
		{
			args:     []string{"-access-key", "asdf"},
			env:      []string{"ACCESS_KEY", "env"},
			wantFlag: "asdf",
		},
		{
			args:     []string{"--", "-access-key", "asdf"},
			env:      []string{"ACCESS_KEY", "env"},
			wantFlag: "env",
		},
		{
			envPrefix: "PREFIX_",
			env:       []string{"ACCESS_KEY", "env"},
			wantFlag:  defaultFlags.accessKey,
		},
		{
			envPrefix: "PREFIX_",
			env:       []string{"PREFIX_ACCESS_KEY", "env"},
			wantFlag:  "env",
		},
		{
			envPrefix: "PREFIX_",
			args:      []string{"-access-key", "asdf"},
			env:       []string{"PREFIX_ACCESS_KEY", "env"},
			wantFlag:  "asdf",
		},
		{
			config: `
			# comment
			-access-key 🔑 # comment
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			# comment
			-access-key=🔑 # comment
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			ACCESS_KEY=🔑
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			UNDEF=🔑
			`,
			wantErr: "undefined",
		},
		{
			envPrefix: "PREFIX_",
			config: `
			PREFIX_ACCESS_KEY=🔑
			`,
			wantFlag: "🔑",
		},
		{
			envPrefix: "PREFIX_",
			config: `
			ACCESS_KEY=🔑
			ACCESS_KEY2=🔑
			`,
			wantErr: "undefined",
		},
		{
			env: []string{"ACCESS_KEY", "env"},
			config: `
			-access-key=🔑
			`,
			wantFlag: "env",
		},
		{
			args: []string{"-access-key", "asdf"},
			config: `
			ACCESS_KEY=🔑
			`,
			wantFlag: "asdf",
		},
		{
			config: `
			-access-key=sss
			ACCESS_KEY=🔑
			`,
			wantErr: "duplicate error",
		},
		{
			config: `
			ACCESS_KEY=🔑
			-access-key=sss
			`,
			wantErr: "duplicate error",
		},
		{
			config: `
			-access-key=🔑
			ACCESS_KEY
			`,
			wantErr: "missing =",
		},
	}
	tempDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, flags := newFlagSet()
			if tt.config != "" {
				f, err := os.CreateTemp(tempDir, "")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_, err = io.Copy(f, strings.NewReader(tt.config))
				if err != nil {
					t.Fatal(err)
				}
				tt.args = append([]string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}, tt.args...)
			}
			for v := range slices.Chunk(tt.env, 2) {
				t.Setenv(v[0], v[1])
			}
			err := Parse(fs, tt.args, tt.envPrefix)
			if (err != nil) && (tt.wantErr != "") {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected err contains %q, but got %q", tt.wantErr, err)
				}
				return
			}
			if (err == nil) && (tt.wantErr != "") {
				t.Error("expected error but got nil")
			}
			if err != nil && (tt.wantErr == "") {
				t.Error(err)
			}
			if tt.wantFlag != "" {
				if g, w := flags.accessKey, tt.wantFlag; g != w {
					t.Errorf("got %q, want %q", g, w)
				}
			}
		})
	}
}

func TestParseLoadFile(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		wantFlag string
		wantErr  string
	}{
		{
			config: `
			-access-key=🔑
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			-access-key=🔑
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			-access-key=🔑
			`,
			wantFlag: "🔑",
		},
		{
			config: `
			-access-key 🔑 extra
			`,
			wantErr: "syntax error",
		},
		{
			config: `
			ACCESS_KEY =🔑
			`,
			wantErr: "syntax error",
		},
		{
			config: `
			ACCESS KEY =🔑
			`,
			wantErr: "syntax error",
		},
	}
	tempDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, flags := newFlagSet()
			var args []string
			f, err := os.CreateTemp(tempDir, "")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, err = io.Copy(f, strings.NewReader(tt.config))
			if err != nil {
				t.Fatal(err)
			}
			args = []string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}
			err = Parse(fs, args, "")
			if (err != nil) && (tt.wantErr != "") {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected err contains %q, but got %q", tt.wantErr, err)
				}
				return
			}
			if (err == nil) && (tt.wantErr != "") {
				t.Error("expected error but got nil")
			}
			if err != nil && (tt.wantErr == "") {
				t.Error(err)
			}
			if tt.wantFlag != "" {
				if g, w := flags.accessKey, tt.wantFlag; g != w {
					t.Errorf("got %q, want %q", g, w)
				}
			}
		})
	}
}

func TestParseLoadFileFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		configEnv string
		envPrefix string
		wantFlag  string
		wantErr   string
	}{
		{
			configEnv: `
			-access-key=env
			`,
			wantFlag: "env",
		},
		{
			envPrefix: "PREFIX_",
			configEnv: `
			-access-key=env
			`,
			wantFlag: "env",
		},
		{
			configEnv: `
			-access-key=env
			`,
			config: `
			-access-key=🔑
			`,
			wantFlag: "🔑",
		},
		{
			envPrefix: "PREFIX_",
			configEnv: `
			-access-key=env
			`,
			config: `
			-access-key=🔑
			`,
			wantFlag: "🔑",
		},
	}
	tempDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, flags := newFlagSet()
			var args []string
			if tt.configEnv != "" {
				f, err := os.CreateTemp(tempDir, "")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_, err = io.Copy(f, strings.NewReader(tt.configEnv))
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv(tt.envPrefix+strings.ToUpper(configFlagName), f.Name())
			}
			if tt.config != "" {
				f, err := os.CreateTemp(tempDir, "")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_, err = io.Copy(f, strings.NewReader(tt.config))
				if err != nil {
					t.Fatal(err)
				}
				args = []string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}
			}
			err := Parse(fs, args, tt.envPrefix)
			if (err != nil) && (tt.wantErr != "") {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected err contains %q, but got %q", tt.wantErr, err)
				}
				return
			}
			if (err == nil) && (tt.wantErr != "") {
				t.Error("expected error but got nil")
			}
			if err != nil && (tt.wantErr == "") {
				t.Error(err)
			}
			if tt.wantFlag != "" {
				if g, w := flags.accessKey, tt.wantFlag; g != w {
					t.Errorf("got %q, want %q", g, w)
				}
			}
		})
	}
}
