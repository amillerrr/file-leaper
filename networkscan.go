package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Global channel for sending messages from goroutines to the program
var programChannel chan tea.Msg

// Selected host message
type selectedHostMsg struct {
	host string
}

// Scan started message
type scanStartedMsg struct{}

// Network scan result message
type scanResultMsg struct {
	hosts []hostItem
}

// Network scan progress message
type scanProgressMsg struct {
	progress int
	total    int
}

// Manual host input message
type promptForManualHostMsg struct{}

// Host item for the list
type hostItem struct {
	ip       string
	hostname string
	online   bool
}

func (i hostItem) Title() string {
	if i.hostname != "" {
		return fmt.Sprintf("%s (%s)", i.ip, i.hostname)
	}
	return i.ip
}

func (i hostItem) Description() string {
	if i.online {
		return "Online"
	}
	return "Offline"
}

func (i hostItem) FilterValue() string {
	return i.ip + " " + i.hostname
}

// Network scanner model
type networkScanModel struct {
	list          list.Model
	spinner       spinner.Model
	hostInput     textinput.Model
	scanning      bool
	showingInput  bool
	progress      int
	total         int
	err           error
	config        *AppConfig
	lastHostnames map[string]string
}

// Create a new network scanner model
func newNetworkScanModel() networkScanModel {
	config := GetConfig()

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Initialize text input for manual host entry
	hostInput := textinput.New()
	hostInput.Placeholder = "hostname or IP address"
	hostInput.Focus()
	hostInput.CharLimit = 100
	hostInput.Width = 40
	hostInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	hostInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Initialize delegate with custom styles
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#25A065"))

	// Create empty list
	hostList := list.New([]list.Item{}, delegate)
	hostList.Title = "Available Hosts (enter to select, a to add manually, r to rescan)"
	hostList.SetShowStatusBar(false)
	hostList.SetFilteringEnabled(true)
	hostList.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1)

	return networkScanModel{
		list:          hostList,
		spinner:       s,
		hostInput:     hostInput,
		scanning:      true,
		config:        config,
		lastHostnames: make(map[string]string),
	}
}

// Initialize the network scanner
func (m networkScanModel) Init() tea.Cmd {
	// Initialize the channel for network messages
	programChannel = make(chan tea.Msg)

	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			// Start network scan in a goroutine
			go scanNetworkAsync(m.config)
			// Return a message that indicates scanning has started
			return scanStartedMsg{}
		},
	)
}

// Update the network scanner
func (m networkScanModel) Update(msg tea.Msg) (networkScanModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If showing input field, handle that first
		if m.showingInput {
			switch msg.String() {
			case "enter":
				host := m.hostInput.Value()
				if host != "" {
					m.showingInput = false
					return m, func() tea.Msg {
						return selectedHostMsg{host: host}
					}
				}
			case "esc":
				m.showingInput = false
				return m, nil
			default:
				var cmd tea.Cmd
				m.hostInput, cmd = m.hostInput.Update(msg)
				return m, cmd
			}
		}

		if m.scanning {
			// Don't handle most keys during scanning
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc":
				return m, func() tea.Msg {
					return backToMenuMsg{}
				}
			}
			break
		}

		switch msg.String() {
		case "enter":
			// Select host and proceed to transfer
			item, ok := m.list.SelectedItem().(hostItem)
			if ok && item.online {
				return m, func() tea.Msg {
					return selectedHostMsg{host: item.ip}
				}
			}

		case "a":
			// Add host manually
			m.showingInput = true
			return m, m.hostInput.Focus()

		case "r":
			// Restart scan
			m.scanning = true
			m.progress = 0
			m.total = 0
			programChannel = make(chan tea.Msg)
			return m, tea.Batch(
				m.spinner.Tick,
				func() tea.Msg {
					go scanNetworkAsync(m.config)
					return scanStartedMsg{}
				},
			)
		}

	case promptForManualHostMsg:
		m.showingInput = true
		return m, m.hostInput.Focus()

	case scanStartedMsg:
		// Start listening for scan updates
		return m, waitForScanUpdate

	case scanResultMsg:
		// Convert to list items
		items := make([]list.Item, len(msg.hosts))
		for i, host := range msg.hosts {
			items[i] = host

			// Save hostname to config
			if host.hostname != "" && !strings.HasSuffix(host.hostname, "(this device)") {
				m.config.AddKnownHost(host.ip, host.hostname)
			}

			// Save for use in the UI
			m.lastHostnames[host.ip] = host.hostname
		}

		// Update list and stop scanning
		m.list.SetItems(items)
		m.scanning = false
		return m, nil

	case scanProgressMsg:
		// Update progress
		m.progress = msg.progress
		m.total = msg.total
		// Continue listening for updates
		return m, waitForScanUpdate

	case errMsg:
		// Handle error
		m.err = msg.err
		m.scanning = false
		return m, nil

	case spinner.TickMsg:
		// Update spinner only if scanning
		if m.scanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	// Handle list updates
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// Command to wait for the next network scan update
func waitForScanUpdate() tea.Msg {
	return <-programChannel
}

