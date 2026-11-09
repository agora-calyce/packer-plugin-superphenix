package postprocessor

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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
	stallTimeout = 60 * time.Second // sans octet reçu pendant ce délai, la connexion est coupée et le transfert repris
	maxResumes   = 20
)

// detectDisk liste les disques du helper hors racine, loop, cdrom et cloud-init (label cidata).
const detectDisk = `root=$(lsblk -ndo PKNAME "$(findmnt -no SOURCE /)"); for d in $(lsblk -dnpo NAME -e 7,11); do ` +
	`[ "${d#/dev/}" = "$root" ] && continue; [ "$(sudo blkid -s LABEL -o value $d 2>/dev/null)" = cidata ] && continue; echo $d; done`

// progress note l'instant du dernier octet reçu, pour détecter un transfert figé.
type progress struct {
	w    io.Writer
	last atomic.Int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.last.Store(time.Now().UnixNano())
	return p.w.Write(b)
}

// localWriter mémorise la première erreur d'écriture locale (disque plein, droits...) : elle n'a rien à voir
// avec le réseau, donc reprendre le téléchargement serait inutile.
type localWriter struct {
	w   io.Writer
	err error
}

func (l *localWriter) Write(b []byte) (int, error) {
	n, err := l.w.Write(b)
	if err != nil && l.err == nil {
		l.err = err
	}
	return n, err
}

// pull copie remoteOut à partir de l'offset off vers w. Une connexion figée est fermée par un chien de garde.
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

// sshFetch : attend cloud-init, convertit dev en qcow2 sur le helper, puis rapatrie le fichier via stdout SSH (avec reprises).
func sshFetch(ctx context.Context, ui packersdk.Ui, host, user string, signer ssh.Signer, dev, out string) (err error) {
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 15 * time.Second,
		// ponytail: pas de vérif de clé hôte (helper éphémère, clé inconnue d'avance), à durcir via console série si besoin.
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
	// Ctrl-C : fermer la connexion interrompt sessions et téléchargement en cours.
	// ponytail: lit cli sans verrou ; une course avec la reconnexion est théorique (au pire, fermeture tardive).
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
	// cloud-init installe qemu-utils ; --wait bloque jusqu'à la fin.
	if _, err := run("sudo cloud-init status --wait; command -v qemu-img"); err != nil {
		return err
	}
	if dev == "" {
		// L'ordre des disques n'est pas garanti : on prend celui qui n'est ni la racine du helper ni le disque cloud-init.
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
		// Diagnostic : disposition des disques du helper et espace libre (aide à choisir disk_device).
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
	// Échec rapide : ne pas télécharger des Go vers un disque qui ne peut pas les contenir.
	if free, ferr := freeBytes(filepath.Dir(out)); ferr == nil && uint64(size) > free {
		return fmt.Errorf("not enough free space for %s: the qcow2 is %d bytes, only %d are free in %s (free some space or point output_path to a larger volume)",
			out, size, free, filepath.Dir(out))
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(out) // pas de qcow2 partiel
		}
	}()
	lw := &localWriter{w: f}
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
		perr := pull(cli, lw, got)
		if lw.err != nil {
			return fmt.Errorf("write %s: %w", out, lw.err)
		}
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
