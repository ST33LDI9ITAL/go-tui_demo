package main

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/grindlemire/go-tui"
)

var (
	app              *tui.App
	inputState       = tui.NewState("")
	textArea         *tui.TextArea
	textAreaEl       *tui.Element
	postRenderCursor func()
	eventLogMu       sync.Mutex
	eventLog         []string
	eventScroll      *tui.Element // PERSISTENT — created once, reused across renders
	eventList        *tui.Element // persistent list inside eventScroll
	lastWidth        int          // track terminal width for TextArea recreation
	unseenEvents     int          // events that arrived while user was scrolled up
	atBottomOnLastCheck bool = true // was user at bottom on last check? starts true
	bgStyle          = tui.NewStyle().Background(tui.ANSIColor(236))
	frameInner       int

	// ── Command/tag fuzzy matching ──
	allCommands  = []string{"/help", "/start", "/stop", "/quit", "/command", "/context", "/cracker", "/settings", "/test-scroll"}
	allTags      = []string{"@work", "@personal", "@urgent", "@low", "@feature", "@bug", "@docs", "@review", "@wip", "@done"}
	filteredCmds []string
	matchKind    string
	selectedIdx  = tui.NewState(0)
	lastText     string
	matches      *tui.Element // persistent scrollable match list
	cmdActive    bool         // true when filter is active
	fuzzyCanceled bool        // Escape cancels fuzzy until text changes
)

type testComponent struct{}

func elementAbsPos(el *tui.Element) (int, int) {
	f := reflect.ValueOf(el).Elem().FieldByName("layout")
	if !f.IsValid() {
		return 0, 0
	}
	return int(f.FieldByName("AbsoluteX").Float()), int(f.FieldByName("AbsoluteY").Float())
}

func insertNL() {
	rv := reflect.ValueOf(textArea).Elem().FieldByName("cursorPos")
	if !rv.IsValid() {
		return
	}
	cs := (*tui.State[int])(unsafe.Pointer(rv.Pointer()))
	pos := cs.Get()
	if pos < 0 {
		pos = 0
	}
	text := inputState.Get()
	n := len([]rune(text))
	if pos > n {
		pos = n
	}
	// Build prefix and suffix using grapheme iteration
	var prefix, suffix strings.Builder
	gc := 0
	for _, r := range text {
		if gc < pos {
			prefix.WriteRune(r)
		} else {
			suffix.WriteRune(r)
		}
		gc++
	}
	inputState.Set(prefix.String() + "\n" + suffix.String())
	cs.Set(pos + 1)
}

func quit() {
	cleanup()
	os.Exit(0)
}

func submitInput(text string) {
	s := strings.TrimSpace(text)
	if s == "" {
		inputState.Set("")
		return
	}
	if s == "/q" || s == "/quit" {
		quit()
	}

	// If there's a selected command from fuzzy matching, use that
	var cmd string
	if cmdActive && len(filteredCmds) > 0 {
		sel := selectedIdx.Get()
		if sel >= 0 && sel < len(filteredCmds) {
			cmd = filteredCmds[sel]
		}
	}
	if cmd == "/settings" {
		eventLogMu.Lock()
		eventLog = append(eventLog, "/settings (settings mode not implemented)")
		eventLogMu.Unlock()
		inputState.Set("")
		return
	}
	if cmd == "/test-scroll" {
		go testScrollGenerator()
		inputState.Set("")
		return
	}
	if cmd == "" {
		cmd = s
	}

	eventLogMu.Lock()
	eventLog = append(eventLog, cmd)
	eventLogMu.Unlock()
	inputState.Set("")
	cmdActive = false
}



// ── Fuzzy matching (from test2) ──

func extractTagQuery(text string) (string, bool) {
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return "", false
	}
	r := text[idx:]
	if sp := strings.Index(r, " "); sp >= 0 {
		r = r[:sp]
	}
	return r, true
}

