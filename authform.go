package main

import (
	"fmt"
	"os/user"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message sent when authentication is complete
type authCompleteMsg struct {
	host     string
	username string
	password string
	destDir  string
	files    []string
}

// Authentication form model
type authFormModel struct {
	host       string
	files      []string
	inputs     []textinput.Model
	focusIndex int
	err        error
	config     *AppConfig
}

// Create a new text input field
func newTextInput(placeholder string, isPassword bool) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 100
	input.Width = 40

	if isPassword {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}

	return input
}

// Create a new authentication form model
func newAuthFormModel(host string, files []string, config *AppConfig) authFormModel {
	// Try to get current username
	currentUser, err := user.Current()
	defaultUsername := "user"
	if err == nil && currentUser != nil {
		defaultUsername = currentUser.Username
	}

	// Create form fields
	usernameInput := newTextInput(defaultUsername, false)
	usernameInput.Focus()
	usernameInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	usernameInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	usernameInput.SetValue(defaultUsername)

	passwordInput := newTextInput("password", true)
	passwordInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	passwordInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Use destination directory from config
	defaultDir := config.DefaultDestDir
	if defaultDir == "" {
		defaultDir = "/home/" + defaultUsername
	}

	dirInput := newTextInput(defaultDir, false)
	dirInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	dirInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	dirInput.SetValue(defaultDir)

	return authFormModel{
		host:       host,
		files:      files,
		inputs:     []textinput.Model{usernameInput, passwordInput, dirInput},
		focusIndex: 0,
		config:     config,
	}
}

// Initialize the authentication form
func (m authFormModel) Init() tea.Cmd {
	return textinput.Blink
}

// Validate form inputs
func (m authFormModel) validateInputs() error {
	// Validate username
	if m.inputs[0].Value() == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Validate destination directory
	destDir := m.inputs[2].Value()
	if destDir == "" {
		return fmt.Errorf("destination directory cannot be empty")
	}

	// Ensure path is absolute
	if !strings.HasPrefix(destDir, "/") {
		return fmt.Errorf("destination directory must be an absolute path")
	}

	// Check for invalid characters that could cause issues
	invalidChars := []string{"`", "$", "&", "|", ";", "<", ">", "(", ")", "\\", "\"", "'"}
	for _, char := range invalidChars {
		if strings.Contains(destDir, char) {
			return fmt.Errorf("destination directory contains invalid character: %s", char)
		}
	}

	return nil
}

// Update the authentication form
func (m authFormModel) Update(msg tea.Msg) (authFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "up", "down":
			// Cycle between input fields
			s := msg.String()

			// Cycle focus
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			// Wrap around
			if m.focusIndex < 0 {
				m.focusIndex = len(m.inputs) - 1
			} else if m.focusIndex >= len(m.inputs) {
				m.focusIndex = 0
			}

			// Update focus
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i < len(m.inputs); i++ {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}

			return m, tea.Batch(cmds...)

		case "enter":
			// If on the last field, submit the form
			if m.focusIndex == len(m.inputs)-1 {
				username := m.inputs[0].Value()
				password := m.inputs[1].Value()
				destDir := m.inputs[2].Value()

				// Validate inputs
				if err := m.validateInputs(); err != nil {
					m.err = err
					return m, nil
				}

				// Save destination directory to config
				m.config.DefaultDestDir = destDir
				m.config.Save()

				// Ensure destination directory has a trailing slash
				if !strings.HasSuffix(destDir, "/") {
					destDir += "/"
				}

				// Return auth complete message
				return m, func() tea.Msg {
					return authCompleteMsg{
						host:     m.host,
						username: username,
						password: password,
						destDir:  destDir,
						files:    m.files,
					}
				}
			}

			// Otherwise, move to the next field
			m.focusIndex++
			if m.focusIndex >= len(m.inputs) {
				m.focusIndex = 0
			}

			// Update focus
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i < len(m.inputs); i++ {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}

			return m, tea.Batch(cmds...)
		}
	}

	// Update the focused input
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	return m, cmd
}

// Render the authentication form
func (m authFormModel) View() string {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("Transfer Files to %s\n\n", m.host))
	content.WriteString(fmt.Sprintf("Transferring %d files\n\n", len(m.files)))

	// Display error if present
	if m.err != nil {
		content.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Render(fmt.Sprintf("Error: %v\n\n", m.err)))
	}

	content.WriteString("Please enter connection details:\n\n")

	// Username field
	content.WriteString("Username: ")
	content.WriteString(m.inputs[0].View())
	content.WriteString("\n\n")

	// Password field
	content.WriteString("Password: ")
	content.WriteString(m.inputs[1].View())
	content.WriteString("\n\n")

	// Destination directory field
	content.WriteString("Destination Directory: ")
	content.WriteString(m.inputs[2].View())
	content.WriteString("\n\n")

	content.WriteString("Tab: next field | Shift+Tab: previous field | Enter: submit")

	return content.String()
}
