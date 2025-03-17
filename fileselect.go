package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message sent when files are selected
type selectedFilesMsg struct {
	files []string
}

// Message to return to menu
type backToMenuMsg struct{}

// Result of loading a directory
type loadDirectoryResultMsg struct {
	items      []list.Item
	currentDir string
}

// File selection model
type fileSelectModel struct {
	list          list.Model
	currentDir    string
	selectedFiles map[string]bool
	err           error
}

// File item for the list
type fileItem struct {
	path       string
	name       string
	isDir      bool
	size       int64
	isSelected bool
}

func (i fileItem) Title() string {
	if i.isDir {
		return i.name + "/"
	}
	return i.name
}

func (i fileItem) Description() string {
	if i.isDir {
		return "Directory"
	}

	// Format file size
	size := float64(i.size)
	units := []string{"B", "KB", "MB", "GB"}
	unitIndex := 0

	for size >= 1024 && unitIndex < len(units)-1 {
		size /= 1024
		unitIndex++
	}

	if i.isSelected {
		return fmt.Sprintf("%.1f %s [SELECTED]", size, units[unitIndex])
	}
	return fmt.Sprintf("%.1f %s", size, units[unitIndex])
}

func (i fileItem) FilterValue() string {
	return i.name
}

// Create a new file selection model
func newFileSelectModel() fileSelectModel {
	currentDir, err := os.Getwd()
	if err != nil {
		return fileSelectModel{err: err}
	}

	// Initialize delegate with custom styles
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))

	// Create empty list
	fileList := list.New([]list.Item{}, delegate)
	fileList.Title = "Select Files (space to select, enter to navigate, tab to confirm)"
	fileList.SetShowStatusBar(false)
	fileList.SetFilteringEnabled(true)
	fileList.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1)

	return fileSelectModel{
		list:          fileList,
		currentDir:    currentDir,
		selectedFiles: make(map[string]bool),
	}
}

// Initialize the file selection screen
func (m fileSelectModel) Init() tea.Cmd {
	return m.loadDirectory(m.currentDir)
}

// Update the file selection model
func (m fileSelectModel) Update(msg tea.Msg) (fileSelectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case loadDirectoryResultMsg:
		// Update model with the loaded directory
		m.list.SetItems(msg.items)
		m.currentDir = msg.currentDir
		return m, nil

	case errMsg:
		// Handle error
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			// Confirm selection and return to main menu
			if len(m.selectedFiles) > 0 {
				// Convert map to slice
				files := make([]string, 0, len(m.selectedFiles))
				for file := range m.selectedFiles {
					files = append(files, file)
				}

				// Add debug log
				// fmt.Printf("Selecting files: %v\n", files)

				// Add selected paths to config's recent paths
				config := GetConfig()
				for _, file := range files {
					config.AddRecentPath(file)
				}

				return m, func() tea.Msg {
					return selectedFilesMsg{files: files}
				}
			}

		case " ":
			// Toggle selection of current item
			selectedItem, ok := m.list.SelectedItem().(fileItem)
			if ok && !selectedItem.isDir {
				fullPath := filepath.Join(m.currentDir, selectedItem.name)

				// Toggle selection
				if _, exists := m.selectedFiles[fullPath]; exists {
					delete(m.selectedFiles, fullPath)
				} else {
					m.selectedFiles[fullPath] = true
				}

				// Reload directory to update UI
				return m, m.loadDirectory(m.currentDir)
			}

		case "enter":
			// Navigate into directory or select file
			selectedItem, ok := m.list.SelectedItem().(fileItem)
			if ok && selectedItem.isDir {
				newDir := filepath.Join(m.currentDir, selectedItem.name)
				return m, m.loadDirectory(newDir)
			}

		case "backspace":
			// Go up one directory
			parentDir := filepath.Dir(m.currentDir)
			if parentDir != m.currentDir {
				return m, m.loadDirectory(parentDir)
			}

		case "r":
			// Refresh current directory
			return m, m.loadDirectory(m.currentDir)

		case "h":
			// Go to home directory
			homeDir, err := os.UserHomeDir()
			if err == nil {
				return m, m.loadDirectory(homeDir)
			}
		}
	}

	// Handle list updates
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// Load files from a directory
func (m fileSelectModel) loadDirectory(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Create a custom error message instead of using tea.Error
			return errMsg{err: err}
		}

		// Convert entries to list items
		items := make([]list.Item, 0, len(entries))

		// Add parent directory option if not at root
		if dir != "/" {
			items = append(items, fileItem{
				path:  filepath.Dir(dir),
				name:  "..",
				isDir: true,
			})
		}

		// Add all directory entries
		for _, entry := range entries {
			// Skip hidden files
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			fullPath := filepath.Join(dir, entry.Name())

			items = append(items, fileItem{
				path:       fullPath,
				name:       entry.Name(),
				isDir:      entry.IsDir(),
				size:       info.Size(),
				isSelected: m.selectedFiles[fullPath],
			})
		}

		// Sort directories first, then by name
		sort.Slice(items, func(i, j int) bool {
			// Cast to fileItem to access properties
			fileI := items[i].(fileItem)
			fileJ := items[j].(fileItem)

			// Special case for parent directory ".."
			if fileI.name == ".." {
				return true
			}
			if fileJ.name == ".." {
				return false
			}

			// Directories come before files
			if fileI.isDir && !fileJ.isDir {
				return true
			}
			if !fileI.isDir && fileJ.isDir {
				return false
			}

			// Then sort by name
			return fileI.name < fileJ.name
		})

		// Return a message with the items and directory instead of modifying the model directly
		return loadDirectoryResultMsg{
			items:      items,
			currentDir: dir,
		}
	}
}

// Render the file selection view
func (m fileSelectModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress any key to continue", m.err)
	}

	title := fmt.Sprintf("Directory: %s", m.currentDir)
	help := "\nSpacebar: select file | Enter: open dir | Backspace: parent dir | r: refresh | h: home dir | Tab: confirm selection"

	// Show selected files count
	selectedCount := len(m.selectedFiles)
	selectionStatus := ""
	if selectedCount > 0 {
		selectionStatus = fmt.Sprintf("\n\nSelected %d files", selectedCount)
	}

	return title + "\n\n" + m.list.View() + selectionStatus + help
}