var testMessages = []string{
	"Short message with ✨ sparkle!",
	"Medium message ☕ with some coffee and a very long line that wraps around the entire width of the frame",
	"Line 1\nLine 2\nLine 3 — three line message with ✨☕🌈",
	"Multi line test 🚀\nLine two here\nLine three\nLine four with 🌟star🌟",
	"Hello! This is a longer message with lots of text to wrap around and fill multiple lines in the event frame so we can test scrolling behavior properly. 🎉",
	"A 🎉 party\n🕺 dance\n💃 more dancing\n🎊 confetti\n🥳 celebration emoji!",
	"CJK test: 你好世界 这是中文测试 看看能不能正确显示和换行 🌈",
	"Very short ✔️",
	"Line 1 — start\nLine 2 — middle content here with some 💖\nLine 3 — end here with 👍\nLine 4 — extra trailing line for testing\nLine 5 — final line with 🏁",
	"Flag test 🏁 with double-width emoji at various positions in this line to test wrapping ✨✨✨ at the end here too!",
	"Another short line ✔️",
	"Medium line ☕☕☕ with a bit more text for wrapping and scrolling test purposes here",
	"Line 1 of three\nLine 2 of three\nLine 3 of three — multi paragraph test with ✨🌟",
	"A longer single paragraph that should wrap around the frame nicely to test how autoscroll handles large multiline content blocks in the event area 🚀🎉💖",
	"Final test message with 🌙✔️⭐☀️🌈🍕🚀🎉💖👍🏆🔥🎸🌟 at various positions throughout this very long line that wraps around the entire frame width for testing purposes",
	"ZWJ sequence 👨‍👩‍👧‍👦 family emoji with zero-width joiner spanning multiple code points",
	"Regional flag indicator 🇺🇸🇯🇵🇩🇪 with two regional letter symbols combining into flag",
	"Keycap sequence #️⃣ 1️⃣ 9️⃣ 0️⃣ with combining enclosing keycap codepoints",
	"Skin tone modifier 👍🏿 waving hand 🖐️👋🏾 with Fitzpatrick modifiers applied",
	"VS15 text presentation ☺︎✆︎☎︎ with FE0E forcing text style on emoji-capable BMP chars",
}






func testScrollGenerator() {
	for i := 0; i < len(testMessages); i++ {
		time.Sleep(500 * time.Millisecond)
		msg := testMessages[i]
		// Append directly and trigger render (same as manual submit)
		eventLogMu.Lock()
		eventLog = append(eventLog, msg)
		eventLogMu.Unlock()
		app.MarkDirty()
	}
}

func fuzzyFilter(items []string, query string) []string {
	if query == "" {
		return nil
	}
	type sc struct {
		item  string
		score int
	}
	var m []sc
	q := strings.ToLower(query)
	for _, it := range items {
		il := strings.ToLower(it)
		if s, ok := subsequenceScore(q, il); ok {
			m = append(m, sc{item: it, score: s})
		}
	}
	sort.Slice(m, func(i, j int) bool { return m[i].score > m[j].score })
	r := make([]string, len(m))
	for i, v := range m {
		r[i] = v.item
	}
	return r
}

func subsequenceScore(query, target string) (int, bool) {
	qi := 0
	sc := 0
	cn := 0
	fm := -1
	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if query[qi] == target[ti] {
			if cn > 0 {
				cn++
				sc += 15 * cn
			} else {
				cn = 1
				sc += 10
				if fm < 0 {
					fm = ti
				}
			}
			qi++
		} else {
			cn = 0
		}
	}
	if qi < len(query) {
		return 0, false
	}
	if fm == 0 {
		sc += 50
	} else {
		sc += max(0, 50-fm*5)
	}
	sc += max(0, 20-len(target))
	return sc, true
}

// ── Render ──

