# File-Leaper

A modern, feature-rich terminal UI application for transferring files over SSH/SFTP, scanning networks, and converting video files.
Originally built while working on the Quantum Leap re-boot series to manage media updates and quickly sending them to the target machine on-set.

## Features

- **Beautiful Terminal UI**: Intuitive interface powered by [Bubble Tea](https://github.com/charmbracelet/bubbletea) with interactive menus, progress bars, and spinners
- **File Transfer**: Securely transfer files to remote servers via SSH/SFTP
- **Network Scanning**: Discover available hosts on local networks
- **SSH Key Management**:
  - Generate new SSH keys (RSA, ED25519)
  - Install keys on remote hosts
  - Use password or key-based authentication
- **Video Conversion**: Convert video files between formats using FFmpeg
- **Persistent Configuration**: Remembers settings and recently used paths

## Installation

### Prerequisites

- Go 1.19 or later
- FFmpeg (for video conversion features)

### From Source

```bash
# Clone the repository
git clone https://github.com/yourusername/file-leaper.git
cd file-leaper

# Build the application
go build -o file-leaper

# Run the application
./file-leaper
```

### Options

```
Usage of filetransfer:
  -debug
        Enable debug logging
  -version
        Display version information
```

## Usage Guide

### File Transfer Workflow

1. **Select Files**: Choose one or more files to transfer
2. **Scan Network**: Find target hosts on your local network
3. **Connect**: Enter username, password, and destination directory
4. **SSH Key**: Select an existing SSH key or generate a new one
5. **Transfer**: Files are transferred with real-time progress indication

### Video Conversion

1. **Select File**: Choose a video file to convert
2. **Set Options**: Select output format, quality, and additional FFmpeg arguments
3. **Convert**: The file will be converted with progress indication

### Keyboard Controls

- **Global**:
  - `q` or `Ctrl+C`: Quit the application
  - `Esc`: Return to previous screen or main menu
- **Navigation**:
  - Arrow keys: Move through lists and menus
  - `Enter`: Select an item
  - `Tab`/`Shift+Tab`: Navigate between form fields
- **File Selection**:
  - `Space`: Toggle file selection
  - `Tab`: Confirm selection
  - `Enter`: Navigate into directory
  - `Backspace`: Go to parent directory
  - `r`: Refresh current directory
  - `h`: Go to home directory

## Configuration

The application stores its configuration in `~/.file-leaper/config.json`. This includes:

- Default destination directory
- Recently used paths
- Known hosts and their hostnames
- SSH key preferences
- Network scan settings

## Logs

Logs are stored in `~/.file-leaper/logs/` with timestamps for troubleshooting.

## Development

### Project Structure

- `main.go`: Entry point and main application model
- `fileselect.go`: File selection functionality
- `networkscan.go`: Network scanning capabilities
- `transfer.go`: File transfer implementation using SFTP
- `sshkey.go`: SSH key selection and management
- `keygen.go`: SSH key generation
- `keyinstall.go`: SSH key installation on remote hosts
- `videoconvert.go`: Video conversion using FFmpeg
- `config.go`: Configuration management
- `logger.go`: Logging system
- `confirmdialog.go`: Confirmation dialog implementation
- `authform.go`: Authentication form for SSH connections

### Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea): Terminal UI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss): Style definitions for terminal applications
- [pkg/sftp](https://github.com/pkg/sftp): SFTP client package
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh): SSH client implementation

## Security Considerations

- SSH keys are stored with proper permissions (0600)
- Password inputs use masked input fields
- Host key verification is implemented (with fallback options)

## License

[MIT License](LICENSE)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Acknowledgements

- [Charm](https://charm.sh/) for their amazing terminal UI libraries
- All open-source contributors to the project dependencies

---

_Note: Replace screenshots with actual application screenshots_
