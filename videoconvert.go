package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Video conversion has its own channel to avoid conflicts with network scanning
var videoConversionChannel chan tea.Msg

// Message sent when conversion is complete
type conversionCompleteMsg struct{}

// Message sent when conversion fails
type conversionErrorMsg struct {
	err error
}

// Message sent when conversion progress updates
type conversionProgressMsg struct {
	progress float64
	detail   string
}

// Message to return to main menu from video conversion
type backToMenuFromConvertMsg struct{}

// Video conversion model
type videoConvertModel struct {
	fileSelector     fileSelectModel
	fileSelector     fileSelectModel
	formatList       list.Model
	qualityInput     textinput.Model
	additionalArgs   textinput.Model
	focusedField     int
	converting       bool
	complete         bool
	currentScreen    int // 0 = file select, 1 = options, 2 = conversion
	selectedFile     string
	destinationFile  string
	progress         progress.Model
	spinner          spinner.Model
	err              error
	status           string
	supportedFormats []string
	windowSize       tea.WindowSizeMsg
	ctx              context.Context
	cancelFunc       context.CancelFunc
	duration         float64
	detailText       string
}

// Create a new video conversion model
func newVideoConvertModel() videoConvertModel {
	// Initialize file selector
	fileSelector := newFileSelectModel()

	// Initialize file selector
	fileSelector := newFileSelectModel()

	// Supported formats for conversion
	supportedFormats := []string{"mp4", "mov", "avi", "mkv", "webm", "flv", "wmv", "gif", "mp3", "aac", "wav", "flac", "ogg"}

	// Create items for the format list
	formatListItems := make([]list.Item, len(supportedFormats))
	for i, format := range supportedFormats {
		formatListItems[i] = formatListItem{title: format}
	}

	// Initialize format list
	formatList := list.New(formatListItems, list.NewDefaultDelegate())
	formatList.Title = "Select Output Format"
	formatList.SetShowHelp(false)
	formatList.SetShowStatusBar(false)
	formatList.SetFilteringEnabled(false)
	formatList.SetHeight(5) // Initial height, will be adjusted by WindowSizeMsg
	formatList.SetWidth(20) // Initial width

	// Initialize quality input
	qualityInput := textinput.New()
	qualityInput.Placeholder = "medium (low/medium/high)"
	qualityInput.CharLimit = 10
	qualityInput.Width = 20
	qualityInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	qualityInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	qualityInput.Blur()

	// Initialize additional arguments input
	additionalArgs := textinput.New()
	additionalArgs.Placeholder = "optional ffmpeg arguments"
	additionalArgs.CharLimit = 100
	additionalArgs.Width = 40
	additionalArgs.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	additionalArgs.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	additionalArgs.Blur()

	// Initialize progress bar
	prog := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Set default values
	qualityInput.SetValue("medium")

	return videoConvertModel{
		fileSelector:     fileSelector,
		formatList:       formatList,
		qualityInput:     qualityInput,
		additionalArgs:   additionalArgs,
		focusedField:     focusFormatList,
		progress:         prog,
		spinner:          s,
		currentScreen:    0,
		supportedFormats: supportedFormats, // Use the locally defined slice
		status:           "Select a video file to convert",
		ctx:              ctx,
		cancelFunc:       cancel,
	}
}

// Initialize the video conversion screen
func (m videoConvertModel) Init() tea.Cmd {
	// Initialize a separate channel for video conversion
	videoConversionChannel = make(chan tea.Msg)

	// We need to initialize the file selector properly
	return m.fileSelector.Init()
}

// Wait for conversion updates
func waitForVideoConversionUpdate() tea.Msg {
	return <-videoConversionChannel
}

