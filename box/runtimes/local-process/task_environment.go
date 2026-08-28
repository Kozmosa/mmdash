package localprocess

// Task environment construction. A bare-metal task never inherits the Gateway
// environment: only an explicit allowlist plus the frozen task environment is
// forwarded, and per-task HOME/temporary directories isolate host state. The
// Gateway, Box Token, Git, SSH and cloud provider credentials therefore cannot
// leak into the trusted-host execution.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// windowsTaskVariables are the system variables Windows tooling (including
// Python) requires to locate its own installation.
var windowsTaskVariables = []string{
	"SystemRoot", "SystemDrive", "ComSpec", "PATHEXT", "WINDIR",
	"ProgramData", "ProgramFiles", "ProgramFiles(x86)", "CommonProgramFiles",
	"CommonProgramFiles(x86)", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE",
	"PROCESSOR_IDENTIFIER", "PROCESSOR_LEVEL", "PROCESSOR_REVISION", "OS",
}

// unixTaskVariables keep locale and timezone behaviour deterministic without
// exposing credentials.
var unixTaskVariables = []string{
	"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "TZ",
}

// taskBaseEnvironment builds the allowlisted base environment. An empty
// homeDir/tmpDir keeps the inherited variables; the task path isolates both.
func taskBaseEnvironment(homeDir, tmpDir, workspace string) []string {
	environment := make([]string, 0, 32)
	if path := os.Getenv("PATH"); path != "" {
		environment = append(environment, "PATH="+path)
	}
	variables := unixTaskVariables
	if os.PathSeparator == '\\' {
		variables = windowsTaskVariables
	}
	for _, name := range variables {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	if homeDir != "" {
		environment = append(environment, "HOME="+homeDir)
		if os.PathSeparator == '\\' {
			environment = append(environment, "USERPROFILE="+homeDir)
		}
	}
	if tmpDir != "" {
		if os.PathSeparator == '\\' {
			environment = append(environment, "TMP="+tmpDir, "TEMP="+tmpDir)
		} else {
			environment = append(environment, "TMPDIR="+tmpDir)
		}
	}
	if workspace != "" {
		environment = append(environment,
			"MMDASH_WORKSPACE="+workspace,
		)
	}
	environment = append(environment,
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONUNBUFFERED=1",
	)
	return environment
}

// taskEnvironment assembles the complete frozen environment of one task: the
// allowlisted base, the documented MMDASH task variables, an optional cached
// virtual environment and the experiment-provided variables.
func taskEnvironment(specEnvironment map[string]string, workspace, outputDir, experimentID, parametersFile, homeDir, tmpDir, venvDir string) ([]string, error) {
	environment := taskBaseEnvironment(homeDir, tmpDir, workspace)
	environment = append(environment,
		"MMDASH_OUTPUT_DIR="+outputDir,
		"MMDASH_EXPERIMENT_ID="+experimentID,
	)
	if parametersFile != "" {
		environment = append(environment, "MMDASH_PARAMETERS_FILE="+parametersFile)
	}
	if venvDir != "" {
		environment = append(environment, "VIRTUAL_ENV="+venvDir)
	}
	// The cached environment's interpreter directory wins over the inherited
	// PATH so `python` inside the task resolves to the frozen environment.
	if venvDir != "" {
		binDir := venvBinDir(venvDir)
		for index, entry := range environment {
			if strings.HasPrefix(entry, "PATH=") {
				environment[index] = "PATH=" + binDir + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
				break
			}
		}
	}
	seen := map[string]int{}
	for _, entry := range environment {
		name := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			name = entry[:index]
		}
		seen[name]++
	}
	for name, value := range specEnvironment {
		if name == "" || strings.ContainsAny(name, "=\x00 \t\r\n") {
			return nil, errors.New("invalid environment variable name")
		}
		if name == "PATH" {
			return nil, errors.New("the task environment must not replace PATH")
		}
		if previous, exists := seen[name]; exists && previous > 0 {
			return nil, errors.New("the task environment must not override the allowlisted variable " + name)
		}
		environment = append(environment, name+"="+value)
	}
	return environment, nil
}

func venvBinDir(venv string) string {
	return filepath.Dir(venvPython("", venv))
}
