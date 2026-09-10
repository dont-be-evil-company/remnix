package config

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	schemaURL     = "https://remnix.app/config.schema.json"
	schemaComment = "# yaml-language-server: $schema=" + schemaURL
)

const defaultYAMLIndent = 4

type yamlIndent struct {
	spaces int
	tabs   bool
}

func marshalUserConfig(c *Config, existing []byte) ([]byte, error) {
	style := detectYAMLIndent(existing)
	var fresh yaml.Node
	if err := fresh.Encode(c); err != nil {
		return nil, err
	}
	toEncode := &fresh
	if len(bytes.TrimSpace(existing)) > 0 {
		var old yaml.Node
		if err := yaml.Unmarshal(existing, &old); err == nil && old.Kind == yaml.DocumentNode && len(old.Content) > 0 {
			applyYAML(&old, &fresh, false)
			tmpl, err := defaultConfigNode()
			if err != nil {
				return nil, err
			}
			fillMissing(&old, tmpl)
			toEncode = &old
		}
	} else {
		tmpl, err := defaultConfigNode()
		if err != nil {
			return nil, err
		}
		applyYAML(tmpl, &fresh, true)
		if c.Sync.Enabled == nil {
			setMappingBool(tmpl, []string{"sync", "enabled"}, c.Sync.IsEnabled())
		}
		toEncode = tmpl
	}
	stripSchemaComments(toEncode)
	return encodeUserYAML(toEncode, style)
}

const defaultUserConfigYAML = `version: 2
disable_auto_migrate: false
sync:
    enabled: false
    interval: 5m
    gc_interval: 1h
    rclone_engine: embedded
    endpoints: []
    callbacks: []
daemon:
    compact_interval: 5m
    config_watch_interval: 30s
suggest:
    enabled: true
    menu: false
    menu_max: 8
    completions: false
    icons:
        typed: "›"
        history: "*"
        completion: "+"
    accept:
        - Right
pty_proxy:
    enabled: false
    height: 100
ui:
    colors:
        accent: "#F5C2E7"
        title: "#CBA6F7"
        muted: "#585B70"
        rule: "#313244"
        badge: "#89B4FA"
        duration: "#A6E3A1"
        failed: "#F38BA8"
        time: "#7F849C"
        text: "#CDD6F4"
        syntax:
            command: "#89B4FA"
            keyword: "#CBA6F7"
            flag: "#FAB387"
            string: "#A6E3A1"
            comment: "#6C7086"
            operator: "#F38BA8"
            variable: "#89DCEB"
            path: "#94E2D5"
            number: "#F9E2AF"
            argument: "#CDD6F4"
    icons:
        cursor: "❯"
        suggestion_typed: "›"
        suggestion_history: "*"
        suggestion_completion: "+"
        separator: "·"
        move_up_down: "↑↓"
`

func defaultConfigNode() (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(defaultUserConfigYAML), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func setMappingBool(root *yaml.Node, path []string, v bool) {
	n := unwrapDoc(root)
	for i, key := range path {
		if n == nil || n.Kind != yaml.MappingNode {
			return
		}
		var val *yaml.Node
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == key {
				val = n.Content[j+1]
				break
			}
		}
		if val == nil {
			return
		}
		if i == len(path)-1 {
			val.Kind = yaml.ScalarNode
			val.Style = 0
			val.Tag = "!!bool"
			if v {
				val.Value = "true"
			} else {
				val.Value = "false"
			}
			val.Content = nil
			return
		}
		n = val
	}
}

func encodeUserYAML(n *yaml.Node, style yamlIndent) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(style.spaces)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	body := buf.Bytes()
	if style.tabs {
		body = leadingSpacesToTabs(body, style.spaces)
	}
	return prependSchemaHeader(body), nil
}

