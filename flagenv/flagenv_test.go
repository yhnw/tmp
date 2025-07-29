package flagenv

import (
	"flag"
	"fmt"
	"io"
	"os"
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
		name               string
		args               []string
		getEnv             func(string) string
		configFileFlagName string
		config             string
		envVarPrefix       string
		checkErr           func(error)
		checkFlags         func(*flags)
	}{
		{
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, defaultFlags.accessKey; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			args: []string{"-access-key", "asdf"},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "asdf"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getEnv: func(key string) string {
				if key == "ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "env"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			args: []string{"-access-key", "asdf"},
			getEnv: func(key string) string {
				if key == "ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "asdf"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			envVarPrefix: "PREFIX_",
			getEnv: func(key string) string {
				if key == "ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, defaultFlags.accessKey; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			envVarPrefix: "PREFIX_",
			getEnv: func(key string) string {
				if key == "PREFIX_ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "env"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			envVarPrefix: "PREFIX_",
			args:         []string{"-access-key", "asdf"},
			getEnv: func(key string) string {
				if key == "ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "asdf"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			configFileFlagName: "config",
			config: `
			# comment
			-access-key=🔑 # comment
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			configFileFlagName: "config",
			config: `
			ACCESS_KEY=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			envVarPrefix:       "PREFIX_",
			configFileFlagName: "config",
			config: `
			PREFIX_ACCESS_KEY=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			envVarPrefix:       "PREFIX_",
			configFileFlagName: "config",
			config: `
			ACCESS_KEY=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, defaultFlags.accessKey; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getEnv: func(key string) string {
				if key == "ACCESS_KEY" {
					return "env"
				}
				return ""
			},
			configFileFlagName: "config",
			config: `
			-access-key=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "env"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			args:               []string{"-access-key", "asdf"},
			configFileFlagName: "config",
			config: `
			ACCESS_KEY=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "asdf"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			configFileFlagName: "config",
			config: `
			-access-key=sss
			ACCESS_KEY=🔑
			`,
			checkErr: func(err error) {
				if !strings.Contains(err.Error(), "duplicate error") {
					t.Errorf("expected duplicate error but got %q", err)
				}
			},
		},
		{
			configFileFlagName: "config",
			config: `
			ACCESS_KEY=🔑
			-access-key=sss
			`,
			checkErr: func(err error) {
				if !strings.Contains(err.Error(), "duplicate error") {
					t.Errorf("expected duplicate error but got %q", err)
				}
			},
		},
	}
	tempDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, flags := newFlagSet()
			if tt.getEnv == nil {
				tt.getEnv = os.Getenv
			}
			if tt.configFileFlagName != "" {
				f, err := os.CreateTemp(tempDir, "")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_, err = io.Copy(f, strings.NewReader(tt.config))
				if err != nil {
					t.Fatal(err)
				}
				tt.args = append([]string{fmt.Sprintf("-%s=%s", tt.configFileFlagName, f.Name())}, tt.args...)
			}
			err := Parse(fs, tt.args, tt.getEnv, tt.configFileFlagName, tt.envVarPrefix)
			if err != nil {
				if tt.checkErr != nil {
					tt.checkErr(err)
				} else {
					t.Fatal(err)
				}
			}
			if tt.checkFlags != nil {
				tt.checkFlags(flags)
			}
		})
	}
}

func TestParseLoadFile(t *testing.T) {
	tests := []struct {
		name               string
		getArgs            func(string) []string
		getEnv             func(string) string
		configFileFlagName string
		config             string
		checkErr           func(error)
		checkFlags         func(*flags)
	}{
		{
			getArgs: func(file string) []string {
				return []string{"-conf", file}
			},
			configFileFlagName: "conf",
			config: `
			-access-key=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getArgs: func(file string) []string {
				return []string{"--conf", file}
			},
			configFileFlagName: "conf",
			config: `
			-access-key=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getArgs: func(file string) []string {
				return []string{"-conf=" + file}
			},
			configFileFlagName: "conf",
			config: `
			-access-key=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getArgs: func(file string) []string {
				return []string{"--conf=" + file}
			},
			configFileFlagName: "conf",
			config: `
			-access-key=🔑
			`,
			checkFlags: func(flags *flags) {
				if g, w := flags.accessKey, "🔑"; g != w {
					t.Errorf("got %s, want %s", g, w)
				}
			},
		},
		{
			getArgs: func(file string) []string {
				return []string{"-conf"}
			},
			configFileFlagName: "conf",
			config: `
			-access-key=🔑
			`,
			checkErr: func(err error) {
				if !strings.Contains(err.Error(), "missing arguments") {
					t.Errorf("expected missing arguments error but got %q", err)
				}
			},
		},
	}
	tempDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, flags := newFlagSet()
			if tt.getEnv == nil {
				tt.getEnv = os.Getenv
			}
			var args []string
			if tt.configFileFlagName != "" {
				f, err := os.CreateTemp(tempDir, "")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_, err = io.Copy(f, strings.NewReader(tt.config))
				if err != nil {
					t.Fatal(err)
				}
				args = tt.getArgs(f.Name())
			}
			err := Parse(fs, args, tt.getEnv, tt.configFileFlagName, "")
			if err != nil {
				if tt.checkErr != nil {
					tt.checkErr(err)
				} else {
					t.Fatal(err)
				}
			}
			if tt.checkFlags != nil {
				tt.checkFlags(flags)
			}
		})
	}
}
