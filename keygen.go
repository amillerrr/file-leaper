package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/crypto/ssh"
)

// Key generation result message
type keyGenResultMsg struct {
	keyPath   string
	publicKey string
	username  string
	host      string
	destDir   string
	files     []string
	err       error
}

// Message to install the key on remote host
type installKeyMsg struct {
	keyPath   string
	publicKey string
	username  string
	host      string
	destDir   string
	files     []string
}

// Key generation model
type keyGenModel struct {
	inputs              []textinput.Model
	focusIndex          int
	generating          bool
	generatedKey        string
	username            string
	host                string
	destDir             string
	files               []string
	err                 error
	showingConfirmation bool
	confirmDialog       confirmDialogModel
}

// Create a new key generation model
func newKeyGenModel(username, host, destDir string, files []string, defaultKeyType string, defaultKeyBits int) keyGenModel {
	// Create form fields
	keyNameInput := newTextInput("id_rsa", false)
	keyNameInput.Focus()
	keyNameInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	keyNameInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	keyTypeInput := newTextInput("rsa", false) // Can be "rsa", "ed25519", etc.
	keyTypeInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	keyTypeInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	keyBitsInput := newTextInput("4096", false)
	keyBitsInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	keyBitsInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	keyCommentInput := newTextInput(username+"@"+host, false)
	keyCommentInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	keyCommentInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Set default values based on config
	keyTypeInput.SetValue(defaultKeyType)
	keyBitsInput.SetValue(strconv.Itoa(defaultKeyBits))

	// If default is ed25519, name should reflect that
	if defaultKeyType == "ed25519" {
		keyNameInput.SetValue("id_ed25519")
	}

	// Set comment
	keyCommentInput.SetValue(username + "@" + host)

	return keyGenModel{
		inputs:     []textinput.Model{keyNameInput, keyTypeInput, keyBitsInput, keyCommentInput},
		focusIndex: 0,
		username:   username,
		host:       host,
		destDir:    destDir,
		files:      files,
	}
}