func fillMissing(dst, tmpl *yaml.Node) {
	if dst == nil || tmpl == nil {
		return
	}
	tmpl = unwrapDoc(tmpl)
	if dst.Kind == yaml.DocumentNode {
		if len(dst.Content) == 0 {
			dst.Content = []*yaml.Node{tmpl}
			return
		}
		fillMissing(dst.Content[0], tmpl)
		return
	}
	if tmpl.Kind != yaml.MappingNode || dst.Kind != yaml.MappingNode {
		return
	}
	have := map[string]*yaml.Node{}
	for i := 0; i+1 < len(dst.Content); i += 2 {
		have[dst.Content[i].Value] = dst.Content[i+1]
	}
	for i := 0; i+1 < len(tmpl.Content); i += 2 {
		k, v := tmpl.Content[i], tmpl.Content[i+1]
		if existing, ok := have[k.Value]; ok {
			if existing.Kind == yaml.MappingNode && v.Kind == yaml.MappingNode {
				fillMissing(existing, v)
			}
			continue
		}
		dst.Content = append(dst.Content, k, v)
	}
}

func applyYAML(dst, src *yaml.Node, keep bool) {
	if dst == nil || src == nil {
		return
	}
	src = unwrapDoc(src)
	if dst.Kind == yaml.DocumentNode {
		if len(dst.Content) == 0 {
			dst.Content = []*yaml.Node{src}
			return
		}
		applyYAML(dst.Content[0], src, keep)
		return
	}
	if src.Kind == yaml.MappingNode && dst.Kind == yaml.MappingNode {
		applyMapping(dst, src, keep)
		return
	}
	if src.Kind == yaml.SequenceNode && dst.Kind == yaml.SequenceNode {
		applySequence(dst, src, keep)
		return
	}
	copyValueKeepComments(dst, src)
}

func unwrapDoc(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return n.Content[0]
	}
	return n
}

func applyMapping(dst, src *yaml.Node, keep bool) {
	srcVal := map[string]*yaml.Node{}
	var srcKeys []*yaml.Node
	for i := 0; i+1 < len(src.Content); i += 2 {
		k := src.Content[i]
		srcKeys = append(srcKeys, k)
		srcVal[k.Value] = src.Content[i+1]
	}
	used := map[string]bool{}
	var content []*yaml.Node
	for i := 0; i+1 < len(dst.Content); i += 2 {
		k, v := dst.Content[i], dst.Content[i+1]
		sv, ok := srcVal[k.Value]
		if !ok {
			if keep {
				content = append(content, k, v)
			}
			continue
		}
		applyYAML(v, sv, keep)
		content = append(content, k, v)
		used[k.Value] = true
	}
	for _, k := range srcKeys {
		if used[k.Value] {
			continue
		}
		content = append(content, k, srcVal[k.Value])
	}
	dst.Content = content
}

func applySequence(dst, src *yaml.Node, keep bool) {
	if sequenceHasIDs(src) || sequenceHasIDs(dst) {
		applyIdentifiedSeq(dst, src, keep)
		return
	}
	old := dst.Content
	used := make([]bool, len(old))
	next := make([]*yaml.Node, 0, len(src.Content))
	for _, sn := range src.Content {
		if i := matchSeqItem(old, used, sn); i >= 0 {
			applyYAML(old[i], sn, keep)
			next = append(next, old[i])
			used[i] = true
			continue
		}
		next = append(next, sn)
	}
	dst.Content = next
}

func applyIdentifiedSeq(dst, src *yaml.Node, keep bool) {
	oldByID := map[string]*yaml.Node{}
	var anonymous []*yaml.Node
	for _, n := range dst.Content {
		if id := mappingString(n, "id"); id != "" {
			oldByID[id] = n
			continue
		}
		anonymous = append(anonymous, n)
	}
	next := make([]*yaml.Node, 0, len(src.Content))
	ai := 0
	for _, sn := range src.Content {
		if id := mappingString(sn, "id"); id != "" {
			if old, ok := oldByID[id]; ok {
				applyYAML(old, sn, keep)
				next = append(next, old)
				continue
			}
		}
		if ai < len(anonymous) {
			applyYAML(anonymous[ai], sn, keep)
			next = append(next, anonymous[ai])
			ai++
			continue
		}
		next = append(next, sn)
	}
	dst.Content = next
}

