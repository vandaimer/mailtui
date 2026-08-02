package ui

import "testing"

func TestCalculateLayoutAtResponsiveBreakpoints(t *testing.T) {
	tests := []struct {
		name                                       string
		width, height                              int
		mode                                       layoutMode
		folderWidth, messageWidth, readerWidth     int
		messageHeight, readerHeight, readerContent int
	}{
		{name: "below medium", width: 71, height: 32, mode: narrowLayout, folderWidth: 71, messageWidth: 71, readerWidth: 71, messageHeight: 30, readerHeight: 30, readerContent: 27},
		{name: "at medium", width: 72, height: 32, mode: mediumLayout, folderWidth: 22, messageWidth: 50, readerWidth: 50, messageHeight: 13, readerHeight: 17, readerContent: 14},
		{name: "below wide", width: 111, height: 32, mode: mediumLayout, folderWidth: 31, messageWidth: 80, readerWidth: 80, messageHeight: 13, readerHeight: 17, readerContent: 14},
		{name: "at wide", width: 112, height: 32, mode: wideLayout, folderWidth: 24, messageWidth: 35, readerWidth: 53, messageHeight: 30, readerHeight: 30, readerContent: 27},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := calculateLayout(test.width, test.height)
			if !got.usable || got.mode != test.mode {
				t.Fatalf("layout usability/mode = %v/%v", got.usable, got.mode)
			}
			if got.folders.width != test.folderWidth || got.messages.width != test.messageWidth || got.reader.width != test.readerWidth {
				t.Fatalf("pane widths = %d/%d/%d", got.folders.width, got.messages.width, got.reader.width)
			}
			if got.messages.height != test.messageHeight || got.reader.height != test.readerHeight {
				t.Fatalf("message/reader heights = %d/%d", got.messages.height, got.reader.height)
			}
			if got.reader.contentHeight != test.readerContent {
				t.Fatalf("reader content height = %d, want %d", got.reader.contentHeight, test.readerContent)
			}
		})
	}
}

func TestCalculateLayoutKeepsSmallTerminalsValid(t *testing.T) {
	for _, test := range []struct {
		width  int
		mode   layoutMode
		reader int
	}{
		{width: 42, mode: narrowLayout, reader: 8},
		{width: 71, mode: narrowLayout, reader: 8},
		{width: 72, mode: mediumLayout, reader: 4},
		{width: 111, mode: mediumLayout, reader: 4},
		{width: 112, mode: wideLayout, reader: 8},
	} {
		got := calculateLayout(test.width, minimumTerminalHeight)
		if !got.usable || got.mode != test.mode {
			t.Fatalf("width %d: usability/mode = %v/%v", test.width, got.usable, got.mode)
		}
		if got.reader.height != test.reader || got.reader.contentHeight < 1 {
			t.Fatalf("width %d: reader geometry = %#v", test.width, got.reader)
		}
		if got.mode == mediumLayout && got.messages.height+got.reader.height != got.bodyHeight {
			t.Fatalf("width %d: stacked heights %d + %d != %d", test.width, got.messages.height, got.reader.height, got.bodyHeight)
		}
	}
}

func TestCalculateLayoutRejectsUnsupportedTerminalSize(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {41, 32}, {42, 9}} {
		if got := calculateLayout(size[0], size[1]); got.usable {
			t.Fatalf("layout %dx%d is usable", size[0], size[1])
		}
	}
}

func TestCalculateLayoutPaneDimensionsCompose(t *testing.T) {
	for width := minimumTerminalWidth; width <= 160; width++ {
		for height := minimumTerminalHeight; height <= 40; height++ {
			got := calculateLayout(width, height)
			if got.bodyHeight != height-terminalChromeHeight {
				t.Fatalf("%dx%d: body height = %d", width, height, got.bodyHeight)
			}
			switch got.mode {
			case wideLayout:
				if got.folders.width+got.messages.width+got.reader.width != width {
					t.Fatalf("%dx%d: wide pane widths do not compose", width, height)
				}
			case mediumLayout:
				if got.folders.width+got.messages.width != width || got.messages.width != got.reader.width {
					t.Fatalf("%dx%d: medium pane widths do not compose", width, height)
				}
				if got.messages.height+got.reader.height != got.bodyHeight {
					t.Fatalf("%dx%d: medium pane heights do not compose", width, height)
				}
			case narrowLayout:
				if got.reader.width != width || got.reader.height != got.bodyHeight {
					t.Fatalf("%dx%d: narrow pane does not fill the body", width, height)
				}
			}
		}
	}
}
