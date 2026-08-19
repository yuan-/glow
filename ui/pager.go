package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glow/v3/utils"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/ansi"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/termenv"
)

const (
	statusBarHeight = 1
	lineNumberWidth = 4
)

var pagerHelpHeight int

type (
	contentRenderedMsg string
	reloadMsg          struct{}
)

type pagerState int

const (
	pagerStateBrowse pagerState = iota
	pagerStateStatusMessage
)

type pagerModel struct {
	common   *commonModel
	viewport viewport.Model
	state    pagerState
	showHelp bool

	statusMessage      string
	statusMessageTimer *time.Timer

	// Current document being rendered, sans-glamour rendering. We cache
	// it here so we can re-render it on resize.
	currentDocument markdown

	watcher *fsnotify.Watcher

	// TOC (Table of Contents) support
	toc         tocModel
	tocRaw      string // 原始 Markdown 內容，用於提取標題
	tocRendered string // 渲染後的內容，用於 TOC 行號定位

	// Search support
	search        SearchModel
	preSearchYOff int // 啟動搜尋前的捲動位置，結束搜尋時還原
}

func newPagerModel(common *commonModel) pagerModel {
	// Init viewport
	vp := viewport.New()

	m := pagerModel{
		common:   common,
		state:    pagerStateBrowse,
		viewport: vp,
		toc:      newTocModel(),
		search:   NewSearchModel(),
	}
	m.initWatcher()
	return m
}

func (m *pagerModel) setSize(w, h int) {
	// common 的尺寸已由頂層 model 更新，這裡重新計算版面
	m.applyLayout()
}

// applyLayout 重新計算 viewport 尺寸：
// 永遠保留 footer 一行；搜尋時再保留搜尋輸入列一行；開啟 help 時再扣除 help 高度。
func (m *pagerModel) applyLayout() {
	if m.common.width <= 0 || m.common.height <= 0 {
		return
	}
	m.viewport.SetWidth(m.common.width)
	h := m.common.height - statusBarHeight
	if m.search.IsSearching() {
		h-- // 保留搜尋輸入列一行
		m.search.input.SetWidth(max(10, m.common.width-32))
	}
	if m.showHelp {
		if pagerHelpHeight == 0 {
			pagerHelpHeight = strings.Count(m.helpView(), "\n")
		}
		h -= (statusBarHeight + pagerHelpHeight)
	}
	if h < 1 {
		h = 1
	}
	m.viewport.SetHeight(h)
	if m.viewport.PastBottom() {
		m.viewport.GotoBottom()
	}
}

func (m *pagerModel) setContent(s string) {
	m.viewport.SetContent(s)
}

// handleTocKey 處理 TOC 開啟時的鍵盤輸入。
// TOC 會消費所有按鍵，避免誤觸發文件快捷鍵（q/esc/h 等已由頂層 model 放行）。
func (m *pagerModel) handleTocKey(key string) []tea.Cmd {
	var cmds []tea.Cmd
	visible := m.toc.maxVisible
	if visible <= 0 {
		visible = max(1, m.viewport.Height()-6)
	}
	switch key {
	case "up", "k":
		m.toc.moveSel(-1, visible)
	case "down", "j":
		m.toc.moveSel(1, visible)
	case keyEnter:
		if line := m.toc.selectedLine(); line >= 0 {
			m.setViewportTo(line - 2) // 留 2 行邊距
		}
		m.toc.closeToc()
	case keyEsc, "t", "q":
		m.toc.closeToc()
	default:
		// 忽略其他按鍵
	}
	return cmds
}

// handleSearchKey 處理搜尋模式的鍵盤輸入。
// 輸入模式（searchActive）：字元進入輸入框，enter 確認，esc 取消。
// 確認模式（searchConfirmed）：n/N/enter 切換匹配項，esc/q 結束搜尋。
// 僅處理鍵盤訊息；其他訊息直接忽略。
func (m *pagerModel) handleSearchKey(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd
	switch m.search.state {
	case searchActive:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		switch m.search.state {
		case searchHidden:
			// esc 取消
			cmds = append(cmds, m.leaveSearch()...)
		case searchConfirmed:
			// enter 確認：跳到第一個匹配項
			cmds = append(cmds, m.refreshSearchView()...)
		default:
			// 仍在輸入：即時高亮所有匹配項（不捲動）
			if m.search.query != "" {
				m.setContent(m.search.GetHighlightedContent())
			}
		}
	case searchConfirmed:
		keyMsg, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}
		switch keyMsg.String() {
		case "n", keyEnter:
			m.search.NextMatch()
		case "N", "shift+tab":
			m.search.PrevMatch()
		case keyEsc, "q":
			cmds = append(cmds, m.leaveSearch()...)
			return cmds
		default:
			return nil
		}
		cmds = append(cmds, m.refreshSearchView()...)
	}
	return cmds
}

// leaveSearch 結束搜尋並還原內容、捲動位置與版面。
func (m *pagerModel) leaveSearch() []tea.Cmd {
	var cmds []tea.Cmd
	m.search.StopSearch()
	m.setContent(m.search.rendered)
	m.setViewportTo(m.preSearchYOff)
	m.applyLayout()
	return cmds
}

// refreshSearchView 更新高亮內容，並在確認模式下捲動到當前匹配項。
func (m *pagerModel) refreshSearchView() []tea.Cmd {
	var cmds []tea.Cmd
	m.setContent(m.search.GetHighlightedContent())
	if m.search.state == searchConfirmed && m.search.currentIdx >= 0 {
		if line := m.search.matchLine(m.search.currentIdx); line >= 0 {
			// 將匹配項放在 viewport 上三分之一附近
			margin := m.viewport.Height() / 3
			if margin > 5 {
				margin = 5
			}
			if margin < 1 {
				margin = 1
			}
			m.setViewportTo(line - margin)
		}
	}
	return cmds
}

// setViewportTo 將 viewport 捲動到指定行（自動鉗制在合法範圍內）。
func (m *pagerModel) setViewportTo(line int) {
	if line < 0 {
		line = 0
	}
	m.viewport.SetYOffset(line)
}

func (m *pagerModel) toggleHelp() {
	m.showHelp = !m.showHelp
	m.setSize(m.common.width, m.common.height)
	if m.viewport.PastBottom() {
		m.viewport.GotoBottom()
	}
}

type pagerStatusMessage struct {
	message string
	isError bool
}

// Perform stuff that needs to happen after a successful markdown stash. Note
// that the returned command should be sent back the through the pager
// update function.
func (m *pagerModel) showStatusMessage(msg pagerStatusMessage) tea.Cmd {
	// Show a success message to the user
	m.state = pagerStateStatusMessage
	m.statusMessage = msg.message
	if m.statusMessageTimer != nil {
		m.statusMessageTimer.Stop()
	}
	m.statusMessageTimer = time.NewTimer(statusMessageTimeout)

	return waitForStatusMessageTimeout(pagerContext, m.statusMessageTimer)
}

func (m *pagerModel) unload() {
	log.Debug("unload")
	if m.showHelp {
		m.toggleHelp()
	}
	if m.statusMessageTimer != nil {
		m.statusMessageTimer.Stop()
	}
	m.state = pagerStateBrowse
	m.toc.closeToc()
	m.search.StopSearch()
	m.viewport.SetContent("")
	m.viewport.SetYOffset(0)
	m.unwatchFile()
}

