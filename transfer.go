package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Global channel for sending messages from transfer goroutines to the program
var transferChannel chan tea.Msg

// Command to wait for the next update from the transfer
func waitForTransferUpdate() tea.Msg {
	return <-transferChannel
}

// Transfer complete message
type transferCompleteMsg struct{}

// Transfer started message
type transferStartedMsg struct{}

// Transfer progress message
type transferProgressMsg struct {
	filename string
	progress float64
	status   string
}

// Transfer error message
type transferErrorMsg struct {
	err error
}

// File transfer model with authentication details
type transferModel struct {
	host          string
	username      string
	password      string
	keyPath       string
	destDir       string
	files         []string
	status        string
	currentFile   string
	progress      progress.Model
	spinner       spinner.Model
	transferring  bool
	complete      bool
	err           error
	totalFiles    int
	filesComplete int
	ctx           context.Context
	cancelFunc    context.CancelFunc
}

// New constructor for key-based authentication
func newTransferModelWithKey(keyPath, username, password, host, destDir string, files []string) transferModel {
	// Initialize progress bar
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	return transferModel{
		host:         host,
		username:     username,
		password:     password,
		keyPath:      keyPath,
		destDir:      destDir,
		files:        files,
		progress:     p,
		spinner:      s,
		transferring: true,
		totalFiles:   len(files),
		status:       "Preparing to transfer files...",
		ctx:          ctx,
		cancelFunc:   cancel,
	}
}

// Initialize the transfer
func (m transferModel) Init() tea.Cmd {
	// Initialize transfer channel
	transferChannel = make(chan tea.Msg)

	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			// Start transfer in a goroutine with context
			go func() {
				m.transferFilesAsync()

				// Ensure message is sent even if function doesn't complete normally
				select {
				case transferChannel <- transferCompleteMsg{}:
					// Message sent
				case <-m.ctx.Done():
					// Context cancelled
				}
			}()

			// Return a message that indicates transfer has started
			return transferStartedMsg{}
		},
	)
}

// Update the transfer model
func (m transferModel) Update(msg tea.Msg) (transferModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" && m.transferring {
			// Cancel the transfer if Escape is pressed during transfer
			m.cancelFunc()
			m.transferring = false
			m.status = "Transfer cancelled"
			return m, nil
		}

		if m.complete || m.err != nil {
			if msg.String() == "enter" {
				return m, func() tea.Msg {
					return backToMenuMsg{}
				}
			}
		}

	case transferStartedMsg:
		// Continue listening for transfer messages
		return m, waitForTransferUpdate

	case transferProgressMsg:
		// Update progress
		m.currentFile = msg.filename
		m.status = msg.status
		cmd := m.progress.SetPercent(msg.progress)
		return m, tea.Batch(cmd, waitForTransferUpdate)

	case transferCompleteMsg:
		// Transfer complete
		m.transferring = false
		m.complete = true
		m.status = "Transfer complete!"

		// Reset progress
		cmd := m.progress.SetPercent(1.0)
		return m, cmd

	case transferErrorMsg:
		// Transfer error
		m.transferring = false
		m.err = msg.err
		m.status = fmt.Sprintf("Error: %v", msg.err)
		Error("Transfer error: %v", msg.err)
		return m, nil

	case progress.FrameMsg:
		// Update progress bar with proper type assertion
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case spinner.TickMsg:
		// Update spinner
		spinnerModel, cmd := m.spinner.Update(msg)
		m.spinner = spinnerModel
		return m, cmd
	}

	return m, nil
}

