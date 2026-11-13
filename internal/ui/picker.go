package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// errRawUnsupported is returned on platforms without raw-mode support.
var errRawUnsupported = errors.New("raw terminal mode unsupported on this platform")

// PickerItem is one selectable row of a Picker screen.
type PickerItem struct {
	Label   string
	Hint    string
	Default bool // preselect this item
}

// Picker renders an interactive single-choice selector on a terminal.
// When raw mode is unavailable it degrades to a numbered line prompt.
type Picker struct {
	Out     io.Writer                        // destination for rendering
	In      io.Reader                        // key input; defaults to os.Stdin
	StdinFd uintptr                          // fd used to enable raw mode (usually os.Stdin.Fd())
	MakeRaw func(fd uintptr) (func(), error) // injectable raw-mode hook
}

// Select displays the items and returns the chosen index. It returns -1
// with a nil error when the user cancels (q, Esc, Ctrl-C), or an error when
// interaction is impossible (no items, input closed).
func (p *Picker) Select(title string, items []PickerItem) (int, error) {
	if len(items) == 0 {
		return -1, errors.New("nothing to select")
	}
	if p.In == nil {
		p.In = os.Stdin
	}

	makeRawFn := p.MakeRaw
	if makeRawFn == nil {
		makeRawFn = makeRaw
	}

	restore, rawErr := makeRawFn(p.StdinFd)
	if rawErr != nil {
		return p.selectByLine(title, items)
	}
	defer restore()

	return p.selectByKeys(title, items)
}

// keyQueue delivers single bytes from an underlying reader, buffering
// multi-byte reads into a FIFO.
type keyQueue struct {
	src     io.Reader
	fifo    []byte
	readErr error // sticky error from the underlying reader
}

func newKeyQueue(src io.Reader) *keyQueue {
	return &keyQueue{src: src}
}

// next returns the next byte, blocking until one is available.
func (q *keyQueue) next() (byte, error) {
	if len(q.fifo) == 0 {
		if err := q.fill(); err != nil {
			return 0, err
		}
	}
	b := q.fifo[0]
	q.fifo = q.fifo[1:]
	return b, nil
}

// buffered reports whether pending bytes are already available.
func (q *keyQueue) buffered() bool {
	return len(q.fifo) > 0
}

// fill buffers the next read. It returns an error immediately when no bytes
// were read, or stores the error to surface on the following fill once the
// buffered bytes are consumed.
func (q *keyQueue) fill() error {
	if q.readErr != nil {
		return q.readErr
	}
	buf := make([]byte, 16)
	n, err := q.src.Read(buf)
	if n > 0 {
		q.fifo = append(q.fifo, buf[:n]...)
	}
	if err != nil {
		q.readErr = err
		if n == 0 {
			return err
		}
	}
	return nil
}

// selectByKeys runs the raw-mode interaction loop: arrow keys or j/k move
// the cursor, digits pick directly, Enter confirms, q/Esc/Ctrl-C cancels.
func (p *Picker) selectByKeys(title string, items []PickerItem) (int, error) {
	sel := defaultIndex(items)
	linesDrawn := p.render(title, items, sel)

	queue := newKeyQueue(p.In)
	for {
		b, err := queue.next()
		if err != nil {
			p.clearLines(linesDrawn)
			return -1, fmt.Errorf("reading selection input: %w", err)
		}

		switch b {
		case '\r', '\n':
			p.clearLines(linesDrawn)
			return sel, nil
		case 0x03, 'q':
			p.clearLines(linesDrawn)
			return -1, nil
		case 0x1b:
			action, escErr := p.resolveEscape(queue)
			if escErr != nil {
				p.clearLines(linesDrawn)
				return -1, nil
			}
			if action == moveUp {
				sel = dec(sel, len(items))
				linesDrawn = p.rerender(title, items, sel, linesDrawn)
			} else if action == moveDown {
				sel = inc(sel, len(items))
				linesDrawn = p.rerender(title, items, sel, linesDrawn)
			}
		case 'j':
			sel = inc(sel, len(items))
			linesDrawn = p.rerender(title, items, sel, linesDrawn)
		case 'k':
			sel = dec(sel, len(items))
			linesDrawn = p.rerender(title, items, sel, linesDrawn)
		default:
			if b >= '1' && b <= '9' {
				idx := int(b-'1') + 1
				if idx <= len(items) {
					p.clearLines(linesDrawn)
					return idx - 1, nil
				}
			}
		}
	}
}

