// SPDX-Licence-Identifier: EUPL-1.2

// Package anim is a cycling-character gradient "generating" animation,
// ported from charmbracelet/mods (MIT, archived 2026-03) and maintained
// here: random characters cycle and settle into a label, an optional
// truecolour gradient ramp cycles behind them, then an ellipsis spinner
// takes over. Anim satisfies tui.Model (Init/Update/View), so it drives like
// any other widget under a tui.Program.
//
// Usage example:
//
//	a := anim.New(15, "Generating")
//	p := tea.NewProgram(a)
//	if _, err := p.Run(); err != nil {
//		// handle err
//	}
package anim

import (
	"math/rand"
	"slices"
	"strings"
	"time"

	tea "dappco.re/go/render/display/tui"
	"dappco.re/go/render/display/tui/spinner"
	"dappco.re/go/render/display/tui/style"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

const (
	charCyclingFPS   = time.Second / 22
	colourCycleFPS   = time.Second / 5
	maxCyclingChars  = 120
	minRampSize      = 3
	minColourCycleSz = 2
)

var charRunes = []rune("0123456789abcdefABCDEF~!@#$£€%^&*()+=_")

type charState int

const (
	charInitialState charState = iota
	charCyclingState
	charEndOfLifeState
)

// cyclingChar is a single animated character.
type cyclingChar struct {
	finalValue   rune // if < 0 cycle forever
	currentValue rune
	initialDelay time.Duration
	lifetime     time.Duration
}

func (c cyclingChar) randomRune() rune {
	return charRunes[rand.Intn(len(charRunes))] //nolint:gosec
}

func (c cyclingChar) state(start time.Time) charState {
	now := time.Now()
	if now.Before(start.Add(c.initialDelay)) {
		return charInitialState
	}
	if c.finalValue > 0 && now.After(start.Add(c.initialDelay)) {
		return charEndOfLifeState
	}
	return charCyclingState
}

type stepCharsMsg struct{}

func stepChars() tea.Cmd {
	return tea.Tick(charCyclingFPS, func(time.Time) tea.Msg {
		return stepCharsMsg{}
	})
}

type colourCycleMsg struct{}

func cycleColours() tea.Cmd {
	return tea.Tick(colourCycleFPS, func(time.Time) tea.Msg {
		return colourCycleMsg{}
	})
}

// Anim is the model that manages the animation shown while output is being
// generated: size random-cycling characters settle into label, coloured
// with a truecolour gradient ramp when the terminal supports it, then an
// ellipsis spinner takes over.
type Anim struct {
	start           time.Time
	cyclingChars    []cyclingChar
	labelChars      []cyclingChar
	ramp            []style.Style
	label           []rune
	ellipsis        spinner.Model
	ellipsisStarted bool
}

// Anim satisfies tea.Model.
var _ tea.Model = Anim{}

// New builds an Anim of size random-cycling characters (clamped to
// maxCyclingChars) that settle into label, followed by an ellipsis spinner.
// On a truecolour terminal (size >= 3) the cycling characters are painted
// with a pink-to-purple gradient ramp that cycles as the animation runs.
func New(size uint, label string) Anim {
	n := min(int(size), maxCyclingChars)

	gap := " "
	if n == 0 {
		gap = ""
	}

	a := Anim{
		start:    time.Now(),
		label:    []rune(gap + label),
		ellipsis: spinner.New(spinner.WithSpinner(spinner.Ellipsis)),
	}

	// If we're in truecolour mode (and there are enough cycling characters)
	// colour the cycling characters with a gradient ramp.
	if n >= minRampSize && termenv.ColorProfile() == termenv.TrueColor {
		// Double capacity for colour cycling: we reverse and append the
		// ramp for seamless transitions.
		a.ramp = make([]style.Style, n, n*2)
		ramp := makeGradientRamp(n)
		for i, colour := range ramp {
			a.ramp[i] = style.New().Foreground(colour)
		}
		reversed := slices.Clone(a.ramp)
		slices.Reverse(reversed)
		a.ramp = append(a.ramp, reversed...) // reverse and append for colour cycling
	}

	makeDelay := func(nn int32, ms time.Duration) time.Duration {
		return time.Duration(rand.Int31n(nn)) * (time.Millisecond * ms) //nolint:gosec
	}

	makeInitialDelay := func() time.Duration {
		return makeDelay(8, 60)
	}

	// Initial characters that cycle forever.
	a.cyclingChars = make([]cyclingChar, n)

	for i := 0; i < n; i++ {
		a.cyclingChars[i] = cyclingChar{
			finalValue:   -1, // cycle forever
			initialDelay: makeInitialDelay(),
		}
	}

	// Label text that only cycles for a little while.
	a.labelChars = make([]cyclingChar, len(a.label))

	for i, r := range a.label {
		a.labelChars[i] = cyclingChar{
			finalValue:   r,
			initialDelay: makeInitialDelay(),
			lifetime:     makeDelay(5, 180),
		}
	}

	return a
}

// Init starts the character-cycling and colour-cycling ticks.
func (Anim) Init() tea.Cmd {
	return tea.Batch(stepChars(), cycleColours())
}

// Update handles messages, advancing the animation one step at a time.
func (a Anim) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.(type) {
	case stepCharsMsg:
		a.updateChars(&a.cyclingChars)
		a.updateChars(&a.labelChars)

		if !a.ellipsisStarted {
			var eol int
			for _, c := range a.labelChars {
				if c.state(a.start) == charEndOfLifeState {
					eol++
				}
			}
			if eol == len(a.label) {
				// If our entire label has reached end of life, start the
				// ellipsis "spinner" after a short pause.
				a.ellipsisStarted = true
				cmd = tea.Tick(time.Millisecond*220, func(time.Time) tea.Msg {
					return a.ellipsis.Tick()
				})
			}
		}

		return a, tea.Batch(stepChars(), cmd)
	case colourCycleMsg:
		if len(a.ramp) < minColourCycleSz {
			return a, nil
		}
		a.ramp = append(a.ramp[1:], a.ramp[0])
		return a, cycleColours()
	case spinner.TickMsg:
		var cmd tea.Cmd
		a.ellipsis, cmd = a.ellipsis.Update(msg)
		return a, cmd
	default:
		return a, nil
	}
}