// Initialize the key generation form
func (m keyGenModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update the key generation form
func (m keyGenModel) Update(msg tea.Msg) (keyGenModel, tea.Cmd) {
	// If showing confirmation dialog, handle that first
	if m.showingConfirmation {
		updatedDialog, cmd := m.confirmDialog.Update(msg)
		m.confirmDialog = updatedDialog
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Skip key handling during generation
		if m.generating {
			return m, nil
		}

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
			// If on the last field, check if key exists first
			if m.focusIndex == len(m.inputs)-1 {
				keyName := m.inputs[0].Value()

				// Check if key already exists
				home, err := os.UserHomeDir()
				if err != nil {
					m.err = fmt.Errorf("failed to get home directory: %v", err)
					return m, nil
				}

				keyPath := filepath.Join(home, ".ssh", keyName)

				if _, err := os.Stat(keyPath); err == nil {
					// Key exists, show confirmation
					m.showingConfirmation = true
					m.confirmDialog = newConfirmDialog(
						"Key Already Exists",
						fmt.Sprintf("The key %s already exists. Overwrite?", keyName),
						"Overwrite", "Cancel",
						func(confirmed bool) tea.Msg {
							if confirmed {
								m.showingConfirmation = false
								m.generating = true

								keyName := m.inputs[0].Value()
								keyType := m.inputs[1].Value()
								keyBits := m.inputs[2].Value()
								keyComment := m.inputs[3].Value()

								return m.generateKey(keyName, keyType, keyBits, keyComment)
							} else {
								m.showingConfirmation = false
								return nil
							}
						},
					)
					return m, nil
				}

				// If key doesn't exist, proceed with generation
				m.generating = true

				keyName = m.inputs[0].Value()
				keyType := m.inputs[1].Value()
				keyBits := m.inputs[2].Value()
				keyComment := m.inputs[3].Value()

				// Generate the key
				return m, func() tea.Msg {
					return m.generateKey(keyName, keyType, keyBits, keyComment)
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

	case keyGenResultMsg:
		// Handle key generation result
		if msg.err != nil {
			m.generating = false
			m.err = msg.err
			return m, nil
		}

		// Ask if user wants to install the key on the remote host
		m.showingConfirmation = true
		m.confirmDialog = newConfirmDialog(
			"Key Generated Successfully",
			fmt.Sprintf("Install this key on %s@%s?", msg.username, msg.host),
			"Yes, Install Key", "No, Use Key Without Installing",
			func(confirmed bool) tea.Msg {
				if confirmed {
					// Install the key
					return installKeyMsg{
						keyPath:   msg.keyPath,
						publicKey: msg.publicKey,
						username:  msg.username,
						host:      msg.host,
						destDir:   msg.destDir,
						files:     msg.files,
					}
				} else {
					// Skip installation, proceed to transfer
					return keySelectedMsg{
						keyPath:  msg.keyPath,
						username: msg.username,
						host:     msg.host,
						destDir:  msg.destDir,
						files:    msg.files,
					}
				}
			},
		)
		return m, nil
	}

	// Update the focused input
	var cmd tea.Cmd
	if m.focusIndex < len(m.inputs) {
		m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	}
	return m, cmd
}

// Generate SSH key pair
func (m keyGenModel) generateKey(name, keyType, bits, comment string) tea.Msg {
	// Get user's home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return keyGenResultMsg{err: err}
	}

	// SSH directory
	sshDir := filepath.Join(home, ".ssh")

	// Create SSH directory if it doesn't exist
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		if err := os.Mkdir(sshDir, 0700); err != nil {
			return keyGenResultMsg{err: err}
		}
	}

	// Private key path
	keyPath := filepath.Join(sshDir, name)

	// Public key path
	pubKeyPath := keyPath + ".pub"

	// Generate appropriate key type
	var privateKey []byte
	var publicKeyString string

	switch strings.ToLower(keyType) {
	case "ed25519":
		// Generate Ed25519 key
		Info("Generating ED25519 key: %s", keyPath)
		publicKey, privateKeyData, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return keyGenResultMsg{err: fmt.Errorf("failed to generate Ed25519 key: %v", err)}
		}

		// Convert to SSH format
		sshPublicKey, err := ssh.NewPublicKey(publicKey)
		if err != nil {
			return keyGenResultMsg{err: fmt.Errorf("failed to create SSH public key: %v", err)}
		}

		// Format private key in OpenSSH format
		privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKeyData)
		if err != nil {
			return keyGenResultMsg{err: fmt.Errorf("failed to marshal private key: %v", err)}
		}

		// Create PEM block for private key
		pemBlock := &pem.Block{
			Type:  "OPENSSH PRIVATE KEY",
			Bytes: privateKeyBytes,
		}
		privateKey = pem.EncodeToMemory(pemBlock)

		// Format public key
		publicKeyString = fmt.Sprintf("%s %s", ssh.MarshalAuthorizedKey(sshPublicKey), comment)

	case "rsa":
		// Parse the key bits
		bitSize, err := strconv.Atoi(bits)
		if err != nil || bitSize < 2048 {
			bitSize = 4096 // Default to 4096 if invalid
		}

		Info("Generating RSA key (%d bits): %s", bitSize, keyPath)

		// Generate RSA key
		rsaKey, err := rsa.GenerateKey(rand.Reader, bitSize)
		if err != nil {
			return keyGenResultMsg{err: fmt.Errorf("failed to generate RSA key: %v", err)}
		}

		// Convert to SSH format
		sshPublicKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
		if err != nil {
			return keyGenResultMsg{err: fmt.Errorf("failed to create SSH public key: %v", err)}
		}

		// Create PEM block for private key
		pemBlock := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
		}
		privateKey = pem.EncodeToMemory(pemBlock)

		// Format public key
		publicKeyString = fmt.Sprintf("%s %s", ssh.MarshalAuthorizedKey(sshPublicKey), comment)

	default:
		return keyGenResultMsg{err: fmt.Errorf("unsupported key type: %s", keyType)}
	}

	// Write private key with proper permissions
	if err := os.WriteFile(keyPath, privateKey, 0600); err != nil {
		return keyGenResultMsg{err: err}
	}

	// Write public key
	if err := os.WriteFile(pubKeyPath, []byte(publicKeyString), 0644); err != nil {
		return keyGenResultMsg{err: err}
	}

	Info("Key generation successful: %s", keyPath)

	return keyGenResultMsg{
		keyPath:   keyPath,
		publicKey: publicKeyString,
		username:  m.username,
		host:      m.host,
		destDir:   m.destDir,
		files:     m.files,
	}
}

// Render the key generation view
func (m keyGenModel) View() string {
	// If showing confirmation dialog, show that instead
	if m.showingConfirmation {
		return m.confirmDialog.View()
	}

	if m.err != nil {
		return fmt.Sprintf("Key Generation Error: %v\n\nPress any key to return", m.err)
	}

	if m.generating {
		return "Generating SSH key...\n\nPlease wait, this may take a moment."
	}

	var content strings.Builder

	content.WriteString("Generate SSH Key\n\n")

	// Key name field
	content.WriteString("Key Filename: ")
	content.WriteString(m.inputs[0].View())
	content.WriteString("\n\n")

	// Key type field
	content.WriteString("Key Type (rsa, ed25519): ")
	content.WriteString(m.inputs[1].View())
	content.WriteString("\n\n")

	// Key bits field
	content.WriteString("Key Bits (2048, 4096): ")
	content.WriteString(m.inputs[2].View())
	content.WriteString("\n\n")

	// Key comment field
	content.WriteString("Key Comment: ")
	content.WriteString(m.inputs[3].View())
	content.WriteString("\n\n")

	content.WriteString("Tab: next field | Shift+Tab: previous field | Enter: submit")

	return content.String()
}
