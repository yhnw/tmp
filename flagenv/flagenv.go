package flagenv

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"strings"
)

func Parse(
	fs *flag.FlagSet,
	argsWithoutProgramName []string,
	getEnv func(string) string,
	configFileFlagName string,
	envVarPrefix string,
) error {
	var (
		args            = argsWithoutProgramName
		flagsFromFile   []string
		envVarsFromFile map[string]string
		err             error
	)

	if getEnv == nil {
		getEnv = os.Getenv
	}

	if len(args) > 0 && configFileFlagName != "" {
		if arg, ok := strings.CutPrefix(args[0], "-"); ok {
			arg, _ = strings.CutPrefix(arg, "-")
			flagName, value, ok := strings.Cut(arg, "=")
			if flagName == configFileFlagName {
				args = args[1:]
				if !ok && len(args) == 0 {
					return fmt.Errorf("flagenv: missing arguments to -%s", configFileFlagName)
				}
				fileName := value
				if !ok {
					fileName = args[0]
					args = args[1:]
				}
				flagsFromFile, envVarsFromFile, err = loadConfigFile(fileName)
				if err != nil {
					return fmt.Errorf("flagenv: failed to load config file: %v", err)
				}
			}
		}
	}

	fs.VisitAll(func(f *flag.Flag) {
		name := envVarPrefix + flagNameToEnvName(f.Name)
		if env := cmp.Or(getEnv(name), envVarsFromFile[name]); env != "" {
			flagsFromFile = append(flagsFromFile, fmt.Sprintf("-%s=%s", f.Name, env))
		}
	})
	args = append(flagsFromFile, args...)
	return fs.Parse(args)
}

func loadConfigFile(fileName string) (flags []string, envVars map[string]string, err error) {
	envVars = map[string]string{}
	dup := map[string]struct{}{}
	b, err := os.ReadFile(fileName)
	if err != nil {
		return nil, nil, err
	}
	lineNumber := 0
	for line := range strings.Lines(string(b)) {
		lineNumber++
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if flag := strings.HasPrefix(line, "-"); flag {
			flagName, _, _ := strings.Cut(line[len("-"):], "=")
			env := flagNameToEnvName(flagName)
			if _, dup := dup[env]; dup {
				return nil, nil, dupError(fileName, lineNumber, flagName)
			}
			dup[env] = struct{}{}
			flags = append(flags, line)
		} else {
			if envName, value, ok := strings.Cut(line, "="); ok {
				if _, dup := dup[envName]; dup {
					return nil, nil, dupError(fileName, lineNumber, envName)
				}
				dup[envName] = struct{}{}
				envVars[envName] = value
			}
		}
	}
	return flags, envVars, nil
}

func flagNameToEnvName(flagName string) string {
	name := strings.ToUpper(flagName)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

func dupError(fileName string, lineNumber int, dup string) error {
	return fmt.Errorf("%s:%d: duplicate error: %q", fileName, lineNumber, dup)
}
