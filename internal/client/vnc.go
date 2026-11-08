package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"
)

// VNC : client RFB minimal (clavier seulement) sur le WebSocket `instance/{id}/vnc`.
// ponytail: sécurité RFB "None" uniquement (celle du proxy Superphenix) ; pas d'écran, pas de souris.
type VNC struct{ ws *websocket.Conn }

// DialVNC ouvre la console VNC de l'instance et fait la poignée de main RFB 3.8.
func (c *Client) DialVNC(ctx context.Context, id string) (*VNC, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/api/spx-ctrl/%s/%s/instance/%s/vnc", c.BaseURL, c.OrgID, c.AZ, c.ProjectID, id))
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	cfg, err := websocket.NewConfig(u.String(), origin)
	if err != nil {
		return nil, err
	}
	cfg.Header.Set("Authorization", "api-token "+c.Token)
	ws, err := cfg.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("vnc: %w", err)
	}
	ws.PayloadType = websocket.BinaryFrame
	v := &VNC{ws}
	if err := v.handshake(); err != nil {
		ws.Close()
		return nil, fmt.Errorf("vnc handshake: %w", err)
	}
	return v, nil
}

func (v *VNC) handshake() error {
	ver := make([]byte, 12)
	if _, err := io.ReadFull(v.ws, ver); err != nil {
		return err
	}
	if !strings.HasPrefix(string(ver), "RFB 003.") {
		return fmt.Errorf("unexpected banner %q", ver)
	}
	if _, err := v.ws.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	var n [1]byte
	if _, err := io.ReadFull(v.ws, n[:]); err != nil {
		return err
	}
	if n[0] == 0 {
		return errors.New("server refused the connection")
	}
	types := make([]byte, n[0])
	if _, err := io.ReadFull(v.ws, types); err != nil {
		return err
	}
	if !strings.ContainsRune(string(types), 1) {
		return fmt.Errorf("no supported security type (offered %v, need None)", types)
	}
	if _, err := v.ws.Write([]byte{1}); err != nil {
		return err
	}
	var res [4]byte
	if _, err := io.ReadFull(v.ws, res[:]); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(res[:]) != 0 {
		return errors.New("security handshake failed")
	}
	if _, err := v.ws.Write([]byte{1}); err != nil { // ClientInit : shared
		return err
	}
	init := make([]byte, 24) // ServerInit : taille, format pixel, longueur du nom
	if _, err := io.ReadFull(v.ws, init); err != nil {
		return err
	}
	_, err := io.CopyN(io.Discard, v.ws, int64(binary.BigEndian.Uint32(init[20:])))
	return err
}

// KeyEvent envoie un appui (down) ou relâchement de touche (keysym X11).
func (v *VNC) KeyEvent(key uint32, down bool) error {
	m := []byte{4, 0, 0, 0, 0, 0, 0, 0}
	if down {
		m[1] = 1
	}
	binary.BigEndian.PutUint32(m[4:], key)
	_, err := v.ws.Write(m)
	return err
}

func (v *VNC) Close() error { return v.ws.Close() }
