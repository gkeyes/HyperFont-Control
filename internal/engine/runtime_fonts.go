package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Copy once into the boot-owned tmpfs. Both font directories hard-link this
// inode, so per-app removal of /data/adb bind mounts cannot reveal empty font
// placeholders to Android's already-shared font map.
func (s *Store) runtimeFont(g Generation, name string) (string, error) {
	want, ok := g.Fonts[name]
	if !ok || name != "HFC-"+want+".ttf" || !assetPattern.MatchString(want) {
		return "", fmt.Errorf("无效运行字体")
	}
	dir := filepath.Join(s.Runtime, "payload")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if st, err := os.Lstat(dst); err == nil {
		if !st.Mode().IsRegular() {
			return "", fmt.Errorf("运行字体不是普通文件")
		}
		got, err := hashFile(dst)
		if err != nil || got != want {
			return "", fmt.Errorf("运行字体校验失败")
		}
		return dst, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	src, err := os.Open(s.data("generations", g.ID, "fonts", name))
	if err != nil {
		return "", err
	}
	defer src.Close()
	f, err := os.CreateTemp(dir, ".font-")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, h), src); err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return "", fmt.Errorf("字体复制校验失败")
	}
	if err = f.Chmod(0644); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}
