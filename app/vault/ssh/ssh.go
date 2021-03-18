package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// VaultSSHClient represents a Vault server and the operations available on it over an SSH Connection. For operations
// involving the Vault API, see the vault/api/VaultAPIClient interface instead
type VaultSSHClient interface {
	// WriteFile writes a file to the SSH server, overwriting what's already there
	WriteFile(sourceFile io.Reader, hostDestination string) error
	// AddIPCLockCapabilityToFile attempts to call setcap over SSH to add IPC_LOCK capability to an executable. Requires
	// sudo privileges
	AddIPCLockCapabilityToFile(filename string) error
	// Close closes the underlying SSH connection
	Close() error
}

type sshClient struct {
	Client *ssh.Client
}

func NewClient(address, username, password string) (VaultSSHClient, error) {
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, err
	}

	return &sshClient{conn}, nil
}

func (c *sshClient) WriteFile(sourceFile io.Reader, hostDestination string) error {
	sftpClient, close, err := newSFTPClient(c.Client)
	if err != nil {
		return err
	}
	defer close()

	// Delete file if it exists already, otherwise create a new file
	dstFile, err := sftpClient.OpenFile(hostDestination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return ErrNoPermissions
		} else if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		} else if strings.Contains(err.Error(), "SSH_FX_FAILURE") {
			// FIXME: can this error occur for any other reasons?
			return ErrFileBusy
		}
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, sourceFile)
	if err != nil {
		return err
	}

	err = makeFileExecutable(dstFile)
	if err != nil {
		return err
	}

	return nil
}

func newSFTPClient(conn *ssh.Client) (*sftp.Client, func(), error) {
	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		return nil, nil, err
	}

	closeConns := func() {
		sftpClient.Close()
	}

	return sftpClient, closeConns, nil
}

func makeFileExecutable(file *sftp.File) error {
	err := file.Chmod(0775)
	if err != nil {
		return err
	}

	return nil
}

func (c *sshClient) AddIPCLockCapabilityToFile(filename string) error {
	session, err := c.Client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	// TODO: is sudo there?
	err = session.Run(fmt.Sprintf("sudo setcap cap_ipc_lock=ep %s", filename))
	if err != nil {
		return err
	}

	return nil
}

func (c *sshClient) Close() error {
	return c.Client.Close()
}
