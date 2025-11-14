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

func run[T any](t *testing.T, fn func(*testing.T, T), name string, tc T) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Helper()
		t.Log("@" + t.Name())
		fn(t, tc)
	})
}

func TestParse(t *testing.T) {
	tempDir := t.TempDir()

	type testCase struct {
		args      []string
		env       []string
		config    string
		envPrefix string
		wantFlag  string
		wantErr   string
	}

	testFunc := func(t *testing.T, tc testCase) {
		fs, flags := newFlagSet()
		if tc.config != "" {
			f, err := os.CreateTemp(tempDir, "")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, err = io.Copy(f, strings.NewReader(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			tc.args = append([]string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}, tc.args...)
		}
		for v := range slices.Chunk(tc.env, 2) {
			t.Setenv(v[0], v[1])
		}
		err := Parse(fs, tc.args, tc.envPrefix)
		if (err != nil) && (tc.wantErr != "") {
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected err contains %q, but got %q", tc.wantErr, err)
			}
			return
		}
		if (err == nil) && (tc.wantErr != "") {
			t.Error("expected error but got nil")
		}
		if err != nil && (tc.wantErr == "") {
			t.Error(err)
		}
		if tc.wantFlag != "" {
			if g, w := flags.accessKey, tc.wantFlag; g != w {
				t.Errorf("got %q, want %q", g, w)
			}
		}
	}

	run(t, testFunc, "", testCase{
		args:     []string{},
		wantFlag: defaultFlags.accessKey,
	})
	run(t, testFunc, "", testCase{
		args:     []string{"-access-key", "asdf"},
		wantFlag: "asdf",
	})
	run(t, testFunc, "", testCase{
		args:     []string{"--", "-access-key", "asdf"},
		wantFlag: defaultFlags.accessKey,
	})
	run(t, testFunc, "", testCase{
		env:      []string{"ACCESS_KEY", "env"},
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		args:     []string{"--"},
		env:      []string{"ACCESS_KEY", "env"},
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		args:     []string{"-access-key", "asdf"},
		env:      []string{"ACCESS_KEY", "env"},
		wantFlag: "asdf",
	})
	run(t, testFunc, "", testCase{
		args:     []string{"--", "-access-key", "asdf"},
		env:      []string{"ACCESS_KEY", "env"},
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		env:       []string{"ACCESS_KEY", "env"},
		wantFlag:  defaultFlags.accessKey,
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		env:       []string{"PREFIX_ACCESS_KEY", "env"},
		wantFlag:  "env",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		args:      []string{"-access-key", "asdf"},
		env:       []string{"PREFIX_ACCESS_KEY", "env"},
		wantFlag:  "asdf",
	})
	run(t, testFunc, "", testCase{
		config: `
			# comment
			-access-key 🔑 # comment
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		config: `
			# comment
			-access-key=🔑 # comment
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		config: `
			ACCESS_KEY=🔑
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		config: `
			UNDEF=🔑
			`,
		wantErr: "undefined",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		config: `
			PREFIX_ACCESS_KEY=🔑
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		config: `
			ACCESS_KEY=🔑
			ACCESS_KEY2=🔑
			`,
		wantErr: "undefined",
	})
	run(t, testFunc, "", testCase{
		env: []string{"ACCESS_KEY", "env"},
		config: `
			-access-key=🔑
			`,
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		args: []string{"-access-key", "asdf"},
		config: `
			ACCESS_KEY=🔑
			`,
		wantFlag: "asdf",
	})
	run(t, testFunc, "", testCase{
		config: `
			-access-key=sss
			ACCESS_KEY=🔑
			`,
		wantErr: "duplicate error",
	})
	run(t, testFunc, "", testCase{
		config: `
			ACCESS_KEY=🔑
			-access-key=sss
			`,
		wantErr: "duplicate error",
	})
	run(t, testFunc, "", testCase{
		config: `
			-access-key=🔑
			ACCESS_KEY
			`,
		wantErr: "missing =",
	})

}

func TestParseLoadFileFromEnv(t *testing.T) {
	tempDir := t.TempDir()

	type testCase struct {
		config    string
		configEnv string
		envPrefix string
		wantFlag  string
		wantErr   string
	}

	testFunc := func(t *testing.T, tc testCase) {
		fs, flags := newFlagSet()
		var args []string
		if tc.configEnv != "" {
			f, err := os.CreateTemp(tempDir, "")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, err = io.Copy(f, strings.NewReader(tc.configEnv))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv(tc.envPrefix+strings.ToUpper(configFlagName), f.Name())
		}
		if tc.config != "" {
			f, err := os.CreateTemp(tempDir, "")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, err = io.Copy(f, strings.NewReader(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			args = []string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}
		}
		err := Parse(fs, args, tc.envPrefix)
		if (err != nil) && (tc.wantErr != "") {
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected err contains %q, but got %q", tc.wantErr, err)
			}
			return
		}
		if (err == nil) && (tc.wantErr != "") {
			t.Error("expected error but got nil")
		}
		if err != nil && (tc.wantErr == "") {
			t.Error(err)
		}
		if tc.wantFlag != "" {
			if g, w := flags.accessKey, tc.wantFlag; g != w {
				t.Errorf("got %q, want %q", g, w)
			}
		}
	}

	run(t, testFunc, "", testCase{
		configEnv: `
			-access-key=env
			`,
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		configEnv: `
			-access-key=env
			`,
		wantFlag: "env",
	})
	run(t, testFunc, "", testCase{
		configEnv: `
			-access-key=env
			`,
		config: `
			-access-key=🔑
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		configEnv: `
			-access-key=env
			`,
		config: `
			-access-key=🔑
			`,
		wantFlag: "🔑",
	})
	// should error
	run(t, testFunc, "", testCase{
		envPrefix: "PREFIX_",
		config: `
			PREFIX_ACCESS_KEY=foo
			-access-key=bar
			`,
		wantFlag: "foo",
	})
}

func TestParseLoadFile(t *testing.T) {
	tempDir := t.TempDir()

	type testCase struct {
		config   string
		wantFlag string
		wantErr  string
	}

	testFunc := func(t *testing.T, tc testCase) {
		fs, flags := newFlagSet()
		var args []string
		f, err := os.CreateTemp(tempDir, "")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, err = io.Copy(f, strings.NewReader(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		args = []string{fmt.Sprintf("-%s=%s", configFlagName, f.Name())}
		err = Parse(fs, args, "")
		if (err != nil) && (tc.wantErr != "") {
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected err contains %q, but got %q", tc.wantErr, err)
			}
			return
		}
		if (err == nil) && (tc.wantErr != "") {
			t.Error("expected error but got nil")
		}
		if err != nil && (tc.wantErr == "") {
			t.Error(err)
		}
		if tc.wantFlag != "" {
			if g, w := flags.accessKey, tc.wantFlag; g != w {
				t.Errorf("got %q, want %q", g, w)
			}
		}
	}

	run(t, testFunc, "", testCase{
		config: `
			-access-key=🔑
			`,
		wantFlag: "🔑",
	})
	run(t, testFunc, "", testCase{
		config: `
			-access-key 🔑 extra
			`,
		wantErr: "syntax error",
	})
	run(t, testFunc, "", testCase{
		config: `
			ACCESS_KEY =🔑
			`,
		wantErr: "syntax error",
	})
	run(t, testFunc, "", testCase{
		config: `
			ACCESS KEY =🔑
			`,
		wantErr: "syntax error",
	})
}
