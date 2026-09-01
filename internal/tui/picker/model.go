package picker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type mode int

const (
	modeBrowse mode = iota
	modeFilter
	modeMkdir
	modeRename
	modeDeleteEmpty
	modeDeleteRecursive
	modeDest
	modeHelp
	modePath
)

type Result struct {
	Path     string
	Canceled bool
	Join     bool
	Choice   DestChoice
}

type Options struct {
	Title       string
	Start       string
	ConfirmDest bool
	CreateMode  bool
}

type Model struct {
	fs          BrowserFS
	opts        Options
	path        textinput.Model
	prompt      textinput.Model
	entries     []Entry
	visible     []Entry
	cursor      int
	showHidden  bool
	filter      string
	mode        mode
	status      string
	errMsg      string
	width       int
	height      int
	selected    string
	quitting    bool
	canceled    bool
	join        bool
	choice      DestChoice
	dest        DestReport
	destChoices []DestChoice
	destCursor  int
	pendingDel  string
}

func New(fs BrowserFS, opts Options) Model {
	if opts.Title == "" {
		opts.Title = "Select folder"
	}
	start := opts.Start
	if start == "" {
		start = fs.Current()
	}
	start = ExpandPath(start)
	fs.SetCurrent(start)

	pi := textinput.New()
	pi.SetValue(start)
	pi.Focus()
	pr := textinput.New()
	m := Model{
		fs:     fs,
		opts:   opts,
		path:   pi,
		prompt: pr,
		width:  80,
		height: 24,
	}
	if fs.Capabilities().Expensive {
		m.status = "Listing..."
	} else {
		m.reloadSync()
	}
	return m
}

type listedMsg struct {
	path    string
	entries []Entry
	err     error
}

func (m Model) Selected() Result {
	return Result{Path: m.selected, Canceled: m.canceled, Join: m.join, Choice: m.choice}
}

func (m *Model) reload() tea.Cmd {
	if m.fs.Capabilities().Expensive {
		m.status = "Listing..."
		m.entries = nil
		m.visible = nil
		return m.listCmd()
	}
	m.reloadSync()
	return nil
}

func (m *Model) reloadSync() {
	ctx := context.Background()
	cur := m.fs.Current()
	entries, err := m.fs.List(ctx, cur)
	if err != nil {
		m.errMsg = fmt.Sprintf("Cannot open directory: %v", err)
		m.entries = nil
		m.visible = nil
		return
	}
	m.errMsg = ""
	m.entries = entries
	m.applyFilter()
	m.path.SetValue(cur)
}

func (m Model) listCmd() tea.Cmd {
	cur := m.fs.Current()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		entries, err := m.fs.List(ctx, cur)
		return listedMsg{path: cur, entries: entries, err: err}
	}
}

func (m *Model) applyFilter() {
	m.visible = nil
	parent := m.fs.Parent(m.fs.Current())
	if parent != m.fs.Current() {
		m.visible = append(m.visible, Entry{Name: "..", Path: parent, IsDir: true, DisplaySuffix: "/"})
	}
	q := strings.ToLower(m.filter)
	for _, e := range m.entries {
		if !m.showHidden && hiddenName(e.Name) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		m.visible = append(m.visible, e)
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.path.Focus()}
	if m.fs.Capabilities().Expensive {
		cmds = append(cmds, m.listCmd())
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		return m, nil
	case listedMsg:
		if msg.path != m.fs.Current() {
			return m, nil
		}
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Cannot open directory: %v", msg.err)
			m.entries = nil
			m.visible = nil
			m.status = "type a path (Ctrl+L) or create a folder with n"
			return m, nil
		}
		m.errMsg = ""
		m.status = ""
		m.entries = msg.entries
		m.applyFilter()
		m.path.SetValue(msg.path)
		return m, nil
	case tea.KeyReleaseMsg:
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.quitting = true
		m.canceled = true
		return m, tea.Quit
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeBrowse
		return m, nil
	case modeMkdir, modeRename, modeDeleteRecursive, modeFilter, modePath:
		return m.handlePrompt(msg)
	case modeDeleteEmpty:
		return m.handleDeleteEmpty(key)
	case modeDest:
		return m.handleDest(key)
	}
	switch key {
	case "esc":
		m.quitting = true
		m.canceled = true
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "ctrl+n":
		if m.cursor+1 < len(m.visible) {
			m.cursor++
		}
		return m, nil
	case "enter":
		return m.openOrJump()
	case " ", "space":
		return m.selectHighlighted()
	case "ctrl+enter":
		return m.selectCurrent()
	case "tab":
		return m.completePath()
	case "ctrl+l":
		return m.beginPrompt(modePath, "Path:", m.fs.Current())
	case "n":
		return m.beginPrompt(modeMkdir, "New folder name:", "")
	case "r":
		return m.beginRename()
	case "d":
		return m.beginDelete()
	case "h":
		return m.goParent()
	case ".":
		m.showHidden = !m.showHidden
		m.applyFilter()
		return m, nil
	case "/":
		return m.beginPrompt(modeFilter, "Filter:", m.filter)
	}
	var cmd tea.Cmd
	m.path, cmd = m.path.Update(msg)
	return m, cmd
}

