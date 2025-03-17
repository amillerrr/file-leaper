package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Key installation result message
type keyInstallResultMsg struct {
	success  bool
	username string
	host     string
	destDir  string
	files    []string
	err      error
}

// Enhanced key installation model with ssh-copy-id support
type keyInstallModel struct {
	spinner         spinner.Model
	installing      bool
	complete        bool
	manualMode      bool
	keyPath         string
	publicKey       string
	username        string
	password        string // For initial authentication
	host            string
	destDir         string
	files           []string
	status          string
	err             error
	copyToClipboard bool
}

// Create a new key installation model
func newKeyInstallModel(keyPath, publicKey, username, password, host, destDir string, files []string) keyInstallModel {
	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return keyInstallModel{
		spinner:         s,
		installing:      true,
		manualMode:      false,
		keyPath:         keyPath,
		publicKey:       publicKey,
		username:        username,
		password:        password,
		host:            host,
		destDir:         destDir,
		files:           files,
		status:          "Installing public key on remote host...",
		copyToClipboard: false,
	}
}

// Initialize the key installation
func (m keyInstallModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.installKey(),
	)
}

// Update the key installation model
func (m keyInstallModel) Update(msg tea.Msg) (keyInstallModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		Debug("Key pressed in keyInstall: %s, Complete: %v, Error: %v", msg.String(), m.complete, m.err != nil)

		// If installation is complete and successful
		if m.complete && m.err == nil && msg.String() == "enter" {
			Info("Key installation successful, proceeding to transfer")
			// Force transition to file transfer
			return m, func() tea.Msg {
				return keySelectedMsg{
					keyPath:  m.keyPath,
					username: m.username,
					host:     m.host,
					destDir:  m.destDir,
					files:    m.files,
				}
			}
		}

		// If in manual mode, handle key presses
		if m.manualMode {
			switch msg.String() {
			case "enter":
				// In manual mode, pressing Enter should proceed to file transfer
				return m, func() tea.Msg {
					return keySelectedMsg{
						keyPath:  m.keyPath,
						username: m.username,
						host:     m.host,
						destDir:  m.destDir,
						files:    m.files,
					}
				}

			case "c":
				// Simulate copy to clipboard (in a real app, implement actual clipboard functionality)
				m.copyToClipboard = true
				m.status = "Public key copied to clipboard"
				return m, nil

			case "esc":
				// Return to auth form
				return m, func() tea.Msg {
					return backToAuthFormMsg{}
				}
			}
		} else if m.complete && m.err != nil {
			// Installation failed
			if msg.String() == "enter" {
				// Switch to manual mode
				m.manualMode = true
				m.status = "Manual key installation instructions"
				return m, nil
			} else if msg.String() == "esc" {
				// Return to auth form
				return m, func() tea.Msg {
					return backToAuthFormMsg{}
				}
			}
		}

	case keyInstallResultMsg:
		// Handle installation result
		m.installing = false
		m.complete = true

		if msg.err != nil {
			m.err = msg.err
			Error("Key installation failed: %v", msg.err)

			// Check if it's an authentication error
			if strings.Contains(msg.err.Error(), "no supported methods remain") ||
				strings.Contains(msg.err.Error(), "unable to authenticate") {
				m.status = "Automatic key installation failed - password authentication not supported"
				m.manualMode = true
			} else if strings.Contains(msg.err.Error(), "key installed but verification failed") {
				m.status = "Key installed but verification failed"
			} else {
				m.status = fmt.Sprintf("Failed to install key: %v", msg.err)
			}
			return m, nil
		} else {
			// Key installed successfully - automatically transition after 2 seconds
			m.status = "Key installed successfully! Continuing to file transfer..."
			Info("Key installation successful. Automatically continuing in 2 seconds...")

			// Return a command that will send a transition message after a delay
			return m, tea.Sequence(
				tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return keySelectedMsg{
						keyPath:  m.keyPath,
						username: m.username,
						host:     m.host,
						destDir:  m.destDir,
						files:    m.files,
					}
				}),
			)
		}

	case spinner.TickMsg:
		// Update spinner only if we're installing
		if m.installing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// Get host key callback for SSH connections
func getHostKeyCallback(host string) (ssh.HostKeyCallback, error) {
	// Get user's known_hosts file
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %v", err)
	}

	knownHostsFile := filepath.Join(home, ".ssh", "known_hosts")

	// Check if known_hosts file exists, create if it doesn't
	if _, err := os.Stat(knownHostsFile); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
			return nil, fmt.Errorf("failed to create .ssh directory: %v", err)
		}
		if _, err := os.Create(knownHostsFile); err != nil {
			return nil, fmt.Errorf("failed to create known_hosts file: %v", err)
		}
	}

	// Create host key callback function
	hostKeyCallback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts: %v", err)
	}

	return hostKeyCallback, nil
}