// Update the video conversion model
func (m videoConvertModel) Update(msg tea.Msg) (videoConvertModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowSize = msg
		if m.currentScreen == 0 {
			h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
			m.fileSelector.list.SetSize(msg.Width-h, msg.Height-v)
		} else if m.currentScreen == 1 { // Options screen
			// Adjust list height, leave some space for other inputs and prompts
			listHeight := msg.Height / 3
			if listHeight < 3 { // Ensure minimum height
				listHeight = 3
			}
			m.formatList.SetHeight(listHeight)
			m.formatList.SetWidth(msg.Width / 2) // Adjust width as well
		}

	case tea.KeyMsg:
		// Global key handling
		switch msg.String() {
		case "ctrl+c":
			// Cancel any ongoing conversion
			if m.converting {
				m.cancelFunc()
			}
			return m, tea.Quit

		case "esc":
			// Cancel any ongoing conversion
			if m.converting {
				m.cancelFunc()
				m.converting = false
				m.status = "Conversion cancelled"
				return m, nil
			}

			if m.currentScreen == 0 {
				// Return to main menu from file selection
				return m, func() tea.Msg {
					return backToMenuFromConvertMsg{}
				}
			} else if m.currentScreen == 1 {
				// Back to file selection from options
				m.currentScreen = 0
				return m, nil
			} else if m.currentScreen == 2 && (m.complete || m.err != nil) {
				// Go back to options when conversion is done
				m.currentScreen = 1
				return m, nil
			}
		}

	case selectedFilesMsg:
		if len(msg.files) == 1 {
			// User selected a file
			m.selectedFile = msg.files[0]
			m.currentScreen = 1
			return m, nil
		} else if len(msg.files) > 1 {
			// Multiple files not supported for now
			m.err = fmt.Errorf("please select only one file for conversion")
			return m, nil
		}

	case loadDirectoryResultMsg:
		// Forward this message to the file selector
		newFileSelector, cmd := m.fileSelector.Update(msg)
		m.fileSelector = newFileSelector
		return m, cmd

	case conversionCompleteMsg:
		m.complete = true
		m.converting = false
		m.status = "Conversion complete!"
		cmd := m.progress.SetPercent(1.0)
		return m, cmd

	case conversionErrorMsg:
		m.err = msg.err
		m.converting = false
		m.status = fmt.Sprintf("Error: %v", msg.err)
		Error("Video conversion error: %v", msg.err)
		return m, nil

	case conversionProgressMsg:
		m.detailText = msg.detail
		cmd := m.progress.SetPercent(msg.progress)
		return m, cmd

	case spinner.TickMsg:
		if m.converting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	// Handle updates for file selector
	if m.currentScreen == 0 {
		newFileSelector, cmd := m.fileSelector.Update(msg)
		m.fileSelector = newFileSelector
		return m, cmd
	}

	// Handle updates for options screen
	if m.currentScreen == 1 {
		var cmd tea.Cmd
		keyMsg, isKeyMsg := msg.(tea.KeyMsg)

		if isKeyMsg {
			switch keyMsg.String() {
			case "tab":
				m.focusedField = (m.focusedField + 1) % 3
				if m.focusedField == focusFormatList {
					m.qualityInput.Blur()
					m.additionalArgs.Blur()
					// List doesn't have Focus(), it's implicitly focused
				} else if m.focusedField == focusQualityInput {
					m.qualityInput.Focus()
					m.additionalArgs.Blur()
				} else { // focusAdditionalArgs
					m.qualityInput.Blur()
					m.additionalArgs.Focus()
				}
				return m, nil // No further command from tab
			case "shift+tab":
				m.focusedField = (m.focusedField - 1 + 3) % 3
				if m.focusedField == focusFormatList {
					m.qualityInput.Blur()
					m.additionalArgs.Blur()
				} else if m.focusedField == focusQualityInput {
					m.qualityInput.Focus()
					m.additionalArgs.Blur()
				} else { // focusAdditionalArgs
					m.qualityInput.Blur()
					m.additionalArgs.Focus()
				}
				return m, nil // No further command from shift+tab

			case "enter":
				switch m.focusedField {
				case focusFormatList:
					m.focusedField = focusQualityInput
					m.qualityInput.Focus()
					m.additionalArgs.Blur()
					// List remains visually "active" as it was the last to process non-navigation keys
					return m, nil
				case focusQualityInput:
					m.focusedField = focusAdditionalArgs
					m.qualityInput.Blur()
					m.additionalArgs.Focus()
					return m, nil
				case focusAdditionalArgs:
					// Start conversion only if Enter is pressed on the last field
					if m.selectedFile != "" {
						selectedFormatItem, ok := m.formatList.SelectedItem().(formatListItem)
						if !ok {
							m.err = fmt.Errorf("no format selected or invalid item type")
							return m, nil
						}
						format := selectedFormatItem.Title()

						// Basic validation (already from supported list, but good check)
						isValidFormat := false
						for _, sf := range m.supportedFormats {
							if sf == format {
								isValidFormat = true
								break
							}
						}
						if !isValidFormat {
							m.err = fmt.Errorf("invalid format selected: %s", format)
							return m, nil
						}

						m.currentScreen = 2
						m.converting = true
						m.destinationFile = getDestinationFilename(m.selectedFile, format)
						m.ctx, m.cancelFunc = context.WithCancel(context.Background())
						return m, tea.Batch(m.convertVideo(), m.spinner.Tick)
					}
					return m, nil // Should not happen if selectedFile is empty, but good to return
				}
			}
		}

		// Route message to the focused component
		var cmds []tea.Cmd
		if m.focusedField == focusFormatList {
			m.formatList, cmd = m.formatList.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.focusedField == focusQualityInput {
			m.qualityInput, cmd = m.qualityInput.Update(msg)
			cmds = append(cmds, cmd)
		} else { // focusAdditionalArgs
			m.additionalArgs, cmd = m.additionalArgs.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Handle conversion screen keyboard events
	if m.currentScreen == 2 && (m.complete || m.err != nil) {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			// Return to main menu after conversion is done
			return m, func() tea.Msg {
				return backToMenuFromConvertMsg{}
			}
		}
	}

	return m, nil
}

// Get video duration using ffprobe
func getVideoDuration(filePath string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", filePath)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get video duration: %v", err)
	}

	// Parse duration
	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %v", err)
	}

	return duration, nil
}

