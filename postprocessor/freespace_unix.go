//go:build !windows

package postprocessor

import "syscall"

// freeBytes retourne l'espace disponible pour un utilisateur non privilégié dans dir.
func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