func (m pagerModel) update(msg tea.Msg) (pagerModel, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()

		// 子模式優先處理：TOC / 搜尋開啟時先消費按鍵，
		// 避免輸入的文字觸發文件導航快捷鍵（q/esc/h 等已由頂層 model 放行）。
		// 注意：這裡 return 後，按鍵不會再傳給 viewport。
		if m.toc.state == tocVisible {
			cmds = append(cmds, m.handleTocKey(key)...)
			return m, tea.Batch(cmds...)
		}

		if m.search.IsSearching() {
			cmds = append(cmds, m.handleSearchKey(msg)...)
			return m, tea.Batch(cmds...)
		}

		switch key {
		case "q", keyEsc:
			if m.state != pagerStateBrowse {
				m.state = pagerStateBrowse
				return m, nil
			}
		case "home", "g":
			m.viewport.GotoTop()
		case "end", "G":
			m.viewport.GotoBottom()

		case "d":
			m.viewport.HalfPageDown()

		case "u":
			m.viewport.HalfPageUp()

		case "e":
			lineno := int(math.RoundToEven(float64(m.viewport.TotalLineCount()) * m.viewport.ScrollPercent()))
			if m.viewport.AtTop() {
				lineno = 0
			}
			log.Info(
				"opening editor",
				"file", m.currentDocument.localPath,
				"line", fmt.Sprintf("%d/%d", lineno, m.viewport.TotalLineCount()),
			)
			return m, openEditor(m.currentDocument.localPath, lineno)

		case "c":
			// Copy using OSC 52
			termenv.Copy(m.currentDocument.Body)
			// Copy using native system clipboard
			_ = clipboard.WriteAll(m.currentDocument.Body)
			cmds = append(cmds, m.showStatusMessage(pagerStatusMessage{"Copied contents", false}))

		case "r":
			return m, loadLocalMarkdown(&m.currentDocument)

		case "?":
			m.toggleHelp()

		// TOC: 按下 t 顯示目錄
		case "t":
			// 依目前 viewport 大小決定面板最多顯示的列數
			m.toc.maxVisible = max(1, m.viewport.Height()-6)
			if !m.toc.updateToc(m.currentDocument.Body, m.tocRendered) {
				cmds = append(cmds, m.showStatusMessage(pagerStatusMessage{"No headings found", false}))
			}

		// 搜尋: 按下 / 啟動搜尋模式
		case "/":
			// 使用渲染後的內容作為搜尋來源（尚未渲染完成時退回原始內容）
			renderedContent := m.tocRendered
			if renderedContent == "" {
				renderedContent = m.currentDocument.Body
			}
			m.preSearchYOff = m.viewport.YOffset()
			m.search.StartSearch(renderedContent)
			m.applyLayout()
			cmds = append(cmds, textinput.Blink)
		}

	// Glow has rendered the content
	case contentRenderedMsg:
		log.Info("content rendered", "state", m.state)

		rendered := string(msg)
		// 儲存原始 Markdown 內容與渲染後內容供 TOC / 搜尋使用
		m.tocRaw = m.currentDocument.Body
		m.tocRendered = rendered
		if m.toc.state == tocVisible {
			// 重新渲染後更新標題跳轉位置（保留選取與捲動）
			m.toc.refresh(m.tocRendered)
		}
		m.setContent(m.tocRendered)
		if m.search.IsSearching() {
			// 內容已重新渲染，舊的匹配位置無效：以新內容重建搜尋
			m.search.StartSearch(m.tocRendered)
			m.applyLayout()
			cmds = append(cmds, textinput.Blink)
			cmds = append(cmds, m.refreshSearchView()...)
		}
		cmds = append(cmds, m.watchFile)

	// The file was changed on disk and we're reloading it
	case reloadMsg:
		return m, loadLocalMarkdown(&m.currentDocument)

	// We've finished editing the document, potentially making changes. Let's
	// retrieve the latest version of the document so that we display
	// up-to-date contents.
	case editorFinishedMsg:
		return m, loadLocalMarkdown(&m.currentDocument)

	// We've received terminal dimensions, either for the first time or
	// after a resize
	case tea.WindowSizeMsg:
		return m, renderWithGlamour(m, m.currentDocument.Body)

	case statusMessageTimeoutMsg:
		m.state = pagerStateBrowse
	}

	// 鍵盤訊息已由上方的 switch 處理（子模式會直接 return）；
	// 這裡把非鍵盤訊息（滑鼠等）以及常規導航按鍵傳給 viewport。
	if m.toc.state == tocVisible {
		// TOC 開啟時忽略滑鼠
		if _, isMouse := msg.(tea.MouseMsg); isMouse {
			return m, tea.Batch(cmds...)
		}
	}
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m pagerModel) View() string {
	var b strings.Builder

	// 文件區域高度（與 applyLayout 一致：已扣除 footer / 搜尋列 / help）
	areaH := m.viewport.Height()
	if areaH < 0 {
		areaH = 0
	}

	// 渲染 viewport 內容，並補/截成恰好 areaH 行，讓浮動面板可以逐行貼上
	rows := strings.Split(m.viewport.View(), "\n")
	if len(rows) < areaH {
		padded := make([]string, areaH)
		copy(padded, rows)
		rows = padded
	} else if len(rows) > areaH {
		rows = rows[:areaH]
	}

	// 繪製 TOC 浮動面板：貼在 viewport 內容「上方」（置中），而不是追加到螢幕外
	if m.toc.visible() {
		panel := m.toc.panel(m.common.width, areaH, m.common.styles)
		if panel != "" {
			plines := strings.Split(panel, "\n")
			pw := runewidth.StringWidth(plines[0])
			x := max(0, (m.common.width-pw)/2)
			y := max(0, (areaH-len(plines))/2)
			for i, l := range plines {
				r := y + i
				if r >= areaH {
					break
				}
				rows[r] = strings.Repeat(" ", x) + truncate.String(l, uint(m.common.width-x))
			}
		}
	}

	b.WriteString(strings.Join(rows, "\n"))
	if areaH > 0 {
		b.WriteByte('\n')
	}

	// 搜尋模式：在 footer 上方顯示搜尋輸入列
	if m.search.IsSearching() {
		b.WriteString(m.searchLineView())
		b.WriteByte('\n')
	}

	// Footer
	m.statusBarView(&b)

	// Help
	if m.showHelp {
		b.WriteByte('\n')
		b.WriteString(m.helpView())
	}

	return b.String()
}