func (m Model) handlePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		m.mode = modeBrowse
		m.status = ""
		return m, nil
	}
	if key == "enter" {
		val := strings.TrimSpace(m.prompt.Value())
		switch m.mode {
		case modeFilter:
			m.filter = val
			m.mode = modeBrowse
			m.applyFilter()
		case modePath:
			m.path.SetValue(val)
			m.mode = modeBrowse
			return m.openOrJump()
		case modeMkdir:
			return m.createDir(val)
		case modeRename:
			return m.renameSelected(val)
		case modeDeleteRecursive:
			if val == filepath.Base(m.pendingDel) {
				return m.doDelete(true)
			}
			m.status = "name did not match; delete cancelled"
			m.mode = modeBrowse
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m Model) beginPrompt(md mode, title, value string) (tea.Model, tea.Cmd) {
	m.mode = md
	m.status = title
	m.prompt.SetValue(value)
	m.prompt.Focus()
	return m, m.prompt.Focus()
}

func (m Model) createDir(name string) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	if err := ValidateDirName(name); err != nil {
		m.status = err.Error()
		return m, nil
	}
	p := m.fs.Join(m.fs.Current(), name)
	if err := m.fs.Mkdir(context.Background(), p); err != nil {
		m.status = err.Error()
		return m, nil
	}
	cmd := m.reload()
	m.focusNamed(name)
	m.status = "created " + name
	return m, cmd
}

func (m *Model) focusNamed(name string) {
	for i, e := range m.visible {
		if e.Name == name {
			m.cursor = i
			return
		}
	}
}

func (m Model) highlighted() (Entry, bool) {
	if len(m.visible) == 0 || m.cursor < 0 || m.cursor >= len(m.visible) {
		return Entry{}, false
	}
	return m.visible[m.cursor], true
}

func (m Model) beginRename() (tea.Model, tea.Cmd) {
	e, ok := m.highlighted()
	if !ok || e.Name == ".." {
		return m, nil
	}
	if IsProtected(e.Path) || IsFSRoot(e.Path) {
		m.status = "cannot rename a protected path"
		return m, nil
	}
	return m.beginPrompt(modeRename, "Rename:", e.Name)
}

func (m Model) renameSelected(name string) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	e, ok := m.highlighted()
	if !ok {
		return m, nil
	}
	if err := ValidateDirName(name); err != nil {
		m.status = err.Error()
		return m, nil
	}
	dst := m.fs.Join(m.fs.Current(), name)
	if err := m.fs.Rename(context.Background(), e.Path, dst); err != nil {
		m.status = err.Error()
		return m, nil
	}
	cmd := m.reload()
	m.focusNamed(name)
	return m, cmd
}

func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	e, ok := m.highlighted()
	if !ok || e.Name == ".." {
		return m, nil
	}
	if err := CanDelete(e.Path); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.pendingDel = e.Path
	empty, err := m.fs.IsEmpty(context.Background(), e.Path)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	if empty {
		m.mode = modeDeleteEmpty
		m.status = fmt.Sprintf("Delete %q?", e.Name)
		return m, nil
	}
	n, exact, _ := m.fs.CountChildren(context.Background(), e.Path, 5000)
	extra := fmt.Sprintf("%d entries", n)
	if !exact {
		extra = "contents (count truncated)"
	}
	m.mode = modeDeleteRecursive
	m.status = fmt.Sprintf("%q is not empty (%s). Type the folder name to confirm recursive delete:", e.Name, extra)
	m.prompt.SetValue("")
	m.prompt.Focus()
	return m, m.prompt.Focus()
}