// Scan the network asynchronously
func scanNetworkAsync(config *AppConfig) {
	// Get local IP and interface information
	interfaces, err := net.Interfaces()
	if err != nil {
		programChannel <- errMsg{err: fmt.Errorf("failed to get network interfaces: %v", err)}
		return
	}

	var localAddrs []net.IP
	var localNetworks []*net.IPNet

	// Find all IPv4 addresses and networks on local interfaces
	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipv4 := ipnet.IP.To4(); ipv4 != nil {
					localAddrs = append(localAddrs, ipv4)
					localNetworks = append(localNetworks, ipnet)
				}
			}
		}
	}

	if len(localAddrs) == 0 {
		programChannel <- errMsg{err: fmt.Errorf("no local IPv4 addresses found")}
		return
	}

	// Get common ports to scan
	commonPorts := config.CommonPorts
	if len(commonPorts) == 0 {
		commonPorts = []int{22, 80, 443, 445, 3389, 8080, 8443}
	}

	var hosts []hostItem
	var mutex sync.Mutex
	var wg sync.WaitGroup

	// Scan each network
	for netIdx, ipnet := range localNetworks {
		// Get IP range to scan from CIDR
		ip := localAddrs[netIdx]
		mask := ipnet.Mask

		// Convert IP and mask to integers for manipulation
		ipInt := ip2int(ip)

		// Calculate network start and broadcast addresses
		ones, bits := mask.Size()
		netmask := uint32(0xFFFFFFFF) << (bits - ones)

		// Calculate first and last IPs in the network
		networkIP := ipInt & netmask
		broadcastIP := networkIP | ^netmask

		// Determine range to scan (skip network and broadcast addresses)
		firstIP := networkIP + 1
		lastIP := broadcastIP - 1

		// Calculate total IPs to scan
		total := int(lastIP - firstIP + 1)

		if total > 1024 {
			// For very large networks, limit the scan
			programChannel <- scanProgressMsg{
				progress: 0,
				total:    total,
			}

			// Only scan a reasonable subset
			lastIP = firstIP + 254
			total = 255
		}

		// Use a semaphore to limit concurrent scans
		semaphore := make(chan struct{}, 50)

		// Create a context with timeout from config
		timeout := config.ScanTimeout
		if timeout <= 0 {
			timeout = 60
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()

		// Scan the IP range
		for i := firstIP; i <= lastIP; i++ {
			select {
			case <-ctx.Done():
				// Timeout or cancellation
				programChannel <- errMsg{err: fmt.Errorf("scan timeout after %d seconds", timeout)}
				return
			default:
				scanIP := int2ip(i)

				// Skip scanning local IP
				isLocalIP := false
				for _, localIP := range localAddrs {
					if scanIP.Equal(localIP) {
						isLocalIP = true
						// Add self to the list
						hostname, _ := getHostname(scanIP.String())
						mutex.Lock()
						hosts = append(hosts, hostItem{
							ip:       scanIP.String(),
							hostname: hostname + " (this device)",
							online:   true,
						})
						mutex.Unlock()
						break
					}
				}

				if isLocalIP {
					continue
				}

				wg.Add(1)
				semaphore <- struct{}{}

				go func(ip net.IP) {
					defer wg.Done()
					defer func() { <-semaphore }()

					ipStr := ip.String()

					// Check if we already know this host's hostname
					knownHostname := config.GetKnownHostname(ipStr)

					// Try to connect to common ports
					isAlive := false
					for _, port := range commonPorts {
						conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ipStr, port), 200*time.Millisecond)
						if err == nil {
							conn.Close()
							isAlive = true
							break
						}
					}

					// If still not detected, try ICMP ping as backup
					if !isAlive && isHostAlive(ipStr) {
						isAlive = true
					}

					if isAlive {
						hostname := knownHostname
						// If we don't have a cached hostname, try to look it up
						if hostname == "" {
							hostname, _ = getHostname(ipStr)
						}

						mutex.Lock()
						hosts = append(hosts, hostItem{
							ip:       ipStr,
							hostname: hostname,
							online:   true,
						})
						mutex.Unlock()
					}

					// Report progress
					progress := int(i-firstIP) + 1
					programChannel <- scanProgressMsg{
						progress: progress,
						total:    total,
					}
				}(scanIP)
			}
		}
	}

	// Wait with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Sort hosts by IP
		sort.Slice(hosts, func(i, j int) bool {
			return hosts[i].ip < hosts[j].ip
		})

		programChannel <- scanResultMsg{hosts: hosts}
	case <-time.After(time.Duration(config.ScanTimeout) * time.Second):
		programChannel <- errMsg{err: fmt.Errorf("network scan timed out after %d seconds", config.ScanTimeout)}
	}
}