// Transfer files using SFTP
func (m transferModel) transferFilesAsync() {
	// Choose authentication method
	var auth []ssh.AuthMethod

	if m.keyPath != "" && m.keyPath != "password" {
		// Key-based authentication
		key, err := os.ReadFile(m.keyPath)
		if err != nil {
			transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to read key file: %v", err)}
			return
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to parse key: %v", err)}
			return
		}

		auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
		Info("Using key authentication with %s", m.keyPath)
	} else {
		// Password authentication
		auth = []ssh.AuthMethod{ssh.Password(m.password)}
		Info("Using password authentication")
	}

	// Use proper host key verification
	hostKeyCallback, err := getHostKeyCallback(m.host)
	if err != nil {
		// Fall back to insecure for development, but log warning
		Warn("Using insecure host key verification: %v", err)
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	// Set up SSH client configuration
	config := &ssh.ClientConfig{
		User:            m.username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	// Try to connect
	transferChannel <- transferProgressMsg{
		filename: "",
		progress: 0,
		status:   fmt.Sprintf("Connecting to %s as %s...", m.host, m.username),
	}

	// Ensure host has port
	hostPort := m.host
	if !strings.Contains(hostPort, ":") {
		hostPort += ":22"
	}

	client, err := ssh.Dial("tcp", hostPort, config)
	if err != nil {
		transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to connect: %v", err)}
		return
	}
	defer client.Close()

	// Create a new SFTP client
	transferChannel <- transferProgressMsg{
		filename: "",
		progress: 0,
		status:   "Connected. Initializing SFTP session...",
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to create SFTP client: %v", err)}
		return
	}
	defer sftpClient.Close()

	// Create the destination directory if it doesn't exist
	err = sftpClient.MkdirAll(m.destDir)
	if err != nil {
		transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to create destination directory: %v", err)}
		return
	}

	// Transfer each file
	for i, filePath := range m.files {
		select {
		case <-m.ctx.Done():
			// Transfer cancelled
			transferChannel <- transferErrorMsg{err: fmt.Errorf("transfer cancelled")}
			return
		default:
			// Get file info
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to stat file %s: %v", filePath, err)}
				continue
			}

			if fileInfo.IsDir() {
				// Skip directories for now
				continue
			}

			fileName := fileInfo.Name()
			destPath := filepath.Join(m.destDir, fileName)

			// Send file transfer start message
			transferChannel <- transferProgressMsg{
				filename: fileName,
				progress: 0,
				status:   fmt.Sprintf("Transferring %s to %s", fileName, destPath),
			}

			// Open source file
			srcFile, err := os.Open(filePath)
			if err != nil {
				transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to open source file %s: %v", filePath, err)}
				continue
			}

			// Create destination file
			dstFile, err := sftpClient.Create(destPath)
			if err != nil {
				srcFile.Close()
				transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to create destination file %s: %v", destPath, err)}
				continue
			}

			// Set up progress reader
			fileSize := fileInfo.Size()
			progressReader := &ProgressReader{
				Reader:     srcFile,
				Size:       fileSize,
				fileName:   fileName,
				lastUpdate: time.Now(),
				Progress: func(bytesRead int64) {
					progress := float64(bytesRead) / float64(fileSize)
					// Only send progress updates every 1% to avoid flooding
					if bytesRead == fileSize || int(progress*100)%1 == 0 {
						transferChannel <- transferProgressMsg{
							filename: fileName,
							progress: progress,
							status:   fmt.Sprintf("Transferring %s: %.1f%%", fileName, progress*100),
						}
					}
				},
			}

			// Copy the file with progress updates
			_, err = io.Copy(dstFile, progressReader)
			srcFile.Close()
			dstFile.Close()

			if err != nil {
				transferChannel <- transferErrorMsg{err: fmt.Errorf("failed to transfer file %s: %v", fileName, err)}
				continue
			}

			// Update progress for completed file
			m.filesComplete = i + 1
			transferChannel <- transferProgressMsg{
				filename: fileName,
				progress: 1.0,
				status:   fmt.Sprintf("Successfully transferred %s", fileName),
			}

			Info("File transferred: %s (%d bytes)", fileName, fileSize)
		}
	}

	// All files transferred successfully
	Info("All files transferred successfully")
	transferChannel <- transferCompleteMsg{}
}

// Progress reader for tracking file transfer
type ProgressReader struct {
	Reader     io.Reader
	Size       int64
	BytesRead  int64
	fileName   string
	Progress   func(int64)
	lastUpdate time.Time
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.BytesRead += int64(n)

		// Limit progress updates to avoid UI freezing
		now := time.Now()
		if now.Sub(pr.lastUpdate) >= 100*time.Millisecond || pr.BytesRead == pr.Size {
			pr.Progress(pr.BytesRead)
			pr.lastUpdate = now

			// Log transfer progress periodically
			if pr.BytesRead == pr.Size || pr.BytesRead%(pr.Size/10) == 0 {
				percentage := float64(pr.BytesRead) * 100 / float64(pr.Size)
				Debug("Transferring %s: %.1f%% (%d/%d bytes)", pr.fileName, percentage, pr.BytesRead, pr.Size)
			}
		}
	}
	return n, err
}

// Render the transfer view
func (m transferModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Transfer Error:\n\n%v\n\nPress Enter to return to the menu", m.err)
	}

	var content strings.Builder

	content.WriteString(fmt.Sprintf("Transferring files to %s as %s\n", m.host, m.username))
	content.WriteString(fmt.Sprintf("Destination: %s\n", m.destDir))

	// Show authentication method
	if m.keyPath != "" && m.keyPath != "password" {
		content.WriteString(fmt.Sprintf("Authentication: Using SSH key (%s)\n\n", filepath.Base(m.keyPath)))
	} else {
		content.WriteString("Authentication: Using password\n\n")
	}

	if m.transferring {
		content.WriteString(m.spinner.View() + " " + m.status + "\n\n")

		if m.currentFile != "" {
			content.WriteString(fmt.Sprintf("Current file: %s\n", m.currentFile))
			content.WriteString(m.progress.View() + "\n")
		}

		content.WriteString(fmt.Sprintf("\nProgress: %d/%d files complete", m.filesComplete, m.totalFiles))
		content.WriteString("\n\nPress Esc to cancel transfer")
	} else if m.complete {
		content.WriteString("✓ " + m.status + "\n\n")
		content.WriteString(fmt.Sprintf("Successfully transferred %d files to %s\n\n", m.totalFiles, m.host))
		content.WriteString("Press Enter to return to the main menu")
	}

	return content.String()
}
