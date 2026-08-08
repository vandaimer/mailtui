package ui

import (
	"strings"

	"mailtui/internal/message"
)

// messageProjection is an immutable view of one folder's messages after
// filtering. Message paths are the durable selection identity; positions are
// derived from the current visible order.
type messageProjection struct {
	messages []message.Message
	visible  []int
	selected int
}

func projectMessages(messages []message.Message, query, selectedPath string) messageProjection {
	projection := messageProjection{
		messages: messages,
		visible:  make([]int, 0, len(messages)),
		selected: -1,
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for index := range messages {
		if !matchesMessage(messages[index], query) {
			continue
		}
		position := len(projection.visible)
		projection.visible = append(projection.visible, index)
		if selectedPath != "" && messages[index].Path == selectedPath && projection.selected < 0 {
			projection.selected = position
		}
	}
	if projection.selected < 0 && len(projection.visible) > 0 {
		projection.selected = 0
	}
	return projection
}

func matchesMessage(item message.Message, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join([]string{
		item.Subject, item.From, item.To, item.Cc, item.Bcc,
	}, "\n")), query)
}

func (projection messageProjection) Len() int {
	return len(projection.visible)
}

func (projection messageProjection) Message(position int) *message.Message {
	if position < 0 || position >= len(projection.visible) {
		return nil
	}
	return &projection.messages[projection.visible[position]]
}

func (projection messageProjection) Selected() *message.Message {
	return projection.Message(projection.selected)
}

func (projection messageProjection) SelectedPath() string {
	selected := projection.Selected()
	if selected == nil {
		return ""
	}
	return selected.Path
}

func (projection messageProjection) SelectedPosition() int {
	return projection.selected
}

func (projection messageProjection) Move(delta int) string {
	if projection.selected < 0 {
		return ""
	}
	return projection.pathAt(clamp(projection.selected+delta, 0, len(projection.visible)-1))
}

func (projection messageProjection) Boundary(end bool) string {
	if len(projection.visible) == 0 {
		return ""
	}
	if end {
		return projection.pathAt(len(projection.visible) - 1)
	}
	return projection.pathAt(0)
}

func (projection messageProjection) pathAt(position int) string {
	item := projection.Message(position)
	if item == nil {
		return ""
	}
	return item.Path
}