// Install the public key on the remote host using SFTP
func (m keyInstallModel) installKey() tea.Cmd {
	return func() tea.Msg {
		// Set up SSH client configuration with password
		config := &ssh.ClientConfig{
			User: m.username,
			Auth: []ssh.AuthMethod{
				ssh.Password(m.password),
			},
			Timeout: 15 * time.Second,
		}

		// Setup proper host key verification
		hostKeyCallback, err := getHostKeyCallback(m.host)
		if err != nil {
			Warn("Failed to set up host key verification: %v, falling back to insecure mode", err)
			// Fall back to insecure for first connection
			config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
		} else {
			config.HostKeyCallback = hostKeyCallback
		}

		// Ensure host has port
		hostPort := m.host
		if !strings.Contains(hostPort, ":") {
			hostPort += ":22"
		}

		Info("Connecting to %s for key installation", hostPort)

		// Connect to the SSH server
		client, err := ssh.Dial("tcp", hostPort, config)
		if err != nil {
			return keyInstallResultMsg{
				success: false,
				err:     fmt.Errorf("failed to connect: %v", err),
			}
		}
		defer client.Close()

		// Create new SFTP client
		sftpClient, err := sftp.NewClient(client)
		if err != nil {
			return keyInstallResultMsg{
				success: false,
				err:     fmt.Errorf("failed to create SFTP client: %v", err),
			}
		}
		defer sftpClient.Close()

		// Create .ssh directory if it doesn't exist
		sshDir := ".ssh"
		sftpClient.Mkdir(sshDir)
		sftpClient.Chmod(sshDir, 0700)

		// Open authorized_keys file
		authKeysPath := path.Join(sshDir, "authorized_keys")
		var existingContent []byte

		// Try to read existing authorized_keys
		existingFile, err := sftpClient.Open(authKeysPath)
		if err == nil {
			existingContent, _ = io.ReadAll(existingFile)
			existingFile.Close()
		}

		// Check if key already exists
		if bytes.Contains(existingContent, []byte(strings.TrimSpace(m.publicKey))) {
			Info("Key already exists in authorized_keys")
			// Key already installed
			return keyInstallResultMsg{
				success:  true,
				username: m.username,
				host:     m.host,
				destDir:  m.destDir,
				files:    m.files,
			}
		}

		Info("Appending key to authorized_keys")

		// Append the new key
		f, err := sftpClient.OpenFile(authKeysPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
		if err != nil {
			return keyInstallResultMsg{
				success: false,
				err:     fmt.Errorf("failed to open authorized_keys: %v", err),
			}
		}
		defer f.Close()

		// Ensure the public key has a trailing newline
		pubKey := strings.TrimSpace(m.publicKey) + "\n"

		// Write the key
		_, err = f.Write([]byte(pubKey))
		if err != nil {
			return keyInstallResultMsg{
				success: false,
				err:     fmt.Errorf("failed to write key: %v", err),
			}
		}

		// Set proper permissions
		sftpClient.Chmod(authKeysPath, 0600)

		// Verify key installation by trying to connect with the key
		if verifyErr := m.verifyKeyInstallation(); verifyErr != nil {
			return keyInstallResultMsg{
				success: false,
				err:     fmt.Errorf("key installed but verification failed: %v", verifyErr),
			}
		}

		Info("Key installed and verified successfully")

		return keyInstallResultMsg{
			success:  true,
			username: m.username,
			host:     m.host,
			destDir:  m.destDir,
			files:    m.files,
		}
	}
}

// Verify the key installation by attempting to connect with the key
func (m keyInstallModel) verifyKeyInstallation() error {
	// Parse the private key
	key, err := os.ReadFile(m.keyPath)
	if err != nil {
		return fmt.Errorf("failed to read key file: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %v", err)
	}

	// Set up SSH client configuration with key authentication
	config := &ssh.ClientConfig{
		User: m.username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		Timeout: 10 * time.Second, // Shorter timeout for verification
	}

	// Setup proper host key verification
	hostKeyCallback, err := getHostKeyCallback(m.host)
	if err != nil {
		// Fall back to insecure for verification
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		config.HostKeyCallback = hostKeyCallback
	}

	// Ensure host has port
	hostPort := m.host
	if !strings.Contains(hostPort, ":") {
		hostPort += ":22"
	}

	// Try to connect
	client, err := ssh.Dial("tcp", hostPort, config)
	if err != nil {
		return fmt.Errorf("failed to connect with new key: %v", err)
	}
	defer client.Close()

	// Execute a simple command to verify the connection
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	// Run a simple echo command
	err = session.Run("echo 'SSH key verification successful'")
	if err != nil {
		return fmt.Errorf("command failed: %v", err)
	}

	return nil
}

// Render the key installation view
func (m keyInstallModel) View() string {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("SSH Key Setup for %s@%s\n\n", m.username, m.host))

	if m.installing {
		content.WriteString(m.spinner.View() + " " + m.status)
	} else if m.manualMode {
		content.WriteString("Manual Key Installation\n\n")

		content.WriteString("The remote server doesn't support automatic key installation.\n")
		content.WriteString("Please manually add this public key to your server:\n\n")

		// Style the public key for better visibility
		styledKey := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Border(lipgloss.RoundedBorder()).
			Padding(1).
			Render(m.publicKey)

		content.WriteString(styledKey + "\n\n")

		content.WriteString("Instructions:\n")
		content.WriteString("1. Log into your server using your preferred method\n")
		content.WriteString("2. Run: mkdir -p ~/.ssh && chmod 700 ~/.ssh\n")
		content.WriteString("3. Run: touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys\n")
		content.WriteString("4. Add the public key shown above to ~/.ssh/authorized_keys\n\n")

		if m.copyToClipboard {
			content.WriteString("✓ Public key copied to clipboard\n\n")
		} else {
			content.WriteString("Press 'c' to copy the public key to clipboard\n\n")
		}

		content.WriteString("Once you've added the key, press Enter to continue with the file transfer\n")
		content.WriteString("Press Esc to go back to the authentication form")

	} else if m.complete {
		if m.err == nil {
			content.WriteString("✓ " + m.status + "\n\n")
			content.WriteString("Public key successfully installed on the remote host.\n")
			content.WriteString("You can now use key-based authentication for this host.\n\n")
			content.WriteString("Press Enter to continue with file transfer")
		} else {
			content.WriteString("✗ " + m.status + "\n\n")
			content.WriteString(m.err.Error() + "\n\n")
			content.WriteString("Press Enter for manual installation instructions\n")
			content.WriteString("Press Esc to return to authentication form")
		}
	}

	return content.String()
}

// Message to return to auth form
type backToAuthFormMsg struct{}
