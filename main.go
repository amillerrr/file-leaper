package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen identifiers
type screen int

const (
	menuScreen screen = iota
	fileSelectScreen
	networkScanScreen
	keySelectionScreen
	keyGenScreen
	keyInstallScreen
	authFormScreen
	transferScreen
	videoConvertScreen
	confirmDialogScreen
)

// Version information
const (
	AppName    = "File Transfer Utility"
	AppVersion = "0.1.0"
)

// Main application model
type model struct {
	currentScreen  screen
	previousScreen screen
	mainMenu       list.Model
	fileSelector   fileSelectModel
	networkScanner networkScanModel
	authForm       authFormModel
	keySelection   keySelectionModel
	keyGen         keyGenModel
	keyInstall     keyInstallModel
	transferView   transferModel
	videoConverter videoConvertModel
	confirmDialog  confirmDialogModel
	windowSize     tea.WindowSizeMsg
	selectedFiles  []string
	targetHost     string
	config         *AppConfig
	// Stored data when host key verification is in progress
	pendingHostKeyData hostKeyVerificationMsg
}

// Custom error message type
type errMsg struct {
	err error
}

func (e errMsg) Error() string { return e.err.Error() }

// Menu item definition
type item struct {
	title, desc string
}

func (i item) Title() string { return i.title }

func (i item) Description() string { return i.desc }

func (i item) FilterValue() string { return i.title }

// Initialize the main application model
func initialModel() model {
	config := GetConfig()

	// Set up main menu items
	items := []list.Item{
		item{title: "Select Files", desc: "Choose files to transfer"},
		item{title: "Scan Network", desc: "Find hosts on local network"},
		item{title: "Convert Video", desc: "Utility to convert video file types"},
		item{title: "Exit", desc: "Exit the application"},
	}

	// Create and configure main menu
	mainMenu := list.New(items, list.NewDefaultDelegate())
	mainMenu.Title = AppName + " v" + AppVersion
	mainMenu.SetShowStatusBar(false)
	mainMenu.SetFilteringEnabled(false)
	mainMenu.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1)

	// Create initial model
	return model{
		currentScreen: menuScreen,
		mainMenu:      mainMenu,
		config:        config,
		// Other screens will be initialized when they're accessed
	}
}

// Initialize the application
func (m model) Init() tea.Cmd {
	return nil
}