func sequenceHasIDs(n *yaml.Node) bool {
	for _, item := range n.Content {
		if mappingString(item, "id") != "" {
			return true
		}
	}
	return false
}

func matchSeqItem(old []*yaml.Node, used []bool, src *yaml.Node) int {
	if src.Kind == yaml.ScalarNode {
		for i, n := range old {
			if used[i] || n.Kind != yaml.ScalarNode || n.Value != src.Value {
				continue
			}
			return i
		}
		return -1
	}
	for i := range old {
		if !used[i] {
			return i
		}
	}
	return -1
}

func mappingString(n *yaml.Node, key string) string {
	if n == nil || n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1].Value
		}
	}
	return ""
}

func copyValueKeepComments(dst, src *yaml.Node) {
	head, line, foot := dst.HeadComment, dst.LineComment, dst.FootComment
	style := dst.Style
	*dst = yaml.Node{
		Kind:        src.Kind,
		Style:       src.Style,
		Tag:         src.Tag,
		Value:       src.Value,
		Anchor:      src.Anchor,
		Alias:       src.Alias,
		Content:     src.Content,
		HeadComment: head,
		LineComment: line,
		FootComment: foot,
	}
	if style != 0 && dst.Kind == yaml.ScalarNode {
		dst.Style = style
	}
}

func stripSchemaComments(n *yaml.Node) {
	if n == nil {
		return
	}
	n.HeadComment = stripSchemaCommentText(n.HeadComment)
	n.LineComment = stripSchemaCommentText(n.LineComment)
	n.FootComment = stripSchemaCommentText(n.FootComment)
	for _, c := range n.Content {
		stripSchemaComments(c)
	}
}

func stripSchemaCommentText(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	keep := make([]string, 0, len(lines))
	for _, line := range lines {
		if isSchemaComment(strings.TrimSpace(line)) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func detectYAMLIndent(data []byte) yamlIndent {
	def := yamlIndent{spaces: defaultYAMLIndent}
	if len(data) == 0 {
		return def
	}
	for _, raw := range bytes.Split(data, []byte("\n")) {
		line := bytes.TrimRight(raw, "\r")
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		content := bytes.TrimSpace(line[i:])
		if len(content) == 0 || content[0] == '#' || bytes.Equal(content, []byte("---")) || bytes.Equal(content, []byte("...")) {
			continue
		}
		if i == 0 {
			continue
		}
		if content[0] == '-' {
			continue
		}
		ws := line[:i]
		if bytes.IndexByte(ws, '\t') >= 0 {
			return yamlIndent{spaces: defaultYAMLIndent, tabs: true}
		}
		return yamlIndent{spaces: clampYAMLIndent(len(ws))}
	}
	return def
}

func clampYAMLIndent(n int) int {
	if n < 2 {
		return 2
	}
	if n > 9 {
		return 9
	}
	return n
}

func leadingSpacesToTabs(data []byte, width int) []byte {
	width = clampYAMLIndent(width)
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, len(lines))
	for i, line := range lines {
		n := 0
		for n < len(line) && line[n] == ' ' {
			n++
		}
		if n == 0 {
			out[i] = line
			continue
		}
		tabs := n / width
		rem := n % width
		repl := make([]byte, 0, tabs+len(line)-n+rem)
		for j := 0; j < tabs; j++ {
			repl = append(repl, '\t')
		}
		repl = append(repl, line[n-rem:]...)
		out[i] = repl
	}
	return bytes.Join(out, []byte("\n"))
}

func prependSchemaHeader(body []byte) []byte {
	s := stripYAMLPreamble(string(body))
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(schemaComment + "\n---\n" + s)
}

func stripYAMLPreamble(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	for {
		line, rest, found := strings.Cut(s, "\n")
		plain := strings.TrimRight(strings.TrimLeft(line, " \t"), "\r")
		if plain == "" && found {
			s = rest
			continue
		}
		if plain == "---" || plain == "..." || isSchemaComment(plain) {
			if found {
				s = rest
				continue
			}
			return ""
		}
		return s
	}
}

func isSchemaComment(line string) bool {
	return strings.HasPrefix(line, "# yaml-language-server: $schema=")
}
