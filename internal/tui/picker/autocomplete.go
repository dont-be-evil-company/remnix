package picker

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dont-be-evil-company/remnix/internal/config"
)

func ExpandPath(s string) string {
	return config.Expand(strings.TrimSpace(s))
}

func Complete(ctx context.Context, fs BrowserFS, typed string, showHidden bool) (completed string, matches []string, err error) {
	typed = strings.TrimSpace(typed)
	expanded := ExpandPath(typed)
	dir := expanded
	prefix := ""
	if !strings.HasSuffix(typed, "/") && !strings.HasSuffix(typed, string(filepath.Separator)) {
		dir = fs.Parent(expanded)
		prefix = strings.ToLower(filepath.Base(expanded))
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	entries, err := fs.List(ctx, dir)
	if err != nil {
		return typed, nil, err
	}
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		if !showHidden && hiddenName(e.Name) {
			continue
		}
		if prefix == "" || strings.HasPrefix(strings.ToLower(e.Name), prefix) {
			matches = append(matches, e.Name)
		}
	}
	if len(matches) == 1 {
		return fs.Join(dir, matches[0]) + string(filepath.Separator), matches, nil
	}
	if len(matches) > 1 && prefix != "" {
		common := longestCommonPrefix(matches)
		if len(common) > len(prefix) {
			return fs.Join(dir, common), matches, nil
		}
	}
	return typed, matches, nil
}

func longestCommonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	p := names[0]
	for _, n := range names[1:] {
		i := 0
		pl, nl := strings.ToLower(p), strings.ToLower(n)
		for i < len(pl) && i < len(nl) && pl[i] == nl[i] {
			i++
		}
		p = p[:i]
		if p == "" {
			return ""
		}
	}
	return p
}