// searchLineView 繪製搜尋輸入列（提示字元 + 輸入框 + 匹配資訊 + 快捷鍵提示）。
func (m *pagerModel) searchLineView() string {
	input := m.search.input.View()
	line := " " + input

	if info := m.search.GetMatchInfo(); info != "" {
		line += "  " + m.common.styles.searchInfoStyle.Render(info)
	}

	hint := "enter search   esc cancel"
	if m.search.state == searchConfirmed {
		hint = "n next   N prev   esc close"
	}
	hintStr := m.common.styles.tocHintStyle.Render(hint)

	pad := m.common.width - ansi.PrintableRuneWidth(line) - runewidth.StringWidth(hint)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	line += hintStr
	return truncate.String(line, uint(max(1, m.common.width)))
}

func (m pagerModel) statusBarView(b *strings.Builder) {
	const (
		minPercent               float64 = 0.0
		maxPercent               float64 = 1.0
		percentToStringMagnitude float64 = 100.0
	)

	showStatusMessage := m.state == pagerStateStatusMessage
	styles := m.common.styles

	// Logo
	logo := glowLogoView(m.common.styles)

	// Scroll percent
	percent := math.Max(minPercent, math.Min(maxPercent, m.viewport.ScrollPercent()))
	scrollPercent := fmt.Sprintf(" %3.f%% ", percent*percentToStringMagnitude)
	if showStatusMessage {
		scrollPercent = styles.statusBarMessageScrollPosStyle(scrollPercent)
	} else {
		scrollPercent = styles.statusBarScrollPosStyle(scrollPercent)
	}

	// "Help" note
	var helpNote string
	if showStatusMessage {
		helpNote = styles.statusBarMessageHelpStyle(" ? Help ")
	} else {
		helpNote = styles.statusBarHelpStyle(" ? Help ")
	}

	// Note
	var note string
	if showStatusMessage {
		note = m.statusMessage
	} else {
		note = m.currentDocument.Note
	}
	note = truncate.StringWithTail(" "+note+" ", uint(max(0, //nolint:gosec
		m.common.width-
			ansi.PrintableRuneWidth(logo)-
			ansi.PrintableRuneWidth(scrollPercent)-
			ansi.PrintableRuneWidth(helpNote),
	)), ellipsis)
	if showStatusMessage {
		note = styles.statusBarMessageStyle(note)
	} else {
		note = styles.statusBarNoteStyle(note)
	}

	// Empty space
	padding := max(0,
		m.common.width-
			ansi.PrintableRuneWidth(logo)-
			ansi.PrintableRuneWidth(note)-
			ansi.PrintableRuneWidth(scrollPercent)-
			ansi.PrintableRuneWidth(helpNote),
	)
	emptySpace := strings.Repeat(" ", padding)
	if showStatusMessage {
		emptySpace = styles.statusBarMessageStyle(emptySpace)
	} else {
		emptySpace = styles.statusBarNoteStyle(emptySpace)
	}

	fmt.Fprintf(b, "%s%s%s%s%s",
		logo,
		note,
		emptySpace,
		scrollPercent,
		helpNote,
	)
}

