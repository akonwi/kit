package skills

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RexName is the identity of Kit's optional Rex skill, which a user or
// project skill of the same name replaces.
const RexName = "rex"

//go:embed rex.md
var rexDefinition string

// rexAppCommand is where Rex.app installs the rex command, under the user's
// home directory on macOS.
var rexAppCommand = filepath.Join("Library", "Application Support", "com.superlogical.rex", "RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS", "rex")

// rexSystemCommands are where Rex's Linux package and its single-binary
// install put the rex command, for a server whose PATH leaves them out, as a
// service manager's may.
var rexSystemCommands = []string{"/usr/local/bin/rex", "/usr/bin/rex"}

// FindRex returns the path of the rex command on this machine, or empty: the
// one in REX_BIN_DIR, then on PATH, then where Rex.app installs it, then where
// Rex installs it on Linux. It only looks for the file, and runs nothing.
func FindRex(getenv func(string) string, lookPath func(string) (string, error), home string) string {
	candidates := make([]string, 0, 3+len(rexSystemCommands))
	if directory := getenv("REX_BIN_DIR"); filepath.IsAbs(directory) {
		candidates = append(candidates, filepath.Join(directory, "rex"))
	}
	if found, err := lookPath("rex"); err == nil && filepath.IsAbs(found) {
		candidates = append(candidates, found)
	}
	if filepath.IsAbs(home) {
		candidates = append(candidates, filepath.Join(home, rexAppCommand))
	}
	candidates = append(candidates, rexSystemCommands...)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

// findRexCommand finds the rex command for new loaders. Tests replace it so
// their results don't depend on the machine running them.
var findRexCommand = findRexHere

// findRexHere is FindRex for this process.
func findRexHere() string {
	home, _ := os.UserHomeDir()
	return FindRex(os.Getenv, exec.LookPath, home)
}

// rexSkill is the Rex skill for the rex command at path.
func rexSkill(path string) Skill {
	skill := embeddedSkill(RexName, rexDefinition)
	skill.Content = strings.Replace(skill.Content, "{{rex}}", shellQuote(path), 1)
	return skill
}

// shellQuote quotes value for a POSIX shell.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