// Handle updates based on messages
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Handle window resizing
		m.windowSize = msg

		// Update list sizes
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.mainMenu.SetSize(msg.Width-h, msg.Height-v)

		if m.currentScreen == fileSelectScreen {
			m.fileSelector.list.SetSize(msg.Width-h, msg.Height-v)
		} else if m.currentScreen == networkScanScreen {
			m.networkScanner.list.SetSize(msg.Width-h, msg.Height-v)
		} else if m.currentScreen == videoConvertScreen {
			m.videoConverter.fileSelector.list.SetSize(msg.Width-h, msg.Height-v)
		} else if m.currentScreen == keySelectionScreen {
			m.keySelection.list.SetSize(msg.Width-h, msg.Height-v)
		}

	case tea.KeyMsg:
		// Global key handlers
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.currentScreen == menuScreen {
				return m, tea.Quit
			}
		case "esc":
			if m.currentScreen != menuScreen && m.currentScreen != confirmDialogScreen {
				// Store previous screen for potential return
				m.previousScreen = m.currentScreen

				// Clean up resources when leaving screens
				if m.currentScreen == transferScreen && m.transferView.transferring {
					// Cancel any ongoing transfers
					m.transferView.cancelFunc()
				}

				// Return to main menu
				m.currentScreen = menuScreen
				return m, nil
			}
		}

	// Custom messages
	case backToMenuMsg:
		m.currentScreen = menuScreen
		return m, nil

	case selectedFilesMsg:
		fmt.Printf("Received selectedFilesMsg with %d files\n", len(msg.files))
		m.selectedFiles = msg.files
		prevScreen := m.currentScreen
		m.currentScreen = menuScreen
		fmt.Printf("Transitioning to %v (value: %d)\n", menuScreen, prevScreen)
		return m, func() tea.Msg {
			return nil
		}

	case selectedHostMsg:
		m.targetHost = msg.host
		Info("Selected host: %s", msg.host)
		// Initialize auth form
		m.authForm = newAuthFormModel(m.targetHost, m.selectedFiles, m.config)
		m.currentScreen = authFormScreen
		return m, m.authForm.Init()

	case authCompleteMsg:
		Info("Authentication complete for %s@%s", msg.username, msg.host)
		// Initialize key selection screen
		m.keySelection = newKeySelectionModel(
			msg.username,
			msg.host,
			msg.destDir,
			msg.files,
		)

		m.keySelection.list.SetSize(
			m.windowSize.Width-4,
			m.windowSize.Height-4)
		m.currentScreen = keySelectionScreen
		return m, m.keySelection.Init()

	case generateKeyMsg:
		Info("Generating key for %s@%s", msg.username, msg.host)
		// Initialize key generation screen
		m.keyGen = newKeyGenModel(
			msg.username,
			msg.host,
			msg.destDir,
			msg.files,
			m.config.DefaultKeyType,
			m.config.DefaultKeyBits,
		)
		m.currentScreen = keyGenScreen
		return m, m.keyGen.Init()

	case keySelectedMsg:
		Info("Key selected for %s@%s: %s", msg.username, msg.host, msg.keyPath)
		// Initialize transfer model with key authentication
		m.transferView = newTransferModelWithKey(
			msg.keyPath,
			msg.username,
			m.authForm.inputs[1].Value(),
			msg.host,
			msg.destDir,
			msg.files,
		)

		// Force screen transition
		m.currentScreen = transferScreen

		// Initialize the transfer view
		return m, m.transferView.Init()

	case installKeyMsg:
		Info("Installing key for %s@%s", msg.username, msg.host)
		// Initialize key installation screen
		m.keyInstall = newKeyInstallModel(
			msg.keyPath,
			msg.publicKey,
			msg.username,
			m.authForm.inputs[1].Value(),
			msg.host,
			msg.destDir,
			msg.files,
		)
		m.currentScreen = keyInstallScreen
		return m, m.keyInstall.Init()

	case backToAuthFormMsg:
		// Return to auth form
		m.currentScreen = authFormScreen
		return m, nil

	case transferCompleteMsg:
		// Reset selected files and host after transfer
		m.selectedFiles = nil
		m.targetHost = ""
		m.currentScreen = menuScreen
		return m, nil

	case backToMenuFromConvertMsg:
		m.currentScreen = menuScreen
		return m, nil

	case showConfirmDialogMsg:
		// Create confirmation dialog
		m.previousScreen = m.currentScreen
		m.confirmDialog = newConfirmDialog(
			msg.title,
			msg.message,
			msg.yesMessage,
			msg.noMessage,
			msg.callback,
		)
		m.currentScreen = confirmDialogScreen
		return m, nil

	case confirmDialogResultMsg:
		// Handle confirmation dialog result
		if strContext, ok := msg.context.(string); ok && strContext == "hostKeyVerification" {
			if m.transferView.hostKeyDecisionChan != nil {
				m.transferView.hostKeyDecisionChan <- msg.confirmed
			}
			// Clear pending data after decision
			m.pendingHostKeyData = hostKeyVerificationMsg{}
			// Return to the transfer screen, which is waiting for the decision
			m.currentScreen = m.previousScreen
			// The transfer screen will then update based on the decision (retry or show error)
			return m, waitForTransferUpdate // Listen for next message from transfer goroutine
		}
		// Default handling for other dialogs
		m.currentScreen = m.previousScreen
		return m, msg.cmd

	case hostKeyVerificationMsg: // New message from transfer.go
		m.pendingHostKeyData = msg
		m.previousScreen = m.currentScreen // Should be transferScreen
		m.currentScreen = confirmDialogScreen

		fingerprint := ssh.FingerprintSHA256(msg.key)
		dialogMessage := fmt.Sprintf("The authenticity of host '%s' can't be established.\n%s key fingerprint is %s.\nAre you sure you want to continue connecting (this time only)?",
			msg.host, msg.key.Type(), fingerprint)

		return m, func() tea.Msg {
			return showConfirmDialogMsg{
				title:      "Host Key Verification",
				message:    dialogMessage,
				yesMessage: "Yes",
				noMessage:  "No",
				callback: func(confirmed bool) tea.Msg {
					// Pass context along with the result
					return confirmDialogResultMsg{confirmed: confirmed, context: "hostKeyVerification"}
				},
			}
		}

	case loadDirectoryResultMsg:
		// Forward directory loading messages to the appropriate screen
		if m.currentScreen == videoConvertScreen {
			newVideoConverter, cmd := m.videoConverter.Update(msg)
			m.videoConverter = newVideoConverter
			return m, cmd
		} else if m.currentScreen == fileSelectScreen {
			newFileSelector, cmd := m.fileSelector.Update(msg)
			m.fileSelector = newFileSelector
			return m, cmd
		}
	}

	// Screen-specific updates
	switch m.currentScreen {
	case menuScreen:
		var cmd tea.Cmd
		m.mainMenu, cmd = m.mainMenu.Update(msg)
		cmds = append(cmds, cmd)

		// Handle menu selection
		if msg, ok := msg.(tea.KeyMsg); ok && msg.Type == tea.KeyEnter {
			selectedItem, ok := m.mainMenu.SelectedItem().(item)
			if ok {
				switch selectedItem.title {
				case "Select Files":
					m.fileSelector = newFileSelectModel()
					m.fileSelector.list.SetSize(
						m.windowSize.Width-4,
						m.windowSize.Height-4)
					m.currentScreen = fileSelectScreen
					return m, m.fileSelector.Init()

				case "Convert Video":
					// Check if FFmpeg is installed before proceeding
					ffmpegInstalled, _ := checkFFmpegInstalled() // Error from checkFFmpegInstalled is ignored for now
					if !ffmpegInstalled {
						return m, func() tea.Msg {
							return showConfirmDialogMsg{
								title:      "FFmpeg Not Found",
								message:    "FFmpeg is required for video conversion but was not found in your system's PATH.\nPlease install FFmpeg and ensure it is in your PATH to use this feature.",
								yesMessage: "OK",
								noMessage:  "", // This makes it a single-button dialog
								callback: func(confirmed bool) tea.Msg {
									// No action needed on callback, user remains on menuScreen.
									return nil
								},
							}
						}
					}

					// FFmpeg is installed, proceed to initialize video conversion screen.
					// The videoConvertModel will start at its own file selection screen (currentScreen = 0).
					m.videoConverter = newVideoConvertModel()
					// The videoConverter's Init method will be called, and it will handle
					// its initial setup, including file selector sizing via WindowSizeMsg.
					m.currentScreen = videoConvertScreen
					return m, m.videoConverter.Init()

				case "Scan Network":
					m.networkScanner = newNetworkScanModel()
					m.networkScanner.list.SetSize(
						m.windowSize.Width-4,
						m.windowSize.Height-4)
					m.currentScreen = networkScanScreen
					return m, m.networkScanner.Init()

				case "Exit":
					return m, tea.Quit
				}
			}
		}

	case fileSelectScreen:
		newFileSelector, cmd := m.fileSelector.Update(msg)
		m.fileSelector = newFileSelector
		cmds = append(cmds, cmd)

	case confirmDialogScreen:
		newDialog, cmd := m.confirmDialog.Update(msg)
		m.confirmDialog = newDialog
		cmds = append(cmds, cmd)

	case videoConvertScreen:
		newVideoConverter, cmd := m.videoConverter.Update(msg)
		m.videoConverter = newVideoConverter
		cmds = append(cmds, cmd)

	case networkScanScreen:
		newNetworkScanner, cmd := m.networkScanner.Update(msg)
		m.networkScanner = newNetworkScanner
		cmds = append(cmds, cmd)

	case authFormScreen:
		newAuthForm, cmd := m.authForm.Update(msg)
		m.authForm = newAuthForm
		cmds = append(cmds, cmd)

	case keySelectionScreen:
		newKeySelection, cmd := m.keySelection.Update(msg)
		m.keySelection = newKeySelection
		cmds = append(cmds, cmd)

	case keyGenScreen:
		newKeyGen, cmd := m.keyGen.Update(msg)
		m.keyGen = newKeyGen
		cmds = append(cmds, cmd)

	case keyInstallScreen:
		newKeyInstall, cmd := m.keyInstall.Update(msg)
		m.keyInstall = newKeyInstall
		cmds = append(cmds, cmd)

	case transferScreen:
		newTransferView, cmd := m.transferView.Update(msg)
		m.transferView = newTransferView
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// Render the current screen
func (m model) View() string {
	// Header with app name
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#FF5F87")).
		Padding(0, 1).
		Render(AppName + " v" + AppVersion)

	// Status line showing selected files
	fileStatus := ""
	if len(m.selectedFiles) > 0 {
		fileStatus = fmt.Sprintf("Selected %d files", len(m.selectedFiles))
		if m.targetHost != "" {
			fileStatus += fmt.Sprintf(" for transfer to %s", m.targetHost)
		}
	}

	// Footer with key bindings
	footer := "\nPress q to quit, Esc to return to menu"

	// Content based on current screen
	var content string
	switch m.currentScreen {
	case menuScreen:
		content = m.mainMenu.View()
	case fileSelectScreen:
		content = m.fileSelector.View()
	case networkScanScreen:
		content = m.networkScanner.View()
	case authFormScreen:
		content = m.authForm.View()
	case keySelectionScreen:
		content = m.keySelection.View()
	case keyGenScreen:
		content = m.keyGen.View()
	case keyInstallScreen:
		content = m.keyInstall.View()
	case transferScreen:
		content = m.transferView.View()
	case videoConvertScreen:
		content = m.videoConverter.View()
	case confirmDialogScreen:
		content = m.confirmDialog.View()
	default:
		content = "Unknown screen"
	}

	// Combine all elements
	view := header + "\n\n" + content
	if fileStatus != "" && m.currentScreen != confirmDialogScreen {
		view += "\n\n" + fileStatus
	}
	if m.currentScreen != confirmDialogScreen {
		view += footer
	}

	return lipgloss.NewStyle().Margin(1, 2).Render(view)
}

// Program entry point
func main() {
	// Parse command line flags
	debugMode := flag.Bool("debug", false, "Enable debug logging")
	version := flag.Bool("version", false, "Display version information")
	flag.Parse()

	// Display version if requested
	if *version {
		fmt.Printf("%s v%s\n", AppName, AppVersion)
		os.Exit(0)
	}

	// Initialize logger
	logLevel := LevelInfo
	if *debugMode {
		logLevel = LevelDebug
	}

	err := InitLogger(logLevel, *debugMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
	}
	defer CloseLogger()

	// Log startup
	Info("Starting %s v%s", AppName, AppVersion)

	// Handle SIGTERM gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		Info("Received termination signal, shutting down")
		CloseLogger()
		os.Exit(0)
	}()

	// Start the application
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		Fatal("Error running program: %v", err)
	}

	Info("Application exiting normally")
}

func dumpGoroutineStacks() {
	buf := make([]byte, 1024*1024)
	buf = buf[:runtime.Stack(buf, true)]
	fmt.Println(string(buf))
}
