package ui

const (
	minimumTerminalWidth  = 42
	minimumTerminalHeight = 10
	mediumBreakpoint      = 72
	wideBreakpoint        = 112

	terminalChromeHeight = 2 // one header row and one footer row
	paneBorderWidth      = 2
	paneHeadingHeight    = 1
	minimumStackedHeight = paneBorderWidth + paneHeadingHeight + 1
)

type layoutMode uint8

const (
	narrowLayout layoutMode = iota
	mediumLayout
	wideLayout
)

type paneGeometry struct {
	width         int
	height        int
	innerWidth    int
	innerHeight   int
	contentWidth  int
	contentHeight int
}

type layoutPlan struct {
	usable     bool
	mode       layoutMode
	bodyHeight int
	folders    paneGeometry
	messages   paneGeometry
	reader     paneGeometry
}

func calculateLayout(width, height int) layoutPlan {
	plan := layoutPlan{usable: width >= minimumTerminalWidth && height >= minimumTerminalHeight}
	if !plan.usable {
		return plan
	}

	plan.bodyHeight = max(1, height-terminalChromeHeight)
	switch {
	case width >= wideBreakpoint:
		plan.mode = wideLayout
		folderWidth := max(22, width*22/100)
		messageWidth := max(34, width*32/100)
		plan.folders = newPaneGeometry(folderWidth, plan.bodyHeight)
		plan.messages = newPaneGeometry(messageWidth, plan.bodyHeight)
		plan.reader = newPaneGeometry(width-folderWidth-messageWidth, plan.bodyHeight)
	case width >= mediumBreakpoint:
		plan.mode = mediumLayout
		folderWidth := max(22, width*28/100)
		detailWidth := width - folderWidth
		messageHeight := clamp(plan.bodyHeight*44/100, minimumStackedHeight, plan.bodyHeight-minimumStackedHeight)
		plan.folders = newPaneGeometry(folderWidth, plan.bodyHeight)
		plan.messages = newPaneGeometry(detailWidth, messageHeight)
		plan.reader = newPaneGeometry(detailWidth, plan.bodyHeight-messageHeight)
	default:
		plan.mode = narrowLayout
		pane := newPaneGeometry(width, plan.bodyHeight)
		plan.folders, plan.messages, plan.reader = pane, pane, pane
	}
	return plan
}

func newPaneGeometry(width, height int) paneGeometry {
	innerWidth := max(1, width-paneBorderWidth)
	innerHeight := max(1, height-paneBorderWidth)
	return paneGeometry{
		width:         width,
		height:        height,
		innerWidth:    innerWidth,
		innerHeight:   innerHeight,
		contentWidth:  max(1, innerWidth-2),
		contentHeight: max(1, innerHeight-paneHeadingHeight),
	}
}
