package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	MaxPackageBytes = 20 << 20
	MaxWASMBytes    = 16 << 20
	MaxAssetBytes   = 2 << 20
	MaxPackageFiles = 128
)

type Package struct {
	Manifest    Manifest
	ManifestRaw json.RawMessage
	WASM        []byte
	Assets      map[string][]byte
	SHA256      string
}

func WASMDigest(wasm []byte) string {
	hash := sha256.Sum256(wasm)
	return hex.EncodeToString(hash[:])
}

// ReadPackage validates the complete archive before writing any bytes to disk.
func ReadPackage(r io.Reader) (*Package, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxPackageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxPackageBytes {
		return nil, fmt.Errorf("plugin package exceeds %d bytes", MaxPackageBytes)
	}
	return ReadPackageBytes(data)
}

func ReadPackageBytes(data []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid plugin package: %w", err)
	}
	if len(zr.File) > MaxPackageFiles {
		return nil, fmt.Errorf("too many files in plugin package")
	}
	var total int64
	p := &Package{Assets: map[string][]byte{}}
	hash := sha256.Sum256(data)
	p.SHA256 = hex.EncodeToString(hash[:])
	for _, file := range zr.File {
		name, err := cleanPackageName(file.Name)
		if err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		total += int64(file.UncompressedSize64)
		if total > MaxPackageBytes || file.UncompressedSize64 > MaxWASMBytes {
			return nil, fmt.Errorf("expanded plugin package is too large")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, MaxWASMBytes+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(content)) != int64(file.UncompressedSize64) {
			return nil, fmt.Errorf("truncated plugin file %q", name)
		}
		switch {
		case name == "manifest.json":
			manifest, err := ParseManifest(content)
			if err != nil {
				return nil, err
			}
			p.Manifest, p.ManifestRaw = manifest, append(json.RawMessage(nil), content...)
		case name == "plugin.wasm":
			if len(content) < 8 || !bytes.Equal(content[:4], []byte("\x00asm")) {
				return nil, fmt.Errorf("plugin.wasm is not a WebAssembly module")
			}
			p.WASM = content
		case strings.HasPrefix(name, "ui/") && len(name) > 3:
			if len(content) > MaxAssetBytes {
				return nil, fmt.Errorf("plugin UI asset exceeds size limit")
			}
			p.Assets[strings.TrimPrefix(name, "ui/")] = content
		default:
			return nil, fmt.Errorf("unsupported file in plugin package: %s", name)
		}
	}
	if p.ManifestRaw == nil || len(p.WASM) == 0 {
		return nil, fmt.Errorf("package must contain manifest.json and plugin.wasm")
	}
	if p.Manifest.UI != nil {
		if _, ok := p.Assets[p.Manifest.UI.Entry]; !ok {
			return nil, fmt.Errorf("plugin UI entry is missing")
		}
	}
	return p, nil
}

func cleanPackageName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || !safePackagePath(name) {
		return "", fmt.Errorf("unsafe plugin package path")
	}
	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", fmt.Errorf("invalid plugin path")
	}
	return cleaned, nil
}

func safePackagePath(name string) bool {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return false
	}
	cleaned := path.Clean(name)
	return cleaned == name && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func PluginDir(root, id string) (string, error) {
	if !pluginIDPattern.MatchString(id) || strings.HasSuffix(id, "-") {
		return "", fmt.Errorf("invalid plugin id")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, id))
	if err != nil {
		return "", err
	}
	if filepath.Dir(target) != base {
		return "", fmt.Errorf("plugin path escapes installation directory")
	}
	return target, nil
}
