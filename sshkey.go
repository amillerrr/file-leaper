package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message sent when a key is selected
type keySelectedMsg struct {
	keyPath  string
	username string
	host     string
	destDir  string
	files    []string
}

// Message to generate a new key
type generateKeyMsg struct {
	username string
	host     string
	destDir  string
	files    []string
}

// Message returned when keys are found
type keysFoundMsg struct {
	keys []keyItem
}

// SSH key item for the list
type keyItem struct {
	name    string
	path    string
	comment string
	keyType string
}

// Methods required for the list item interface
func (i keyItem) Title() string {
	if i.keyType != "" {
		return fmt.Sprintf("%s (%s)", i.name, i.keyType)
	}
	return i.name
}

func (i keyItem) Description() string { return i.comment }

func (i keyItem) FilterValue() string { return i.name + " " + i.comment }

// Key selection model
type keySelectionModel struct {
	list     list.Model
	spinner  spinner.Model
	scanning bool
	username string
	host     string
	destDir  string
	files    []string
	err      error
}

// Create a new key selection model
func newKeySelectionModel(username, host, destDir string, files []string) keySelectionModel {
	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Initialize delegate with custom styles
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))

	// Create empty list
	keyList := list.New([]list.Item{}, delegate)
	keyList.Title = "Select SSH Key"
	keyList.SetShowStatusBar(false)
	keyList.SetFilteringEnabled(true)
	keyList.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1)

	return keySelectionModel{
		list:     keyList,
		spinner:  s,
		scanning: true,
		username: username,
		host:     host,
		destDir:  destDir,
		files:    files,
	}
}

// Initialize the key selection screen
func (m keySelectionModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.scanForKeys(),
	)
}

