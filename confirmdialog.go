package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message to show a confirmation dialog
type showConfirmDialogMsg struct {
	title      string
	message    string
	yesMessage string
	noMessage  string
	callback   func(bool) tea.Msg
}

// Message returned when the dialog is complete
type confirmDialogResultMsg struct {
	confirmed bool
	cmd       tea.Cmd
}

// Confirmation dialog model
type confirmDialogModel struct {
	title      string
	message    string
	yesMessage string
	noMessage  string
	callback   func(bool) tea.Msg
}

// Create a new confirmation dialog
func newConfirmDialog(title, message, yesMsg, noMsg string, callback func(bool) tea.Msg) confirmDialogModel {
	if yesMsg == "" {
		yesMsg = "Yes"
	}
	if noMsg == "" {
		noMsg = "No"
	}

	return confirmDialogModel{
		title:      title,
		message:    message,
		yesMessage: yesMsg,
		noMessage:  noMsg,
		callback:   callback,
	}
}

// Initialize the confirmation dialog
func (m confirmDialogModel) Init() tea.Cmd {
	return nil
}

// Update the confirmation dialog
func (m confirmDialogModel) Update(msg tea.Msg) (confirmDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch strings.ToLower(msg.String()) {
		case "y", "enter":
			return m, func() tea.Msg {
				result := m.callback(true)
				return confirmDialogResultMsg{
					confirmed: true,
					cmd: func() tea.Msg {
						return result
					},
				}
			}
		case "n", "esc":
			return m, func() tea.Msg {
				result := m.callback(false)
				return confirmDialogResultMsg{
					confirmed: false,
					cmd: func() tea.Msg {
						return result
					},
				}
			}
		}
	}
	return m, nil
}

// Render the confirmation dialog
func (m confirmDialogModel) View() string {
	var content strings.Builder

	// Style the title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#FF5F87")).
		Padding(0, 1).
		Render(m.title)

	content.WriteString(title + "\n\n")
	content.WriteString(m.message + "\n\n")

	// Style the buttons
	yes := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#25A065")).
		Render(m.yesMessage + " (y/Enter)")

	no := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF5F87")).
		Render(m.noMessage + " (n/Esc)")

	content.WriteString(yes + "   " + no)

	// Apply border and padding to the entire dialog
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#888888")).
		Padding(1, 2).
		Render(content.String())
}