func (m Model) handleDeleteEmpty(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "n":
		m.mode = modeBrowse
		m.status = ""
		return m, nil
	case "enter", "y":
		return m.doDelete(false)
	}
	return m, nil
}

func (m Model) doDelete(recursive bool) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	if err := CanDelete(m.pendingDel); err != nil {
		m.status = err.Error()
		return m, nil
	}
	if err := m.fs.Remove(context.Background(), m.pendingDel, recursive); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = "deleted " + filepath.Base(m.pendingDel)
	return m, m.reload()
}

func (m Model) goParent() (tea.Model, tea.Cmd) {
	p := m.fs.Parent(m.fs.Current())
	m.fs.SetCurrent(p)
	m.cursor = 0
	return m, m.reload()
}

func (m Model) openOrJump() (tea.Model, tea.Cmd) {
	typed := strings.TrimSpace(m.path.Value())
	if typed != "" && ExpandPath(typed) != m.fs.Current() {
		target := ExpandPath(typed)
		e, err := m.fs.Stat(context.Background(), target)
		if err != nil {
			m.status = fmt.Sprintf("Cannot open directory: %v", err)
			return m, nil
		}
		if !e.IsDir {
			m.status = "not a directory"
			return m, nil
		}
		m.fs.SetCurrent(target)
		m.cursor = 0
		return m, m.reload()
	}
	e, ok := m.highlighted()
	if !ok || !e.IsDir {
		return m, nil
	}
	if e.Inaccessible {
		m.status = "Cannot open directory: permission denied"
		return m, nil
	}
	m.fs.SetCurrent(e.Path)
	m.cursor = 0
	return m, m.reload()
}

func (m Model) selectHighlighted() (tea.Model, tea.Cmd) {
	e, ok := m.highlighted()
	if !ok {
		return m.selectCurrent()
	}
	if e.Name == ".." {
		return m.selectCurrent()
	}
	if !e.IsDir {
		return m, nil
	}
	return m.finishSelect(e.Path)
}

func (m Model) selectCurrent() (tea.Model, tea.Cmd) {
	return m.finishSelect(m.fs.Current())
}

func (m Model) finishSelect(path string) (tea.Model, tea.Cmd) {
	if !m.opts.ConfirmDest {
		m.selected = path
		m.choice = ChoiceUse
		m.quitting = true
		return m, tea.Quit
	}
	rep, err := ClassifyDest(context.Background(), path)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.dest = rep
	m.destChoices = ChoicesFor(rep.Kind)
	m.destCursor = 0
	m.mode = modeDest
	return m, nil
}

func (m Model) handleDest(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.mode = modeBrowse
		return m, nil
	case "up", "ctrl+p":
		if m.destCursor > 0 {
			m.destCursor--
		}
	case "down", "ctrl+n":
		if m.destCursor+1 < len(m.destChoices) {
			m.destCursor++
		}
	case "enter":
		ch := m.destChoices[m.destCursor]
		switch ch {
		case ChoiceCancel, ChoiceSelectAnother:
			if ch == ChoiceCancel {
				m.canceled = true
				m.quitting = true
				return m, tea.Quit
			}
			m.mode = modeBrowse
			return m, nil
		case ChoiceJoin:
			m.selected = m.dest.Path
			m.join = true
			m.choice = ch
			m.quitting = true
			return m, tea.Quit
		case ChoiceCreateSubfolder:
			p := m.fs.Join(m.dest.Path, "syncsh")
			_ = m.fs.Mkdir(context.Background(), p)
			m.selected = p
			m.choice = ch
			m.quitting = true
			return m, tea.Quit
		case ChoiceUse:
			if m.dest.Kind == DestSyncshSubdirExists {
				m.selected = m.dest.Subdir
			} else {
				m.selected = m.dest.Path
			}
			m.choice = ch
			m.quitting = true
			return m, tea.Quit
		case ChoiceProceedAnyway:
			m.selected = m.dest.Path
			m.choice = ch
			m.quitting = true
			return m, tea.Quit
		case ChoiceInspect, ChoiceChooseSubfolderName:
			m.status = m.dest.Probe.Message
			m.mode = modeBrowse
			m.choice = ch
			return m, nil
		}
	}
	return m, nil
}