// Update the key selection model
func (m keySelectionModel) Update(msg tea.Msg) (keySelectionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't handle keys during scanning
		if m.scanning {
			break
		}

		switch msg.String() {
		case "enter":
			// Check if "Generate New Key" is selected
			item, ok := m.list.SelectedItem().(keyItem)
			if !ok {
				m.err = fmt.Errorf("invalid selection")
				return m, nil
			}

			if item.path == "generate" {
				return m, func() tea.Msg {
					return generateKeyMsg{
						username: m.username,
						host:     m.host,
						destDir:  m.destDir,
						files:    m.files,
					}
				}
			} else if item.path == "password" {
				// Use password authentication
				return m, func() tea.Msg {
					return keySelectedMsg{
						keyPath:  "password",
						username: m.username,
						host:     m.host,
						destDir:  m.destDir,
						files:    m.files,
					}
				}
			}

			// Otherwise, return the selected key
			return m, func() tea.Msg {
				return keySelectedMsg{
					keyPath:  item.path,
					username: m.username,
					host:     m.host,
					destDir:  m.destDir,
					files:    m.files,
				}
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case keysFoundMsg:
		// Update list with found keys
		m.scanning = false

		// Create list items
		var items []list.Item

		// Debug logging for keys
		Debug("Total SSH keys found: %d", len(msg.keys))

		// Add each key as a list item
		for _, key := range msg.keys {
			Debug("Adding key to list: Name=%s, Type=%s, Comment=%s",
				key.name, key.keyType, key.comment)
			items = append(items, key)
		}

		// Add "Use Password Authentication" option
		passwordOption := keyItem{
			name:    "Use Password Authentication",
			path:    "password",
			comment: "Use password instead of key authentication",
		}
		items = append(items, passwordOption)
		Debug("Added password authentication option")

		// Add "Generate New Key" option at the end
		generateOption := keyItem{
			name:    "Generate New Key",
			path:    "generate",
			comment: "Create a new SSH key pair",
		}
		items = append(items, generateOption)
		Debug("Added generate new key option")

		// Log the final number of items
		Debug("Total items in list (including special options): %d", len(items))

		// Set list items with error checking
		if err := m.list.SetItems(items); err != nil {
			Error("Failed to set list items: %v", err)
			m.err = fmt.Errorf("could not populate SSH keys: %v", err)
			return m, nil
		}

		return m, nil

	case errMsg:
		// Handle error
		m.scanning = false
		m.err = msg.err
		return m, nil
	}

	// Handle list updates
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// Scan for existing SSH keys
func (m keySelectionModel) scanForKeys() tea.Cmd {
	return func() tea.Msg {
		// Get user's home directory
		home, err := os.UserHomeDir()
		if err != nil {
			return errMsg{err: err}
		}

		// SSH directory
		sshDir := filepath.Join(home, ".ssh")

		// Try to read the directory
		entries, err := os.ReadDir(sshDir)
		if os.IsNotExist(err) {
			// SSH directory doesn't exist yet, return an empty list with no error
			return keysFoundMsg{keys: []keyItem{}}
		} else if err != nil {
			// Other filesystem error
			return errMsg{err: fmt.Errorf("error reading SSH directory: %v", err)}
		}

		// Look for private keys (no .pub extension)
		var keys []keyItem
		for _, entry := range entries {
			name := entry.Name()

			// Debug: print all entries
			Debug("Found SSH directory entry: %s", name)

			// Skip public keys, known_hosts, config, etc.
			if strings.HasSuffix(name, ".pub") || name == "known_hosts" || name == "config" || name == "authorized_keys" {
				continue
			}

			// Check if it's a hidden file
			if strings.HasPrefix(name, ".") {
				Debug("Skipping hidden file: %s", name)
				continue
			}

			// Check if it's likely a private key
			path := filepath.Join(sshDir, name)
			info, err := entry.Info()
			if err != nil {
				Debug("Error getting file info for %s: %v", name, err)
				continue
			}

			// Debug: print file mode
			Debug("File %s mode: %v", name, info.Mode())

			// Private keys should be regular files with restricted permissions
			if !info.Mode().IsRegular() {
				Debug("Skipping non-regular file: %s", name)
				continue
			}

			// Try to read the file to determine if it's a valid SSH private key
			keyData, err := os.ReadFile(path)
			if err != nil {
				Debug("Error reading file %s: %v", name, err)
				continue
			}

			// Debug: print first few bytes of the file
			maxPrintBytes := 100
			if len(keyData) < maxPrintBytes {
				maxPrintBytes = len(keyData)
			}
			Debug("First %d bytes of %s: %s", maxPrintBytes, name, string(keyData[:maxPrintBytes]))

			// Check if it looks like a PEM encoded key
			if !isPEMFormatted(keyData) {
				Debug("File %s does not look like a PEM key", name)
				continue
			}

			// Determine key type with more detailed debugging
			keyType := "unknown"
			keyTypeDebug := func(keyTypeStr string, condition bool) {
				if condition {
					keyType = keyTypeStr
					Debug("Detected key type as %s for %s", keyTypeStr, name)
				}
			}

			keyTypeDebug("RSA", bytes.Contains(keyData, []byte("BEGIN RSA PRIVATE KEY")))

			if keyType == "unknown" {
				keyTypeDebug("OPENSSH", bytes.Contains(keyData, []byte("BEGIN OPENSSH PRIVATE KEY")))
			}

			// If OPENSSH key, try to determine more specific type
			if keyType == "OPENSSH" {
				pubKeyPath := path + ".pub"
				if pubData, err := os.ReadFile(pubKeyPath); err == nil {
					parts := strings.Fields(string(pubData))
					if len(parts) >= 1 {
						keyTypeMap := map[string]string{
							"ssh-rsa":     "RSA",
							"ssh-ed25519": "ED25519",
							"ecdsa-sha2":  "ECDSA",
						}
						for prefix, typeName := range keyTypeMap {
							if strings.HasPrefix(parts[0], prefix) {
								keyType = typeName
								Debug("Refined key type for %s to %s based on public key", name, keyType)
								break
							}
						}
					}
				}
			}

			// Additional key type checks
			keyTypeDebug("ECDSA", bytes.Contains(keyData, []byte("BEGIN EC PRIVATE KEY")))
			keyTypeDebug("DSA", bytes.Contains(keyData, []byte("BEGIN DSA PRIVATE KEY")))

			// Try to read the corresponding public key for comment
			comment := name
			pubPath := path + ".pub"
			if pubData, err := os.ReadFile(pubPath); err == nil {
				parts := strings.Fields(string(pubData))
				if len(parts) >= 3 {
					comment = parts[2]
					Debug("Found comment for %s: %s", name, comment)
				}
			}

			// Create a key item
			keys = append(keys, keyItem{
				name:    name,
				path:    path,
				comment: comment,
				keyType: keyType,
			})

			Debug("Added key item: name=%s, path=%s, comment=%s, type=%s", name, path, comment, keyType)
		}

		// Return found keys as a proper message
		return keysFoundMsg{keys: keys}
	}
}

// Helper function to check if a byte array looks like a PEM formatted key
func isPEMFormatted(data []byte) bool {
	// Look for common PEM header strings
	pemHeaders := []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN DSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
		"-----BEGIN PRIVATE KEY-----",
	}

	for _, header := range pemHeaders {
		if bytes.Contains(data, []byte(header)) {
			return true
		}
	}

	return false
}

// Render the key selection view
func (m keySelectionModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress any key to return", m.err)
	}

	// Show a spinner while scanning
	if m.scanning {
		return fmt.Sprintf("Scanning for SSH keys...\n\n%s", m.spinner.View())
	}

	// Get count of items
	itemCount := len(m.list.Items())

	// Show more detailed message if no keys found
	if itemCount <= 2 { // Only the "Generate New Key" and "Use Password" options
		Debug("No SSH keys detected. Item count: %d", itemCount)
		return "No SSH keys found in ~/.ssh directory.\n\n" +
			"Possible reasons:\n" +
			"- No SSH keys have been generated\n" +
			"- Keys are not in the standard OPENSSH format\n" +
			"- Keys may have incorrect file permissions\n\n" +
			"Press Enter to use password authentication\n" +
			"Or select 'Generate New Key' to create a new SSH key pair\n" +
			"Press Esc to go back"
	}

	title := fmt.Sprintf("SSH Key Selection for %s@%s", m.username, m.host)

	// Add some helpful instructions
	keyInfo := "\nImportant: The remote host must be configured to accept the selected authentication method.\n"
	keyInfo += "If you're using keys, make sure the public key is already in the remote's authorized_keys file\n"
	keyInfo += "or select 'Generate New Key' to create and install a new key.\n"

	help := "\nEnter: select key | Esc: back to auth form"

	return title + "\n\n" + m.list.View() + keyInfo + help
}
