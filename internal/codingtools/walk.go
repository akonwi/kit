package codingtools

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/akonwi/kit/internal/ignorefile"
)

var errStopWalk = errors.New("stop walk")

type walkEntry struct {
	Absolute string
	Relative string
	IsDir    bool
}

func walkWorkspace(ctx context.Context, root string, visit func(walkEntry) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return visit(walkEntry{Absolute: root, Relative: filepath.Base(root)})
	}

	var walkDirectory func(string, string, []ignorefile.Rule, bool) error
	walkDirectory = func(directory, relativeDirectory string, inherited []ignorefile.Rule, parentIgnored bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rules := inherited
		if !parentIgnored {
			local, err := readIgnoreRules(ctx, filepath.Join(directory, ".gitignore"), filepath.ToSlash(relativeDirectory))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if len(local) > 0 {
				rules = append(append([]ignorefile.Rule(nil), inherited...), local...)
			}
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.Name() == ".git" && entry.IsDir() {
				continue
			}
			relative := entry.Name()
			if relativeDirectory != "" {
				relative = filepath.Join(relativeDirectory, entry.Name())
			}
			relative = filepath.ToSlash(relative)
			ignored := ignorefile.Ignored(relative, entry.IsDir(), parentIgnored, rules)
			item := walkEntry{
				Absolute: filepath.Join(directory, entry.Name()),
				Relative: relative,
				IsDir:    entry.IsDir(),
			}
			if !ignored {
				if err := visit(item); err != nil {
					return err
				}
			}
			if entry.IsDir() && !ignored {
				if err := walkDirectory(item.Absolute, relative, rules, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walkDirectory(root, "", nil, false)
}

func readIgnoreRules(ctx context.Context, filePath, base string) ([]ignorefile.Rule, error) {
	file, err := openRegularFile(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ignorefile.Parse(ctx, file, base)
}