func (m Model) completePath() (tea.Model, tea.Cmd) {
	done, matches, err := Complete(context.Background(), m.fs, m.path.Value(), m.showHidden)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.path.SetValue(done)
	if len(matches) > 1 {
		m.status = strings.Join(matches, "  ")
	} else if len(matches) == 1 {
		target := ExpandPath(done)
		if st, err := m.fs.Stat(context.Background(), strings.TrimRight(target, string(filepath.Separator))); err == nil && st.IsDir {
			m.fs.SetCurrent(st.Path)
			return m, m.reload()
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	var b strings.Builder
	title := m.opts.Title
	if title == "" {
		title = "Select folder"
	}
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "Path: %s\n\n", m.path.Value())
	if m.mode == modeHelp {
		b.WriteString("Enter open   Space select highlighted   Ctrl+Enter select current\n")
		b.WriteString("n new   r rename   d delete   h parent   . hidden   / filter   Tab complete\n")
		b.WriteString("Esc cancel   ? help\n")
		return altView(b.String())
	}
	if m.mode == modeDest {
		b.WriteString(destMessage(m.dest) + "\n\n")
		for i, ch := range m.destChoices {
			cur := "  "
			if i == m.destCursor {
				cur = "> "
			}
			fmt.Fprintf(&b, "%s%s\n", cur, destChoiceLabel(ch, m.dest))
		}
		return altView(b.String())
	}
	if m.errMsg != "" {
		fmt.Fprintf(&b, "%s\n", m.errMsg)
	}
	for i, e := range m.visible {
		cur := "  "
		if i == m.cursor {
			cur = "> "
		}
		mark := e.DisplaySuffix
		if e.Inaccessible {
			mark = " (permission denied)"
		}
		fmt.Fprintf(&b, "%s%s%s\n", cur, e.Name, mark)
	}
	if m.mode == modeMkdir || m.mode == modeRename || m.mode == modeDeleteRecursive || m.mode == modeFilter || m.mode == modePath {
		fmt.Fprintf(&b, "\n%s %s", m.status, m.prompt.View())
	} else if m.mode == modeDeleteEmpty {
		fmt.Fprintf(&b, "\n%s  Enter delete   Esc cancel\n", m.status)
	} else if m.status != "" {
		fmt.Fprintf(&b, "\n%s\n", m.status)
	}
	b.WriteString("\n↑/↓ move   Enter open   Space/Ctrl+Enter select   n new folder   r rename   d delete   ? help\n")
	return altView(b.String())
}

func altView(s string) tea.View {
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func destMessage(d DestReport) string {
	switch d.Kind {
	case DestEmpty:
		return "Use " + d.Path
	case DestUnrelated:
		return "The selected folder is not empty.\n\nsyncsh will create files and directories such as:\n  metadata/\n  keys/\n  events/\n  checkpoints/\n  acks/"
	case DestValidRepo:
		return "An existing syncsh repository was found here."
	case DestPartialRepo, DestUnsupported:
		return "This folder contains files that look like an incomplete or damaged\nsyncsh repository.\n\n" + d.Probe.Message
	case DestSyncshSubdirExists:
		return "A \"syncsh\" subfolder already exists."
	default:
		return d.Probe.Message
	}
}

func destChoiceLabel(ch DestChoice, d DestReport) string {
	switch ch {
	case ChoiceUse:
		if d.Kind == DestSyncshSubdirExists {
			return "Use " + d.Subdir
		}
		return "Use " + d.Path
	case ChoiceCreateSubfolder:
		return `Create a "syncsh" subfolder here`
	case ChoiceSelectAnother:
		return "Select another folder"
	case ChoiceProceedAnyway:
		return "Proceed anyway"
	case ChoiceJoin:
		return "Join the existing repository"
	case ChoiceChooseSubfolderName:
		return "Choose a different subfolder name"
	case ChoiceInspect:
		return "Inspect / run diagnostics"
	case ChoiceCancel:
		return "Cancel"
	default:
		return ""
	}
}

func Run(fs BrowserFS, opts Options) (Result, error) {
	m := New(fs, opts)
	prog := tea.NewProgram(m)
	final, err := prog.Run()
	if err != nil {
		return Result{Canceled: true}, err
	}
	return final.(Model).Selected(), nil
}