type escapeAction int

const (
	escapeIgnore escapeAction = iota
	moveUp
	moveDown
)

// resolveEscape decodes an escape sequence following a bare ESC byte. Input
// closing mid-sequence or an unrecognized sequence cancels the picker.
func (p *Picker) resolveEscape(queue *keyQueue) (escapeAction, error) {
	if !queue.buffered() {
		if err := queue.fill(); err != nil {
			return escapeIgnore, err
		}
	}
	second, err := queue.next()
	if err != nil {
		return escapeIgnore, err
	}
	if second != '[' && second != 'O' {
		return escapeIgnore, nil
	}

	if !queue.buffered() {
		if err := queue.fill(); err != nil {
			return escapeIgnore, err
		}
	}
	third, err := queue.next()
	if err != nil {
		return escapeIgnore, err
	}
	switch third {
	case 'A':
		return moveUp, nil
	case 'B':
		return moveDown, nil
	}
	return escapeIgnore, nil
}

func inc(sel int, count int) int {
	if sel >= count-1 {
		return 0
	}
	return sel + 1
}

func dec(sel int, count int) int {
	if sel <= 0 {
		return count - 1
	}
	return sel - 1
}

func defaultIndex(items []PickerItem) int {
	for i, item := range items {
		if item.Default {
			return i
		}
	}
	return 0
}

// render paints the full selector screen and returns the number of lines.
func (p *Picker) render(title string, items []PickerItem, sel int) int {
	fmt.Fprintln(p.Out, "\x1b[1m"+title+"\x1b[0m")
	for i := range items {
		p.renderItem(i, sel, items)
	}
	fmt.Fprintf(p.Out, "\x1b[2m%s\x1b[0m\n",
		"  ↵ launch · ↑/↓ or j/k move · 1-"+strconv.Itoa(len(items))+" pick · q abort")
	return len(items) + 2
}

func (p *Picker) renderItem(i int, sel int, items []PickerItem) {
	item := items[i]
	pointer := "  "
	style := "\x1b[2m"
	if i == sel {
		pointer = "❯ "
		style = "\x1b[1m"
	}
	line := pointer + style + item.Label + "\x1b[0m"
	if item.Hint != "" {
		line += "\x1b[2m  " + item.Hint + "\x1b[0m"
	}
	fmt.Fprintln(p.Out, line)
}

func (p *Picker) clearLines(lines int) {
	for i := 0; i < lines; i++ {
		fmt.Fprint(p.Out, "\x1b[1A\x1b[2K")
	}
}

func (p *Picker) rerender(title string, items []PickerItem, sel int, linesDrawn int) int {
	p.clearLines(linesDrawn)
	return p.render(title, items, sel)
}

// selectByLine is the non-raw fallback: numbered prompt read line-by-line.
func (p *Picker) selectByLine(title string, items []PickerItem) (int, error) {
	fmt.Fprintln(p.Out, title)
	for i, item := range items {
		hint := ""
		if item.Hint != "" {
			hint = "  " + item.Hint
		}
		fmt.Fprintf(p.Out, "  %d) %s%s\n", i+1, item.Label, hint)
	}
	fmt.Fprintf(p.Out, "Enter choice [1-%d] (q to abort): ", len(items))

	line, err := bufio.NewReader(p.In).ReadString('\n')
	if err != nil {
		return -1, fmt.Errorf("reading selection: %w", err)
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return defaultIndex(items), nil
	}
	if strings.EqualFold(line, "q") {
		return -1, nil
	}
	num, err := strconv.Atoi(line)
	if err != nil || num < 1 || num > len(items) {
		return -1, fmt.Errorf("invalid selection %q", line)
	}
	return num - 1, nil
}
