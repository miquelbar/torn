package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// fakeRaw returns an injectable MakeRaw that succeeds and records restoration.
func fakeRaw(restored *bool) func(fd uintptr) (func(), error) {
	return func(fd uintptr) (func(), error) {
		return func() { *restored = true }, nil
	}
}

// failingRaw simulates platforms without raw-mode support.
func failingRaw() func(fd uintptr) (func(), error) {
	return func(fd uintptr) (func(), error) {
		return nil, errRawUnsupported
	}
}

func pickerWith(input string, out *bytes.Buffer, makeRaw func(fd uintptr) (func(), error)) *Picker {
	return &Picker{
		Out:     out,
		In:      strings.NewReader(input),
		MakeRaw: makeRaw,
	}
}

func items3() []PickerItem {
	return []PickerItem{
		{Label: "claude", Hint: "● ready"},
		{Label: "vibe", Hint: "● active", Default: true},
		{Label: "agy", Hint: "● cooldown 1h"},
	}
}

func TestPickerSelectsDefaultWithImmediateEnter(t *testing.T) {
	var out bytes.Buffer
	var restored bool
	p := pickerWith("\r", &out, fakeRaw(&restored))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected default selection 1, got %d", idx)
	}
	if !restored {
		t.Error("expected raw mode to be restored")
	}
	if !strings.Contains(out.String(), "vibe") {
		t.Errorf("expected rendered items, got: %q", out.String())
	}
}

func TestPickerArrowNavigationWrap(t *testing.T) {
	var out bytes.Buffer
	pv := false
	p := pickerWith("\x1b[B\x1b[B\r", &out, fakeRaw(&pv))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	// Start at default 1; two downs wrap to 0.
	if idx != 0 {
		t.Errorf("expected wrapped selection 0, got %d", idx)
	}
}

func TestPickerVimKeysAndUpWrap(t *testing.T) {
	var out bytes.Buffer
	pv := false
	p := pickerWith("jkj\r", &out, fakeRaw(&pv))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	// Start 1: j→2, k→1, j→2.
	if idx != 2 {
		t.Errorf("expected selection 2, got %d", idx)
	}

	out.Reset()
	p = pickerWith("kk\r", &out, fakeRaw(&pv))
	idx, err = p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	// Start 1: k→0, k→2 (wrap).
	if idx != 2 {
		t.Errorf("expected wrapped selection 2, got %d", idx)
	}
}

func TestPickerDigitDirectSelect(t *testing.T) {
	var out bytes.Buffer
	pv := false
	p := pickerWith("3", &out, fakeRaw(&pv))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 2 {
		t.Errorf("expected selection 2, got %d", idx)
	}
}

func TestPickerCancelWithQ(t *testing.T) {
	var out bytes.Buffer
	pv := false
	p := pickerWith("q", &out, fakeRaw(&pv))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != -1 {
		t.Errorf("expected -1 on cancel, got %d", idx)
	}
}

func TestPickerCancelWithEscapeAndCtrlC(t *testing.T) {
	for _, key := range []string{"\x1b", "\x03"} {
		var out bytes.Buffer
		pv := false
		p := pickerWith(key, &out, fakeRaw(&pv))

		idx, err := p.Select("Pick a CLI", items3())
		if err != nil {
			t.Fatalf("Select failed for %q: %v", key, err)
		}
		if idx != -1 {
			t.Errorf("expected -1 on cancel for %q, got %d", key, idx)
		}
	}
}

func TestPickerDigitOutOfRangeIgnored(t *testing.T) {
	var out bytes.Buffer
	pv := false
	p := pickerWith("9\r", &out, fakeRaw(&pv))

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected unchanged default 1, got %d", idx)
	}
}

func TestPickerFallbackToLineModeWhenRawUnsupported(t *testing.T) {
	var out bytes.Buffer
	// Line mode: user types the number followed by newline.
	p := pickerWith("3\n", &out, failingRaw())

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 2 {
		t.Errorf("expected selection 2, got %d", idx)
	}
	if !strings.Contains(out.String(), "1)") {
		t.Errorf("expected numbered line-mode menu, got: %q", out.String())
	}
}

func TestPickerLineModeEmptyInputSelectsDefault(t *testing.T) {
	var out bytes.Buffer
	p := pickerWith("\n", &out, failingRaw())

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected default selection 1, got %d", idx)
	}
}

func TestPickerLineModeCancel(t *testing.T) {
	var out bytes.Buffer
	p := pickerWith("q\n", &out, failingRaw())

	idx, err := p.Select("Pick a CLI", items3())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != -1 {
		t.Errorf("expected -1 on cancel, got %d", idx)
	}
}

func TestPickerLineModeInvalidInput(t *testing.T) {
	var out bytes.Buffer
	p := pickerWith("nope\n", &out, failingRaw())

	if _, err := p.Select("Pick a CLI", items3()); err == nil {
		t.Error("expected error for invalid line-mode input")
	}
}

func TestPickerNoItems(t *testing.T) {
	var out bytes.Buffer
	p := pickerWith("", &out, failingRaw())

	if _, err := p.Select("Pick a CLI", nil); err == nil {
		t.Error("expected error with no items")
	}
}

func TestPickerInputClosed(t *testing.T) {
	var out bytes.Buffer
	closedErr := errors.New("closed")
	p := &Picker{
		Out: &out,
		In:  failingReader{err: closedErr},
		MakeRaw: func(fd uintptr) (func(), error) {
			return func() {}, nil
		},
	}

	if _, err := p.Select("Pick a CLI", items3()); !errors.Is(err, closedErr) {
		t.Errorf("expected read error, got: %v", err)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }
