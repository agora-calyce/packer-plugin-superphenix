package postprocessor

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"golang.org/x/crypto/ssh"
)

type transferFunc func(ctx context.Context, ui packersdk.Ui, host, user string, signer ssh.Signer, dev, out string) error

const (
	remoteOut    = "/var/tmp/out.qcow2"
	stallTimeout = 60 * time.Second // no byte received within this delay: the connection is cut and the transfer resumed
	maxResumes   = 20
)

// detectDisk lists the helper's disks excluding root, loop, cdrom and cloud-init (cidata label).
const detectDisk = `root=$(lsblk -ndo PKNAME "$(findmnt -no SOURCE /)"); for d in $(lsblk -dnpo NAME -e 7,11); do ` +
	`[ "${d#/dev/}" = "$root" ] && continue; [ "$(sudo blkid -s LABEL -o value $d 2>/dev/null)" = cidata ] && continue; echo $d; done`

// progress records the time of the last byte received, to detect a stalled transfer.
type progress struct {
	w    io.Writer
	last atomic.Int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.last.Store(time.Now().UnixNano())
	return p.w.Write(b)
}

// pull copies remoteOut from offset off to w. A stalled connection is closed by a watchdog.
func pull(cli *ssh.Client, w io.Writer, off int64) error {
	s, err := cli.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	pw := &progress{w: w}
	pw.last.Store(time.Now().UnixNano())
	s.Stdout = pw
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if time.Since(time.Unix(0, pw.last.Load())) > stallTimeout {
					cli.Close()
					return
				}
			}
		}
	}()
	return s.Run(fmt.Sprintf("tail -c +%d %s", off+1, remoteOut))
}

// sshFetch: waits for cloud-init, converts dev to qcow2 on the helper, then downloads the file through SSH stdout (with resume).
func sshFetch(ctx context.Context, ui packersdk.Ui, host, user string, signer ssh.Signer, dev, out string) (err error) {
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 15 * time.Second,
		// ponytail: no host key check (ephemeral helper, key unknown in advance), harden via serial console if needed.
		HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	connect := func() (*ssh.Client, error) {
		deadline := time.Now().Add(10 * time.Minute)
		for {
			cli, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), cfg)
			if err == nil {
				return cli, nil
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				return nil, fmt.Errorf("ssh to helper %s: %w (Packer must run inside the subnet or behind a VPN to reach the helper's private IP)", host, err)
			}
			time.Sleep(5 * time.Second)
		}
	}
	cli, err := connect()
	if err != nil {
		return err
	}
	// Ctrl-C: closing the connection interrupts ongoing sessions and download.
	// ponytail: reads cli without a lock; a race with reconnection is theoretical (worst case, late close).
	stop := context.AfterFunc(ctx, func() { cli.Close() })
	defer stop()
	defer func() { cli.Close() }()

	run := func(cmd string) ([]byte, error) {
		s, err := cli.NewSession()
		if err != nil {
			return nil, err
		}
		defer s.Close()
		o, err := s.CombinedOutput(cmd)
		if err != nil {
			return o, fmt.Errorf("%q: %w: %s", cmd, err, o)
		}
		return o, nil
	}
	// cloud-init installs qemu-utils; --wait blocks until it finishes.
	if _, err := run("sudo cloud-init status --wait; command -v qemu-img"); err != nil {
		return err
	}
	if dev == "" {
		// Disk order is not guaranteed: pick the one that is neither the helper's root nor the cloud-init disk.
		o, err := run(detectDisk)
		if err != nil {
			return err
		}
		if disks := strings.Fields(string(o)); len(disks) == 1 {
			dev = disks[0]
		} else {
			return fmt.Errorf("cannot detect the exported disk (candidates: %q); set disk_device", disks)
		}
		ui.Say("Exported disk detected: " + dev)
	}
	if _, err := run(fmt.Sprintf("sudo qemu-img convert -f raw -O qcow2 %s %s", dev, remoteOut)); err != nil {
		// Diagnostic: helper's disk layout and free space (helps choosing disk_device).
		if o, _ := run("lsblk -o NAME,SIZE,TYPE,FSTYPE,LABEL,MOUNTPOINT; df -h /var/tmp"); len(o) > 0 {
			err = fmt.Errorf("%w\nhelper disks:\n%s", err, o)
		}
		return err
	}
	sz, err := run("stat -c %s " + remoteOut)
	if err != nil {
		return err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sz)), 10, 64)
	if err != nil {
		return fmt.Errorf("remote size %q: %w", sz, err)
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(out) // no partial qcow2
		}
	}()
	var got int64
	for resumes := 0; got < size; resumes++ {
		if resumes > maxResumes {
			return fmt.Errorf("download qcow2: gave up after %d resumes at %d/%d bytes", maxResumes, got, size)
		}
		if resumes > 0 {
			ui.Say(fmt.Sprintf("Download stalled at %d/%d bytes, resuming...", got, size))
			cli.Close()
			if cli, err = connect(); err != nil {
				return err
			}
		}
		perr := pull(cli, f, got)
		if got, err = f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return fmt.Errorf("download qcow2: %w", ctx.Err())
		}
		if perr != nil && got < size {
			continue
		}
	}
	return nil
}