// Helper functions to convert IP addresses to integers and back
func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func int2ip(nn uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip
}

// Get the local IP address
func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no local IP address found")
}

// Get hostname for an IP address
func getHostname(ip string) (string, error) {
	names, err := net.LookupAddr(ip)
	if err != nil || len(names) == 0 {
		return "", err
	}

	// Remove trailing dot
	hostname := names[0]
	if hostname[len(hostname)-1] == '.' {
		hostname = hostname[:len(hostname)-1]
	}

	return hostname, nil
}

// Check if a host is online using ping
func isHostAlive(ip string) bool {
	// Use different ping commands based on OS
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("ping", "-n", "1", "-w", "500", ip)
	case "darwin", "linux":
		cmd = exec.Command("ping", "-c", "1", "-W", "1", ip)
	default:
		return false
	}

	// Run the command with a timeout
	err := cmd.Start()
	if err != nil {
		return false
	}

	// Create a done channel
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Wait for the command to finish or timeout
	select {
	case err := <-done:
		return err == nil
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
		return false
	}
}

// Render the network scanner
func (m networkScanModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress any key to return to menu, or 'r' to rescan", m.err)
	}

	if m.showingInput {
		return "Enter hostname or IP address:\n\n" + m.hostInput.View() + "\n\nPress Enter to connect or Esc to cancel"
	}

	if m.scanning {
		spinnerView := m.spinner.View()
		progress := ""
		if m.total > 0 {
			progress = fmt.Sprintf(" (%d/%d hosts scanned)", m.progress, m.total)
		}
		return fmt.Sprintf("Scanning local network%s\n\n%s", progress, spinnerView)
	}

	hostCount := len(m.list.Items())
	countText := fmt.Sprintf("\n\nFound %d hosts on the network", hostCount)

	return m.list.View() + countText + "\n\nPress Enter to select a host, 'a' to add manually, 'r' to rescan"
}