func (a *Anim) updateChars(chars *[]cyclingChar) {
	for i, c := range *chars {
		switch c.state(a.start) {
		case charInitialState:
			(*chars)[i].currentValue = '.'
		case charCyclingState:
			(*chars)[i].currentValue = c.randomRune()
		case charEndOfLifeState:
			(*chars)[i].currentValue = c.finalValue
		}
	}
}

// View renders the animation's current frame.
func (a Anim) View() tea.View {
	var b strings.Builder

	for i, c := range a.cyclingChars {
		if len(a.ramp) > i {
			b.WriteString(a.ramp[i].Render(string(c.currentValue)))
			continue
		}
		b.WriteRune(c.currentValue)
	}

	for _, c := range a.labelChars {
		b.WriteRune(c.currentValue)
	}

	return tea.NewView(b.String() + a.ellipsis.View())
}

// makeGradientRamp builds a length-long pink-to-purple colour ramp
// (#F967DC → #6B50FF), blended in Luv space. colorful.Color satisfies
// style.Paint (image/color.Color) directly via its own RGBA method, so each
// blended step is stored as-is — no hex round trip needed.
func makeGradientRamp(length int) []style.Paint {
	const startColour = "#F967DC"
	const endColour = "#6B50FF"
	var (
		c        = make([]style.Paint, length)
		start, _ = colorful.Hex(startColour)
		end, _   = colorful.Hex(endColour)
	)
	for i := range length {
		c[i] = start.BlendLuv(end, float64(i)/float64(length))
	}
	return c
}