func (c *testComponent) Render(a *tui.App) *tui.Element {
	testChar := "✔"
	title := fmt.Sprintf(" 🐷 > 📁 sloppygo > 🌿 feat/tui-event-panel (%s)", testChar)

	root := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column))
	content := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row), tui.WithFlexGrow(1))

	w, _ := a.Size()
	frameInner = w - 20

	// Recreate TextArea when terminal width changes
	if textArea == nil || w != lastWidth {
		lastWidth = w
		textArea = tui.NewTextArea(
			tui.WithTextAreaValue(inputState),
			tui.WithTextAreaAutoFocus(true),
			tui.WithTextAreaWidth(w - 2),
			tui.WithTextAreaOnSubmit(submitInput),
			tui.WithTextAreaVirtualCursor(false),
		)
		textArea.BindApp(a)
		inputState.BindApp(a)
		selectedIdx.BindApp(a)
	}

	leftCol := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column), tui.WithFlexGrow(1))

	// Create scrollable event log ONCE and reuse (preserves scroll state)
	if eventScroll == nil {
		eventScroll = tui.New(
			
			tui.WithFlexGrow(1),
			tui.WithScrollable(tui.ScrollVertical),
			tui.WithScrollbarHidden(true),
		)
		eventList = tui.New(
			
		)
		eventScroll.AddChild(eventList)
	}

	// ── Compute fuzzy matches ──
	text := inputState.Get()
	if text != lastText {
		lastText = text
		selectedIdx.Set(0)
		fuzzyCanceled = false
	}
	filteredCmds = nil
	matchKind = ""
	cmdActive = false
	if text != "" && !fuzzyCanceled {
		if strings.HasPrefix(text, "/") {
			matchKind = "cmd"
			filteredCmds = fuzzyFilter(allCommands, text)
			cmdActive = true
		} else if q, ok := extractTagQuery(text); ok {
			matchKind = "tag"
			filteredCmds = fuzzyFilter(allTags, q)
			cmdActive = true
		}
	}
	if len(filteredCmds) > 0 {
		sel := selectedIdx.Get()
		if sel >= len(filteredCmds) {
			sel = len(filteredCmds) - 1
			selectedIdx.Set(sel)
		}
	}

	// ── Add new events ──
	eventLogMu.Lock()
	existing := len(eventList.Children())
	toAdd := eventLog[existing:]
	for _, entry := range toAdd {
		// Use native go-tui wrapping — HeightForWidth accounts for wrapped lines
		frame := tui.New(
			tui.WithBorder(tui.BorderRounded),
			tui.WithBorderTitle(" 💬 "),
			tui.WithBackground(bgStyle),
			tui.WithWidth(frameInner),
			tui.WithText("  " + entry),
		)
		eventList.AddChild(frame)
	}

	// Only autoscroll if user is at bottom (stale maxY = curY when at bottom)
	if len(toAdd) > 0 {
		_, curY := eventScroll.ScrollOffset()
		_, maxY := eventScroll.MaxScroll()
		if curY >= maxY-3 {
			eventScroll.ScrollToBottom()
		} else {
			unseenEvents += len(toAdd)
		}
	}
	eventLogMu.Unlock()

	leftCol.AddChild(eventScroll)
	
	content.AddChild(leftCol)

	sidebar := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column), tui.WithWidth(16), tui.WithBorder(tui.BorderRounded))
	sidebar.AddChild(tui.New(tui.WithText("  chat")))
	sidebar.AddChild(tui.New(tui.WithFlexGrow(1)))
	content.AddChild(sidebar)

	root.AddChild(content)

	// ── Fuzzy match panel (between event log and input frame) ──
	if len(filteredCmds) > 0 {
		sel := selectedIdx.Get()
		if matches == nil {
			matches = tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithScrollable(tui.ScrollVertical),
				tui.WithScrollbarHidden(true),
				tui.WithBorder(tui.BorderRounded),
			)
		}
		// Set height: cap at 8 rows, min 1 row + 2 border = up to 10
		mh := len(filteredCmds)
		if mh > 8 {
			mh = 8
		}
		rv := reflect.ValueOf(matches).Elem().FieldByName("style")
		sp := (*tui.LayoutStyle)(unsafe.Pointer(rv.UnsafeAddr()))
		sp.Height = tui.Fixed(mh + 2) // content + border

		matches.RemoveAllChildren()
		for i, cmd := range filteredCmds {
			st := tui.NewStyle()
			p := "  "
			if i == sel {
				st = st.Foreground(tui.Cyan).Bold()
				p = "> "
			} else {
				st = st.Foreground(tui.White)
			}
			matches.AddChild(tui.New(tui.WithText(p+cmd), tui.WithTextStyle(st)))
		}
		// Auto-scroll to keep selected visible
		matches.ScrollTo(0, max(sel-1, 0))
		root.AddChild(matches)
	}

	// ── Input frame ──
	frame := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column), tui.WithBorder(tui.BorderRounded), tui.WithBorderTitle(title))
	el := textArea.Render(a)
	textAreaEl = el
	frame.AddChild(el)
	root.AddChild(frame)

	os.Stdout.WriteString("\033[?2004h")

	postRenderCursor = func() {
		if textArea == nil || textAreaEl == nil {
			return
		}
		cv := reflect.ValueOf(textArea).Elem().FieldByName("cursorPos")
		if !cv.IsValid() || cv.IsNil() {
			return
		}
		cp := int(cv.Elem().FieldByName("value").Int())

		actualWidth := textAreaEl.ContentRect().Width
		if actualWidth <= 0 {
			actualWidth = w - 4
		}

		text := inputState.Get()
		row, col := 0, 0
		gc := 0
		for _, r := range text {
			if gc >= cp {
				break
			}
			if r == '\n' {
				row++
				col = 0
				gc++
				continue
			}
			if col >= actualWidth {
				row++
				col = 0
			}
			col += tui.RuneWidth(r)
			gc++
		}
		if col >= actualWidth {
			row++
			col = 0
		}

		ax, ay := elementAbsPos(textAreaEl)
		if sof := reflect.ValueOf(textArea).Elem().FieldByName("tempScrollOffset"); sof.IsValid() {
			row -= int(sof.Int())
			if row < 0 {
				row = 0
			}
		}
		a.Terminal().ShowCursor()
		a.Terminal().SetCursor(ax+col, ay+row)
	}
	return root
}

// ── Key bindings ──

