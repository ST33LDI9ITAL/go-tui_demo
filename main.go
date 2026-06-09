package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/grindlemire/go-tui"
)

var app *tui.App

type testComponent struct{}

func (c *testComponent) Render(app *tui.App) *tui.Element {
	testChar := "✔" // try "x", "✨", "❄", "A"

	title := fmt.Sprintf(" 🐷 > 📁 sloppygo > 🌿 feat/tui-event-panel (%s)", testChar)

	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
	)

	// Main content row: event area + sidebar
	content := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithFlexGrow(1),
	)

	// Left column: event log + notifications
	leftCol := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1),
	)
	leftCol.AddChild(tui.New(
		tui.WithText("Event log area"),
		tui.WithFlexGrow(1),
	))
	// Notification bar
	leftCol.AddChild(tui.New(
		tui.WithText("  > test notification bar"),
		tui.WithHeight(1),
	))
	content.AddChild(leftCol)

	// Sidebar (right column)
	sidebar := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(16),
		tui.WithBorder(tui.BorderRounded),
	)
	sidebar.AddChild(tui.New(tui.WithText("  chat")))
	sidebar.AddChild(tui.New(tui.WithFlexGrow(1)))
	content.AddChild(sidebar)

	root.AddChild(content)

	// Status bar frame (input frame)
	frame := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderTitle(title),
	)
	frame.AddChild(tui.New(tui.WithText("> ")))
	root.AddChild(frame)

	return root
}

func (c *testComponent) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.On(tui.KeyCtrlC, func(ke tui.KeyEvent) {
			if app != nil {
				app.Close()
			}
			os.Exit(0)
		}),
	}
}

func (c *testComponent) Watchers() []tui.Watcher { return nil }

func main() {
	var err error
	app, err = tui.NewApp(tui.WithRootComponent(&testComponent{}))
	if err != nil {
		panic(err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		app.Close()
		os.Exit(0)
	}()

	if err := app.Run(); err != nil {
		panic(err)
	}
}