// Monitor FFmpeg progress
func (m videoConvertModel) monitorProgress(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)

	// Pattern to extract time information
	timeRegex := regexp.MustCompile(`time=(\d+):(\d+):(\d+\.\d+)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Extract current time
		matches := timeRegex.FindStringSubmatch(line)
		if len(matches) >= 4 {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.ParseFloat(matches[3], 64)

			currentTime := float64(hours*3600) + float64(minutes*60) + seconds
			progress := currentTime / m.duration

			// Ensure progress is capped at 1.0
			if progress > 1.0 {
				progress = 1.0
			}

			// Limit updates to avoid overwhelming the UI
			// It's acceptable to drop some intermediate progress messages if the channel is full,
			// as critical completion/error messages are sent blockingly elsewhere.
			select {
			case videoConversionChannel <- conversionProgressMsg{
				progress: progress, // This progress is now capped
				detail:   fmt.Sprintf("Time: %s", matches[0]),
			}:
			default:
				// Skip this update if the channel is blocked
			}
		}
	}
}

// Helper function to generate destination filename
func getDestinationFilename(selectedFile, format string) string {
	ext := filepath.Ext(selectedFile)
	baseName := strings.TrimSuffix(filepath.Base(selectedFile), ext)
	outputDir := filepath.Dir(selectedFile)
	return filepath.Join(outputDir, fmt.Sprintf("%s_converted.%s", baseName, format))
}

// Convert the selected video file
func (m videoConvertModel) convertVideo() tea.Cmd {
	return func() tea.Msg {
		// Get conversion settings
		selectedFormatItem, ok := m.formatList.SelectedItem().(formatListItem)
		if !ok {
			return conversionErrorMsg{err: fmt.Errorf("could not determine selected format")}
		}
		format := selectedFormatItem.Title()
		quality := m.qualityInput.Value()
		additionalArgs := m.additionalArgs.Value()

		// Map quality setting to ffmpeg parameters
		var crf string
		switch strings.ToLower(quality) {
		case "low":
			crf = "28"
		case "medium":
			crf = "23"
		case "high":
			crf = "18"
		default:
			crf = "23" // Default to medium
		}

		// Validate format
		if format == "" {
			return conversionErrorMsg{err: fmt.Errorf("output format cannot be empty")}
		}

		// Get video duration
		duration, err := getVideoDuration(m.selectedFile)
		if err != nil {
			return conversionErrorMsg{err: err}
		}
		m.duration = duration

		// Build ffmpeg command
		args := []string{
			"-i", m.selectedFile,
			"-c:v", "libx264",
			"-crf", crf,
			"-preset", "medium",
			"-c:a", "aac",
			"-b:a", "128k",
		}

		// Add any additional arguments
		if additionalArgs != "" {
			additionalArgsSlice := strings.Fields(additionalArgs)
			args = append(args, additionalArgsSlice...)
		}

		// Add output file as the last argument
		args = append(args, m.destinationFile)

		Info("Starting video conversion: %s -> %s", m.selectedFile, m.destinationFile)
		Debug("FFmpeg command: ffmpeg %s", strings.Join(args, " "))

		// Create command with context for cancellation
		cmd := exec.CommandContext(m.ctx, "ffmpeg", args...)

		// Set up stderr pipe for progress monitoring
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			return conversionErrorMsg{err: fmt.Errorf("failed to create stderr pipe: %v", err)}
		}

		// Start command
		err = cmd.Start()
		if err != nil {
			return conversionErrorMsg{err: fmt.Errorf("failed to start ffmpeg: %v", err)}
		}

		// Monitor progress in a separate goroutine
		go m.monitorProgress(stderrPipe)

		// Wait for command to finish
		err = cmd.Wait()
		if err != nil {
			// Check if context was cancelled
			if m.ctx.Err() == context.Canceled {
				return conversionErrorMsg{err: fmt.Errorf("conversion cancelled")}
			}
			return conversionErrorMsg{err: fmt.Errorf("conversion failed: %v", err)}
		}

		// Check if output file exists
		if _, err := os.Stat(m.destinationFile); os.IsNotExist(err) {
			return conversionErrorMsg{err: fmt.Errorf("conversion failed: output file not created")}
		}

		Info("Video conversion completed successfully: %s", m.destinationFile)

		// Conversion completed successfully
		return conversionCompleteMsg{}
	}
}

// Render the video conversion view
func (m videoConvertModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress Esc to go back, Enter to return to main menu", m.err)
	}

	var content strings.Builder

	// Show different content based on current screen
	switch m.currentScreen {
	case 0: // File selection
		return m.fileSelector.View()

	case 1: // Conversion options
		content.WriteString(fmt.Sprintf("Selected file: %s\n\n", filepath.Base(m.selectedFile)))
		content.WriteString("Conversion Options\n\n")

		// Output format list
		content.WriteString("Output Format:\n") // Prompt for the list
		content.WriteString(m.formatList.View())
		content.WriteString("\n\n")

		// Quality field
		qualityPrompt := "Quality (low/medium/high): "
		if m.focusedField == focusQualityInput {
			qualityPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(qualityPrompt)
		}
		content.WriteString(qualityPrompt)
		content.WriteString(m.qualityInput.View())
		content.WriteString("\n\n")

		// Additional arguments field
		argsPrompt := "Additional FFmpeg Arguments (optional): "
		if m.focusedField == focusAdditionalArgs {
			argsPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(argsPrompt)
		}
		content.WriteString(argsPrompt)
		content.WriteString(m.additionalArgs.View())
		content.WriteString("\n\n")

		// Help text
		content.WriteString("Tab/Shift+Tab: cycle focus | Enter: next field / start conversion | Esc: back")

	case 2: // Conversion progress
		content.WriteString("Video Conversion\n\n")

		selectedFormatItem, ok := m.formatList.SelectedItem().(formatListItem)
		format := "selected_format" // Default/fallback
		if ok {
			format = selectedFormatItem.Title()
		}

		if m.converting {
			content.WriteString(fmt.Sprintf("%s Converting %s to %s format...\n\n",
				m.spinner.View(),
				filepath.Base(m.selectedFile),
				format)) // Use selected format
			content.WriteString(m.progress.View() + "\n")

			if m.detailText != "" {
				content.WriteString("\n" + m.detailText)
			}

			content.WriteString("\n\nPress Esc to cancel conversion")
		} else if m.complete {
			content.WriteString(fmt.Sprintf("✓ %s\n\n", m.status))
			content.WriteString(fmt.Sprintf("Original: %s\n", filepath.Base(m.selectedFile)))
			content.WriteString(fmt.Sprintf("Converted: %s\n\n", filepath.Base(m.destinationFile)))
			content.WriteString("Press Enter to return to the main menu or Esc to go back to options")
		}
	}

	return content.String()
}

// checkFFmpegInstalled checks if ffmpeg is installed and in PATH.
func checkFFmpegInstalled() (bool, error) {
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		// If LookPath returns an error, ffmpeg is not found or not executable
		return false, err
	}
	return true, nil
}

// formatListItem implements list.Item for the format selection list.
type formatListItem struct {
	title string
}

func (i formatListItem) Title() string       { return i.title }
func (i formatListItem) Description() string { return "" } // Not needed for this list
func (i formatListItem) FilterValue() string { return i.title }

// Constants for focused field in video conversion options
const (
	focusFormatList = iota
	focusQualityInput
	focusAdditionalArgs
)
