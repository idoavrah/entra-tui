package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// leftToTerminal switches reordering off for one test, and back on after it.
func leftToTerminal(t *testing.T) {
	t.Helper()
	bidi.SetReordering(false)
	t.Cleanup(func() { bidi.SetReordering(true) })
}

func TestBSwitchesRightToLeftHandlingEverywhere(t *testing.T) {
	t.Cleanup(func() { bidi.SetReordering(true) })

	for name, m := range map[string]Model{
		"dashboard": newDashboardModel(t),
		"browse":    browsing(t),
		"detail":    groupDetail(t, false),
	} {
		m = send(t, m, press("b"))
		if bidi.Reordering() {
			t.Fatalf("%s: b did not hand right-to-left text to the terminal", name)
		}
		if !strings.Contains(m.flash, "left to the terminal") {
			t.Errorf("%s: flash = %q, want it to say who reorders now", name, m.flash)
		}
		m = send(t, m, press("b"))
		if !bidi.Reordering() || !strings.Contains(m.flash, "reordered by entra-tui") {
			t.Errorf("%s: a second b did not take it back (flash %q)", name, m.flash)
		}
	}
}

func TestSwitchingRedrawsTheOpenPane(t *testing.T) {
	t.Cleanup(func() { bidi.SetReordering(true) })

	m := groupDetail(t, false)
	m.detail.Object["displayName"] = hebrewOwner
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()
	if !strings.Contains(m.detailVP.View(), reverseRunes(hebrewOwner)) {
		t.Fatal("the pane does not show the Hebrew reordered to begin with")
	}

	// The properties sit pre-rendered in the viewport, so without a redraw
	// the switch would not show until something else changed.
	m = send(t, m, press("b"))
	if !strings.Contains(m.detailVP.View(), hebrewOwner) {
		t.Error("the pane still shows the Hebrew reordered after handing it to the terminal")
	}
}

func TestTerminalIsToldWhoReorders(t *testing.T) {
	t.Cleanup(func() { bidi.SetReordering(true) })

	var out bytes.Buffer
	m := newDashboardModel(t)
	m.opts.TerminalOut = &out

	m.announceBidi()()
	next, _ := m.toggleBidi()
	next.(Model).announceBidi()()

	if got, want := out.String(), bidi.ExplicitMode+bidi.ImplicitMode; got != want {
		t.Errorf("terminal received %q, want %q", got, want)
	}

	// Tests leave TerminalOut nil, and nothing may be written then.
	m.opts.TerminalOut = nil
	if m.announceBidi() != nil {
		t.Error("a model with no terminal output still sends the mode sequence")
	}
}

func TestPromptShowsTypedHebrewAsTypedWhenTheTerminalReorders(t *testing.T) {
	leftToTerminal(t)

	m := browsing(t)
	next, _ := m.openPrompt(modeSearch, "")
	m = typeKeys(t, next.(Model), "שרה")
	if got, want := m.promptValue(), m.input.View(); got != want {
		t.Errorf("prompt = %q, want the input's own view, cursor and all", got)
	}
}

func TestHelpSaysWhoReordersAndWhy(t *testing.T) {
	m := newDashboardModel(t)
	m.opts.BidiReason = "auto: iTerm2 3.7 reorders it itself"
	if got := m.bidiHelp(); !strings.Contains(got, "reordered by entra-tui") ||
		!strings.Contains(got, "iTerm2 3.7") {
		t.Errorf("help line %q does not say who reorders and how that was decided", got)
	}
}

func TestFlashReordersANameInIt(t *testing.T) {
	m := newDashboardModel(t)
	m.flash = "added " + hebrewOwner + " as member"
	if line := m.renderStatusLine(); !strings.Contains(line, reverseRunes(hebrewOwner)) {
		t.Errorf("status line %q shows the name in logical order", line)
	}
}

func TestRolePickerPromptReordersTheApplicationName(t *testing.T) {
	m := newDashboardModel(t)
	m.modal = modalRole
	m.modalTarget = graph.Item{"id": "sp1", "displayName": hebrewOwner}
	m.modalRoles = []graph.AppRole{{ID: "r1", DisplayName: "Reader"}}

	if prompt := m.pickLines(120)[0]; !strings.Contains(prompt, reverseRunes(hebrewOwner)) {
		t.Errorf("picker prompt %q shows the application name in logical order", prompt)
	}
}

func TestSwitchingRemembersTheChoiceForThisTerminal(t *testing.T) {
	t.Cleanup(func() { bidi.SetReordering(true) })

	var remembered []bool
	m := newDashboardModel(t)
	m.opts.Terminal = "iTerm2"
	m.opts.RememberBidi = func(reorder bool) error {
		remembered = append(remembered, reorder)
		return nil
	}

	m = send(t, m, press("b"))
	if len(remembered) != 1 || remembered[0] {
		t.Fatalf("remembered %v, want the one choice made: left to the terminal", remembered)
	}
	if !strings.Contains(m.flash, "remembered for iTerm2") {
		t.Errorf("flash = %q, want it to say the choice will outlast the session", m.flash)
	}

	m.opts.RememberBidi = func(bool) error { return errors.New("read-only file system") }
	m = send(t, m, press("b"))
	if !strings.Contains(m.flash, "not remembered: read-only file system") {
		t.Errorf("flash = %q, want it to admit the choice was not kept", m.flash)
	}
	if !bidi.Reordering() {
		t.Error("a choice that could not be remembered was not applied either")
	}
}
