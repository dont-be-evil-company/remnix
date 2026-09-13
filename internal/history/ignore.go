package history

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
)

type ignoreRules struct {
	exact map[string]struct{}
	res   []*regexp.Regexp
}

func (r ignoreRules) match(command string) bool {
	if _, ok := r.exact[command]; ok {
		return true
	}
	for _, re := range r.res {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

type fileStamp struct {
	path  string
	mtime time.Time
	ok    bool
}

func stampFile(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{path: path}
	}
	return fileStamp{path: path, mtime: st.ModTime(), ok: true}
}

type ignoreCache struct {
	mu       sync.Mutex
	txtPath  string
	rePath   string
	txtMtime time.Time
	reMtime  time.Time
	txtOK    bool
	reOK     bool
	rules    ignoreRules
}

var userIgnore ignoreCache

func (c *ignoreCache) match(command string) bool {
	return c.rulesFor().match(command)
}

func (c *ignoreCache) rulesFor() ignoreRules {
	txt := stampFile(config.IgnoreCommandsPath())
	re := stampFile(config.IgnoreCommandsRegexPath())
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.txtPath == txt.path && c.rePath == re.path &&
		c.txtOK == txt.ok && c.reOK == re.ok &&
		c.txtMtime.Equal(txt.mtime) && c.reMtime.Equal(re.mtime) {
		return c.rules
	}
	c.txtPath, c.rePath = txt.path, re.path
	c.txtMtime, c.reMtime = txt.mtime, re.mtime
	c.txtOK, c.reOK = txt.ok, re.ok
	c.rules = loadIgnore(txt, re)
	return c.rules
}

func loadIgnore(txt, re fileStamp) ignoreRules {
	rules := ignoreRules{exact: map[string]struct{}{}}
	if txt.ok {
		for _, line := range readIgnoreLines(txt.path) {
			rules.exact[line] = struct{}{}
		}
	}
	if re.ok {
		for _, line := range readIgnoreLines(re.path) {
			compiled, err := regexp.Compile(line)
			if err != nil {
				slog.Warn("remnix: ignore-commands.regex", "line", line, "err", err)
				continue
			}
			rules.res = append(rules.res, compiled)
		}
	}
	return rules
}

func readIgnoreLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
