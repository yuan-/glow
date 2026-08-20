// Package ui provides the main UI for the glow application.
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glow/v3/utils"
	"github.com/charmbracelet/log"
	"github.com/muesli/gitcha"
)

const (
	statusMessageTimeout = time.Second * 3 // how long to show status messages like "stashed!"
	ellipsis             = "…"
)

var markdownExtensions = []string{
	"*.md", "*.mdown", "*.mkdn", "*.mkd", "*.markdown",
}

// NewProgram returns a new Tea program.
func NewProgram(cfg Config, content string) *tea.Program {
	log.Debug(
		"Starting glow",
		"glamour",
		cfg.GlamourEnabled,
	)

	m := newModel(cfg, content)
	return tea.NewProgram(m)
}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

type (
	initLocalFileSearchMsg struct {
		cwd string
		ch  chan gitcha.SearchResult
	}
)

type (
	foundLocalFileMsg       gitcha.SearchResult
	localFileSearchFinished struct{}
	statusMessageTimeoutMsg applicationContext
)

// applicationContext indicates the area of the application something applies
// to. Occasionally used as an argument to commands and messages.
type applicationContext int

const (
	stashContext applicationContext = iota
	pagerContext
)

// state is the top-level application state.
type state int

const (
	stateShowStash state = iota
	stateShowDocument
)

func (s state) String() string {
	return map[state]string{
		stateShowStash:    "showing file listing",
		stateShowDocument: "showing document",
	}[s]
}

// Common stuff we'll need to access in all models.
type commonModel struct {
	cfg    Config
	cwd    string
	width  int
	height int
	styles Styles
}

type model struct {
	common   *commonModel
	state    state
	fatalErr error

	// Sub-models
	stash stashModel
	pager pagerModel

	// Bookmarks（跨文件共享，持久化到磁碟）
	bookmarks *bookmarkStore

	// 記住每個檔案的捲動位置（僅限本次執行期間）：路徑 → 渲染內容行號
	positionMem map[string]int

	// Channel that receives paths to local markdown files
	// (via the github.com/muesli/gitcha package)
	localFileFinder chan gitcha.SearchResult
}

// pagerSubmode reports whether the pager has an active submode (TOC panel,
// search, or bookmark list) that should consume navigation keys before the
// top level handles them.
func pagerSubmode(m model) bool {
	return m.state == stateShowDocument &&
		(m.pager.toc.visible() || m.pager.search.IsSearching() || m.pager.bookmarkList.visible)
}

// unloadDocument unloads a document from the pager. Note that while this
// method alters the model we also need to send along any commands returned.
func (m *model) unloadDocument() []tea.Cmd {
	// 離開前記住目前捲動位置（再回來時還原）
	if m.state == stateShowDocument && m.pager.currentDocument.localPath != "" {
		m.positionMem[m.pager.currentDocument.localPath] = m.pager.viewport.YOffset()
	}
	m.state = stateShowStash
	m.stash.viewState = stashStateReady
	m.pager.unload()
	m.pager.showHelp = false
	// 清空目前文件，讓「重新進入同一檔案」可以區別於「重新載入同一檔案」
	m.pager.currentDocument = markdown{}

	var batch []tea.Cmd
	if !m.stash.shouldSpin() {
		batch = append(batch, m.stash.spinner.Tick)
	}
	// 本地文件搜尋尚未啟動過（例如直接指定檔案啟動）：
	// 現在啟動，讓回文件列表時內容已就緒
	if !m.stash.loaded {
		batch = append(batch, findLocalFiles(*m.common))
	}
	return batch
}