func (m pagerModel) helpView() (s string) {
	col1 := []string{
		"g/home  go to top",
		"G/end   go to bottom",
		"c       copy contents",
		"e       edit this document",
		"r       reload this document",
		"esc     back to files",
		"q       quit",
	}

	s += "\n"
	s += "k/↑      up                  " + col1[0] + "\n"
	s += "j/↓      down                " + col1[1] + "\n"
	s += "b/pgup   page up             " + col1[2] + "\n"
	s += "f/pgdn   page down           " + col1[3] + "\n"
	s += "u        ½ page up           " + col1[4] + "\n"
	s += "d        ½ page down         "

	if len(col1) > 5 {
		s += col1[5]
	}

	s = indent(s, 2)

	// Fill up empty cells with spaces for background coloring
	if m.common.width > 0 {
		lines := strings.Split(s, "\n")
		for i := 0; i < len(lines); i++ {
			l := runewidth.StringWidth(lines[i])
			n := max(m.common.width-l, 0)
			lines[i] += strings.Repeat(" ", n)
		}

		s = strings.Join(lines, "\n")
	}

	return m.common.styles.helpViewStyle(s)
}

// COMMANDS

func renderWithGlamour(m pagerModel, md string) tea.Cmd {
	return func() tea.Msg {
		s, err := glamourRender(m, md)
		if err != nil {
			log.Error("error rendering with Glamour", "error", err)
			return errMsg{err}
		}
		return contentRenderedMsg(s)
	}
}

// This is where the magic happens.
func glamourRender(m pagerModel, markdown string) (string, error) {
	trunc := lipgloss.NewStyle().MaxWidth(m.viewport.Width() - lineNumberWidth).Render

	if !m.common.cfg.GlamourEnabled {
		return markdown, nil
	}

	isCode := !utils.IsMarkdownFile(m.currentDocument.Note)
	width := max(0, min(int(m.common.cfg.GlamourMaxWidth), m.viewport.Width())) //nolint:gosec
	if isCode {
		width = 0
	}

	options := []glamour.TermRendererOption{
		utils.GlamourStyle(m.common.cfg.GlamourStyle, isCode),
		glamour.WithWordWrap(width),
	}

	if m.common.cfg.PreserveNewLines {
		options = append(options, glamour.WithPreservedNewLines())
	}
	r, err := glamour.NewTermRenderer(options...)
	if err != nil {
		return "", fmt.Errorf("error creating glamour renderer: %w", err)
	}

	if isCode {
		markdown = utils.WrapCodeBlock(markdown, filepath.Ext(m.currentDocument.Note))
	}

	out, err := r.Render(markdown)
	if err != nil {
		return "", fmt.Errorf("error rendering markdown: %w", err)
	}

	if isCode {
		out = strings.TrimSpace(out)
	}

	// trim lines
	lines := strings.Split(out, "\n")

	var content strings.Builder
	for i, s := range lines {
		if isCode || m.common.cfg.ShowLineNumbers {
			content.WriteString(m.common.styles.lineNumberStyle(fmt.Sprintf("%"+fmt.Sprint(lineNumberWidth)+"d", i+1)))
			content.WriteString(trunc(s))
		} else {
			content.WriteString(s)
		}

		// don't add an artificial newline after the last split
		if i+1 < len(lines) {
			content.WriteRune('\n')
		}
	}

	return content.String(), nil
}

func (m *pagerModel) initWatcher() {
	var err error
	m.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Error("error creating fsnotify watcher", "error", err)
	}
}

func (m *pagerModel) watchFile() tea.Msg {
	dir := m.localDir()

	if err := m.watcher.Add(dir); err != nil {
		log.Error("error adding dir to fsnotify watcher", "error", err)
		return nil
	}

	log.Info("fsnotify watching dir", "dir", dir)

	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok || event.Name != m.currentDocument.localPath {
				continue
			}

			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}

			log.Debug("fsnotify event", "file", event.Name, "event", event.Op)
			return reloadMsg{}
		case err, ok := <-m.watcher.Errors:
			if !ok {
				continue
			}
			log.Debug("fsnotify error", "dir", dir, "error", err)
		}
	}
}

func (m *pagerModel) unwatchFile() {
	dir := m.localDir()

	err := m.watcher.Remove(dir)
	if err == nil {
		log.Debug("fsnotify dir unwatched", "dir", dir)
	} else {
		log.Error("fsnotify fail to unwatch dir", "dir", dir, "error", err)
	}
}

func (m *pagerModel) localDir() string {
	return filepath.Dir(m.currentDocument.localPath)
}
