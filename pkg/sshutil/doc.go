// Package sshutil provides shared SSH/SFTP client utilities for DNSWeaver providers.
//
// This package provides a unified way to manage remote file systems and execute
// commands via SSH, enabling providers like dnsmasq, Pi-hole, and hosts-file to
// manage DNS configurations on remote systems.
//
// # Overview
//
// The package provides three main components:
//
//   - [Client]: Manages SSH connections with pooling and keepalive
//   - [SFTPFileSystem]: Implements [FileSystem] interface over SFTP
//   - [SSHCommandRunner]: Implements [CommandRunner] interface over SSH exec
//
// # Basic Usage
//
//	// Configure SSH connection
//	config := &sshutil.Config{
//		Host:    "dns-server.local",
//		Port:    22,
//		User:    "admin",
//		KeyFile: "/path/to/key",
//	}
//
//	// Create client
//	client, err := sshutil.NewClient(config)
//	if err != nil {
//		return err
//	}
//	defer client.Close()
//
//	// Connect
//	if err := client.Connect(ctx); err != nil {
//		return err
//	}
//
//	// Use SFTP filesystem
//	fs := sshutil.NewSFTPFileSystem(client)
//	if err := fs.Connect(ctx); err != nil {
//		return err
//	}
//	defer fs.Close()
//
//	data, err := fs.ReadFile("/etc/dnsmasq.d/custom.conf")
//
//	// Use command runner
//	runner := sshutil.NewSSHCommandRunner(client)
//	if err := runner.Run(ctx, "systemctl reload dnsmasq"); err != nil {
//		return err
//	}
//
// # Configuration from Environment
//
// The package supports loading configuration from environment variables using
// the Docker secrets pattern (values can be in files via _FILE suffix):
//
//	config, err := sshutil.LoadConfig("DNSWEAVER_PIHOLE_SSH_")
//
// This will look for environment variables like:
//   - DNSWEAVER_PIHOLE_SSH_HOST
//   - DNSWEAVER_PIHOLE_SSH_USER
//   - DNSWEAVER_PIHOLE_SSH_KEY_FILE (or DNSWEAVER_PIHOLE_SSH_KEY_FILE_FILE for Docker secrets)
//
// # Interface Compatibility
//
// The [FileSystem] and [CommandRunner] interfaces are designed to match the
// interfaces defined in providers/dnsmasq/client.go, allowing existing providers
// to easily adopt SSH-based remote management:
//
//	// FileSystem can be used anywhere os file operations are needed
//	type FileSystem interface {
//		ReadFile(path string) ([]byte, error)
//		WriteFile(path string, data []byte, perm os.FileMode) error
//		Stat(path string) (os.FileInfo, error)
//		MkdirAll(path string, perm os.FileMode) error
//	}
//
//	// CommandRunner can be used for reload commands
//	type CommandRunner interface {
//		Run(ctx context.Context, command string) error
//	}
//
// # Security Considerations
//
// By default, the package disables strict host key checking for ease of use
// in internal networks. For production environments with stricter security
// requirements, enable host key verification by setting StrictHostKeyChecking
// to true and providing a known_hosts file path.
//
// SSH key-based authentication is strongly recommended over password authentication.
// When using Docker secrets, store keys in mounted secret files rather than
// environment variables.
package sshutil
