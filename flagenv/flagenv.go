package flagenv

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func Parse(
	fs *flag.FlagSet,
	argsWithoutProgramName []string,
	configFileFlagName string,
	envVarPrefix string,
) error {
	var (
		args            = argsWithoutProgramName
		flagsFromFile   []string
		envVarsFromFile map[string]string
		envVars         map[string]bool
		err             error
	)

	if configFileFlagName != "" {
		configPath := os.Getenv(envVarPrefix + flagNameToEnvName(configFileFlagName))
		if len(args) > 0 {
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
						// -config path
						fileName = args[0]
						args = args[1:]
					}
					configPath = fileName
				}
			}
		}
		if configPath != "" {
			flagsFromFile, envVarsFromFile, err = loadConfigFile(configPath)
			if err != nil {
				return fmt.Errorf("flagenv: failed to load config file: %v", err)
			}
		}
	}

	if envVarsFromFile != nil {
		envVars = make(map[string]bool)
		for name := range envVarsFromFile {
			envVars[name] = true
		}
	}

	fs.VisitAll(func(f *flag.Flag) {
		name := envVarPrefix + flagNameToEnvName(f.Name)
		if envVars != nil {
			envVars[name] = false
		}
		if env := cmp.Or(os.Getenv(name), envVarsFromFile[name]); env != "" {
			flagsFromFile = append(flagsFromFile, fmt.Sprintf("-%s=%s", f.Name, env))
		}
	})

	if envVars != nil {
		var unknown []string
		for name, notFound := range envVars {
			if notFound {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			return fmt.Errorf("flagenv: unknown env vars: %v", unknown)
		}
	}

	args = append(flagsFromFile, args...)
	return fs.Parse(args)
}

func loadConfigFile(fileName string) (flags []string, envVars map[string]string, err error) {
	envVars = make(map[string]string)
	envNames := make(map[string]struct{})
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
			flagName, _, ok := strings.Cut(line[len("-"):], "=")
			envName := flagNameToEnvName(flagName)
			if _, dup := envNames[envName]; dup {
				return nil, nil, dupError(fileName, lineNumber, flagName)
			}
			envNames[envName] = struct{}{}
			if ok {
				// -name=value
				flags = append(flags, line)
			} else {
				// -name value
				fields := strings.Fields(line)
				if len(fields) != 2 {
					return nil, nil, syntaxError(fileName, lineNumber, "found extra characters")
				}
				flags = append(flags, fields...)
			}
		} else {
			if fields := strings.Fields(line); len(fields) != 1 {
				return nil, nil, syntaxError(fileName, lineNumber, "found space characters")
			}
			if envName, value, ok := strings.Cut(line, "="); !ok {
				return nil, nil, errors.New("missing =")
			} else {
				if _, dup := envNames[envName]; dup {
					return nil, nil, dupError(fileName, lineNumber, envName)
				}
				envNames[envName] = struct{}{}
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

func syntaxError(fileName string, lineNumber int, reason string) error {
	return fmt.Errorf("%s:%d: syntax error: %s", fileName, lineNumber, reason)
}