func (c *testComponent) KeyMap() tui.KeyMap {
	km := make(tui.KeyMap, 0)

	// Filtered textarea bindings: AnyRune, Backspace, Delete, Left/Right, Home/End
	// These fire unconditionally because our component doesn't implement focusQuerier
	// (focusCheck is nil, so FocusRequired check is skipped in matches())
	km = append(km, textAreaFilteredKeymap()...)

	// Command mode: fuzzy navigation
	if cmdActive && len(filteredCmds) > 0 {
		n := len(filteredCmds)
		km = append(km,
			tui.OnStop(tui.KeyTab, func(ke tui.KeyEvent) {
				sel := selectedIdx.Get()
				selectedIdx.Set((sel + 1) % n)
			}),
			tui.OnStop(tui.KeyUp, func(ke tui.KeyEvent) {
				sel := selectedIdx.Get()
				selectedIdx.Set((sel - 1 + n) % n)
			}),
			tui.OnStop(tui.KeyDown, func(ke tui.KeyEvent) {
				sel := selectedIdx.Get()
				selectedIdx.Set((sel + 1) % n)
			}),
			tui.OnStop(tui.KeyEscape, func(ke tui.KeyEvent) {
				fuzzyCanceled = true
			}),
		)
	} else {
		km = append(km,
			tui.On(tui.KeyUp, func(ke tui.KeyEvent) {
				if eventScroll != nil {
					_, y := eventScroll.ScrollOffset()
					eventScroll.ScrollTo(0, y-3)
				}
			}),
			tui.On(tui.KeyDown, func(ke tui.KeyEvent) {
				if eventScroll != nil {
					_, y := eventScroll.ScrollOffset()
					eventScroll.ScrollTo(0, y+3)
				}
			}),
		)
	}

	km = append(km,
		tui.OnStop(tui.KeyEnter, func(ke tui.KeyEvent) { submitInput(inputState.Get()) }),
		tui.OnStop(tui.KeyEnter.Shift(), func(ke tui.KeyEvent) { insertNL() }),
		tui.OnStop(tui.KeyEnter.Ctrl(), func(ke tui.KeyEvent) { insertNL() }),
		tui.OnStop(tui.Rune('j').Ctrl(), func(ke tui.KeyEvent) { insertNL() }),
		tui.On(tui.KeyCtrlC, func(ke tui.KeyEvent) { quit() }),
	)
	return km
}

func textAreaFilteredKeymap() tui.KeyMap {
	if textArea == nil {
		return nil
	}
	tb := textArea.KeyMap()
	filtered := make(tui.KeyMap, 0, len(tb))
	for _, b := range tb {
		switch b.Pattern.Key {
		case tui.KeyUp, tui.KeyDown, tui.KeyEnter, tui.KeyEscape:
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

func (c *testComponent) HandleMouse(me tui.MouseEvent) bool {
	if eventScroll == nil {
		return false
	}
	switch me.Button {
	case tui.MouseWheelUp:
		_, y := eventScroll.ScrollOffset()
		eventScroll.ScrollTo(0, y-3)
		return true
	case tui.MouseWheelDown:
		_, y := eventScroll.ScrollOffset()
		eventScroll.ScrollTo(0, y+3)
		return true
	}
	return false
}

func (c *testComponent) Watchers() []tui.Watcher {
	if textArea != nil {
		return textArea.Watchers()
	}
	return nil
}

func cleanup() {
	os.Stdout.WriteString("\033[?1006l")
	os.Stdout.WriteString("\033[?1000l")
	os.Stdout.WriteString("\033[?2004l")
	os.Stdout.WriteString("\033[?25h")
	os.Stdout.WriteString("\033[0m")
	os.Stdout.WriteString("\033[?1049l")
	if app != nil {
		func() { defer func() { recover() }(); app.Close() }()
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			cleanup()
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cleanup()
		os.Exit(0)
	}()

	var err error
	app, err = tui.NewApp(
		tui.WithRootComponent(&testComponent{}),
		tui.WithCursor(),
		tui.WithPostRenderHook(func() {
			if postRenderCursor != nil {
				postRenderCursor()
			}
			// Post-render: track atBottom with correct values.
			// If user scrolled to bottom with unseen events, catch up.
			if eventScroll != nil {
				_, curY := eventScroll.ScrollOffset()
				_, maxY := eventScroll.MaxScroll()
				atBottomOnLastCheck = curY >= maxY-3
				if atBottomOnLastCheck && unseenEvents > 0 {
					eventScroll.ScrollToBottom()
					unseenEvents = 0
				}
			}
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		cleanup()
		fmt.Fprintf(os.Stderr, "Run error: %v\n", err)
		os.Exit(1)
	}
}