func newModel(cfg Config, content string) tea.Model {
	common := commonModel{
		cfg:    cfg,
		styles: newStyles(true),
	}

	m := model{
		common:      &common,
		state:       stateShowStash,
		pager:       newPagerModel(&common),
		stash:       newStashModel(&common),
		bookmarks:   newBookmarkStore(),
		positionMem: make(map[string]int),
	}
	m.pager.bookmarks = m.bookmarks

	path := cfg.Path
	if path == "" && content != "" {
		m.state = stateShowDocument
		m.pager.currentDocument = markdown{Body: content}
		return m
	}

	if path == "" {
		path = "."
	}
	info, err := os.Stat(path)
	if err != nil {
		log.Error("unable to stat file", "file", path, "error", err)
		m.fatalErr = err
		return m
	}
	if info.IsDir() {
		m.state = stateShowStash
	} else {
		cwd, _ := os.Getwd()
		m.state = stateShowDocument
		m.pager.currentDocument = markdown{
			localPath: path,
			Note:      stripAbsolutePath(path, cwd),
			Modtime:   info.ModTime(),
		}
		// 讀取並存入原始內容（供 TOC / 搜尋 / 複製使用；重新載入時由
		// loadLocalMarkdown 更新）。Init 會再讀一次用於渲染。
		if content, err := os.ReadFile(path); err == nil {
			m.pager.currentDocument.Body = string(utils.RemoveFrontmatter(content))
		}
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.stash.spinner.Tick, tea.RequestBackgroundColor}

	switch m.state {
	case stateShowStash:
		cmds = append(cmds, findLocalFiles(*m.common))
	case stateShowDocument:
		content, err := os.ReadFile(m.common.cfg.Path)
		if err != nil {
			log.Error("unable to read file", "file", m.common.cfg.Path, "error", err)
			return func() tea.Msg { return errMsg{err} }
		}
		body := string(utils.RemoveFrontmatter(content))
		cmds = append(cmds, renderWithGlamour(m.pager, body))
	}

	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If there's been an error, any key exits
	if m.fatalErr != nil {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.common.styles = newStyles(msg.IsDark())
		m.stash.stylePaginators(m.common.styles)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if pagerSubmode(m) {
				// TOC / 搜尋開啟時 esc 交給 pager 關閉子模式，
				// 而不是直接回文件列表
				break
			}
			if m.state == stateShowDocument || m.stash.viewState == stashStateLoadingDocument {
				batch := m.unloadDocument()
				return m, tea.Batch(batch...)
			}
		case "r":
			var cmd tea.Cmd
			if m.state == stateShowStash {
				// pass through all keys if we're editing the filter
				if m.stash.filterState == filtering {
					m.stash, cmd = m.stash.update(msg)
					return m, cmd
				}
				m.stash.markdowns = nil
				return m, m.Init()
			}

		case "q":
			if pagerSubmode(m) {
				// 搜尋 / TOC 開啟時 q 交給 pager 結束子模式，
				// 避免輸入 q 直接離開程式
				break
			}
			var cmd tea.Cmd

			switch m.state { //nolint:exhaustive
			case stateShowStash:
				// pass through all keys if we're editing the filter
				if m.stash.filterState == filtering {
					m.stash, cmd = m.stash.update(msg)
					return m, cmd
				}
			}

			return m, tea.Quit

		case "left", "h", "delete":
			if pagerSubmode(m) {
				// TOC / 搜尋開啟時 left/h 交給 pager，
				// 避免輸入 h 直接回文件列表
				break
			}
			if m.state == stateShowDocument {
				cmds = append(cmds, m.unloadDocument()...)
				return m, tea.Batch(cmds...)
			}

		case "ctrl+z":
			return m, tea.Suspend

		// Ctrl+C always quits no matter where in the application you are.
		case "ctrl+c":
			return m, tea.Quit
		}

	// Window size is received when starting up and on every resize
	case tea.WindowSizeMsg:
		m.common.width = msg.Width
		m.common.height = msg.Height
		m.stash.setSize(msg.Width, msg.Height)
		m.pager.setSize(msg.Width, msg.Height)

	case initLocalFileSearchMsg:
		m.localFileFinder = msg.ch
		m.common.cwd = msg.cwd
		cmds = append(cmds, findNextLocalFile(m))

	case fetchedMarkdownMsg:
		// We've loaded a markdown file's contents for rendering
		curPath := m.pager.currentDocument.localPath
		m.pager.currentDocument = *msg
		// 記住的捲動位置：
		// - 重新載入同一檔案（r / 編輯後）→ 保持在目前位置
		// - 重新進入同一檔案 → 還原上次離開的位置
		if msg.localPath != "" {
			if msg.localPath == curPath {
				m.pager.pendingRestoreY = m.pager.viewport.YOffset()
			} else if y, ok := m.positionMem[msg.localPath]; ok {
				m.pager.pendingRestoreY = y
			}
		}
		body := string(utils.RemoveFrontmatter([]byte(msg.Body)))
		cmds = append(cmds, renderWithGlamour(m.pager, body))

	case contentRenderedMsg:
		m.state = stateShowDocument

	case localFileSearchFinished:
		// Always pass these messages to the stash so we can keep it updated
		// about network activity, even if the user isn't currently viewing
		// the stash.
		stashModel, cmd := m.stash.update(msg)
		m.stash = stashModel
		return m, cmd

	case foundLocalFileMsg:
		newMd := localFileToMarkdown(m.common.cwd, gitcha.SearchResult(msg))
		m.stash.addMarkdowns(newMd)
		if m.stash.filterApplied() {
			newMd.buildFilterValue()
		}
		if m.stash.shouldUpdateFilter() {
			cmds = append(cmds, filterMarkdowns(m.stash))
		}
		cmds = append(cmds, findNextLocalFile(m))

	case filteredMarkdownMsg:
		if m.state == stateShowDocument {
			newStashModel, cmd := m.stash.update(msg)
			m.stash = newStashModel
			cmds = append(cmds, cmd)
		}
	}

	// Process children
	switch m.state {
	case stateShowStash:
		newStashModel, cmd := m.stash.update(msg)
		m.stash = newStashModel
		cmds = append(cmds, cmd)

	case stateShowDocument:
		newPagerModel, cmd := m.pager.update(msg)
		m.pager = newPagerModel
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	var content string
	switch {
	case m.fatalErr != nil:
		content = errorView(m.common.styles, m.fatalErr, true)
	case m.state == stateShowDocument:
		content = m.pager.View()
	default:
		content = m.stash.view()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	if m.common.cfg.EnableMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func errorView(styles Styles, err error, fatal bool) string {
	exitMsg := "press any key to "
	if fatal {
		exitMsg += "exit"
	} else {
		exitMsg += "return"
	}
	s := fmt.Sprintf("%s\n\n%v\n\n%s",
		styles.errorTitleStyle.Render("ERROR"),
		err,
		styles.subtleStyle.Render(exitMsg),
	)
	return "\n" + indent(s, 3)
}

// COMMANDS

func findLocalFiles(m commonModel) tea.Cmd {
	return func() tea.Msg {
		log.Info("findLocalFiles")
		var (
			cwd = m.cfg.Path
			err error
		)

		if cwd == "" {
			cwd, err = os.Getwd()
		} else {
			var info os.FileInfo
			info, err = os.Stat(cwd)
			if err == nil && info.IsDir() {
				cwd, err = filepath.Abs(cwd)
			} else if err == nil && !info.IsDir() {
				// 直接指定檔案啟動時：以該檔案所在目錄為搜尋根
				cwd, err = filepath.Abs(filepath.Dir(cwd))
			}
		}

		// Note that this is one error check for both cases above
		if err != nil {
			log.Error("error finding local files", "error", err)
			return errMsg{err}
		}

		log.Debug("local directory is", "cwd", cwd)

		// Switch between FindFiles and FindAllFiles to bypass .gitignore rules
		var ch chan gitcha.SearchResult
		if m.cfg.ShowAllFiles {
			ch, err = gitcha.FindAllFilesExcept(cwd, markdownExtensions, nil)
		} else {
			ch, err = gitcha.FindFilesExcept(cwd, markdownExtensions, ignorePatterns(m))
		}

		if err != nil {
			log.Error("error finding local files", "error", err)
			return errMsg{err}
		}

		return initLocalFileSearchMsg{ch: ch, cwd: cwd}
	}
}

func findNextLocalFile(m model) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-m.localFileFinder

		if ok {
			// Okay now find the next one
			return foundLocalFileMsg(res)
		}
		// We're done
		log.Debug("local file search finished")
		return localFileSearchFinished{}
	}
}

func waitForStatusMessageTimeout(appCtx applicationContext, t *time.Timer) tea.Cmd {
	return func() tea.Msg {
		<-t.C
		return statusMessageTimeoutMsg(appCtx)
	}
}

// ETC

// Convert a Gitcha result to an internal representation of a markdown
// document. Note that we could be doing things like checking if the file is
// a directory, but we trust that gitcha has already done that.
func localFileToMarkdown(cwd string, res gitcha.SearchResult) *markdown {
	return &markdown{
		localPath: res.Path,
		Note:      stripAbsolutePath(res.Path, cwd),
		Modtime:   res.Info.ModTime(),
	}
}

func stripAbsolutePath(fullPath, cwd string) string {
	fp, _ := filepath.EvalSymlinks(fullPath)
	cp, _ := filepath.EvalSymlinks(cwd)
	return strings.ReplaceAll(fp, cp+string(os.PathSeparator), "")
}

// Lightweight version of reflow's indent function.
func indent(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	l := strings.Split(s, "\n")
	b := strings.Builder{}
	i := strings.Repeat(" ", n)
	for _, v := range l {
		fmt.Fprintf(&b, "%s%s\n", i, v)
	}
	return b.String()
}
