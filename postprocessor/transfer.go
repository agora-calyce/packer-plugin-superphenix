package postprocessor

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"golang.org/x/crypto/ssh"
)

type transferFunc func(ctx context.Context, ui packersdk.Ui, host, user string, signer ssh.Signer, dev, out string) error

// sshFetch : attend cloud-init, convertit dev en qcow2 sur le helper, puis rapatrie le fichier via stdout SSH.
func sshFetch(ctx context.Context, ui packersdk.Ui, host, user string, signer ssh.Signer, dev, out string) error {
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 15 * time.Second,
		// ponytail: pas de vérif de clé hôte (helper éphémère, clé inconnue d'avance), à durcir via console série si besoin.
		HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	var cli *ssh.Client
	deadline := time.Now().Add(10 * time.Minute)
	for {
		var err error
		if cli, err = ssh.Dial("tcp", net.JoinHostPort(host, "22"), cfg); err == nil {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("ssh to helper: %w", err)
		}
		time.Sleep(5 * time.Second)
	}
	defer cli.Close()

	run := func(cmd string) error {
		s, err := cli.NewSession()
		if err != nil {
			return err
		}
		defer s.Close()
		if o, err := s.CombinedOutput(cmd); err != nil {
			return fmt.Errorf("%q: %w: %s", cmd, err, o)
		}
		return nil
	}
	// cloud-init installe qemu-utils ; --wait bloque jusqu'à la fin.
	if err := run("sudo cloud-init status --wait; command -v qemu-img"); err != nil {
		return err
	}
	if err := run(fmt.Sprintf("sudo qemu-img convert -f raw -O qcow2 %s /var/tmp/out.qcow2", dev)); err != nil {
		return err
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := cli.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	s.Stdout = f
	if err := s.Run("cat /var/tmp/out.qcow2"); err != nil {
		os.Remove(out)
		return fmt.Errorf("download qcow2: %w", err)
	}
	return nil
}
